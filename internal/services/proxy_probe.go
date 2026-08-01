package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"gpt-load/internal/models"

	proxyDialer "golang.org/x/net/proxy"
)

const (
	proxyProbeTarget         = "https://www.gstatic.com/generate_204"
	proxyProbeTimeout        = 5 * time.Second
	maxProxyProbeConcurrency = 8
	maxProxyCheckNodes       = 200

	proxyCheckStatusUnchecked = "unchecked"
	proxyCheckStatusUp        = "up"
	proxyCheckStatusDown      = "down"
)

type proxyProbeOutcome struct {
	Status     string
	HTTPStatus int
	LatencyMS  int64
	ErrorCode  string
}

type proxyCheckItem struct {
	node    models.ProxyNode
	url     string
	outcome proxyProbeOutcome
}

// ProxyCheckResult is the result of one manual check request. Nodes contains
// the refreshed records so clients can update their view without a second GET.
type ProxyCheckResult struct {
	CheckedCount   int             `json:"checked_count"`
	HealthyCount   int             `json:"healthy_count"`
	UnhealthyCount int             `json:"unhealthy_count"`
	Nodes          []ProxyNodeView `json:"nodes"`
}

// Check probes either the requested nodes or the complete pool and persists the
// latest result for every node. A failed probe is a normal result, not a batch
// error, so one dead proxy does not hide the remaining results.
func (s *ProxyPoolService) Check(ctx context.Context, proxyIDs []uint) (*ProxyCheckResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ids := uniqueSortedIDs(proxyIDs)
	if len(ids) > maxProxyCheckNodes {
		return nil, fmt.Errorf("proxy check exceeds the limit of %d nodes", maxProxyCheckNodes)
	}

	query := s.db.Order("id ASC")
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	var nodes []models.ProxyNode
	if err := query.Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("load proxy nodes for check: %w", err)
	}
	if len(ids) > 0 && len(nodes) != len(ids) {
		return nil, errors.New("one or more selected proxy nodes no longer exist")
	}
	if len(nodes) > maxProxyCheckNodes {
		return nil, fmt.Errorf("proxy check exceeds the limit of %d nodes", maxProxyCheckNodes)
	}

	result := &ProxyCheckResult{Nodes: make([]ProxyNodeView, 0, len(nodes))}
	if len(nodes) == 0 {
		return result, nil
	}

	target := s.probeTarget
	if target == "" {
		target = proxyProbeTarget
	}
	timeout := s.probeTimeout
	if timeout <= 0 {
		timeout = proxyProbeTimeout
	}

	items := make(chan proxyCheckItem, len(nodes))
	semaphore := make(chan struct{}, maxProxyProbeConcurrency)
	var wg sync.WaitGroup
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				items <- proxyCheckItem{node: node, outcome: proxyProbeOutcome{
					Status:    proxyCheckStatusDown,
					ErrorCode: "cancelled",
				}}
				return
			}
			defer func() { <-semaphore }()

			proxyURL, err := s.encryptionSvc.Decrypt(node.URL)
			if err != nil {
				items <- proxyCheckItem{node: node, outcome: proxyProbeOutcome{
					Status:    proxyCheckStatusDown,
					ErrorCode: "decrypt_failed",
				}}
				return
			}
			outcome, _ := probeProxy(ctx, proxyURL, target, timeout)
			items <- proxyCheckItem{node: node, url: proxyURL, outcome: outcome}
		}()
	}
	wg.Wait()
	close(items)

	checkedAt := time.Now().UTC()
	collected := make([]proxyCheckItem, 0, len(nodes))
	for item := range items {
		collected = append(collected, item)
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].node.ID < collected[j].node.ID })

	for _, item := range collected {
		updates := map[string]any{
			"check_status":      item.outcome.Status,
			"check_http_status": item.outcome.HTTPStatus,
			"check_latency_ms":  item.outcome.LatencyMS,
			"check_error":       item.outcome.ErrorCode,
			"checked_at":        checkedAt,
		}
		if err := s.db.Model(&models.ProxyNode{}).Where("id = ?", item.node.ID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("persist proxy check %d: %w", item.node.ID, err)
		}

		item.node.CheckStatus = item.outcome.Status
		item.node.CheckHTTPStatus = item.outcome.HTTPStatus
		item.node.CheckLatencyMS = item.outcome.LatencyMS
		item.node.CheckError = item.outcome.ErrorCode
		item.node.CheckedAt = &checkedAt
		result.Nodes = append(result.Nodes, proxyNodeView(item.node, item.url))
		result.CheckedCount++
		if item.outcome.Status == proxyCheckStatusUp {
			result.HealthyCount++
		} else {
			result.UnhealthyCount++
		}
	}
	return result, nil
}

func probeProxy(ctx context.Context, proxyURL, targetURL string, timeout time.Duration) (proxyProbeOutcome, error) {
	outcome := proxyProbeOutcome{Status: proxyCheckStatusDown}
	parsedProxy, err := url.ParseRequestURI(proxyURL)
	if err != nil || parsedProxy.Host == "" {
		outcome.ErrorCode = "invalid_proxy"
		return outcome, nil
	}

	client, err := newProxyProbeClient(parsedProxy, timeout)
	if err != nil {
		outcome.ErrorCode = "invalid_proxy"
		return outcome, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return outcome, fmt.Errorf("create probe request: %w", err)
	}

	started := time.Now()
	response, err := client.Do(request)
	outcome.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		outcome.ErrorCode = classifyProbeError(ctx, err)
		return outcome, nil
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	outcome.HTTPStatus = response.StatusCode
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		outcome.Status = proxyCheckStatusUp
		return outcome, nil
	}
	outcome.ErrorCode = "http_status"
	return outcome, nil
}

func newProxyProbeClient(proxyURL *url.URL, timeout time.Duration) (*http.Client, error) {
	dialTimeout := timeout
	if dialTimeout <= 0 {
		dialTimeout = proxyProbeTimeout
	}
	transport := &http.Transport{
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: dialTimeout,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: dialTimeout,
		}).DialContext,
	}

	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		var auth *proxyDialer.Auth
		if proxyURL.User != nil {
			password, _ := proxyURL.User.Password()
			auth = &proxyDialer.Auth{User: proxyURL.User.Username(), Password: password}
		}
		dialer, err := proxyDialer.SOCKS5("tcp", proxyURL.Host, auth, proxyDialer.Direct)
		if err != nil {
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if contextual, ok := dialer.(proxyDialer.ContextDialer); ok {
				return contextual.DialContext(ctx, network, address)
			}
			return dialer.Dial(network, address)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   dialTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func classifyProbeError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(message, "connection refused") {
		return "connection_refused"
	}
	return "network_error"
}

func proxyNodeView(node models.ProxyNode, decryptedURL string) ProxyNodeView {
	status := node.CheckStatus
	if status == "" {
		status = proxyCheckStatusUnchecked
	}
	var checkedAt *string
	if node.CheckedAt != nil {
		value := node.CheckedAt.UTC().Format(time.RFC3339)
		checkedAt = &value
	}
	return ProxyNodeView{
		ID:              node.ID,
		URL:             decryptedURL,
		CreatedAt:       node.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		CheckStatus:     status,
		CheckHTTPStatus: node.CheckHTTPStatus,
		CheckLatencyMS:  node.CheckLatencyMS,
		CheckError:      node.CheckError,
		CheckedAt:       checkedAt,
	}
}

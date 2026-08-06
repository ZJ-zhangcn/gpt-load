package services

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"gpt-load/internal/encryption"
	"gpt-load/internal/keypool"
	"gpt-load/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxProxyNodesPerImport = 5000

// ProxyPoolService owns proxy-node persistence and the key-to-proxy binding lifecycle.
type ProxyPoolService struct {
	db            *gorm.DB
	keyProvider   *keypool.KeyProvider
	encryptionSvc encryption.Service
	probeTarget   string
	probeTimeout  time.Duration
}

// ProxyImportResult describes a batch proxy import without exposing credentials in logs.
type ProxyImportResult struct {
	AddedCount   int `json:"added_count"`
	IgnoredCount int `json:"ignored_count"`
}

// ProxyRebalanceResult reports the stable round-robin assignment result.
type ProxyRebalanceResult struct {
	BoundKeyCount int          `json:"bound_key_count"`
	ProxyKeyCount map[uint]int `json:"proxy_key_count"`
}

// ProxyGlobalRebalanceResult reports one atomic rebalance across every standard group.
type ProxyGlobalRebalanceResult struct {
	ProcessedGroupCount   int          `json:"processed_group_count"`
	BoundKeyCount         int          `json:"bound_key_count"`
	HealthyProxyCount     int          `json:"healthy_proxy_count"`
	SkippedAggregateCount int          `json:"skipped_aggregate_count"`
	ProxyKeyCount         map[uint]int `json:"proxy_key_count"`
}

// ProxyDeleteResult reports the number of keys whose dedicated proxy was cleared.
type ProxyDeleteResult struct {
	UnboundKeyCount int `json:"unbound_key_count"`
}

// ProxyBatchDeleteResult reports the outcome of an idempotent batch deletion.
type ProxyBatchDeleteResult struct {
	RequestedCount  int `json:"requested_count"`
	DeletedCount    int `json:"deleted_count"`
	IgnoredCount    int `json:"ignored_count"`
	UnboundKeyCount int `json:"unbound_key_count"`
}

// ProxyNodeView is the administrator-facing proxy-node representation.
type ProxyNodeView struct {
	ID              uint    `json:"id"`
	URL             string  `json:"url"`
	CreatedAt       string  `json:"created_at"`
	CheckStatus     string  `json:"check_status"`
	CheckHTTPStatus int     `json:"check_http_status"`
	CheckLatencyMS  int64   `json:"check_latency_ms"`
	CheckError      string  `json:"check_error"`
	CheckedAt       *string `json:"checked_at,omitempty"`
}

func NewProxyPoolService(db *gorm.DB, keyProvider *keypool.KeyProvider, encryptionSvc encryption.Service) *ProxyPoolService {
	return &ProxyPoolService{
		db:            db,
		keyProvider:   keyProvider,
		encryptionSvc: encryptionSvc,
		probeTarget:   proxyProbeTarget,
		probeTimeout:  proxyProbeTimeout,
	}
}

// Import stores valid, unique proxy nodes. URLs are encrypted before persistence and
// deduplicated using the encryption service hash of their normalized plaintext.
func (s *ProxyPoolService) Import(proxiesText string) (*ProxyImportResult, error) {
	entries := splitProxyURLs(proxiesText)
	if len(entries) == 0 {
		return nil, errors.New("no proxy URLs provided")
	}
	if len(entries) > maxProxyNodesPerImport {
		return nil, fmt.Errorf("proxy import exceeds the limit of %d nodes", maxProxyNodesPerImport)
	}

	result := &ProxyImportResult{}
	candidates := make(map[string]string, len(entries))
	candidateHashes := make([]string, 0, len(entries))
	for _, entry := range entries {
		normalized, err := normalizeProxyURL(entry)
		if err != nil {
			result.IgnoredCount++
			continue
		}
		hash := s.encryptionSvc.Hash(normalized)
		if _, duplicate := candidates[hash]; duplicate {
			result.IgnoredCount++
			continue
		}
		candidates[hash] = normalized
		candidateHashes = append(candidateHashes, hash)
	}
	if len(candidates) == 0 {
		return nil, errors.New("no valid proxy URLs provided")
	}

	hashes := append([]string(nil), candidateHashes...)

	var existingHashes []string
	if err := s.db.Model(&models.ProxyNode{}).Where("url_hash IN ?", hashes).Pluck("url_hash", &existingHashes).Error; err != nil {
		return nil, fmt.Errorf("load existing proxy nodes: %w", err)
	}
	existing := make(map[string]struct{}, len(existingHashes))
	for _, hash := range existingHashes {
		existing[hash] = struct{}{}
	}

	nodes := make([]models.ProxyNode, 0, len(candidates)-len(existing))
	for _, hash := range candidateHashes {
		normalized := candidates[hash]
		if _, alreadyExists := existing[hash]; alreadyExists {
			result.IgnoredCount++
			continue
		}
		encryptedURL, err := s.encryptionSvc.Encrypt(normalized)
		if err != nil {
			return nil, fmt.Errorf("encrypt proxy URL: %w", err)
		}
		nodes = append(nodes, models.ProxyNode{URL: encryptedURL, URLHash: hash})
	}
	if len(nodes) == 0 {
		return result, nil
	}

	if err := s.db.Create(&nodes).Error; err != nil {
		return nil, fmt.Errorf("create proxy nodes: %w", err)
	}
	result.AddedCount = len(nodes)
	return result, nil
}

// List returns proxy nodes with their URL decrypted for the authenticated management UI.
func (s *ProxyPoolService) List() ([]ProxyNodeView, error) {
	var nodes []models.ProxyNode
	if err := s.db.Order("id ASC").Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("load proxy nodes: %w", err)
	}

	views := make([]ProxyNodeView, 0, len(nodes))
	for _, node := range nodes {
		proxyURL, err := s.encryptionSvc.Decrypt(node.URL)
		if err != nil {
			return nil, fmt.Errorf("decrypt proxy node %d: %w", node.ID, err)
		}
		views = append(views, proxyNodeView(node, proxyURL))
	}
	return views, nil
}

// Rebalance assigns the selected proxy nodes to every key in one standard group using
// a stable, ID-sorted round robin. Each node may be assigned to multiple keys.
func (s *ProxyPoolService) Rebalance(groupID uint, proxyIDs []uint) (*ProxyRebalanceResult, error) {
	if groupID == 0 {
		return nil, errors.New("group_id is required")
	}
	proxyIDs = uniqueSortedIDs(proxyIDs)
	if len(proxyIDs) == 0 {
		return nil, errors.New("at least one proxy node must be selected")
	}

	result := &ProxyRebalanceResult{ProxyKeyCount: make(map[uint]int)}
	cacheUpdates := make(map[uint]string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var group models.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, groupID).Error; err != nil {
			return fmt.Errorf("load group: %w", err)
		}
		if group.GroupType == "aggregate" {
			return errors.New("proxy assignment is only supported for standard groups")
		}

		var nodes []models.ProxyNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", proxyIDs).Order("id ASC").Find(&nodes).Error; err != nil {
			return fmt.Errorf("load selected proxy nodes: %w", err)
		}
		if len(nodes) != len(proxyIDs) {
			return errors.New("one or more selected proxy nodes no longer exist")
		}

		var keys []models.APIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("group_id = ?", groupID).Order("id ASC").Find(&keys).Error; err != nil {
			return fmt.Errorf("load group keys: %w", err)
		}
		proxyKeyCount, err := bindProxyNodesToKeys(tx, keys, nodes, cacheUpdates)
		if err != nil {
			return err
		}
		result.ProxyKeyCount = proxyKeyCount
		result.BoundKeyCount = len(keys)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshProxyCache(cacheUpdates); err != nil {
		return nil, err
	}
	return result, nil
}

// RebalanceAllHealthy assigns every currently healthy proxy to every key in every
// standard group. Each group starts the same stable ID-sorted round robin, while
// aggregate groups are intentionally skipped because they do not own keys.
func (s *ProxyPoolService) RebalanceAllHealthy() (*ProxyGlobalRebalanceResult, error) {
	result := &ProxyGlobalRebalanceResult{ProxyKeyCount: make(map[uint]int)}
	cacheUpdates := make(map[uint]string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var nodes []models.ProxyNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("check_status = ?", proxyCheckStatusUp).
			Order("id ASC").
			Find(&nodes).Error; err != nil {
			return fmt.Errorf("load healthy proxy nodes: %w", err)
		}
		if len(nodes) == 0 {
			return errors.New("no healthy proxy nodes available; run a real proxy check first")
		}
		result.HealthyProxyCount = len(nodes)

		var allGroups []models.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Find(&allGroups).Error; err != nil {
			return fmt.Errorf("load key groups: %w", err)
		}
		standardGroups := make([]models.Group, 0, len(allGroups))
		groupIDs := make([]uint, 0, len(allGroups))
		for _, group := range allGroups {
			if group.GroupType == "aggregate" {
				result.SkippedAggregateCount++
				continue
			}
			standardGroups = append(standardGroups, group)
			groupIDs = append(groupIDs, group.ID)
		}
		result.ProcessedGroupCount = len(standardGroups)
		if len(groupIDs) == 0 {
			return nil
		}

		var keys []models.APIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id IN ?", groupIDs).
			Order("group_id ASC, id ASC").
			Find(&keys).Error; err != nil {
			return fmt.Errorf("load all group keys: %w", err)
		}
		keysByGroup := make(map[uint][]models.APIKey, len(groupIDs))
		for _, key := range keys {
			keysByGroup[key.GroupID] = append(keysByGroup[key.GroupID], key)
		}

		for _, group := range standardGroups {
			groupKeys := keysByGroup[group.ID]
			if len(groupKeys) == 0 {
				continue
			}
			proxyKeyCount, err := bindProxyNodesToKeys(tx, groupKeys, nodes, cacheUpdates)
			if err != nil {
				return err
			}
			result.BoundKeyCount += len(groupKeys)
			for proxyID, count := range proxyKeyCount {
				result.ProxyKeyCount[proxyID] += count
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshProxyCache(cacheUpdates); err != nil {
		return nil, err
	}
	return result, nil
}

func bindProxyNodesToKeys(tx *gorm.DB, keys []models.APIKey, nodes []models.ProxyNode, cacheUpdates map[uint]string) (map[uint]int, error) {
	proxyKeyCount := make(map[uint]int)
	for index, key := range keys {
		node := nodes[index%len(nodes)]
		if err := tx.Model(&models.APIKey{}).Where("id = ?", key.ID).Update("proxy_id", node.ID).Error; err != nil {
			return nil, fmt.Errorf("bind proxy for key %d: %w", key.ID, err)
		}
		cacheUpdates[key.ID] = node.URL
		proxyKeyCount[node.ID]++
	}
	return proxyKeyCount, nil
}

// Delete clears bindings to a proxy node and deletes it in the same database transaction.
func (s *ProxyPoolService) Delete(proxyID uint) (*ProxyDeleteResult, error) {
	if proxyID == 0 {
		return nil, errors.New("proxy node id is required")
	}

	result := &ProxyDeleteResult{}
	cacheUpdates := make(map[uint]string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var node models.ProxyNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, proxyID).Error; err != nil {
			return fmt.Errorf("load proxy node: %w", err)
		}

		var boundKeys []models.APIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("proxy_id = ?", proxyID).Order("id ASC").Find(&boundKeys).Error; err != nil {
			return fmt.Errorf("load bound keys: %w", err)
		}
		if len(boundKeys) > 0 {
			if err := tx.Model(&models.APIKey{}).Where("proxy_id = ?", proxyID).Update("proxy_id", nil).Error; err != nil {
				return fmt.Errorf("clear bound keys: %w", err)
			}
			for _, key := range boundKeys {
				cacheUpdates[key.ID] = ""
			}
		}
		if err := tx.Delete(&node).Error; err != nil {
			return fmt.Errorf("delete proxy node: %w", err)
		}
		result.UnboundKeyCount = len(boundKeys)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshProxyCache(cacheUpdates); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteMany physically deletes the selected proxy nodes in one transaction.
// Existing key bindings are cleared before deletion and their derived cache
// values are removed after commit. A missing node is treated as already gone
// so repeated submissions remain safe and idempotent.
func (s *ProxyPoolService) DeleteMany(proxyIDs []uint) (*ProxyBatchDeleteResult, error) {
	proxyIDs = uniqueSortedIDs(proxyIDs)
	if len(proxyIDs) == 0 {
		return nil, errors.New("at least one proxy node must be selected")
	}

	result := &ProxyBatchDeleteResult{RequestedCount: len(proxyIDs)}
	cacheUpdates := make(map[uint]string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var nodes []models.ProxyNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", proxyIDs).
			Order("id ASC").
			Find(&nodes).Error; err != nil {
			return fmt.Errorf("load selected proxy nodes: %w", err)
		}
		result.IgnoredCount = len(proxyIDs) - len(nodes)
		if len(nodes) == 0 {
			return nil
		}

		existingIDs := make([]uint, 0, len(nodes))
		for _, node := range nodes {
			existingIDs = append(existingIDs, node.ID)
		}

		var boundKeys []models.APIKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("proxy_id IN ?", existingIDs).
			Order("id ASC").
			Find(&boundKeys).Error; err != nil {
			return fmt.Errorf("load keys bound to selected proxy nodes: %w", err)
		}
		if len(boundKeys) > 0 {
			if err := tx.Model(&models.APIKey{}).
				Where("proxy_id IN ?", existingIDs).
				Update("proxy_id", nil).Error; err != nil {
				return fmt.Errorf("clear bound keys: %w", err)
			}
			for _, key := range boundKeys {
				cacheUpdates[key.ID] = ""
			}
		}

		deleteResult := tx.Where("id IN ?", existingIDs).Delete(&models.ProxyNode{})
		if deleteResult.Error != nil {
			return fmt.Errorf("delete proxy nodes: %w", deleteResult.Error)
		}
		result.DeletedCount = int(deleteResult.RowsAffected)
		result.UnboundKeyCount = len(boundKeys)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshProxyCache(cacheUpdates); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ProxyPoolService) refreshProxyCache(cacheUpdates map[uint]string) error {
	if len(cacheUpdates) == 0 {
		return nil
	}
	if err := s.keyProvider.RefreshProxyURLs(cacheUpdates); err == nil {
		return nil
	}

	// The DB transaction is already committed. Rebuilding the shared cache from the
	// source of truth prevents a stale dedicated proxy if a targeted update fails.
	logrus.Warn("Targeted proxy-cache refresh failed; rebuilding key cache from database")
	if err := s.keyProvider.LoadKeysFromDB(); err != nil {
		return fmt.Errorf("refresh proxy cache: %w", err)
	}
	return nil
}

func splitProxyURLs(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '\n', '\r', '\t', ' ', ',', ';':
			return true
		default:
			return false
		}
	})
}

func normalizeProxyURL(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid proxy URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
		return parsed.String(), nil
	default:
		return "", errors.New("unsupported proxy URL scheme")
	}
}

func uniqueSortedIDs(ids []uint) []uint {
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id != 0 {
			set[id] = struct{}{}
		}
	}
	unique := make([]uint, 0, len(set))
	for id := range set {
		unique = append(unique, id)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	return unique
}

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Register("generic", newGenericChannel)
}

// GenericChannel forwards provider-native requests and adds OpenAI audio aliases
// for upstreams that expose Fish-style TTS/ASR endpoints.
type GenericChannel struct {
	*BaseChannel
}

func newGenericChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("generic", group)
	if err != nil {
		return nil, err
	}

	return &GenericChannel{BaseChannel: base}, nil
}

// ModifyRequest injects the selected key while preserving the caller's
// provider-specific headers, such as Fish Audio's model header.
func (ch *GenericChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, _ *models.Group) {
	if apiKey == nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
}

// IsStreamRequest recognizes transport-level streaming signals while leaving
// binary TTS/ASR responses as normal responses.
func (ch *GenericChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	if strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		return true
	}

	if c.Query("stream") == "true" {
		return true
	}

	var payload struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err == nil {
		return payload.Stream
	}

	return false
}

// ExtractModel supports both generic JSON requests and provider headers such
// as Fish Audio's `model` header.
func (ch *GenericChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	if model := c.GetHeader("model"); model != "" {
		return model
	}

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err == nil {
		return payload.Model
	}
	return ""
}

// ValidateKey performs a low-cost GET against the configured validation
// endpoint. For Fish Audio the default endpoint is /model; callers can set a
// different path on the group when using another generic upstream.
func (ch *GenericChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) (bool, error) {
	upstreamURL := ch.getUpstreamURL()
	if upstreamURL == nil {
		return false, fmt.Errorf("no upstream URL configured for channel %s", ch.Name)
	}
	if strings.TrimSpace(ch.ValidationEndpoint) == "" {
		return false, fmt.Errorf("validation endpoint is required for channel %s", ch.Name)
	}

	endpointURL, err := url.Parse(ch.ValidationEndpoint)
	if err != nil {
		return false, fmt.Errorf("failed to parse validation endpoint: %w", err)
	}

	finalURL := *upstreamURL
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + endpointURL.Path
	finalURL.RawQuery = endpointURL.RawQuery

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, finalURL.String(), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create validation request: %w", err)
	}
	if apiKey != nil {
		req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
	}

	if group != nil && len(group.HeaderRuleList) > 0 {
		headerCtx := utils.NewHeaderVariableContext(group, apiKey)
		utils.ApplyHeaderRules(req, group.HeaderRuleList, headerCtx)
	}

	resp, err := ch.GetHTTPClientForKey(apiKey).Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send validation request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	errorBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("key is invalid (status %d), but failed to read error body: %w", resp.StatusCode, err)
	}

	parsedError := app_errors.ParseUpstreamError(errorBody)
	return false, fmt.Errorf("[status %d] %s", resp.StatusCode, parsedError)
}

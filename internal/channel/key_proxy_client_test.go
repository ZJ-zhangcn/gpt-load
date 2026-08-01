package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"gpt-load/internal/httpclient"
	"gpt-load/internal/models"
)

func proxyURLForClient(t *testing.T, client *http.Client) string {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "upstream.example"}})
	if err != nil {
		t.Fatalf("resolve proxy URL: %v", err)
	}
	if proxyURL == nil {
		return ""
	}
	return proxyURL.String()
}

func TestBaseChannelUsesKeyProxyBeforeGroupProxy(t *testing.T) {
	manager := httpclient.NewHTTPClientManager()
	base := &BaseChannel{
		clientManager: manager,
		clientConfig:  httpclient.Config{ProxyURL: "http://group-proxy.example:8080", RequestTimeout: time.Second},
		streamConfig:  httpclient.Config{ProxyURL: "http://group-proxy.example:8080"},
	}

	keyClient := base.GetHTTPClientForKey(&models.APIKey{ProxyURL: "http://key-proxy.example:8080"})
	if got := proxyURLForClient(t, keyClient); got != "http://key-proxy.example:8080" {
		t.Fatalf("key proxy = %q, want key-specific proxy", got)
	}
	fallbackClient := base.GetHTTPClientForKey(&models.APIKey{})
	if got := proxyURLForClient(t, fallbackClient); got != "http://group-proxy.example:8080" {
		t.Fatalf("fallback proxy = %q, want group proxy", got)
	}
	streamClient := base.GetStreamClientForKey(&models.APIKey{ProxyURL: "http://key-proxy.example:8080"})
	if got := proxyURLForClient(t, streamClient); got != "http://key-proxy.example:8080" {
		t.Fatalf("stream proxy = %q, want key-specific proxy", got)
	}
}

func TestOpenAIValidationUsesKeySpecificProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()

	upstreamURL, err := url.Parse("http://unreachable-upstream.example")
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	channel := &OpenAIChannel{BaseChannel: &BaseChannel{
		Name:               "openai",
		Upstreams:          []UpstreamInfo{{URL: upstreamURL, Weight: 1}},
		TestModel:          "gpt-test",
		ValidationEndpoint: "/v1/chat/completions",
		clientManager:      httpclient.NewHTTPClientManager(),
		clientConfig:       httpclient.Config{ProxyURL: "http://127.0.0.1:1", RequestTimeout: time.Second},
	}}

	valid, err := channel.ValidateKey(context.Background(), &models.APIKey{KeyValue: "test-key", ProxyURL: proxy.URL}, &models.Group{})
	if err != nil {
		t.Fatalf("validate through dedicated proxy: %v", err)
	}
	if !valid {
		t.Fatal("expected 204 from dedicated proxy to validate key")
	}
	if proxyRequests.Load() != 1 {
		t.Fatalf("dedicated proxy received %d requests, want 1", proxyRequests.Load())
	}
}

package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gpt-load/internal/models"
)

func TestProbeProxyUsesHTTPProxyAndReturnsStatusAndLatency(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != target.URL+"/" {
			t.Errorf("proxy received target %q, want %q", r.URL.String(), target.URL+"/")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()

	outcome, err := probeProxy(context.Background(), proxy.URL, target.URL, time.Second)
	if err != nil {
		t.Fatalf("probe proxy: %v", err)
	}
	if outcome.Status != proxyCheckStatusUp {
		t.Fatalf("probe status = %q, want %q", outcome.Status, proxyCheckStatusUp)
	}
	if outcome.HTTPStatus != http.StatusNoContent {
		t.Fatalf("probe HTTP status = %d, want %d", outcome.HTTPStatus, http.StatusNoContent)
	}
	if outcome.LatencyMS < 0 {
		t.Fatalf("probe latency = %d, want non-negative", outcome.LatencyMS)
	}
	if outcome.ErrorCode != "" {
		t.Fatalf("successful probe error code = %q", outcome.ErrorCode)
	}
}

func TestProxyPoolCheckPersistsDownResultWithoutInventingHealth(t *testing.T) {
	service, db, _, _, _ := newProxyPoolTestService(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer target.Close()

	imported, err := service.Import(target.URL)
	if err != nil || imported.AddedCount != 1 {
		t.Fatalf("import probe proxy: result=%+v err=%v", imported, err)
	}

	var node models.ProxyNode
	if err := db.First(&node).Error; err != nil {
		t.Fatalf("load proxy node: %v", err)
	}
	service.probeTarget = target.URL
	service.probeTimeout = time.Second

	result, err := service.Check(context.Background(), []uint{node.ID})
	if err != nil {
		t.Fatalf("check proxy: %v", err)
	}
	if result.CheckedCount != 1 || result.HealthyCount != 0 || result.UnhealthyCount != 1 {
		t.Fatalf("unexpected check result: %+v", result)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].CheckStatus != proxyCheckStatusDown {
		t.Fatalf("unexpected checked nodes: %+v", result.Nodes)
	}

	var checked models.ProxyNode
	if err := db.First(&checked, node.ID).Error; err != nil {
		t.Fatalf("load persisted check: %v", err)
	}
	if checked.CheckStatus != proxyCheckStatusDown {
		t.Fatalf("persisted status = %q, want %q", checked.CheckStatus, proxyCheckStatusDown)
	}
	if checked.CheckHTTPStatus != http.StatusBadGateway {
		t.Fatalf("persisted HTTP status = %d, want %d", checked.CheckHTTPStatus, http.StatusBadGateway)
	}
	if checked.CheckedAt == nil {
		t.Fatal("persisted check timestamp is nil")
	}
	if checked.CheckError != "http_status" {
		t.Fatalf("persisted error code = %q, want http_status", checked.CheckError)
	}
}

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/i18n"
	"gpt-load/internal/keypool"
	"gpt-load/internal/models"
	"gpt-load/internal/services"
	"gpt-load/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newProxyPoolHandlerTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	if err := i18n.Init(); err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.Group{}, &models.ProxyNode{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	encryptionSvc, err := encryption.NewService("handler-proxy-pool-test-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	provider := keypool.NewProvider(db, store.NewMemoryStore(), nil, encryptionSvc)
	return &Server{ProxyPoolService: services.NewProxyPoolService(db, provider, encryptionSvc)}, db
}

func TestProxyPoolHandlersImportRebalanceAndDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, db := newProxyPoolHandlerTestServer(t)
	group := models.Group{Name: "handler-group", DisplayName: "handler-group", ChannelType: "openai", TestModel: "test", Upstreams: []byte("[]")}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	keys := []models.APIKey{
		{GroupID: group.ID, KeyValue: "key-one", KeyHash: "hash-one", Status: models.KeyStatusActive},
		{GroupID: group.ID, KeyValue: "key-two", KeyHash: "hash-two", Status: models.KeyStatusActive},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("create keys: %v", err)
	}

	router := gin.New()
	router.POST("/api/proxies/import", server.ImportProxies)
	router.GET("/api/proxies", server.ListProxies)
	router.POST("/api/proxies/rebalance", server.RebalanceProxies)
	router.DELETE("/api/proxies/:id", server.DeleteProxy)

	importBody := []byte(`{"proxies_text":"http://proxy-a.example:8080\nhttp://proxy-b.example:8080"}`)
	importRequest := httptest.NewRequest(http.MethodPost, "/api/proxies/import", bytes.NewReader(importBody))
	importRequest.Header.Set("Content-Type", "application/json")
	importResponse := httptest.NewRecorder()
	router.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", importResponse.Code, importResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/proxies", nil)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var listPayload struct {
		Data []struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode proxy list: %v", err)
	}
	if len(listPayload.Data) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(listPayload.Data))
	}

	rebalanceBody, _ := json.Marshal(map[string]any{"group_id": group.ID, "proxy_ids": []uint{listPayload.Data[0].ID, listPayload.Data[1].ID}})
	rebalanceRequest := httptest.NewRequest(http.MethodPost, "/api/proxies/rebalance", bytes.NewReader(rebalanceBody))
	rebalanceRequest.Header.Set("Content-Type", "application/json")
	rebalanceResponse := httptest.NewRecorder()
	router.ServeHTTP(rebalanceResponse, rebalanceRequest)
	if rebalanceResponse.Code != http.StatusOK {
		t.Fatalf("rebalance status = %d, body = %s", rebalanceResponse.Code, rebalanceResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/proxies/"+jsonNumber(listPayload.Data[0].ID), nil)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	var deletePayload struct {
		Data struct {
			UnboundKeyCount int `json:"unbound_key_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(deleteResponse.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deletePayload.Data.UnboundKeyCount != 1 {
		t.Fatalf("unbound key count = %d, want 1", deletePayload.Data.UnboundKeyCount)
	}
}

func TestProxyPoolHandlersRejectInvalidImport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, _ := newProxyPoolHandlerTestServer(t)
	router := gin.New()
	router.POST("/api/proxies/import", server.ImportProxies)

	request := httptest.NewRequest(http.MethodPost, "/api/proxies/import", bytes.NewBufferString(`{"proxies_text":"ftp://unsupported.example:21"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid import status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestProxyPoolHandlerBatchDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, db := newProxyPoolHandlerTestServer(t)
	if _, err := server.ProxyPoolService.Import("http://proxy-a.example:8080\nhttp://proxy-b.example:8080"); err != nil {
		t.Fatalf("import proxies: %v", err)
	}
	var nodes []models.ProxyNode
	if err := db.Order("id ASC").Find(&nodes).Error; err != nil {
		t.Fatalf("load proxy nodes: %v", err)
	}

	router := gin.New()
	router.POST("/api/proxies/delete", server.DeleteProxies)
	body, _ := json.Marshal(map[string]any{
		"proxy_ids": []uint{nodes[0].ID, nodes[1].ID, 999999},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/proxies/delete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("batch delete status = %d, body = %s", response.Code, response.Body.String())
	}

	var payload struct {
		Data struct {
			RequestedCount  int `json:"requested_count"`
			DeletedCount    int `json:"deleted_count"`
			IgnoredCount    int `json:"ignored_count"`
			UnboundKeyCount int `json:"unbound_key_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode batch delete response: %v", err)
	}
	if payload.Data.RequestedCount != 3 || payload.Data.DeletedCount != 2 || payload.Data.IgnoredCount != 1 {
		t.Fatalf("unexpected batch delete payload: %+v", payload.Data)
	}
}

func TestProxyPoolHandlersCheckEmptyPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, _ := newProxyPoolHandlerTestServer(t)
	router := gin.New()
	router.POST("/api/proxies/check", server.CheckProxies)

	request := httptest.NewRequest(http.MethodPost, "/api/proxies/check", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("check status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			CheckedCount int `json:"checked_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode check response: %v", err)
	}
	if payload.Data.CheckedCount != 0 {
		t.Fatalf("checked count = %d, want 0", payload.Data.CheckedCount)
	}
}

func TestProxyPoolHandlerRebalanceAllHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, db := newProxyPoolHandlerTestServer(t)
	groups := []models.Group{
		{Name: "handler-global-one", DisplayName: "handler-global-one", ChannelType: "openai", TestModel: "test", Upstreams: []byte("[]")},
		{Name: "handler-global-two", DisplayName: "handler-global-two", ChannelType: "openai", TestModel: "test", Upstreams: []byte("[]")},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}
	keys := []models.APIKey{
		{GroupID: groups[0].ID, KeyValue: "global-key-one", KeyHash: "global-hash-one", Status: models.KeyStatusActive},
		{GroupID: groups[1].ID, KeyValue: "global-key-two", KeyHash: "global-hash-two", Status: models.KeyStatusActive},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("create keys: %v", err)
	}
	if _, err := server.ProxyPoolService.Import("http://proxy-a.example:8080"); err != nil {
		t.Fatalf("import proxy: %v", err)
	}
	if err := db.Model(&models.ProxyNode{}).Where("id > 0").Update("check_status", "up").Error; err != nil {
		t.Fatalf("mark proxy healthy: %v", err)
	}

	router := gin.New()
	router.POST("/api/proxies/rebalance-all", server.RebalanceAllProxies)
	request := httptest.NewRequest(http.MethodPost, "/api/proxies/rebalance-all", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("global rebalance status = %d, body = %s", response.Code, response.Body.String())
	}

	var payload struct {
		Data struct {
			ProcessedGroupCount int `json:"processed_group_count"`
			BoundKeyCount       int `json:"bound_key_count"`
			HealthyProxyCount   int `json:"healthy_proxy_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode global rebalance response: %v", err)
	}
	if payload.Data.ProcessedGroupCount != 2 || payload.Data.BoundKeyCount != 2 || payload.Data.HealthyProxyCount != 1 {
		t.Fatalf("unexpected global rebalance payload: %+v", payload.Data)
	}
}

func jsonNumber(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}

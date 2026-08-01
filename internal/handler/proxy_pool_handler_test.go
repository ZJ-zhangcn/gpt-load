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

func jsonNumber(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}

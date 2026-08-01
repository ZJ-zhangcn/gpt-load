package services

import (
	"fmt"
	"testing"

	"gpt-load/internal/encryption"
	"gpt-load/internal/keypool"
	"gpt-load/internal/models"
	"gpt-load/internal/store"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newProxyPoolTestService(t *testing.T) (*ProxyPoolService, *gorm.DB, *keypool.KeyProvider, encryption.Service, store.Store) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.Group{}, &models.APIKey{}, &models.ProxyNode{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	encryptionSvc, err := encryption.NewService("proxy-pool-test-encryption-key")
	if err != nil {
		t.Fatalf("create encryption service: %v", err)
	}
	keyStore := store.NewMemoryStore()
	provider := keypool.NewProvider(db, keyStore, nil, encryptionSvc)
	return NewProxyPoolService(db, provider, encryptionSvc), db, provider, encryptionSvc, keyStore
}

func createTestGroup(t *testing.T, db *gorm.DB, name string) models.Group {
	t.Helper()
	group := models.Group{
		Name:        name,
		DisplayName: name,
		ChannelType: "openai",
		TestModel:   "gpt-test",
		Upstreams:   []byte("[]"),
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	return group
}

func createTestKeys(t *testing.T, db *gorm.DB, encryptionSvc encryption.Service, groupID uint, count int) []models.APIKey {
	t.Helper()
	keys := make([]models.APIKey, 0, count)
	for i := 0; i < count; i++ {
		plain := fmt.Sprintf("key-%d", i+1)
		encrypted, err := encryptionSvc.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt key: %v", err)
		}
		keys = append(keys, models.APIKey{
			GroupID:  groupID,
			KeyValue: encrypted,
			KeyHash:  encryptionSvc.Hash(plain),
			Status:   models.KeyStatusActive,
		})
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("create keys: %v", err)
	}
	return keys
}

func TestProxyPoolImportEncryptsAndDeduplicatesURLs(t *testing.T) {
	service, db, _, encryptionSvc, _ := newProxyPoolTestService(t)

	result, err := service.Import(`
http://proxy-a.example:8080
socks5://user:pass@proxy-b.example:1080
ftp://unsupported.example:21
http://proxy-a.example:8080
HTTP://PROXY-A.EXAMPLE:8080
`)
	if err != nil {
		t.Fatalf("import proxies: %v", err)
	}
	if result.AddedCount != 2 || result.IgnoredCount != 3 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	var nodes []models.ProxyNode
	if err := db.Order("id ASC").Find(&nodes).Error; err != nil {
		t.Fatalf("load proxy nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].URL == "http://proxy-a.example:8080" {
		t.Fatal("proxy URL was stored in plaintext")
	}
	decrypted, err := encryptionSvc.Decrypt(nodes[0].URL)
	if err != nil {
		t.Fatalf("decrypt stored proxy URL: %v", err)
	}
	if decrypted != "http://proxy-a.example:8080" {
		t.Fatalf("unexpected decrypted proxy URL: %q", decrypted)
	}
}

func TestProxyPoolRebalanceAndDeleteRefreshesBoundKeyCache(t *testing.T) {
	service, db, provider, encryptionSvc, keyStore := newProxyPoolTestService(t)
	group := createTestGroup(t, db, "standard-group")
	keys := createTestKeys(t, db, encryptionSvc, group.ID, 3)
	if err := provider.LoadKeysFromDB(); err != nil {
		t.Fatalf("load keys into cache: %v", err)
	}

	imported, err := service.Import("http://proxy-a.example:8080\nhttp://proxy-b.example:8080")
	if err != nil {
		t.Fatalf("import proxies: %v", err)
	}
	if imported.AddedCount != 2 {
		t.Fatalf("expected 2 imported proxies, got %+v", imported)
	}
	var nodes []models.ProxyNode
	if err := db.Order("id ASC").Find(&nodes).Error; err != nil {
		t.Fatalf("load proxy nodes: %v", err)
	}

	rebalanced, err := service.Rebalance(group.ID, []uint{nodes[1].ID, nodes[0].ID})
	if err != nil {
		t.Fatalf("rebalance proxies: %v", err)
	}
	if rebalanced.BoundKeyCount != 3 {
		t.Fatalf("expected 3 rebound keys, got %+v", rebalanced)
	}

	var reboundKeys []models.APIKey
	if err := db.Where("group_id = ?", group.ID).Order("id ASC").Find(&reboundKeys).Error; err != nil {
		t.Fatalf("load rebound keys: %v", err)
	}
	wantProxyIDs := []uint{nodes[0].ID, nodes[1].ID, nodes[0].ID}
	for i, key := range reboundKeys {
		if key.ProxyID == nil || *key.ProxyID != wantProxyIDs[i] {
			t.Fatalf("key %d got proxy %#v, want %d", key.ID, key.ProxyID, wantProxyIDs[i])
		}
		cached, err := keyStore.HGetAll(fmt.Sprintf("key:%d", key.ID))
		if err != nil {
			t.Fatalf("read key %d cache: %v", key.ID, err)
		}
		if cached["proxy_url"] == "" {
			t.Fatalf("key %d cache did not receive its proxy URL", key.ID)
		}
	}

	deleted, err := service.Delete(nodes[0].ID)
	if err != nil {
		t.Fatalf("delete proxy: %v", err)
	}
	if deleted.UnboundKeyCount != 2 {
		t.Fatalf("expected 2 unbound keys, got %+v", deleted)
	}

	var afterDelete []models.APIKey
	if err := db.Where("group_id = ?", group.ID).Order("id ASC").Find(&afterDelete).Error; err != nil {
		t.Fatalf("load keys after delete: %v", err)
	}
	if afterDelete[0].ProxyID != nil || afterDelete[2].ProxyID != nil {
		t.Fatalf("deleted proxy remained bound: %#v", afterDelete)
	}
	if afterDelete[1].ProxyID == nil || *afterDelete[1].ProxyID != nodes[1].ID {
		t.Fatalf("unrelated proxy binding changed: %#v", afterDelete[1].ProxyID)
	}
	for _, keyID := range []uint{keys[0].ID, keys[2].ID} {
		cached, err := keyStore.HGetAll(fmt.Sprintf("key:%d", keyID))
		if err != nil {
			t.Fatalf("read key %d cache after deletion: %v", keyID, err)
		}
		if cached["proxy_url"] != "" {
			t.Fatalf("key %d still has stale proxy cache: %q", keyID, cached["proxy_url"])
		}
	}

	// A full cache rebuild must also clear an old proxy field for an unbound key.
	if err := keyStore.HSet(fmt.Sprintf("key:%d", keys[0].ID), map[string]any{"proxy_url": nodes[0].URL}); err != nil {
		t.Fatalf("seed stale proxy cache: %v", err)
	}
	if err := provider.LoadKeysFromDB(); err != nil {
		t.Fatalf("reload key cache after proxy deletion: %v", err)
	}
	cached, err := keyStore.HGetAll(fmt.Sprintf("key:%d", keys[0].ID))
	if err != nil {
		t.Fatalf("read rebuilt cache: %v", err)
	}
	if cached["proxy_url"] != "" {
		t.Fatalf("cache rebuild retained stale proxy URL: %q", cached["proxy_url"])
	}
}

func TestProxyPoolRebalanceRejectsAggregateGroups(t *testing.T) {
	service, db, _, encryptionSvc, _ := newProxyPoolTestService(t)
	group := createTestGroup(t, db, "aggregate-group")
	if err := db.Model(&group).Update("group_type", "aggregate").Error; err != nil {
		t.Fatalf("make aggregate group: %v", err)
	}
	createTestKeys(t, db, encryptionSvc, group.ID, 1)
	if _, err := service.Import("http://proxy-a.example:8080"); err != nil {
		t.Fatalf("import proxy: %v", err)
	}
	var node models.ProxyNode
	if err := db.First(&node).Error; err != nil {
		t.Fatalf("load proxy node: %v", err)
	}

	if _, err := service.Rebalance(group.ID, []uint{node.ID}); err == nil {
		t.Fatal("expected aggregate group rebalance to fail")
	}
}

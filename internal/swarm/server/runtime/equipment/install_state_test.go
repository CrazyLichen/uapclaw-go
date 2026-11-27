package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newInstallStateEnv 隔离数据根。
func newInstallStateEnv(t *testing.T) {
	t.Helper()
	t.Setenv(pathutil.EnvDataDir, t.TempDir())
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
}

func TestInstallState_UpsertAndGet(t *testing.T) {
	newInstallStateEnv(t)
	if err := UpsertAgentTemplateMarketplaceEntry("pkg_a", false, "local"); err != nil {
		t.Fatal(err)
	}
	entries := ReadAgentTemplateMarketplaceEntries()
	if len(entries) != 1 {
		t.Fatalf("want 1 entry got %d", len(entries))
	}
	if entries[0]["id"] != "pkg_a" || entries[0]["installed"] != false || entries[0]["source"] != "local" {
		t.Fatalf("entry 形状不符: %+v", entries[0])
	}
	// 二次 upsert 合并既有字段
	if err := UpsertAgentTemplateMarketplaceEntry("pkg_a", true, "local"); err != nil {
		t.Fatal(err)
	}
	entries = ReadAgentTemplateMarketplaceEntries()
	if len(entries) != 1 || entries[0]["installed"] != true {
		t.Fatalf("upsert 应覆盖: %+v", entries)
	}
	// 目录缓存遗留键剥离（Python 注释语义）
	entries[0]["name"] = "残留"
	_ = writeMarketplaceEntries(equipmentMarketplacePath(agentTemplateKind, ""), entries)
	if err := UpsertAgentTemplateMarketplaceEntry("pkg_a", true, "local"); err != nil {
		t.Fatal(err)
	}
	entries = ReadAgentTemplateMarketplaceEntries()
	if _, has := entries[0]["name"]; has {
		t.Fatalf("目录缓存键应剥离: %+v", entries[0])
	}
}

func TestInstallState_Remove(t *testing.T) {
	newInstallStateEnv(t)
	_ = UpsertAgentTemplateMarketplaceEntry("pkg_a", true, "local")
	if err := RemoveAgentTemplateMarketplaceEntry("pkg_a"); err != nil {
		t.Fatal(err)
	}
	if len(ReadAgentTemplateMarketplaceEntries()) != 0 {
		t.Fatal("删除后应空")
	}
	// 无变化不写盘：再删一次不报错
	if err := RemoveAgentTemplateMarketplaceEntry("pkg_a"); err != nil {
		t.Fatalf("幂等删除应成功: %v", err)
	}
}

func TestInstallState_StoreGetByPackageID(t *testing.T) {
	newInstallStateEnv(t)
	store := NewHubInstallStateStore(agentGroupKind)
	rec := &HubInstallStateRecord{
		ID: "asset-uuid-1", PackageID: "grp_x", AssetID: "asset-uuid-1",
		Kind: "agent_group", Installed: true, Source: "hub", Version: "1.2.0",
	}
	if err := store.Set(rec); err != nil {
		t.Fatal(err)
	}
	got := store.Get("asset-uuid-1")
	if got == nil || got.PackageID != "grp_x" || got.Version != "1.2.0" {
		t.Fatalf("get 失败: %+v", got)
	}
	if got2 := store.GetByPackageID("grp_x"); got2 == nil || got2.ID != "asset-uuid-1" {
		t.Fatalf("get_by_package_id 失败: %+v", got2)
	}
	if got3 := store.GetByPackageID("nope"); got3 != nil {
		t.Fatal("未知包应 nil")
	}
	// remove
	if err := store.Remove("asset-uuid-1"); err != nil {
		t.Fatal(err)
	}
	if store.Get("asset-uuid-1") != nil {
		t.Fatal("remove 后应 nil")
	}
}

func TestInstallState_GroupUpdatedAt(t *testing.T) {
	newInstallStateEnv(t)
	if err := UpsertAgentGroupMarketplaceEntry("grp_a", false, "local"); err != nil {
		t.Fatal(err)
	}
	entries := ReadAgentGroupMarketplaceEntries()
	if _, ok := entries[0]["updated_at_ns"]; !ok {
		t.Fatalf("分组 upsert 应带 updated_at_ns: %+v", entries[0])
	}
}

// 编译期守卫：marketplace.json 形状（plugins 数组）与 Python 读取一致。
func TestInstallState_MarketplaceShape(t *testing.T) {
	newInstallStateEnv(t)
	p := equipmentMarketplacePath(agentTemplateKind, "")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"plugins":[{"id":"x","installed":true,"source":"local"}]}`), 0o644)
	entries := readMarketplaceEntries(p)
	if len(entries) != 1 || entries[0]["id"] != "x" {
		t.Fatalf("形状不符: %+v", entries)
	}
	// 损坏 JSON → 空表
	_ = os.WriteFile(p, []byte("not-json"), 0o644)
	if got := readMarketplaceEntries(p); len(got) != 0 {
		t.Fatal("损坏应空表")
	}
	_ = json.Marshal
	_ = os.WriteFile
}

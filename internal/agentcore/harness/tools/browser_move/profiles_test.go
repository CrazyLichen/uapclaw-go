package browser_move

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewBrowserProfileFromDict 测试从字典创建 BrowserProfile
func TestNewBrowserProfileFromDict(t *testing.T) {
	raw := map[string]any{
		"name":           "  test-profile  ",
		"driver_type":    "  LOCAL  ",
		"cdp_url":        "  ws://localhost:9222  ",
		"browser_binary": "/usr/bin/chromium",
		"user_data_dir":  "/tmp/browser-data",
		"debug_port":     float64(9222),
		"host":           "  0.0.0.0  ",
		"extra_args":     []any{"--no-sandbox", "--disable-gpu"},
	}

	p := NewBrowserProfileFromDict(raw)

	if p.Name != "test-profile" {
		t.Errorf("Name = %q, want %q", p.Name, "test-profile")
	}
	if p.DriverType != "local" {
		t.Errorf("DriverType = %q, want %q", p.DriverType, "local")
	}
	if p.CDPURL != "ws://localhost:9222" {
		t.Errorf("CDPURL = %q, want %q", p.CDPURL, "ws://localhost:9222")
	}
	if p.DebugPort != 9222 {
		t.Errorf("DebugPort = %d, want %d", p.DebugPort, 9222)
	}
	if p.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", p.Host, "0.0.0.0")
	}
	if len(p.ExtraArgs) != 2 || p.ExtraArgs[0] != "--no-sandbox" {
		t.Errorf("ExtraArgs = %v, want [--no-sandbox --disable-gpu]", p.ExtraArgs)
	}
}

// TestNewBrowserProfileFromDict_默认值 测试缺失字段使用默认值
func TestNewBrowserProfileFromDict_默认值(t *testing.T) {
	raw := map[string]any{
		"name": "minimal",
	}

	p := NewBrowserProfileFromDict(raw)

	if p.DriverType != "remote" {
		t.Errorf("DriverType = %q, want %q", p.DriverType, "remote")
	}
	if p.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", p.Host, "127.0.0.1")
	}
	if p.DebugPort != 0 {
		t.Errorf("DebugPort = %d, want 0", p.DebugPort)
	}
	if len(p.ExtraArgs) != 0 {
		t.Errorf("ExtraArgs = %v, want empty", p.ExtraArgs)
	}
}

// TestNewBrowserProfileFromDict_空名称 测试空名称
func TestNewBrowserProfileFromDict_空名称(t *testing.T) {
	raw := map[string]any{}
	p := NewBrowserProfileFromDict(raw)
	if p.Name != "" {
		t.Errorf("Name = %q, want empty", p.Name)
	}
}

// TestBrowserProfile_ToDict 测试 ToDict 序列化
func TestBrowserProfile_ToDict(t *testing.T) {
	p := &BrowserProfile{
		Name:       "test",
		DriverType: "remote",
		Host:       "127.0.0.1",
		DebugPort:  9222,
		ExtraArgs:  []string{"--no-sandbox"},
	}

	d := p.ToDict()
	if d["name"] != "test" {
		t.Errorf("name = %v, want test", d["name"])
	}
	if d["debug_port"] != 9222 {
		t.Errorf("debug_port = %v, want 9222", d["debug_port"])
	}
	if d["driver_type"] != "remote" {
		t.Errorf("driver_type = %v, want remote", d["driver_type"])
	}
}

// TestBrowserProfileStore_完整流程 测试完整的 CRUD 流程
func TestBrowserProfileStore_完整流程(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "profiles.json")
	store := NewBrowserProfileStore(path)

	// 初始为空
	if len(store.ListProfiles()) != 0 {
		t.Error("初始应为空")
	}
	if store.SelectedName() != "" {
		t.Error("初始选中名称应为空")
	}
	if store.SelectedProfile() != nil {
		t.Error("初始选中配置应为 nil")
	}

	// Upsert
	p1 := &BrowserProfile{Name: "beta", DriverType: "local", Host: "0.0.0.0"}
	if _, err := store.UpsertProfile(p1, false); err != nil {
		t.Fatalf("UpsertProfile 失败: %v", err)
	}
	p2 := &BrowserProfile{Name: "alpha", DriverType: "remote"}
	if _, err := store.UpsertProfile(p2, true); err != nil {
		t.Fatalf("UpsertProfile 失败: %v", err)
	}

	// 排序列表
	profiles := store.ListProfiles()
	if len(profiles) != 2 {
		t.Fatalf("ListProfiles 长度 = %d, want 2", len(profiles))
	}
	if profiles[0].Name != "alpha" {
		t.Errorf("排序后第一个 = %q, want alpha", profiles[0].Name)
	}

	// 选中状态
	if store.SelectedName() != "alpha" {
		t.Errorf("SelectedName = %q, want alpha", store.SelectedName())
	}
	if sp := store.SelectedProfile(); sp == nil || sp.Name != "alpha" {
		t.Error("SelectedProfile 应为 alpha")
	}

	// GetProfile
	if gp := store.GetProfile("beta"); gp == nil || gp.DriverType != "local" {
		t.Error("GetProfile(beta) 应返回 DriverType=local")
	}
	if gp := store.GetProfile("nonexistent"); gp != nil {
		t.Error("GetProfile(nonexistent) 应返回 nil")
	}
	if gp := store.GetProfile(""); gp != nil {
		t.Error("GetProfile('') 应返回 nil")
	}

	// SelectProfile
	if _, err := store.SelectProfile("beta"); err != nil {
		t.Fatalf("SelectProfile 失败: %v", err)
	}
	if store.SelectedName() != "beta" {
		t.Errorf("SelectProfile 后 SelectedName = %q, want beta", store.SelectedName())
	}

	// SelectProfile 不存在
	if _, err := store.SelectProfile("nonexistent"); err == nil {
		t.Error("SelectProfile 不存在时应返回错误")
	}

	// RemoveProfile
	if !store.RemoveProfile("beta") {
		t.Error("RemoveProfile(beta) 应返回 true")
	}
	if store.GetProfile("beta") != nil {
		t.Error("移除后 GetProfile 应返回 nil")
	}
	if store.SelectedName() != "" {
		t.Errorf("移除选中配置后 SelectedName = %q, want 空", store.SelectedName())
	}

	// RemoveProfile 不存在
	if store.RemoveProfile("nonexistent") {
		t.Error("RemoveProfile 不存在时应返回 false")
	}
	if store.RemoveProfile("") {
		t.Error("RemoveProfile 空名应返回 false")
	}
}

// TestBrowserProfileStore_UpsertProfile_空名称 测试空名称报错
func TestBrowserProfileStore_UpsertProfile_空名称(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewBrowserProfileStore(filepath.Join(tmpDir, "profiles.json"))

	_, err := store.UpsertProfile(&BrowserProfile{Name: ""}, false)
	if err == nil {
		t.Error("空名称应返回错误")
	}
}

// TestBrowserProfileStore_UpsertProfile_清除失效选中 测试移除选中配置后自动清除选中状态
func TestBrowserProfileStore_UpsertProfile_清除失效选中(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewBrowserProfileStore(filepath.Join(tmpDir, "profiles.json"))

	p1 := &BrowserProfile{Name: "selected"}
	store.UpsertProfile(p1, true)
	if store.SelectedName() != "selected" {
		t.Errorf("SelectedName = %q, want selected", store.SelectedName())
	}

	// 移除选中配置
	store.RemoveProfile("selected")
	if store.SelectedName() != "" {
		t.Errorf("移除后 SelectedName = %q, want 空", store.SelectedName())
	}
}

// TestBrowserProfileStore_持久化 测试持久化和重新加载
func TestBrowserProfileStore_持久化(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "profiles.json")

	store := NewBrowserProfileStore(path)
	p1 := &BrowserProfile{Name: "remote-profile", CDPURL: "ws://localhost:9222", ExtraArgs: []string{"--flag1"}}
	store.UpsertProfile(p1, true)

	// 重新加载
	store2 := NewBrowserProfileStore(path)
	if store2.SelectedName() != "remote-profile" {
		t.Errorf("重载后 SelectedName = %q, want remote-profile", store2.SelectedName())
	}
	gp := store2.GetProfile("remote-profile")
	if gp == nil {
		t.Fatal("重载后 GetProfile 应非 nil")
	}
	if gp.CDPURL != "ws://localhost:9222" {
		t.Errorf("CDPURL = %q, want ws://localhost:9222", gp.CDPURL)
	}
	if len(gp.ExtraArgs) != 1 || gp.ExtraArgs[0] != "--flag1" {
		t.Errorf("ExtraArgs = %v, want [--flag1]", gp.ExtraArgs)
	}
}

// TestBrowserProfileStore_文件不存在 测试文件不存在时的加载
func TestBrowserProfileStore_文件不存在(t *testing.T) {
	store := NewBrowserProfileStore("/nonexistent/path/profiles.json")
	if len(store.ListProfiles()) != 0 {
		t.Error("文件不存在时配置列表应为空")
	}
}

// TestBrowserProfileStore_无效JSON 测试无效 JSON 文件的加载
func TestBrowserProfileStore_无效JSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "profiles.json")
	os.WriteFile(path, []byte("not valid json"), 0o644)

	store := NewBrowserProfileStore(path)
	if len(store.ListProfiles()) != 0 {
		t.Error("无效 JSON 时配置列表应为空")
	}
}

// TestBrowserProfileStore_非字典JSON 测试非字典 JSON 的加载
func TestBrowserProfileStore_非字典JSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "profiles.json")
	os.WriteFile(path, []byte(`[1, 2, 3]`), 0o644)

	store := NewBrowserProfileStore(path)
	if len(store.ListProfiles()) != 0 {
		t.Error("非字典 JSON 时配置列表应为空")
	}
}

// TestBrowserProfileStore_Save验证JSON内容 测试保存后 JSON 内容正确
func TestBrowserProfileStore_Save验证JSON内容(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "profiles.json")
	store := NewBrowserProfileStore(path)

	store.UpsertProfile(&BrowserProfile{Name: "z-profile", DriverType: "remote"}, false)
	store.UpsertProfile(&BrowserProfile{Name: "a-profile", DriverType: "local"}, true)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	if payload["selected_profile"] != "a-profile" {
		t.Errorf("selected_profile = %v, want a-profile", payload["selected_profile"])
	}

	profilesList, ok := payload["profiles"].([]any)
	if !ok || len(profilesList) != 2 {
		t.Fatalf("profiles 长度 = %d, want 2", len(profilesList))
	}

	// 验证排序：a-profile 在前
	first := profilesList[0].(map[string]any)
	if first["name"] != "a-profile" {
		t.Errorf("排序后第一个 = %v, want a-profile", first["name"])
	}
}

// TestExpandHome 测试路径展开
func TestExpandHome(t *testing.T) {
	// 非 ~ 开头路径不变
	if result := expandHome("/absolute/path"); result != "/absolute/path" {
		t.Errorf("expandHome(/absolute/path) = %q, want /absolute/path", result)
	}
	// ~ 开头应展开
	if result := expandHome("~/test"); result == "~/test" {
		t.Error("expandHome(~/test) 应展开 ~")
	}
}

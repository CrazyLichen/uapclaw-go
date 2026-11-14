package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// newTestCatalog 构造带固定快照的测试目录。
func newTestCatalog(t *testing.T, cfg map[string]any) *ModelCatalog {
	t.Helper()
	return NewModelCatalog(cfg)
}

// setSessionsRoot 把会话目录指到临时目录并注册恢复。
func setSessionsRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, had := os.LookupEnv("UAPCLAW_DATA_DIR")
	if err := os.Setenv("UAPCLAW_DATA_DIR", dir); err != nil {
		t.Fatal(err)
	}
	path.ResetCache()
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("UAPCLAW_DATA_DIR", old)
		} else {
			_ = os.Unsetenv("UAPCLAW_DATA_DIR")
		}
		path.ResetCache()
	})
	return dir
}

func testCatalogConfig() map[string]any {
	return map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_id": "mdl_a", "is_default": true, "alias": "alias_a",
					"model_client_config": map[string]any{"model_name": "gpt-a", "client_provider": "openai", "api_base": "https://a.example.com", "api_key": "sk-secret"},
					"model_config_obj":    map[string]any{"reasoning_level": true},
				},
				map[string]any{
					"model_id": "mdl_b",
					"model_client_config": map[string]any{"model_name": "gpt-b", "client_provider": "openai"},
				},
			},
			"groups": []any{
				map[string]any{
					"model_group_id": "mgp_a", "display_name": "组A", "enabled": true, "is_default": true,
					"request_config": map[string]any{},
					"routing":        map[string]any{"timeout_seconds": 30, "stream_idle_timeout": 60, "other": "x"},
					"routes": []any{
						map[string]any{"route_id": "rte_1", "model_id": "mdl_a", "enabled": true, "tpm": 100},
						map[string]any{"route_id": "rte_2", "model_id": "mdl_b", "enabled": false},
					},
				},
			},
		},
	}
}

// GetModel/GetGroup：命中与未命中错误码、错误文案。
// 对齐 Python: model_catalog.py:94-104。
func TestModelCatalog_GetModelGetGroup_命中与未命中(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	hit, err := c.GetModel("mdl_a")
	if err != nil || hit.Source != "defaults" {
		t.Fatalf("mdl_a 应命中 defaults: %v %v", hit, err)
	}
	_, err = c.GetModel("mdl_x")
	if err == nil || err.Error() != "unknown model_id \"mdl_x\"" {
		t.Errorf("GetModel 未命中文案不符: %v", err)
	}
	g, err := c.GetGroup("mgp_a")
	if err != nil || asString(g["display_name"]) != "组A" {
		t.Fatalf("mgp_a 应命中: %v", err)
	}
	_, err = c.GetGroup("mgp_x")
	if err == nil || err.Error() != "unknown model_group_id \"mgp_x\"" {
		t.Errorf("GetGroup 未命中文案不符: %v", err)
	}
}

// GetModel 未命中错误码应为 MODEL_SELECTION_NOT_FOUND。
func TestModelCatalog_GetModel_错误码(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	_, err := c.GetModel("mdl_x")
	se, ok := err.(*ModelSelectionError)
	if !ok || se.Code != ModelSelectionNotFound {
		t.Errorf("错误码应为 MODEL_SELECTION_NOT_FOUND: %v", err)
	}
}

// HasSelection：模型存在/不存在、组带路由校验。
// 对齐 Python: model_catalog.py:106-117。
func TestModelCatalog_HasSelection_模型与模型组(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	if !c.HasSelection(&ModelSelection{Type: "model", ID: "mdl_a"}) {
		t.Errorf("mdl_a 应存在")
	}
	if c.HasSelection(&ModelSelection{Type: "model", ID: "mdl_x"}) {
		t.Errorf("mdl_x 不应存在")
	}
	if !c.HasSelection(&ModelSelection{Type: "model_group", ID: "mgp_a"}) {
		t.Errorf("mgp_a 应存在")
	}
	if !c.HasSelection(&ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_1"}) {
		t.Errorf("mgp_a/rte_1 应存在")
	}
	if c.HasSelection(&ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_x"}) {
		t.Errorf("mgp_a/rte_x 不应存在")
	}
	if c.HasSelection(&ModelSelection{Type: "model_group", ID: "mgp_x"}) {
		t.Errorf("mgp_x 不应存在")
	}
}

// GroupDisplayName：显示名回落 id。
// 对齐 Python: model_catalog.py:101-110。
func TestModelCatalog_GroupDisplayName_回落id(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	if got := c.GroupDisplayName("mgp_a"); got != "组A" {
		t.Errorf("显示名应为 组A: %q", got)
	}
	if got := c.GroupDisplayName("mgp_x"); got != "mgp_x" {
		t.Errorf("未命中应回落 id: %q", got)
	}
}

// ListPublicModels：双源安全投影，agentos 标记。
// 对齐 Python: model_catalog.py:120-136。
func TestModelCatalog_ListPublicModels_双源(t *testing.T) {
	cfg := testCatalogConfig()
	models := cfg["models"].(map[string]any)
	models["agentos"] = []any{map[string]any{
		"model_id": "mdl_os",
		"model_client_config": map[string]any{"model_name": "os-model"},
	}}
	c := newTestCatalog(t, cfg)
	list := c.ListPublicModels()
	// 机器核对：defaults 2 + agentos 1 = 3
	if len(list) != 3 {
		t.Fatalf("应有 3 条公开模型，实际 %d", len(list))
	}
	a := list[0]
	if a["model_id"] != "mdl_a" || a["source"] != "defaults" || a["is_default"] != true || a["enabled"] != true || a["alias"] != "alias_a" {
		t.Errorf("defaults 投影字段不符: %v", a)
	}
	os := list[2]
	if os["is_agentos"] != true || os["source"] != "agentos" {
		t.Errorf("agentos 投影标记不符: %v", os)
	}
}

// GetPublicModelDetail：reasoning_level 归一、api_key 剔除、write_only_fields、只读标记。
// 对齐 Python: model_catalog.py:138-171。
func TestModelCatalog_GetPublicModelDetail_脱敏与归一(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	// 不含 api_key
	detail, err := c.GetPublicModelDetail("mdl_a", false)
	if err != nil {
		t.Fatalf("详情获取失败: %v", err)
	}
	mcc := detail["model_client_config"].(map[string]any)
	if _, exists := mcc["api_key"]; exists {
		t.Errorf("include_api_key=false 时 api_key 应剔除")
	}
	if !strings.Contains(strings.Join(toStrSlice(detail["write_only_fields"]), ","), "model_client_config.api_key") {
		t.Errorf("write_only_fields 应含 api_key: %v", detail["write_only_fields"])
	}
	// reasoning_level bool → "on"
	mco := detail["model_config_obj"].(map[string]any)
	if mco["reasoning_level"] != "on" {
		t.Errorf("reasoning_level 应归一为 on: %v", mco["reasoning_level"])
	}
	if detail["source"] != "defaults" || detail["read_only"] != false {
		t.Errorf("defaults 详情标记不符: %v %v", detail["source"], detail["read_only"])
	}
	// include_api_key=true 保留
	detail, _ = c.GetPublicModelDetail("mdl_a", true)
	mcc = detail["model_client_config"].(map[string]any)
	if mcc["api_key"] != "sk-secret" {
		t.Errorf("include_api_key=true 应保留 api_key")
	}
	if _, exists := mcc["custom_headers"]; exists {
		t.Errorf("custom_headers 恒剔除")
	}
}

// ListPublicGroups：白名单键 + routing 三超时键过滤。
// 对齐 Python: model_catalog.py:173-192。
func TestModelCatalog_ListPublicGroups_白名单与超时键(t *testing.T) {
	c := newTestCatalog(t, testCatalogConfig())
	groups := c.ListPublicGroups()
	if len(groups) != 1 {
		t.Fatalf("应有 1 个组，实际 %d", len(groups))
	}
	g := groups[0]
	for _, key := range []string{"model_group_id", "display_name", "enabled", "is_default", "routes", "request_config"} {
		if _, ok := g[key]; !ok {
			t.Errorf("缺少白名单键 %q", key)
		}
	}
	routing := g["routing"].(map[string]any)
	if len(routing) != 2 {
		t.Errorf("routing 应只含两个已配置超时键: %v", routing)
	}
	if _, exists := routing["other"]; exists {
		t.Errorf("routing 的非超时键应过滤")
	}
}

// FindReferences：模型组引用 + agents/team 配置树递归 + 去重。
// 对齐 Python: model_catalog.py:194-238。
func TestModelCatalog_FindReferences_组引用与配置树(t *testing.T) {
	setSessionsRoot(t) // 会话目录指到临时目录，隔离真实环境
	cfg := testCatalogConfig()
	cfg["agents"] = map[string]any{
		"id": "agent_root",
		"model_selection": map[string]any{"type": "model", "id": "mdl_b"},
		"sub": []any{map[string]any{"model_selection": map[string]any{"type": "model", "id": "mdl_b"}}},
	}
	cfg["team"] = map[string]any{"name": "team_x", "model_selection": map[string]any{"type": "model", "id": "mdl_a"}}
	c := newTestCatalog(t, cfg)
	refs, err := c.FindReferences(&ModelSelection{Type: "model", ID: "mdl_a"}, true)
	if err != nil {
		t.Fatalf("FindReferences 失败: %v", err)
	}
	// 机器核对：mdl_a 被组 mgp_a 的 rte_1 引用 1 次 + team 树 1 次 = 2
	if len(refs) != 2 {
		t.Fatalf("mdl_a 应有 2 处引用，实际 %d: %v", len(refs), refs)
	}
	if refs[0].Scope != "model_group" || refs[0].ScopeID != "mgp_a" {
		t.Errorf("第一处应为 model_group/mgp_a: %v", refs[0])
	}
	if refs[1].Scope != "team" || refs[1].ScopeID != "team_x" {
		t.Errorf("第二处应为 team/team_x: %v", refs[1])
	}
	// mdl_b：agents 树两处，owner 不同 → 2 条
	refs, err = c.FindReferences(&ModelSelection{Type: "model", ID: "mdl_b"}, true)
	if err != nil {
		t.Fatalf("FindReferences 失败: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("mdl_b 应有 2 处引用，实际 %d: %v", len(refs), refs)
	}
}

// FindReferences：session metadata 扫描命中 + 不可读时 RUNTIME_UNAVAILABLE（防误删）。
// 对齐 Python: model_catalog.py:201-218。
func TestModelCatalog_FindReferences_session扫描与防误删(t *testing.T) {
	dir := setSessionsRoot(t)
	cfg := testCatalogConfig()
	c := newTestCatalog(t, cfg)
	// 命中：s1 的 metadata 带匹配选择
	s1 := filepath.Join(dir, "agent", "sessions", "s1", "metadata.json")
	if err := os.MkdirAll(filepath.Dir(s1), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"model_selection": {"type": "model", "id": "mdl_a"}}`
	if err := os.WriteFile(s1, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := c.FindReferences(&ModelSelection{Type: "model", ID: "mdl_a"}, true)
	if err != nil {
		t.Fatalf("FindReferences 失败: %v", err)
	}
	// 机器核对：mdl_a = 组 rte_1 引用 1 + session s1 1 = 2（team 树无 mdl_a）
	found := false
	for _, r := range refs {
		if r.Scope == "session" && r.ScopeID == "s1" {
			found = true
		}
	}
	if !found {
		t.Errorf("应命中 session/s1: %v", refs)
	}
	// 防误删：损坏 metadata → RUNTIME_UNAVAILABLE
	s2 := filepath.Join(dir, "agent", "sessions", "s2", "metadata.json")
	if err := os.MkdirAll(filepath.Dir(s2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s2, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = c.FindReferences(&ModelSelection{Type: "model", ID: "mdl_z"}, true)
	se, ok := err.(*ModelSelectionError)
	if !ok || se.Code != ModelRuntimeUnavailable {
		t.Errorf("损坏 metadata 应报 RUNTIME_UNAVAILABLE: %v", err)
	}
}

// matchesSelection：type/id 双匹配。
// 对齐 Python: model_catalog.py:48-53。
func TestMatchesSelection_双匹配(t *testing.T) {
	sel := &ModelSelection{Type: "model", ID: "mdl_a"}
	if !matchesSelection(sel, map[string]any{"type": "model", "id": "mdl_a"}) {
		t.Errorf("匹配应成功")
	}
	if matchesSelection(sel, map[string]any{"type": "model_group", "id": "mdl_a"}) {
		t.Errorf("type 不同不应匹配")
	}
	if matchesSelection(sel, "not-a-map") {
		t.Errorf("非 map 不应匹配")
	}
}

// findEmbeddedSelections：scope_id 回落链 owner → path → scope。
// 对齐 Python: model_catalog.py:66-70。
func TestFindEmbeddedSelections_回落链(t *testing.T) {
	sel := &ModelSelection{Type: "model", ID: "mdl_a"}
	// 无 owner 无 path → scope
	refs := findEmbeddedSelections(map[string]any{"model_selection": map[string]any{"type": "model", "id": "mdl_a"}}, sel, "agent", nil, "")
	if len(refs) != 1 || refs[0].ScopeID != "agent" {
		t.Errorf("应回落 scope: %v", refs)
	}
	// 有 path 无 owner → path 点连接
	refs = findEmbeddedSelections(map[string]any{"model_selection": map[string]any{"type": "model", "id": "mdl_a"}}, sel, "agent", []string{"a", "b"}, "")
	if len(refs) != 1 || refs[0].ScopeID != "a.b" {
		t.Errorf("应回落 path: %v", refs)
	}
}

// LoadModelsConfig：fallback_tag 回填与 by_id 索引。
// 对齐 Python: jiuwenswarm/common/config.py:1828-1850。
func TestLoadModelsConfig_fallbackTag与索引(t *testing.T) {
	snap := LoadModelsConfig(testCatalogConfig())
	hit, ok := snap.ByID["mdl_a"]
	if !ok || hit.Source != "defaults" {
		t.Fatalf("by_id 应含 mdl_a: %v", snap.ByID)
	}
	detail, _ := hit.Entry["model_detail"].(map[string]any)
	if detail["fallback_tag"] != "gpt-a" {
		t.Errorf("fallback_tag 应回填 model_name: %v", detail)
	}
	if len(snap.Groups) != 1 {
		t.Errorf("应有 1 个组")
	}
}

// toStrSlice any 转字符串切片（测试辅助）。
func toStrSlice(v any) []string {
	raw, _ := v.([]string)
	return raw
}

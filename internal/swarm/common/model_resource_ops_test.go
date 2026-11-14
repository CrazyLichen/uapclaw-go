package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// newTestConfig 在临时目录创建可写 Config。
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := "models:\n  defaults: []\n  agentos: []\n  groups: []\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// UpsertModelResource：新条目补 ID、嵌套归一、is_default 互斥。
// 对齐 Python: config.py:2002-2032。
func TestUpsertModelResource_新增与互斥(t *testing.T) {
	cfg := newTestConfig(t)
	saved, err := UpsertModelResource(cfg, map[string]any{
		"model_name": "gpt-a", "api_base": "https://a", "api_key": "sk-1",
		"client_provider": "openai", "is_default": true,
	})
	if err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	// 扁平字段提升进嵌套结构（Python: _normalize_model_entry）
	mcc, _ := saved["model_client_config"].(map[string]any)
	if mcc["model_name"] != "gpt-a" || mcc["api_base"] != "https://a" || mcc["client_provider"] != "openai" {
		t.Errorf("扁平字段应提升: %v", mcc)
	}
	if _, hasFlat := saved["model_name"]; hasFlat {
		t.Errorf("扁平 model_name 应已移除")
	}
	if !strings.HasPrefix(asString(saved["model_id"]), "mdl_") {
		t.Errorf("应补 mdl_ 业务 ID: %v", saved["model_id"])
	}
	// 第二条也设 default → 第一条被清
	saved2, err := UpsertModelResource(cfg, map[string]any{
		"model_name": "gpt-b", "client_provider": "openai", "is_default": true,
	})
	if err != nil {
		t.Fatalf("第二条失败: %v", err)
	}
	data, _ := cfg.Load()
	models := data["models"].(map[string]any)
	defaults := models["defaults"].([]any)
	if len(defaults) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(defaults))
	}
	first := defaults[0].(map[string]any)
	if first["is_default"] != false {
		t.Errorf("is_default 互斥：第一条应被清: %v", first["is_default"])
	}
	if saved2["is_default"] != true {
		t.Errorf("第二条应为默认")
	}
}

// UpsertModelResource：更新合并保留 write-only（省略 api_key 不清空）。
// 对齐 Python: config.py:1923-1934 _merge_model_update + _protect_model_key。
func TestUpsertModelResource_合并保留凭证(t *testing.T) {
	cfg := newTestConfig(t)
	saved, _ := UpsertModelResource(cfg, map[string]any{
		"model_name": "gpt-a", "api_key": "sk-keep", "client_provider": "openai",
	})
	id := asString(saved["model_id"])
	// 更新省略 api_key
	updated, err := UpsertModelResource(cfg, map[string]any{
		"model_id": id, "model_name": "gpt-a2",
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	mcc, _ := updated["model_client_config"].(map[string]any)
	if mcc["model_name"] != "gpt-a2" {
		t.Errorf("模型名应更新: %v", mcc)
	}
	if mcc["api_key"] != "sk-keep" {
		t.Errorf("省略的 api_key 应保留: %v", mcc["api_key"])
	}
}

// UpsertModelResource：AgentOS 只读。
// 对齐 Python: config.py:1936-1942 _require_editable_model。
func TestUpsertModelResource_AgentOS只读(t *testing.T) {
	cfg := newTestConfig(t)
	// 先造一条 agentos 条目进配置
	data, _ := cfg.Load()
	models := data["models"].(map[string]any)
	models["agentos"] = []any{map[string]any{
		"model_id": "mdl_os", "model_client_config": map[string]any{"model_name": "os-model"},
	}}
	if err := cfg.Save(data); err != nil {
		t.Fatal(err)
	}
	_, err := UpsertModelResource(cfg, map[string]any{"model_id": "mdl_os", "model_name": "x"})
	if err == nil || err.Error() != "AgentOS models are read-only" {
		t.Errorf("AgentOS 只读文案不符: %v", err)
	}
}

// UpsertModelResource：非法 reasoning_level。
// 对齐 Python: config.py:2004-2010。
func TestUpsertModelResource_非法reasoningLevel(t *testing.T) {
	cfg := newTestConfig(t)
	_, err := UpsertModelResource(cfg, map[string]any{
		"model_name": "m", "reasoning_level": "bogus",
	})
	if err == nil || err.Error() != "Invalid reasoning_level" {
		t.Errorf("非法档位文案不符: %v", err)
	}
	// 合法档位归一
	saved, err := UpsertModelResource(cfg, map[string]any{
		"model_name": "m", "reasoning_level": "HIGH",
	})
	if err != nil {
		t.Fatalf("合法档位不应报错: %v", err)
	}
	mco := saved["model_config_obj"].(map[string]any)
	if mco["reasoning_level"] != "high" {
		t.Errorf("档位应归一: %v", mco["reasoning_level"])
	}
}

// UpsertModelGroupResource：缺省 ID、路由补 rte、is_default 互斥、enabled 组须有 enabled 路由。
// 对齐 Python: config.py:2034-2085。
func TestUpsertModelGroupResource_组与互斥(t *testing.T) {
	cfg := newTestConfig(t)
	// 先有模型供路由引用
	savedModel, _ := UpsertModelResource(cfg, map[string]any{"model_name": "gpt-a", "client_provider": "openai"})
	modelID := asString(savedModel["model_id"])
	// 无 model_group_id / route_id
	group, err := UpsertModelGroupResource(cfg, map[string]any{
		"display_name": "组A", "enabled": true,
		"routes": []any{map[string]any{"model_id": modelID, "enabled": true}},
	})
	if err != nil {
		t.Fatalf("组新增失败: %v", err)
	}
	if !strings.HasPrefix(asString(group["model_group_id"]), "mgp_") {
		t.Errorf("应补 mgp_ ID")
	}
	route := group["routes"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(asString(route["route_id"]), "rte_") {
		t.Errorf("路由应补 rte_ ID: %v", route["route_id"])
	}
	// enabled 组无 enabled 路由
	_, err = UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": "mgp_new", "display_name": "B", "enabled": true,
		"routes": []any{map[string]any{"model_id": modelID, "enabled": false}},
	})
	if err == nil || err.Error() != "enabled model group must contain an enabled route" {
		t.Errorf("无 enabled 路由文案不符: %v", err)
	}
	// 组默认互斥
	_, err = UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": asString(group["model_group_id"]), "is_default": true, "enabled": true,
		"routes": []any{map[string]any{"model_id": modelID, "enabled": true}},
	})
	if err != nil {
		t.Fatalf("默认组更新失败: %v", err)
	}
	second, err := UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": "mgp_second", "is_default": true, "enabled": true,
		"routes": []any{map[string]any{"model_id": modelID, "enabled": true}},
	})
	if err != nil {
		t.Fatalf("第二组失败: %v", err)
	}
	data, _ := cfg.Load()
	groups := data["models"].(map[string]any)["groups"].([]any)
	first := groups[0].(map[string]any)
	if first["is_default"] != false {
		t.Errorf("组默认互斥：第一组应被清")
	}
	if second["is_default"] != true {
		t.Errorf("第二组应为默认")
	}
}

// UpsertModelGroupResource：routing 三超时键白名单。
// 对齐 Python: config.py:2037-2045。
func TestUpsertModelGroupResource_Routing超时白名单(t *testing.T) {
	cfg := newTestConfig(t)
	savedModel, _ := UpsertModelResource(cfg, map[string]any{"model_name": "m", "client_provider": "openai"})
	modelID := asString(savedModel["model_id"])
	saved, err := UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": "mgp_rt", "enabled": true,
		"routes": []any{map[string]any{"model_id": modelID, "enabled": true}},
		"routing": map[string]any{"timeout_seconds": 30, "strategy": "legacy-x", "stream_idle_timeout": 60},
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	routing := saved["routing"].(map[string]any)
	if len(routing) != 2 {
		t.Errorf("routing 应只含两个超时键: %v", routing)
	}
	if _, exists := routing["strategy"]; exists {
		t.Errorf("旧策略键应剔除")
	}
}

// DeleteModelResource：删除模型联动清路由、空组移除、未命中返回 false。
// 对齐 Python: config.py:1174-1222。
func TestDeleteModelResource_联动清路由(t *testing.T) {
	cfg := newTestConfig(t)
	savedA, _ := UpsertModelResource(cfg, map[string]any{"model_name": "a", "client_provider": "openai"})
	savedB, _ := UpsertModelResource(cfg, map[string]any{"model_name": "b", "client_provider": "openai"})
	idA, idB := asString(savedA["model_id"]), asString(savedB["model_id"])
	_, err := UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": "mgp_del", "enabled": true,
		"routes": []any{
			map[string]any{"route_id": "rte_a", "model_id": idA, "enabled": true},
			map[string]any{"route_id": "rte_b", "model_id": idB, "enabled": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 删除 A：组保留（还有 rte_b）
	deleted, err := DeleteModelResource(cfg, "model", idA)
	if err != nil || !deleted {
		t.Fatalf("删除应成功: %v %v", deleted, err)
	}
	data, _ := cfg.Load()
	groups := data["models"].(map[string]any)["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("组应保留 1 个，实际 %d", len(groups))
	}
	routes := groups[0].(map[string]any)["routes"].([]any)
	if len(routes) != 1 || asString(routes[0].(map[string]any)["route_id"]) != "rte_b" {
		t.Errorf("指向 A 的路由应已清: %v", routes)
	}
	// 删除 B：组空 → 移除
	deleted, _ = DeleteModelResource(cfg, "model", idB)
	if !deleted {
		t.Fatal("删除 B 应成功")
	}
	data, _ = cfg.Load()
	groups = data["models"].(map[string]any)["groups"].([]any)
	if len(groups) != 0 {
		t.Errorf("空组应移除: %d", len(groups))
	}
	// 未命中 → false
	deleted, err = DeleteModelResource(cfg, "model", "mdl_none")
	if err != nil || deleted {
		t.Errorf("未命中应返回 false: %v %v", deleted, err)
	}
	// 删除组（重建 A 供路由引用）
	savedA2, _ := UpsertModelResource(cfg, map[string]any{"model_name": "a2", "client_provider": "openai"})
	idA2 := asString(savedA2["model_id"])
	_, err = UpsertModelGroupResource(cfg, map[string]any{
		"model_group_id": "mgp_x", "enabled": true,
		"routes": []any{map[string]any{"model_id": idA2, "enabled": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err = DeleteModelResource(cfg, "model_group", "mgp_x")
	if err != nil || !deleted {
		t.Errorf("删除组应成功: %v %v", deleted, err)
	}
}

// DeleteModelResource：AgentOS 模型拒绝删除。
func TestDeleteModelResource_AgentOS只读(t *testing.T) {
	cfg := newTestConfig(t)
	data, _ := cfg.Load()
	data["models"].(map[string]any)["agentos"] = []any{map[string]any{
		"model_id": "mdl_os", "model_client_config": map[string]any{"model_name": "os"},
	}}
	_ = cfg.Save(data)
	_, err := DeleteModelResource(cfg, "model", "mdl_os")
	if err == nil || err.Error() != "AgentOS models are read-only" {
		t.Errorf("AgentOS 删除应拒绝: %v", err)
	}
}

// MigrateLegacyModelSelections：旧名迁移与歧义计数。
// 对齐 Python: model_migration.py:24-52。
func TestMigrateLegacyModelSelections_迁移与歧义(t *testing.T) {
	dir := setSessionsRoot(t)
	cfgMap := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{"model_id": "mdl_a", "model_client_config": map[string]any{"model_name": "gpt-a"}},
				map[string]any{"model_id": "mdl_b", "model_client_config": map[string]any{"model_name": "gpt-b"}},
			},
		},
	}
	catalog := NewModelCatalog(cfgMap)
	// s1：旧 model 名可唯一映射 → 迁移
	s1 := filepath.Join(dir, "agent", "sessions", "s1", "metadata.json")
	_ = os.MkdirAll(filepath.Dir(s1), 0o755)
	_ = os.WriteFile(s1, []byte(`{"session_id": "s1", "model": "gpt-a"}`), 0o644)
	// s2：已有 model_selection → 跳过
	s2 := filepath.Join(dir, "agent", "sessions", "s2", "metadata.json")
	_ = os.MkdirAll(filepath.Dir(s2), 0o755)
	_ = os.WriteFile(s2, []byte(`{"session_id": "s2", "model": "gpt-b", "model_selection": {"type": "model", "id": "mdl_b"}}`), 0o644)
	// s3：歧义名（两条同名）→ ambiguous；此处直接用未知名代替歧义路径
	s3 := filepath.Join(dir, "agent", "sessions", "s3", "metadata.json")
	_ = os.MkdirAll(filepath.Dir(s3), 0o755)
	_ = os.WriteFile(s3, []byte(`{"session_id": "s3", "model": "unknown-x"}`), 0o644)
	report, err := MigrateLegacyModelSelections(catalog)
	if err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if report.Sessions != 1 {
		t.Errorf("应迁移 1 个会话，实际 %d", report.Sessions)
	}
	if report.Ambiguous != 1 {
		t.Errorf("歧义应计 1，实际 %d", report.Ambiguous)
	}
	raw, _ := os.ReadFile(s1)
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	sel, _ := data["model_selection"].(map[string]any)
	if sel == nil || sel["type"] != "model" || sel["id"] != "mdl_a" {
		t.Errorf("s1 应写入稳定引用: %v", data["model_selection"])
	}
}

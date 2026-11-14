package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	modelcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newModelsTestEnv 临时 config + 临时会话目录。
func newModelsTestEnv(t *testing.T) *AgentServer {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("models:\n  defaults: []\n  agentos: []\n  groups: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// 会话目录隔离
	oldDataDir, hadDataDir := os.LookupEnv("UAPCLAW_DATA_DIR")
	_ = os.Setenv("UAPCLAW_DATA_DIR", dir)
	path.ResetCache()
	t.Cleanup(func() {
		if hadDataDir {
			_ = os.Setenv("UAPCLAW_DATA_DIR", oldDataDir)
		} else {
			_ = os.Unsetenv("UAPCLAW_DATA_DIR")
		}
		path.ResetCache()
	})
	return &AgentServer{config: c}
}

// modelsRequest 构造请求。
func modelsRequest(method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	req := &schema.AgentRequest{RequestID: "req-1", ChannelID: "ch-1"}
	_ = json.Marshal
	req.ReqMethod = method
	if params != nil {
		raw, _ := json.Marshal(params)
		req.Params = raw
	}
	return req
}

// responsePayload 提取响应 payload。
func responsePayload(t *testing.T, resp *schema.AgentResponse) map[string]any {
	t.Helper()
	// JSON 往返归一：各 handler 返回 []map[string]any 与 []any 混合形态，统一为通用 map
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("payload 序列化失败: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload 解析失败: %v", err)
	}
	return payload
}

// modelsGet 未知模型 → MODEL_SELECTION_NOT_FOUND。
// 对齐 Python: app_web_handlers.py:5887-5889 + ModelSelectionError 透传。
func TestHandleModelsGet_未知模型错误码(t *testing.T) {
	s := newModelsTestEnv(t)
	resp, err := s.HandleModelsGet(t.Context(), modelsRequest(schema.ReqMethodModelsGet, map[string]any{"model_id": "mdl_x"}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatalf("未知模型应失败")
	}
	payload := responsePayload(t, resp)
	if payload["code"] != "MODEL_SELECTION_NOT_FOUND" {
		t.Errorf("错误码不符: %v", payload["code"])
	}
	if payload["message"] != "unknown model_id \"mdl_x\"" {
		t.Errorf("错误文案不符: %v", payload["message"])
	}
}

// models 全链路：upsert → get → groups.upsert → groups.list → references → delete。
// 对齐 Python: _model_resource_rpc 各分支 payload 形状。
func TestHandleModels_全链路Payload形状(t *testing.T) {
	s := newModelsTestEnv(t)
	ctx := t.Context()
	// upsert：payload = {"model": detail, "applied_without_restart": bool}
	resp, err := s.HandleModelsUpsert(ctx, modelsRequest(schema.ReqMethodModelsUpsert, map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai", "api_key": "sk-1", "is_default": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("upsert 应成功: %s", resp.Payload)
	}
	payload := responsePayload(t, resp)
	model, _ := payload["model"].(map[string]any)
	if model == nil || model["model_id"] == nil {
		t.Fatalf("upsert 应返回 model 详情: %v", payload)
	}
	if _, ok := payload["applied_without_restart"]; !ok {
		t.Errorf("upsert 应含 applied_without_restart")
	}
	modelID := model["model_id"].(string)
	// models.get：payload = {"model": ...}
	resp, _ = s.HandleModelsGet(ctx, modelsRequest(schema.ReqMethodModelsGet, map[string]any{"model_id": modelID}))
	payload = responsePayload(t, resp)
	if payload["model"] == nil {
		t.Errorf("get 应返回 model")
	}
	// groups.upsert：payload = {"group": ..., "applied_without_restart"}
	resp, _ = s.HandleModelGroupsUpsert(ctx, modelsRequest(schema.ReqMethodModelGroupsUpsert, map[string]any{
		"group": map[string]any{"display_name": "组A", "enabled": true,
			"routes": []any{map[string]any{"model_id": modelID, "enabled": true}}},
	}))
	payload = responsePayload(t, resp)
	group, _ := payload["group"].(map[string]any)
	if group == nil || group["model_group_id"] == nil {
		t.Fatalf("groups.upsert 应返回 group: %v", payload)
	}
	groupID := group["model_group_id"].(string)
	// groups.list：payload = {"groups": [...]}
	resp, _ = s.HandleModelGroupsList(ctx, modelsRequest(schema.ReqMethodModelGroupsList, nil))
	payload = responsePayload(t, resp)
	groups, _ := payload["groups"].([]any)
	if len(groups) != 1 {
		t.Errorf("list 应含 1 组: %v", payload)
	}
	// references：payload = {"references": [{scope, scope_id}]}
	// （先删除默认组引用：references 针对 model 查询 → model_group 引用）
	resp, _ = s.HandleModelsReferences(ctx, modelsRequest(schema.ReqMethodModelsReferences, map[string]any{
		"type": "model", "id": modelID,
	}))
	payload = responsePayload(t, resp)
	refs, _ := payload["references"].([]any)
	if len(refs) != 1 {
		t.Fatalf("模型应被组引用 1 处: %v", payload)
	}
	ref := refs[0].(map[string]any)
	if ref["scope"] != "model_group" || ref["scope_id"] != groupID {
		t.Errorf("引用形状不符: %v", ref)
	}
	// 删除组后删除模型：payload = {"model_id": id, "deleted": true}
	resp, _ = s.HandleModelGroupsDelete(ctx, modelsRequest(schema.ReqMethodModelGroupsDelete, map[string]any{"model_group_id": groupID}))
	payload = responsePayload(t, resp)
	if payload["deleted"] != true || payload["model_group_id"] != groupID {
		t.Errorf("groups.delete payload 形状不符: %v", payload)
	}
	resp, _ = s.HandleModelsDelete(ctx, modelsRequest(schema.ReqMethodModelsDelete, map[string]any{"model_id": modelID}))
	payload = responsePayload(t, resp)
	if payload["deleted"] != true || payload["model_id"] != modelID {
		t.Errorf("models.delete payload 形状不符: %v", payload)
	}
}

// 删除仍被引用的资源 → MODEL_SELECTION_REFERENCED。
// 对齐 Python: app_web_handlers.py:5909-5912。
func TestHandleModelsDelete_仍被引用(t *testing.T) {
	s := newModelsTestEnv(t)
	ctx := t.Context()
	resp, _ := s.HandleModelsUpsert(ctx, modelsRequest(schema.ReqMethodModelsUpsert, map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai"},
	}))
	modelID := responsePayload(t, resp)["model"].(map[string]any)["model_id"].(string)
	_, _ = s.HandleModelGroupsUpsert(ctx, modelsRequest(schema.ReqMethodModelGroupsUpsert, map[string]any{
		"group": map[string]any{"display_name": "G", "enabled": true,
			"routes": []any{map[string]any{"model_id": modelID, "enabled": true}}},
	}))
	resp, _ = s.HandleModelsDelete(ctx, modelsRequest(schema.ReqMethodModelsDelete, map[string]any{"model_id": modelID}))
	payload := responsePayload(t, resp)
	if payload["code"] != "MODEL_SELECTION_REFERENCED" {
		t.Errorf("错误码不符: %v", payload["code"])
	}
	if payload["error"] != "resource is still referenced" {
		t.Errorf("错误文案不符: %v", payload["error"])
	}
}

// 删除不存在资源 → MODEL_SELECTION_NOT_FOUND。
func TestHandleModelsDelete_不存在(t *testing.T) {
	s := newModelsTestEnv(t)
	resp, _ := s.HandleModelsDelete(t.Context(), modelsRequest(schema.ReqMethodModelsDelete, map[string]any{"model_id": "mdl_none"}))
	payload := responsePayload(t, resp)
	if payload["code"] != "MODEL_SELECTION_NOT_FOUND" || payload["error"] != "resource does not exist" {
		t.Errorf("未命中删除文案不符: %v", payload)
	}
}

// vendors.list：payload = {"vendors": {...三 plan 分组...}}。
func TestHandleVendorsList_分组(t *testing.T) {
	s := newModelsTestEnv(t)
	resp, _ := s.HandleVendorsList(t.Context(), modelsRequest(schema.ReqMethodVendorsList, nil))
	payload := responsePayload(t, resp)
	vendors, _ := payload["vendors"].(map[string]any)
	for _, plan := range []string{"token_plan", "coding_plan", "custom_api"} {
		if _, ok := vendors[plan]; !ok {
			t.Errorf("vendors 缺少 %s 分组", plan)
		}
	}
}

// session.selection.set：写入与读取回；错误码透传。
// 对齐 Python: model_resource_handlers.py:101-123。
func TestHandleSessionSelectionSet_写入与校验(t *testing.T) {
	s := newModelsTestEnv(t)
	ctx := t.Context()
	// 造模型供 resolve 校验
	resp, _ := s.HandleModelsUpsert(ctx, modelsRequest(schema.ReqMethodModelsUpsert, map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai"},
	}))
	modelID := responsePayload(t, resp)["model"].(map[string]any)["model_id"].(string)
	// 设置选择
	resp, err := s.HandleSessionSelectionSet(ctx, modelsRequest(schema.ReqMethodSessionSelectionSet, map[string]any{
		"session_id":      "sess_sel",
		"model_selection": map[string]any{"type": "model", "id": modelID},
	}))
	if err != nil {
		t.Fatal(err)
	}
	payload := responsePayload(t, resp)
	sel, _ := payload["model_selection"].(map[string]any)
	if sel == nil || sel["type"] != "model" || sel["id"] != modelID {
		t.Fatalf("响应应含 model_selection: %v", payload)
	}
	// store 读回验证
	got, err := modelcommonGetSessionModelSelection("sess_sel")
	if err != nil || got == nil || got.ID != modelID {
		t.Errorf("store 应可读回: %v %v", got, err)
	}
	// 未知模型 → MODEL_SELECTION_NOT_FOUND（resolve 校验透传）
	resp, _ = s.HandleSessionSelectionSet(ctx, modelsRequest(schema.ReqMethodSessionSelectionSet, map[string]any{
		"session_id":      "sess_sel2",
		"model_selection": map[string]any{"type": "model", "id": "mdl_ghost"},
	}))
	payload = responsePayload(t, resp)
	if payload["code"] != "MODEL_SELECTION_NOT_FOUND" {
		t.Errorf("resolve 校验应透传错误码: %v", payload["code"])
	}
	// model 引用带 route_id → 校验错误
	resp, _ = s.HandleSessionSelectionSet(ctx, modelsRequest(schema.ReqMethodSessionSelectionSet, map[string]any{
		"session_id":      "sess_sel3",
		"model_selection": map[string]any{"type": "model", "id": modelID, "route_id": "rte_1"},
	}))
	payload = responsePayload(t, resp)
	if !strings.Contains(payload["error"].(string), "route_id is only valid") {
		t.Errorf("DTO 校验文案不符: %v", payload["error"])
	}
}

// vendors.fetch_models：无端点回退预设（vllm-omni）。
func TestHandleVendorsFetchModels_无端点回退(t *testing.T) {
	s := newModelsTestEnv(t)
	resp, _ := s.HandleVendorsFetchModels(t.Context(), modelsRequest(schema.ReqMethodVendorsFetchModels, map[string]any{
		"vendor_key": "vllm-omni", "plan": "custom_api",
	}))
	payload := responsePayload(t, resp)
	if payload["source"] != "preset" || payload["reason"] != "no remote models endpoint" {
		t.Errorf("无端点回退形状不符: %v", payload)
	}
}

// vendors.fetch_models：未知厂商/套餐。
func TestHandleVendorsFetchModels_未知厂商(t *testing.T) {
	s := newModelsTestEnv(t)
	resp, _ := s.HandleVendorsFetchModels(t.Context(), modelsRequest(schema.ReqMethodVendorsFetchModels, map[string]any{
		"vendor_key": "nope", "plan": "custom_api",
	}))
	payload := responsePayload(t, resp)
	if payload["code"] != "BAD_REQUEST" || !strings.Contains(payload["error"].(string), "unknown vendor/plan") {
		t.Errorf("未知厂商文案不符: %v", payload)
	}
}

// modelcommonGetSessionModelSelection 测试辅助：从 swarm/common store 读回。
func modelcommonGetSessionModelSelection(sessionID string) (*modelcommon.ModelSelection, error) {
	return modelcommon.GetSessionModelSelection(sessionID)
}

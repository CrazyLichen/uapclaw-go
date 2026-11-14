//go:build integration

// 模型目录 web RPC 的契约套件（23.4d，27.4 回填闭环）。
// 对应 Python 代码：gateway/channel_manager/web/app_web_handlers.py:5629-5678 注册面 +
// _model_resource_rpc/:5870-5949 与 vendors.*/session.selection.set 分支。
package web_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelsRPCSuite 10 个模型目录 RPC 的 payload 契约快照。
// 跨子系统：swarm/schema（分发）+ swarm/server（RPC handlers）+ swarm/common
// （目录/resolver/store/resource ops）+ swarm/server/session（metadata）。
type ModelsRPCSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModelsRPCSuite 套件入口。
func TestModelsRPCSuite(t *testing.T) {
	suite.Run(t, new(ModelsRPCSuite))
}

// SetupTest 每用例启动独立进程内 AgentServer 并隔离数据目录。
func (s *ModelsRPCSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

// TearDownTest 刷盘并还原环境。
func (s *ModelsRPCSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue(5.0)
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// sendModels 投递模型目录 RPC，返回响应。
func (s *ModelsRPCSuite) sendModels(method string, params map[string]any) map[string]any {
	rid := s.env.Send(method, params, "sess-models", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	raw, err := json.Marshal(resp.Payload)
	s.Require().NoError(err)
	var payload map[string]any
	s.Require().NoError(json.Unmarshal(raw, &payload))
	return payload
}

// TestUpsertGetDelete_模型全链路 upsert → get → delete 的 payload 契约。
// Python: app_web_handlers.py:5870-5949 _model_resource_rpc。
func (s *ModelsRPCSuite) TestUpsertGetDelete_模型全链路() {
	// upsert：{"model": detail, "applied_without_restart"}
	payload := s.sendModels("models.upsert", map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai", "api_key": "sk-1", "is_default": true},
	})
	s.Require().True(payload["model"] != nil, "upsert 应返回 model 详情")
	model := payload["model"].(map[string]any)
	s.Require().NotNil(model["model_id"])
	s.Contains(payload, "applied_without_restart")
	modelID := model["model_id"].(string)

	// get：{"model": ...}，api_key 不外发、write_only_fields 记录
	payload = s.sendModels("models.get", map[string]any{"model_id": modelID})
	detail := payload["model"].(map[string]any)
	s.Require().NotNil(detail)
	mcc := detail["model_client_config"].(map[string]any)
	s.NotContains(mcc, "api_key", "api_key 不外发")
	writeOnly := detail["write_only_fields"].([]any)
	s.Require().Len(writeOnly, 1)
	s.Equal("model_client_config.api_key", writeOnly[0])

	// delete：{"model_id": id, "deleted": true}
	payload = s.sendModels("models.delete", map[string]any{"model_id": modelID})
	s.Equal(true, payload["deleted"])
	s.Equal(modelID, payload["model_id"])

	// 删除后 get → MODEL_SELECTION_NOT_FOUND
	payload = s.sendModels("models.get", map[string]any{"model_id": modelID})
	s.Equal("MODEL_SELECTION_NOT_FOUND", payload["code"])
}

// TestGroupsListUpsertDelete_模型组契约 组三 RPC 的 payload 契约。
func (s *ModelsRPCSuite) TestGroupsListUpsertDelete_模型组契约() {
	// 先 upsert 模型供路由引用
	upModel := s.sendModels("models.upsert", map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai"},
	})
	modelID := upModel["model"].(map[string]any)["model_id"].(string)

	// groups.upsert：{"group": saved, "applied_without_restart"}
	payload := s.sendModels("model_groups.upsert", map[string]any{
		"group": map[string]any{"display_name": "组A", "enabled": true,
			"routes": []any{map[string]any{"model_id": modelID, "enabled": true}}},
	})
	s.Require().NotNil(payload["group"])
	groupID := payload["group"].(map[string]any)["model_group_id"].(string)

	// groups.list：{"groups": [...]} 白名单键
	payload = s.sendModels("model_groups.list", map[string]any{})
	groups := payload["groups"].([]any)
	s.Require().Len(groups, 1)
	card := groups[0].(map[string]any)
	for _, key := range []string{"model_group_id", "display_name", "enabled", "is_default", "routes", "request_config"} {
		s.Contains(card, key)
	}

	// groups.delete：{"model_group_id": id, "deleted": true}
	payload = s.sendModels("model_groups.delete", map[string]any{"model_group_id": groupID})
	s.Equal(true, payload["deleted"])
}

// TestReferences_引用扫描 删除前引用扫描契约。
// Python: app_web_handlers.py:5890-5892 {"references": [r.__dict__ ...]}。
func (s *ModelsRPCSuite) TestReferences_引用扫描() {
	upModel := s.sendModels("models.upsert", map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai"},
	})
	modelID := upModel["model"].(map[string]any)["model_id"].(string)
	s.sendModels("model_groups.upsert", map[string]any{
		"group": map[string]any{"display_name": "G", "enabled": true,
			"routes": []any{map[string]any{"model_id": modelID, "enabled": true}}},
	})
	payload := s.sendModels("models.references", map[string]any{"type": "model", "id": modelID})
	refs := payload["references"].([]any)
	s.Require().Len(refs, 1)
	ref := refs[0].(map[string]any)
	s.Equal("model_group", ref["scope"])
	s.NotEmpty(ref["scope_id"])
	// 被引用 → 删除被拒
	payload = s.sendModels("models.delete", map[string]any{"model_id": modelID})
	s.Equal("MODEL_SELECTION_REFERENCED", payload["code"])
}

// TestVendorsList_分组契约 vendors.list 契约。
// Python: app_web_handlers.py:2570-2580 {"vendors": to_frontend_payload()}。
func (s *ModelsRPCSuite) TestVendorsList_分组契约() {
	payload := s.sendModels("vendors.list", map[string]any{})
	vendors := payload["vendors"].(map[string]any)
	s.Contains(vendors, "reasoning")
	for _, plan := range []string{"token_plan", "coding_plan", "custom_api"} {
		cards := vendors[plan].([]any)
		s.Require().NotEmpty(cards)
		card := cards[0].(map[string]any)
		s.Contains(card, "vendor_key")
		s.Contains(card, "supports_anthropic")
		s.Contains(card, "reasoning_capabilities")
	}
}

// TestVendorsFetchModels_无端点回退 vllm-omni 无远端端点直接回退预设。
func (s *ModelsRPCSuite) TestVendorsFetchModels_无端点回退() {
	payload := s.sendModels("vendors.fetch_models", map[string]any{
		"vendor_key": "vllm-omni", "plan": "custom_api",
	})
	s.Equal("preset", payload["source"])
	s.Equal("no remote models endpoint", payload["reason"])
}

// TestSessionSelectionSet_设置与回落链 session.selection.set 契约 + Restore 回落。
// 跨子系统闭环（回填 B-4/B-5）：写入后 get 读回；未知引用经 resolve 校验透传错误码。
func (s *ModelsRPCSuite) TestSessionSelectionSet_设置与回落链() {
	upModel := s.sendModels("models.upsert", map[string]any{
		"model": map[string]any{"model_name": "gpt-a", "client_provider": "openai"},
	})
	modelID := upModel["model"].(map[string]any)["model_id"].(string)
	// 设置：{"model_selection": {...}}
	payload := s.sendModels("session.selection.set", map[string]any{
		"session_id":      "sess-sel-it",
		"model_selection": map[string]any{"type": "model", "id": modelID},
	})
	sel := payload["model_selection"].(map[string]any)
	s.Equal("model", sel["type"])
	s.Equal(modelID, sel["id"])
	// 未知引用 → MODEL_SELECTION_NOT_FOUND（resolve 准入校验）
	payload = s.sendModels("session.selection.set", map[string]any{
		"session_id":      "sess-sel-it2",
		"model_selection": map[string]any{"type": "model", "id": "mdl_ghost"},
	})
	s.Equal("MODEL_SELECTION_NOT_FOUND", payload["code"])
	// 删除已选模型后 metadata 引用失效——find_references 可见 session 引用
	payload = s.sendModels("models.references", map[string]any{"type": "model", "id": modelID})
	refs := payload["references"].([]any)
	found := false
	for _, r := range refs {
		if r.(map[string]any)["scope"] == "session" {
			found = true
		}
	}
	s.True(found, "session 引用应被扫描到")
}

// TestModelsGet_空目录未知模型 空目录下 get 未知模型错误码。
func (s *ModelsRPCSuite) TestModelsGet_空目录未知模型() {
	payload := s.sendModels("models.get", map[string]any{"model_id": "mdl_none"})
	s.Equal("MODEL_SELECTION_NOT_FOUND", payload["code"])
	// 防误删语义：session metadata 损坏时 references → RUNTIME_UNAVAILABLE（构造在独立 env）
	_ = os.MkdirAll(filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "sessions", "bad-sess"), 0o755)
	_ = os.WriteFile(
		filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "sessions", "bad-sess", "metadata.json"),
		[]byte("{invalid"), 0o644,
	)
	payload = s.sendModels("models.references", map[string]any{"type": "model", "id": "mdl_any"})
	s.Equal("MODEL_RUNTIME_UNAVAILABLE", payload["code"])
}


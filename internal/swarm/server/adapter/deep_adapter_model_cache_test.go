package adapter

import (
	"context"
	"encoding/json"
	"testing"

	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	schema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// agentRequestWithParams 构造带 params 的请求（resolveModelForRequest 只读 params）。
func agentRequestWithParams(t *testing.T, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("序列化 params 失败: %v", err)
	}
	return &schema.AgentRequest{Params: raw}
}

// modelEntry 构造一条 models.defaults 条目。
func modelEntry(name string, tokens int, isDefault bool, alias string) map[string]any {
	mcc := map[string]any{
		"model_name":      name,
		"api_base":        "https://api.example.com/v1",
		"api_key":         "k-" + name,
		"client_provider": "OpenAI",
		"verify_ssl":      false,
	}
	if tokens > 0 {
		mcc["max_input_tokens"] = tokens
	}
	entry := map[string]any{"model_client_config": mcc}
	if isDefault {
		entry["is_default"] = true
	}
	if alias != "" {
		entry["alias"] = alias
	}
	return entry
}

// TestCreateModel_空配置返回nil 覆盖无有效模型条目的错误分支。
func TestCreateModel_空配置返回nil(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.createModel(map[string]any{}); got != nil {
		t.Fatalf("空配置应返回 nil，实际 %v", got)
	}
	if len(d.ModelCache()) != 0 {
		t.Errorf("空配置后缓存应为空，实际 %d", len(d.ModelCache()))
	}
}

// TestCreateModel_默认条目与别名 覆盖 is_default/alias/#index 三条写入分支。
func TestCreateModel_默认条目与别名(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				modelEntry("m1", 8000, false, ""),
				modelEntry("m1", 32000, true, "m1-alias"),
			},
		},
	}

	model := d.createModel(configBase)
	if model == nil {
		t.Fatal("createModel 应返回默认模型")
	}
	cache := d.ModelCache()
	if cache["m1"] == nil {
		t.Error("is_default 条目应以纯 model_name 作为 key")
	}
	if cache["m1-alias"] == nil {
		t.Error("alias 应写入缓存")
	}
	if cache["m1#0"] == nil || cache["m1#1"] == nil {
		t.Error("#index key 应写入缓存")
	}
	if got := d.defaultModelNameSnapshot(); got != "m1" {
		t.Errorf("defaultModelName = %q, want m1", got)
	}
}

// TestCreateModel_单条目推断默认 覆盖 InferIsDefault 单条目自动置默认的路径。
func TestCreateModel_单条目推断默认(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{modelEntry("m2", 16000, false, "")},
		},
	}
	if model := d.createModel(configBase); model == nil {
		t.Fatal("单条目应被推断为默认模型")
	}
	if got := d.defaultModelNameSnapshot(); got != "m2" {
		t.Errorf("defaultModelName = %q, want m2（InferIsDefault 单条目置默认）", got)
	}
}

// TestCreateModel_legacy回退 覆盖 models.default 旧格式。
func TestCreateModel_legacy回退(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{
					"model_name":      "legacy",
					"api_base":        "https://api.example.com/v1",
					"api_key":         "k",
					"client_provider": "OpenAI",
					"verify_ssl":      false,
				},
			},
		},
	}
	if model := d.createModel(configBase); model == nil {
		t.Fatal("legacy 格式应能构建模型")
	}
	if d.ModelCache()["legacy"] == nil {
		t.Error("legacy 模型应写入缓存")
	}
}

// TestResolveModelForRequest_各分支 覆盖请求模型解析的三条路径。
func TestResolveModelForRequest_各分支(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				modelEntry("m1", 32000, true, ""),
				modelEntry("m2", 8000, false, ""),
			},
		},
	}
	model := d.createModel(configBase)
	if model == nil {
		t.Fatal("模型构建失败")
	}

	// 1. 未指定 model_name → 默认模型
	if got := d.resolveModelForRequest(agentRequestWithParams(t, nil)); got != model {
		t.Error("未指定模型时应返回默认模型")
	}

	// 2. 精确命中 #index key
	params := map[string]any{"model_name": "m2#0"}
	if got := d.resolveModelForRequest(agentRequestWithParams(t, params)); got == nil {
		t.Error("按 #index 应命中缓存模型")
	}

	// 3. 纯 model_name 回退（nameToKeys 命中）
	params = map[string]any{"model_name": "m2"}
	if got := d.resolveModelForRequest(agentRequestWithParams(t, params)); got == nil {
		t.Error("按纯 model_name 应回退命中缓存模型")
	}

	// 4. 完全未知 → 默认模型
	params = map[string]any{"model_name": "unknown"}
	if got := d.resolveModelForRequest(agentRequestWithParams(t, params)); got != model {
		t.Errorf("未知模型应回退默认模型")
	}
}

// TestAbortOnGatewayDisconnect_遍历活跃会话 覆盖活跃会话快照遍历。
func TestAbortOnGatewayDisconnect_遍历活跃会话(t *testing.T) {
	d := NewDeepAdapter()
	d.streamEventRail = commrails.NewJiuClawStreamEventRail()
	d.markSessionActive("s1")
	d.markSessionActive("s2")

	d.AbortOnGatewayDisconnect(context.Background())

	// 只验证不 panic 且会话仍在计数（abort 不改变计数）
	if got := d.otherActiveSessions(""); got != 2 {
		t.Errorf("活跃会话数 = %d, want 2", got)
	}
	d.unmarkSessionActive("s1")
	d.unmarkSessionActive("s2")
}

// TestUnmarkSessionActive_归零清理rail 覆盖归零时的 rail 清理分支。
func TestUnmarkSessionActive_归零清理rail(t *testing.T) {
	d := NewDeepAdapter()
	d.streamEventRail = commrails.NewJiuClawStreamEventRail()

	d.markSessionActive("s1")
	d.unmarkSessionActive("s1")

	if d.isSessionActive("s1") {
		t.Error("归零后会话不应活跃")
	}
}

// TestCreateModel_多模型默认按配置顺序 覆盖默认模型选择不依赖 map 随机序。
// 背景：读取侧 config.InferIsDefault 按 model_name 分组推断，不同名的条目标会
// 全部变成 is_default=true，因此「默认模型」实际由 models.defaults 的出现顺序
// 决定（Python `_create_model` 遍历 `_model_name_to_keys` 的插入序）。
// Go 早期用 range map 选择默认名，多模型并存时默认模型会随进程漂移，
// 本用例重复构建多次以覆盖随机序分支。
func TestCreateModel_多模型默认按配置顺序(t *testing.T) {
	for i := 0; i < 20; i++ {
		d := NewDeepAdapter()
		configBase := map[string]any{
			"models": map[string]any{
				"defaults": []any{
					modelEntry("m1", 8000, true, ""),
					modelEntry("m2", 8000, true, ""),
				},
			},
		}

		model := d.createModel(configBase)
		if model == nil {
			t.Fatalf("第 %d 次：createModel 应返回默认模型", i)
		}
		if got := model.ModelConfig.ModelName; got != "m1" {
			t.Fatalf("第 %d 次：默认模型应为列表首个条目 m1，实际 %s", i, got)
		}
	}
}

// TestCreateModel_默认条目无效时回退首个index 覆盖 defaultOrder 为空时的 #index 回退分支。
// 构造同名两条目：默认位（组内首个）条目凭据非法被跳过，plain key 缺失 →
// defaultOrder 为空，createModel 必须回退到首个可用的 #index key。
func TestCreateModel_默认条目无效时回退首个index(t *testing.T) {
	d := NewDeepAdapter()

	invalid := modelEntry("m-same", 8000, true, "")
	invalid["model_client_config"].(map[string]any)["api_key"] = ""
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				invalid,
				modelEntry("m-same", 8000, false, ""),
			},
		},
	}

	model := d.createModel(configBase)
	if model == nil {
		t.Fatal("应回退到可用的 #index 条目，而不是返回 nil")
	}
	if got := model.ModelConfig.ModelName; got != "m-same" {
		t.Errorf("回退模型应为 m-same，实际 %s", got)
	}
	if got := d.ModelCache()["m-same#1"]; got == nil {
		t.Error("可用条目应按 m-same#1 写入缓存")
	}
}

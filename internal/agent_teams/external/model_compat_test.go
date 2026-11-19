package external

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestIsAnthropicProvider 对齐 model_compat.py:18。
func TestIsAnthropicProvider(t *testing.T) {
	if !IsAnthropicProvider("Anthropic-CLI") {
		t.Fatal("含 anthropic（大小写不敏感）应真")
	}
	if IsAnthropicProvider("openai") {
		t.Fatal("openai 应假")
	}
}

// TestProviderFilterForCLI 对齐 model_compat.py:22 三分支。
func TestProviderFilterForCLI(t *testing.T) {
	f := ProviderFilterForCLI("claude")
	if f == nil || !f("anthropic-x") || f("openai-x") {
		t.Fatal("claude 只收 anthropic 兼容 provider")
	}
	f = ProviderFilterForCLI("codex")
	if f == nil || f("anthropic-x") || !f("openai-x") {
		t.Fatal("codex 排除 anthropic 兼容 provider")
	}
	if ProviderFilterForCLI("openclaw") != nil {
		t.Fatal("adapter 后端无过滤器（返回 nil）")
	}
	if ProviderFilterForCLI("unknown-xyz") != nil {
		t.Fatal("未知后端返回 nil")
	}
}

// TestCompatiblePoolModelNames 对齐 model_compat.py:45：过滤 + 去重 + 排序。
func TestCompatiblePoolModelNames(t *testing.T) {
	pool := []models.ModelPoolEntry{
		{ModelName: "m2", APIProvider: "openai"},
		{ModelName: "m1", APIProvider: "anthropic"},
		{ModelName: "m1", APIProvider: "anthropic"}, // 重复名去重
		{ModelName: "", APIProvider: "anthropic"},   // 空名跳过
	}
	got := CompatiblePoolModelNames("claude", pool)
	want := []string{"m1"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v want %v", got, want)
	}
	// codex 过滤掉 anthropic → 只剩 m2
	got = CompatiblePoolModelNames("codex", pool)
	if len(got) != 1 || got[0] != "m2" {
		t.Fatalf("codex: %v", got)
	}
	sort.Strings(got)
}

// TestModelAPIProtocol 对齐 model_compat.py:60。
func TestModelAPIProtocol(t *testing.T) {
	if ModelAPIProtocol("anthropic") != "Anthropic" {
		t.Fatal("anthropic → Anthropic")
	}
	if ModelAPIProtocol("openai") != "OpenAI" {
		t.Fatal("其他 → OpenAI")
	}
}

// TestExternalCLIProviderName_codexOpenAI改写 对齐 model_compat.py:65：
// codex + client_provider=openai + api_base → jiuwen；其他原样。
func TestExternalCLIProviderName_codexOpenAI改写(t *testing.T) {
	cc := &llmschema.ModelClientConfig{ClientProvider: "openai", APIBase: "http://gw"}
	if got := ExternalCLIProviderName(cc, "codex"); got != "jiuwen" {
		t.Fatalf("got %q", got)
	}
	cc2 := &llmschema.ModelClientConfig{ClientProvider: "openai", APIBase: ""}
	if got := ExternalCLIProviderName(cc2, "codex"); got != "openai" {
		t.Fatalf("无 api_base 透传: %q", got)
	}
	cc3 := &llmschema.ModelClientConfig{ClientProvider: "openai", APIBase: "http://gw"}
	if got := ExternalCLIProviderName(cc3, "claude"); got != "openai" {
		t.Fatalf("非 codex 透传: %q", got)
	}
}

// TestTeamModelConfigToExternal 对齐 model_compat.py:90 的映射与 intelli_router
// 跳过。
func TestTeamModelConfigToExternal(t *testing.T) {
	tm := &hschema.TeamModelConfig{
		ModelClientConfig: llmschema.ModelClientConfig{
			ClientProvider: "openai",
			APIBase:        "http://gw",
			APIKey:         "sk-x",
		},
		ModelRequestConfig: &llmschema.ModelRequestConfig{ModelName: "gpt-x"},
	}
	ext := TeamModelConfigToExternal(tm, "codex")
	if ext == nil || ext.Provider != "jiuwen" || ext.Model != "gpt-x" || ext.APIBase != "http://gw" || ext.APIKey != "sk-x" {
		t.Fatalf("映射: %+v", ext)
	}
	intelli := &hschema.TeamModelConfig{
		ModelClientConfig: llmschema.ModelClientConfig{ClientProvider: "intelli_router"},
	}
	if TeamModelConfigToExternal(intelli, "codex") != nil {
		t.Fatal("intelli_router 应跳过")
	}
	if TeamModelConfigToExternal(nil, "codex") != nil {
		t.Fatal("nil 成员模型应跳过")
	}
}

// TestSessionCleanup_claude分支挂起与非claude跳过 对齐 session_cleanup.py 的
// 分支形状（claude 删除面 ⤵️15.7；非 claude 直接返回）。
func TestSessionCleanup_claude分支挂起与非claude跳过(t *testing.T) {
	// 非 claude：直接返回，无任何副作用
	cleanupExternalCLIBackendSession("openclaw", "s1", "beta", &schema.ExternalCliAgentSpec{})
	cleanupExternalCLIBackendSession("claude", "s1", "beta", &schema.ExternalCliAgentSpec{})
}

// TestExternalCLIConfigsForTeam_空命名空间 对齐 _external_cli_configs_for_team
// 的 bucket/spec 缺失分支。
func TestExternalCLIConfigsForTeam_空命名空间(t *testing.T) {
	// metadata.ReadTeamNamespace(nil 会话) 下的路径不便构造完整 SessionFacade；
	// 这里验证 decodeTeamAgentSpec 的形状分支。
	spec, err := decodeTeamAgentSpec(map[string]any{
		"team_name":           "pay",
		"external_cli_agents": []any{map[string]any{"cli_agent": "openclaw"}},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(spec.ExternalCliAgents) != 1 || spec.ExternalCliAgents[0].CLIAgent != "openclaw" {
		t.Fatalf("spec: %+v", spec)
	}
	if _, err := decodeTeamAgentSpec(42); err == nil || !strings.Contains(err.Error(), "unsupported team spec payload") {
		t.Fatalf("got %v", err)
	}
}

// TestRestoreSessionForCleanup_空id报错 对齐 Python create_agent_team_session
// 的必要参数守卫形状。
func TestRestoreSessionForCleanup_空id报错(t *testing.T) {
	if _, err := restoreSessionForCleanup(context.Background(), ""); err == nil {
		t.Fatal("空 session id 应报错")
	}
}

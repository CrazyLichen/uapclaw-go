package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptrBool(b bool) *bool          { return &b }
func ptrFloat(f float64) *float64   { return &f }
func ptrInt(i int) *int             { return &i }
func withAdapters(names ...string) func() {
	old := AdapterNamesFunc
	AdapterNamesFunc = func() []string { return names }
	return func() { AdapterNamesFunc = old }
}

// TestExternalCliAgentSpecValidate_codenBin只限codex 对齐 team.py:504-505。
func TestExternalCliAgentSpecValidate_codenBin只限codex(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "openclaw", CodexBin: "/usr/bin/codex"}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_bin is only valid when cli_agent='codex'") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_codex不支持command 对齐 team.py:477-481。
func TestExternalCliAgentSpecValidate_codex不支持command(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex", Command: []string{"codex", "--x"}}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "Codex SDK config does not support command") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_cliPath不配adapter 对齐 team.py:502-503。
func TestExternalCliAgentSpecValidate_cliPath不配adapter(t *testing.T) {
	defer withAdapters("openclaw", "hermes", "gemini")()
	spec := ExternalCliAgentSpec{CLIAgent: "openclaw", CLIPath: "/bin/x"}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "cli_path is not valid for adapter-backed CLI agents") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_mcp审批模式归属 对齐 team.py:506-509。
func TestExternalCliAgentSpecValidate_mcp审批模式归属(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "claude", MCPDefaultToolsApprovalMode: "auto"}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "mcp_default_tools_approval_mode is only valid when cli_agent='codex'") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_bypass归属_显式false合法 对齐 team.py:510-513：
// 非 codex 显式 false 合法；显式 true 报错。
func TestExternalCliAgentSpecValidate_bypass归属_显式false合法(t *testing.T) {
	if err := (&ExternalCliAgentSpec{CLIAgent: "claude", CodexBypassApprovalsAndSandbox: ptrBool(false)}).Validate(); err != nil {
		t.Fatalf("显式 false 应合法: %v", err)
	}
	spec := ExternalCliAgentSpec{CLIAgent: "claude", CodexBypassApprovalsAndSandbox: ptrBool(true)}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_bypass_approvals_and_sandbox is only valid when cli_agent='codex'") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_codexBypass默认注入 对齐 team.py:473-476：
// codex 且未显式设置 → 默认 true。
func TestExternalCliAgentValidate_codexBypass默认注入(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.CodexBypassApprovalsAndSandbox == nil || *spec.CodexBypassApprovalsAndSandbox != true {
		t.Fatal("codex 成员未显式设置时 bypass 默认注入 true")
	}
}

// TestExternalCliAgentSpecValidate_codexIdle归属 对齐 team.py:514-517。
func TestExternalCliAgentSpecValidate_codexIdle归属(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "claude", CodexTurnIdleTimeoutS: ptrFloat(10)}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_turn_idle_timeout_s is only valid when cli_agent='codex'") {
		t.Fatalf("got %v", err)
	}
	spec2 := ExternalCliAgentSpec{CLIAgent: "claude", CodexTurnIdleRetries: ptrInt(1)}
	err = spec2.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_turn_idle_retries is only valid when cli_agent='codex'") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_claudeIdle归属 对齐 team.py:518-525。
func TestExternalCliAgentSpecValidate_claudeIdle归属(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex", ClaudeTurnIdleTimeoutS: ptrFloat(10)}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude_turn_idle_timeout_s is only valid when cli_agent='claude'") {
		t.Fatalf("got %v", err)
	}
	spec2 := ExternalCliAgentSpec{CLIAgent: "codex", ClaudeMaxBufferSize: ptrInt(1024)}
	err = spec2.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude_max_buffer_size is only valid when cli_agent='claude'") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_adapter禁modelConfig与builtin 对齐 team.py:526-531。
func TestExternalCliAgentSpecValidate_adapter禁modelConfig与builtin(t *testing.T) {
	defer withAdapters("openclaw")()
	spec := ExternalCliAgentSpec{CLIAgent: "openclaw", ExternalModelConfig: &ExternalCliModelConfig{Model: "m"}}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "model_config is not valid for adapter-backed CLI agents") {
		t.Fatalf("got %v", err)
	}
	spec2 := ExternalCliAgentSpec{CLIAgent: "openclaw", BuiltinModels: []ExternalCliBuiltinModel{{Name: "x"}}}
	err = spec2.Validate()
	if err == nil || !strings.Contains(err.Error(), "builtin_models is not valid for adapter-backed CLI agents") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_builtinModels名称唯一 对齐 team.py:533-535。
func TestExternalCliAgentSpecValidate_builtinModels名称唯一(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex", BuiltinModels: []ExternalCliBuiltinModel{
		{Name: "sonnet"}, {Name: "sonnet"},
	}}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "builtin_models names must be unique") {
		t.Fatalf("got %v", err)
	}
}

// TestFindBuiltinModel 对齐 team.py:537-539。
func TestFindBuiltinModel(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex", BuiltinModels: []ExternalCliBuiltinModel{
		{Name: "sonnet", Efforts: []string{"low"}, DefaultEffort: "low"},
	}}
	if spec.FindBuiltinModel("sonnet") == nil {
		t.Fatal("应找到已声明模型")
	}
	if spec.FindBuiltinModel("nope") != nil {
		t.Fatal("未声明模型应返回 nil")
	}
}

// TestBuiltinModelDefaultEffort校验 对齐 team.py:334-341。
func TestBuiltinModelDefaultEffort校验(t *testing.T) {
	ok := ExternalCliBuiltinModel{Name: "m", Efforts: []string{"low", "high"}, DefaultEffort: "low"}
	if err := ok.ValidateBuiltinModelDefaultEffort(); err != nil {
		t.Fatalf("合法 default_effort 不应报错: %v", err)
	}
	bad := ExternalCliBuiltinModel{Name: "m", Efforts: []string{"low"}, DefaultEffort: "high"}
	err := bad.ValidateBuiltinModelDefaultEffort()
	if err == nil || !strings.Contains(err.Error(), "default_effort must be one of its efforts") {
		t.Fatalf("got %v", err)
	}
}

// TestSshTransportValidate_至少一种认证 对齐 ssh_transport.py:41-50。
func TestSshTransportValidate_至少一种认证(t *testing.T) {
	err := (&SshTransportConfig{Host: "h"}).Validate()
	if err == nil || !strings.Contains(err.Error(), "requires at least one auth method") {
		t.Fatalf("got %v", err)
	}
	if err := (&SshTransportConfig{Host: "h", Agent: true}).Validate(); err != nil {
		t.Fatalf("agent=true 应合法: %v", err)
	}
	if err := (&SshTransportConfig{Host: "h", KeyFile: "/k"}).Validate(); err != nil {
		t.Fatalf("key_file 应合法: %v", err)
	}
}

// TestExternalCliSpecModelConfigJSON别名 对齐 team.py:468 alias="model_config"：
// 公开 wire 键是 model_config。
func TestExternalCliSpecModelConfigJSON别名(t *testing.T) {
	raw := `{"cli_agent":"codex","model_config":{"model":"m","api_base":"http://x"}}`
	spec, err := unmarshalExternalCliSpec(raw)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.ExternalModelConfig == nil || spec.ExternalModelConfig.Model != "m" {
		t.Fatalf("model_config 键应映射到 ExternalModelConfig: %+v", spec.ExternalModelConfig)
	}
}

// unmarshalExternalCliSpec 反序列化助手（wire 契约测试用）。
func unmarshalExternalCliSpec(raw string) (*ExternalCliAgentSpec, error) {
	var spec ExternalCliAgentSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptrBool(b bool) *bool        { return &b }
func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int) *int           { return &i }
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
	if err := ok.ValidateDefaultEffort(); err != nil {
		t.Fatalf("合法 default_effort 不应报错: %v", err)
	}
	bad := ExternalCliBuiltinModel{Name: "m", Efforts: []string{"low"}, DefaultEffort: "high"}
	err := bad.ValidateDefaultEffort()
	if err == nil || !strings.Contains(err.Error(), "default_effort must be one of its efforts") {
		t.Fatalf("got %v", err)
	}
}

// TestBuiltinModelName必填 对齐 team.py:322 name min_length=1。
func TestBuiltinModelName必填(t *testing.T) {
	bad := ExternalCliBuiltinModel{Name: "", Efforts: []string{"low"}}
	err := bad.ValidateDefaultEffort()
	if err == nil || !strings.Contains(err.Error(), "name must be at least 1 character") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_数值域 逐条对齐 team.py 数值域约束：
// codex_turn_idle_timeout_s gt=0（:417）/ codex_turn_idle_retries ge=0（:425）/
// claude_turn_idle_timeout_s gt=0（:433）/ claude_max_buffer_size ge=1（:442）。
func TestExternalCliAgentSpecValidate_数值域(t *testing.T) {
	cases := []struct {
		spec ExternalCliAgentSpec
		msg  string
	}{
		{ExternalCliAgentSpec{CLIAgent: "codex", CodexTurnIdleTimeoutS: ptrFloat(0)}, "codex_turn_idle_timeout_s must be greater than 0"},
		{ExternalCliAgentSpec{CLIAgent: "codex", CodexTurnIdleTimeoutS: ptrFloat(-1)}, "codex_turn_idle_timeout_s must be greater than 0"},
		{ExternalCliAgentSpec{CLIAgent: "codex", CodexTurnIdleRetries: ptrInt(-1)}, "codex_turn_idle_retries must be greater than or equal to 0"},
		{ExternalCliAgentSpec{CLIAgent: "claude", ClaudeTurnIdleTimeoutS: ptrFloat(0)}, "claude_turn_idle_timeout_s must be greater than 0"},
		{ExternalCliAgentSpec{CLIAgent: "claude", ClaudeMaxBufferSize: ptrInt(0)}, "claude_max_buffer_size must be greater than or equal to 1"},
	}
	for i, c := range cases {
		err := c.spec.Validate()
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Fatalf("case %d: got %v, want %q", i, err, c.msg)
		}
	}
	// 边界合法值：gt=0 取正、ge=0 取 0、ge=1 取 1
	valid := []ExternalCliAgentSpec{
		{CLIAgent: "codex", CodexTurnIdleTimeoutS: ptrFloat(0.1), CodexTurnIdleRetries: ptrInt(0)},
		{CLIAgent: "claude", ClaudeTurnIdleTimeoutS: ptrFloat(1), ClaudeMaxBufferSize: ptrInt(1)},
	}
	for i, v := range valid {
		if err := v.Validate(); err != nil {
			t.Fatalf("valid case %d: %v", i, err)
		}
	}
}

// TestExternalCliAgentSpecValidate_defaultEffort并入Validate 对齐 team.py:334
// model_validator 的构造期语义：spec 级 Validate 应触发内置模型 default_effort 校验。
func TestExternalCliAgentSpecValidate_defaultEffort并入Validate(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex", BuiltinModels: []ExternalCliBuiltinModel{
		{Name: "sonnet", Efforts: []string{"low"}, DefaultEffort: "high"},
	}}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "default_effort must be one of its efforts") {
		t.Fatalf("got %v", err)
	}
}

// TestExternalCliAgentSpecValidate_mcpServerCommand默认 对齐 team.py:458
// default_factory：装载路径零值时施加 ["uapclawmcpserver"]（D-15.16-b6 入口改名）。
func TestExternalCliAgentSpecValidate_mcpServerCommand默认(t *testing.T) {
	spec := ExternalCliAgentSpec{CLIAgent: "codex"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(spec.MCPServerCommand) != 1 || spec.MCPServerCommand[0] != "uapclawmcpserver" {
		t.Fatalf("默认 mcp_server_command 未施加: %v", spec.MCPServerCommand)
	}
	// 显式配置不被覆盖
	explicit := ExternalCliAgentSpec{CLIAgent: "codex", MCPServerCommand: []string{"custom-mcp"}}
	if err := explicit.Validate(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(explicit.MCPServerCommand) != 1 || explicit.MCPServerCommand[0] != "custom-mcp" {
		t.Fatalf("显式 mcp_server_command 被覆盖: %v", explicit.MCPServerCommand)
	}
}

// TestSshTransportValidate_默认值 对齐 ssh_transport.py:29-34 字段默认：
// port=22 / connect_timeout_s=15.0 / use_exec=True（零值时施加）。
func TestSshTransportValidate_默认值(t *testing.T) {
	cfg := &SshTransportConfig{Host: "h", Agent: true}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != 22 {
		t.Fatalf("port 默认应为 22: %d", cfg.Port)
	}
	if cfg.ConnectTimeoutS != 15.0 {
		t.Fatalf("connect_timeout_s 默认应为 15.0: %v", cfg.ConnectTimeoutS)
	}
	if !cfg.UseExec {
		t.Fatal("use_exec 默认应为 true")
	}
	// 显式值不被覆盖
	explicit := &SshTransportConfig{Host: "h", Agent: true, Port: 2222, ConnectTimeoutS: 30}
	if err := explicit.Validate(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if explicit.Port != 2222 || explicit.ConnectTimeoutS != 30 {
		t.Fatalf("显式值被覆盖: %+v", explicit)
	}
}

// TestTeamAgentSpecValidate_级联externalCli 对齐 pydantic 嵌套构造期校验：
// TeamAgentSpec.Validate 应触发 external_cli_agents 条目的校验与默认值。
func TestTeamAgentSpecValidate_级联externalCli(t *testing.T) {
	spec := TeamAgentSpec{
		ExternalCliAgents: []ExternalCliAgentSpec{
			{CLIAgent: "codex", CodexTurnIdleTimeoutS: ptrFloat(0)},
		},
	}
	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_turn_idle_timeout_s must be greater than 0") {
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

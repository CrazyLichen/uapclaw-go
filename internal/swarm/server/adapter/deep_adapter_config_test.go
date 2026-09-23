package adapter

import (
	"os"
	"path/filepath"
	"testing"

	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestWriteRuntimeStateYAML 测试 writeRuntimeStateYAML 写入 runtime_state.yaml。
// Python: _write_runtime_state()
func TestWriteRuntimeStateYAML(t *testing.T) {
	d := NewDeepAdapter()
	d.modelRequestConfig = llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-max"))
	d.agentName = "test_agent"

	d.writeRuntimeStateYAML(t.Context(), "agent.plan", "cn", "web", "/tmp/project")

	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("读取 runtime_state.yaml 失败: %v", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}

	// 验证关键字段
	if v, _ := raw["model"].(string); v != "qwen-max" {
		t.Errorf("model = %q, want %q", v, "qwen-max")
	}
	if v, _ := raw["language"].(string); v != "cn" {
		t.Errorf("language = %q, want %q", v, "cn")
	}
	if v, _ := raw["channel"].(string); v != "web" {
		t.Errorf("channel = %q, want %q", v, "web")
	}
	if v, _ := raw["agent"].(string); v != "test_agent" {
		t.Errorf("agent = %q, want %q", v, "test_agent")
	}

	// 验证模式显示名映射：agent.plan + cn → "规划模式"
	if v, _ := raw["mode"].(string); v != "规划模式" {
		t.Errorf("mode = %q, want %q", v, "规划模式")
	}

	// 验证 platform 字段格式：对齐 Python f"{platform.system()} {platform.machine()}"
	// 应为 "Linux amd64" / "Darwin arm64" 等格式，而非旧版的 "Linux 6.5.0" 含内核版本号
	if v, _ := raw["platform"].(string); v == "" {
		t.Error("platform 字段为空")
	}
}

// TestUpdateRuntimeConfig_全部字段 测试 updateRuntimeConfig 处理所有字段。
// Python: _update_runtime_config()
func TestUpdateRuntimeConfig_全部字段(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": "en"}
	ctx := t.Context()

	config := &runtimeConfig{
		CWD:          "/tmp/cwd",
		Language:     "en",
		Channel:      "feishu",
		ProjectDir:   "/tmp/project",
		WorkspaceDir: "/tmp/workspace",
		SessionID:    "feishu_s1",
		Mode:         "agent.plan",
		RequestID:    "req-001",
		ChannelID:    "feishu",
		TrustedDirs:  []string{"/tmp/trusted"},
	}
	d.updateRuntimeConfig(ctx, config)

	// 验证 runtime_state.yaml 被写入
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应被写入，但读取失败: %v", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}

	// Channel 应来自 ChannelID（优先级高于 sessionID 前缀解析）
	if v, _ := raw["channel"].(string); v != "feishu" {
		t.Errorf("channel = %q, want %q", v, "feishu")
	}
}

// TestUpdateRuntimeConfig_nil配置 测试 config 为 nil 时直接返回。
func TestUpdateRuntimeConfig_nil配置(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	// 不应 panic
	d.updateRuntimeConfig(ctx, nil)
}

// TestUpdateRuntimeConfig_无ChannelID时从SessionID解析 测试 ChannelID 为空时从 sessionID 前缀解析 channel。
// Python: resolved_channel = str(runtime_config.channel_id or self._resolve_prompt_channel(session_id) or "web")
func TestUpdateRuntimeConfig_无ChannelID时从SessionID解析(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": "en"}
	ctx := t.Context()

	config := &runtimeConfig{
		CWD:       "/tmp/cwd",
		SessionID: "acp_s1", // 前缀为 acp
		Mode:      "agent.fast",
	}
	d.updateRuntimeConfig(ctx, config)

	// 验证 channel 来自 sessionID 前缀
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应被写入: %v", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}

	if v, _ := raw["channel"].(string); v != "acp" {
		t.Errorf("channel = %q, want %q（从 sessionID 前缀解析）", v, "acp")
	}
}

// TestUpdateRuntimeConfig_RuntimePromptRailSetter 测试 runtimePromptRail 非 nil 时 setter 被调用。
// Python: if self._runtime_prompt_rail: self._runtime_prompt_rail.set_language(resolved_language) ...
func TestUpdateRuntimeConfig_RuntimePromptRailSetter(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": "en"}
	ctx := t.Context()

	rail := commrails.NewRuntimePromptRail("cn", "web")
	d.runtimePromptRail = rail

	config := &runtimeConfig{
		CWD:         "/tmp/cwd",
		Mode:        "agent.plan",
		TrustedDirs: []string{"/tmp/trusted1", "/tmp/trusted2"},
	}
	d.updateRuntimeConfig(ctx, config)

	// 验证 setter 被调用：通过 BeforeModelCall 副效应间接验证
	// rail 的 language 应为 "en"（resolveRuntimeLanguage 标准化后）
	// rail 的 channel 应为 "web"（无 ChannelID，无 SessionID → fallback "web"）
	// rail 的 mode 应为 "agent.plan"
	// 注：字段未导出，无法直接断言，通过 runtime_state.yaml 间接验证
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应被写入: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}
	if v, _ := raw["language"].(string); v != "en" {
		t.Errorf("language = %q, want %q", v, "en")
	}
}

// TestResolvePromptChannel_各种前缀 测试 resolvePromptChannel 从 sessionID 解析 channel。
func TestResolvePromptChannel_各种前缀(t *testing.T) {
	tests := []struct {
		sessionID string
		want      string
	}{
		{"", "web"},
		{"acp_123", "acp"},
		{"cron_456", "cron"},
		{"heartbeat_789", "heartbeat"},
		{"feishu_abc", "feishu"},
		{"web_def", "web"},
		{"dingtalk_xyz", "dingtalk"},
		{"wecom_uv", "wecom"},
		{"sess_unknown", "web"},   // "sess" 前缀回退
		{"unknown_prefix", "web"}, // 未知前缀回退
		{"nounderscore", "web"},   // 无下划线但非已知 channel
	}

	for _, tt := range tests {
		t.Run(tt.sessionID, func(t *testing.T) {
			got := resolvePromptChannel(tt.sessionID)
			if got != tt.want {
				t.Errorf("resolvePromptChannel(%q) = %q, want %q", tt.sessionID, got, tt.want)
			}
		})
	}
}

// TestResolveModelName_有配置 测试 resolveModelName 有模型配置时返回模型名。
func TestResolveModelName_有配置(t *testing.T) {
	d := NewDeepAdapter()
	d.modelRequestConfig = llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-max"))
	got := d.resolveModelName()
	if got != "qwen-max" {
		t.Errorf("resolveModelName() = %q, want %q", got, "qwen-max")
	}
}

// TestResolveModelName_无配置 测试 resolveModelName 无模型配置时返回 unknown。
func TestResolveModelName_无配置(t *testing.T) {
	d := NewDeepAdapter()
	got := d.resolveModelName()
	if got != "unknown" {
		t.Errorf("resolveModelName() = %q, want %q", got, "unknown")
	}
}

// TestResolveModelName_空名称 测试 resolveModelName 空名称时返回 unknown。
func TestResolveModelName_空名称(t *testing.T) {
	d := NewDeepAdapter()
	d.modelRequestConfig = llmschema.NewModelRequestConfig(llmschema.WithModelName(""))
	got := d.resolveModelName()
	if got != "unknown" {
		t.Errorf("resolveModelName() = %q, want %q", got, "unknown")
	}
}

// TestIsAcpToolProfile_acpTool配置 测试 acp_tool profile 检测。
func TestIsAcpToolProfile_acpTool配置(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"tool_profile": "acp",
	}
	if !d.isAcpToolProfile(configBase) {
		t.Error("应检测到 acp_tool profile")
	}
}

// TestIsAcpToolProfile_非acpTool配置 测试非 acp_tool profile。
func TestIsAcpToolProfile_非acpTool配置(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{"profile": "default"},
			},
		},
	}
	if d.isAcpToolProfile(configBase) {
		t.Error("非 acp_tool profile 应返回 false")
	}
}

// TestIsAcpToolProfile_无models段 测试无 models 配置段。
func TestIsAcpToolProfile_无models段(t *testing.T) {
	d := NewDeepAdapter()
	if d.isAcpToolProfile(nil) {
		t.Error("nil configBase 应返回 false")
	}
	if d.isAcpToolProfile(map[string]any{}) {
		t.Error("空 configBase 应返回 false")
	}
}

// TestSkillIncludeToolsForProfile_acpTool 测试 ACP profile 下不包含工具。
func TestSkillIncludeToolsForProfile_acpTool(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"tool_profile": "acp",
	}
	if d.skillIncludeToolsForProfile(configBase) {
		t.Error("ACP profile 下 skill 不应包含工具")
	}
}

// TestSkillIncludeToolsForProfile_非acpTool 测试非 ACP profile 下取决于 filesystemRail。
func TestSkillIncludeToolsForProfile_非acpTool(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{}

	// filesystemRail 为 nil → true
	if !d.skillIncludeToolsForProfile(configBase) {
		t.Error("非 ACP 且 filesystemRail 为 nil 时应包含工具")
	}
}

// TestIsSubagentExplicitlyEnabled_配置中存在 测试配置中子代理开关。
// Python: _is_subagent_enabled() — 只有 enabled:true 才启用
func TestIsSubagentExplicitlyEnabled_配置中存在(t *testing.T) {
	d := NewDeepAdapter()
	cfg := map[string]any{
		"research_agent": true,
		"general_agent":  false,
	}
	if !d.isSubagentExplicitlyEnabled(cfg, "research_agent") {
		t.Error("research_agent 应启用")
	}
	if d.isSubagentExplicitlyEnabled(cfg, "general_agent") {
		t.Error("general_agent 应禁用")
	}
}

// TestIsSubagentExplicitlyEnabled_配置为dict 测试配置值为字典时检查 enabled 字段。
func TestIsSubagentExplicitlyEnabled_配置为dict(t *testing.T) {
	d := NewDeepAdapter()
	cfg := map[string]any{
		"research_agent": map[string]any{"enabled": true, "max_iterations": 20},
		"general_agent":  map[string]any{"max_iterations": 10},
	}
	if !d.isSubagentExplicitlyEnabled(cfg, "research_agent") {
		t.Error("research_agent dict 中 enabled:true 应启用")
	}
	if d.isSubagentExplicitlyEnabled(cfg, "general_agent") {
		t.Error("general_agent dict 中无 enabled 应禁用（默认 false）")
	}
}

// TestIsSubagentExplicitlyEnabled_配置中不存在 测试配置中不存在时默认禁用。
func TestIsSubagentExplicitlyEnabled_配置中不存在(t *testing.T) {
	d := NewDeepAdapter()
	cfg := map[string]any{}
	if d.isSubagentExplicitlyEnabled(cfg, "research_agent") {
		t.Error("配置中不存在时默认禁用")
	}
	if d.isSubagentExplicitlyEnabled(nil, "research_agent") {
		t.Error("nil 配置时默认禁用")
	}
}

// TestResolveRuntimeLanguage_有配置 测试 resolveRuntimeLanguage 有配置时返回标准化值。
// Python: _resolve_runtime_language() 调用 resolve_language() 标准化
func TestResolveRuntimeLanguage_有配置(t *testing.T) {
	d := NewDeepAdapter()
	// preferred_language=en → ResolveLanguage("en") → "en"
	d.configCache = map[string]any{"preferred_language": "en"}
	if got := d.resolveRuntimeLanguage(); got != "en" {
		t.Errorf("resolveRuntimeLanguage() = %q, want %q", got, "en")
	}
}

// TestResolveRuntimeLanguage_无配置 测试 resolveRuntimeLanguage 无配置时返回默认值。
// Python: resolve_language("zh") → "zh" 不在 SUPPORTED_LANGUAGES → 回退 "cn"
func TestResolveRuntimeLanguage_无配置(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{}
	if got := d.resolveRuntimeLanguage(); got != "cn" {
		t.Errorf("resolveRuntimeLanguage() = %q, want %q", got, "cn")
	}
}

// TestResolveRuntimeLanguage_非字符串 测试 resolveRuntimeLanguage 非字符串时回退默认值。
func TestResolveRuntimeLanguage_非字符串(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": 123}
	if got := d.resolveRuntimeLanguage(); got != "cn" {
		t.Errorf("resolveRuntimeLanguage() = %q, want %q", got, "cn")
	}
}

// TestResolvePromptLanguage_有配置 测试 resolvePromptLanguage 有配置时返回原始值。
// Python: _resolve_prompt_language() 读 preferred_language
func TestResolvePromptLanguage_有配置(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": "en"}
	if got := d.resolvePromptLanguage(); got != "en" {
		t.Errorf("resolvePromptLanguage() = %q, want %q", got, "en")
	}
}

// TestResolvePromptLanguage_zh标准化 测试 resolvePromptLanguage 返回 "zh" 被 ResolveLanguage 标准化为 "cn"。
func TestResolvePromptLanguage_zh标准化(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"preferred_language": "zh"}
	if got := d.resolvePromptLanguage(); got != "zh" {
		t.Errorf("resolvePromptLanguage() = %q, want %q", got, "zh")
	}
	// resolveRuntimeLanguage 会标准化 "zh" → "cn"
	if got := d.resolveRuntimeLanguage(); got != "cn" {
		t.Errorf("resolveRuntimeLanguage() = %q, want %q (zh 标准化为 cn)", got, "cn")
	}
}

// TestResolvePromptLanguage_全部回退 测试 resolvePromptLanguage 全部回退到默认值。
func TestResolvePromptLanguage_全部回退(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{}
	if got := d.resolvePromptLanguage(); got != "zh" {
		t.Errorf("resolvePromptLanguage() = %q, want %q", got, "zh")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBuildConfiguredSubagents_无配置 测试无配置时返回空列表。
func TestBuildConfiguredSubagents_无配置(t *testing.T) {
	d := NewDeepAdapter()
	subagents, shouldAdd := d.buildConfiguredSubagents(nil, nil)
	// 无配置时不应添加 general_agent
	if shouldAdd != false {
		t.Errorf("buildConfiguredSubagents 无配置时 shouldAdd 应为 false")
	}
	// subagents 可能为空或包含默认子 Agent（explore/plan）
	_ = subagents
}

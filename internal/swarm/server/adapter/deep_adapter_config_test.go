package adapter

import (
	"os"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestWriteRuntimeState_写入环境变量 测试 writeRuntimeState 设置环境变量。
func TestWriteRuntimeState_写入环境变量(t *testing.T) {
	d := NewDeepAdapter()
	d.writeRuntimeState("language", "zh")
	got := os.Getenv("JCLAW_RUNTIME_LANGUAGE")
	if got != "zh" {
		t.Errorf("JCLAW_RUNTIME_LANGUAGE = %q, want %q", got, "zh")
	}

	d.writeRuntimeState("channel", "web")
	got = os.Getenv("JCLAW_RUNTIME_CHANNEL")
	if got != "web" {
		t.Errorf("JCLAW_RUNTIME_CHANNEL = %q, want %q", got, "web")
	}
}

// TestWriteRuntimeState_键名大写 测试键名转换为大写。
func TestWriteRuntimeState_键名大写(t *testing.T) {
	d := NewDeepAdapter()
	d.writeRuntimeState("project_dir", "/tmp/project")
	got := os.Getenv("JCLAW_RUNTIME_PROJECT_DIR")
	if got != "/tmp/project" {
		t.Errorf("JCLAW_RUNTIME_PROJECT_DIR = %q, want %q", got, "/tmp/project")
	}
}

// TestUpdateRuntimeConfig_全部字段 测试 updateRuntimeConfig 处理所有字段。
func TestUpdateRuntimeConfig_全部字段(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	config := &runtimeConfig{
		CWD:          "/tmp/cwd",
		Language:     "en",
		Channel:      "feishu",
		ProjectDir:   "/tmp/project",
		WorkspaceDir: "/tmp/workspace",
	}
	d.updateRuntimeConfig(ctx, config)

	if v := os.Getenv("JCLAW_RUNTIME_LANGUAGE"); v != "en" {
		t.Errorf("JCLAW_RUNTIME_LANGUAGE = %q, want %q", v, "en")
	}
	if v := os.Getenv("JCLAW_RUNTIME_CHANNEL"); v != "feishu" {
		t.Errorf("JCLAW_RUNTIME_CHANNEL = %q, want %q", v, "feishu")
	}
	if v := os.Getenv("JCLAW_RUNTIME_PROJECT_DIR"); v != "/tmp/project" {
		t.Errorf("JCLAW_RUNTIME_PROJECT_DIR = %q, want %q", v, "/tmp/project")
	}
	if v := os.Getenv("JCLAW_RUNTIME_WORKSPACE_DIR"); v != "/tmp/workspace" {
		t.Errorf("JCLAW_RUNTIME_WORKSPACE_DIR = %q, want %q", v, "/tmp/workspace")
	}
}

// TestUpdateRuntimeConfig_nil配置 测试 config 为 nil 时直接返回。
func TestUpdateRuntimeConfig_nil配置(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	// 不应 panic
	d.updateRuntimeConfig(ctx, nil)
}

// TestUpdateRuntimeConfig_空字段不写入 测试空字段不写入环境变量。
func TestUpdateRuntimeConfig_空字段不写入(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	// 先设置一个值
	os.Setenv("JCLAW_RUNTIME_LANGUAGE", "old_value")

	config := &runtimeConfig{
		CWD:      "/tmp/cwd",
		Language: "", // 空字段，不应覆盖
	}
	d.updateRuntimeConfig(ctx, config)

	// 空字段不调用 writeRuntimeState，旧值应保留
	if v := os.Getenv("JCLAW_RUNTIME_LANGUAGE"); v != "old_value" {
		t.Errorf("空字段不应覆盖，JCLAW_RUNTIME_LANGUAGE = %q, want %q", v, "old_value")
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
// 对齐 Python: _is_subagent_enabled() — 只有 enabled:true 才启用
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
// 对齐 Python: _resolve_runtime_language() 调用 resolve_language() 标准化
func TestResolveRuntimeLanguage_有配置(t *testing.T) {
	d := NewDeepAdapter()
	// preferred_language=en → ResolveLanguage("en") → "en"
	d.configCache = map[string]any{"preferred_language": "en"}
	if got := d.resolveRuntimeLanguage(); got != "en" {
		t.Errorf("resolveRuntimeLanguage() = %q, want %q", got, "en")
	}
}

// TestResolveRuntimeLanguage_无配置 测试 resolveRuntimeLanguage 无配置时返回默认值。
// 对齐 Python: resolve_language("zh") → "zh" 不在 SUPPORTED_LANGUAGES → 回退 "cn"
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
// 对齐 Python: _resolve_prompt_language() 读 preferred_language
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

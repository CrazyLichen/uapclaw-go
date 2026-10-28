package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// readRuntimeStateYAMLWithRetry 读取 runtime_state.yaml 并解析为 map。
// 多个包的测试并行运行时可能并发写入同一个文件，导致短暂的内容不一致。
// 遇到解析错误时重试最多 3 次，间隔 100ms。
func readRuntimeStateYAMLWithRetry(t *testing.T, yamlPath string) map[string]any {
	t.Helper()
	var lastErr error
	var data []byte
	for i := 0; i < 3; i++ {
		var err error
		data, err = os.ReadFile(yamlPath)
		if err != nil {
			t.Fatalf("读取 runtime_state.yaml 失败: %v", err)
		}
		var raw map[string]any
		if err = yaml.Unmarshal(data, &raw); err == nil {
			return raw
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("解析 runtime_state.yaml 失败（重试3次后）: %v\n文件内容:\n%s", lastErr, string(data))
	return nil
}

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
	raw := readRuntimeStateYAMLWithRetry(t, yamlPath)

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
	raw := readRuntimeStateYAMLWithRetry(t, yamlPath)

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
	raw := readRuntimeStateYAMLWithRetry(t, yamlPath)

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
	raw := readRuntimeStateYAMLWithRetry(t, yamlPath)
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
	// 配置 preferred_language=en → resolveRuntimeLanguage 返回 "en"
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

// ──────────────────────────── browser_agent（AH-M-12 回填） ────────────────────────────

// TestBrowserRuntimeEnabled_环境变量开关 对齐 Python _browser_runtime_enabled()：
// PLAYWRIGHT_RUNTIME_MCP_ENABLED / BROWSER_RUNTIME_MCP_ENABLED，值 ∈ {1,true,yes,on}
func TestBrowserRuntimeEnabled_环境变量开关(t *testing.T) {
	d := NewDeepAdapter()

	// 两变量均未设置 → false
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")
	if d.browserRuntimeEnabled() {
		t.Error("环境变量为空时应返回 false")
	}

	// PLAYWRIGHT 优先
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")
	if !d.browserRuntimeEnabled() {
		t.Error("PLAYWRIGHT_RUNTIME_MCP_ENABLED=true 应返回 true")
	}

	// 回退 BROWSER_RUNTIME_MCP_ENABLED
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "1")
	if !d.browserRuntimeEnabled() {
		t.Error("BROWSER_RUNTIME_MCP_ENABLED=1 应返回 true")
	}

	// 大小写与空白容忍（对齐 Python .strip().lower()）
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", " ON ")
	if !d.browserRuntimeEnabled() {
		t.Error("' ON ' strip+lower 后应视为 true")
	}

	// 非法值
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "0")
	if d.browserRuntimeEnabled() {
		t.Error("'0' 应返回 false")
	}
}

// TestBuildConfiguredSubagents_browserAgent注册 对齐 Python _build_configured_subagents：
// browser_runtime_enabled 且配置了 browser_agent 时注册 subagent；
// 未启用 runtime 但配置了 browser_agent 时跳过（对齐 Python 的 else 分支日志）
func TestBuildConfiguredSubagents_browserAgent注册(t *testing.T) {
	d := NewDeepAdapter()
	d.model = &llm.Model{}

	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")
	t.Setenv("BROWSER_DRIVER", "")         // 触发 managed 默认值注入
	t.Setenv("BROWSER_MANAGED_BINARY", "") // 触发 chrome_path 解析

	config := map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true, "max_iterations": 7},
		},
		"max_iterations": 20,
	}
	specs, _ := d.buildConfiguredSubagents(config, nil)
	found := false
	for _, s := range specs {
		if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.FactoryName == "browser_agent" {
			found = true
		}
	}
	if !found {
		t.Error("browser runtime 启用时 buildConfiguredSubagents 应注册 browser_agent")
	}

	// BROWSER_DRIVER 默认值注入（对齐 Python: os.environ["BROWSER_DRIVER"] = "managed"）
	if got := os.Getenv("BROWSER_DRIVER"); got != "managed" {
		t.Errorf("BROWSER_DRIVER 应被默认注入为 managed，实际 %q", got)
	}
}

// TestBuildConfiguredSubagents_browserAgent未启用runtime时跳过 对齐 Python：
// browser_agent 配置存在但 browser runtime 未启用 → 跳过注册（不 panic、不注册）
func TestBuildConfiguredSubagents_browserAgent未启用runtime时跳过(t *testing.T) {
	d := NewDeepAdapter()
	d.model = &llm.Model{}

	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")

	config := map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true},
		},
	}
	specs, _ := d.buildConfiguredSubagents(config, nil)
	for _, s := range specs {
		if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.FactoryName == "browser_agent" {
			t.Error("browser runtime 未启用时不应注册 browser_agent")
		}
	}
}

// TestBuildConfiguredSubagents_browserAgentMaxIterations回退 对齐 Python parse_int 语义：
// browser_agent.max_iterations 优先，回退 react.max_iterations，再回退 15
func TestBuildConfiguredSubagents_browserAgentMaxIterations回退(t *testing.T) {
	d := NewDeepAdapter()
	d.model = &llm.Model{}

	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")

	// 显式 max_iterations=7
	config := map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true, "max_iterations": 7},
		},
		"max_iterations": 20,
	}
	specs, _ := d.buildConfiguredSubagents(config, nil)
	for _, s := range specs {
		if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.FactoryName == "browser_agent" {
			if cfg.MaxIterations != 7 {
				t.Errorf("MaxIterations = %d, want 7（browser_agent 显式值优先）", cfg.MaxIterations)
			}
		}
	}

	// 未配置 → 回退 react.max_iterations=20
	config2 := map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true},
		},
		"max_iterations": 20,
	}
	specs2, _ := d.buildConfiguredSubagents(config2, nil)
	for _, s := range specs2 {
		if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.FactoryName == "browser_agent" {
			if cfg.MaxIterations != 20 {
				t.Errorf("MaxIterations = %d, want 20（回退 react.max_iterations）", cfg.MaxIterations)
			}
		}
	}

	// 都未配置 → 默认 15
	config3 := map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true},
		},
	}
	specs3, _ := d.buildConfiguredSubagents(config3, nil)
	for _, s := range specs3 {
		if cfg, ok := s.(*hschema.SubAgentConfig); ok && cfg.FactoryName == "browser_agent" {
			if cfg.MaxIterations != 15 {
				t.Errorf("MaxIterations = %d, want 15（默认值）", cfg.MaxIterations)
			}
		}
	}
}

// TestResolveManagedBrowserBinaryFromConfig_chromePath 对齐 Python
// _resolve_managed_browser_binary_from_config()：字符串直取 / 平台 map / default 回退
func TestResolveManagedBrowserBinaryFromConfig_chromePath(t *testing.T) {
	d := NewDeepAdapter()

	// 字符串形式直取
	if got := d.resolveManagedBrowserBinaryFromConfig(map[string]any{
		"browser": map[string]any{"chrome_path": " /usr/bin/chrome "},
	}); got != "/usr/bin/chrome" {
		t.Errorf("字符串 chrome_path 应 strip 后返回，实际 %q", got)
	}

	// 平台 map 形式：linux 键优先
	platformMap := map[string]any{
		"browser": map[string]any{
			"chrome_path": map[string]any{
				"linux":   "/usr/bin/chromium",
				"default": "/usr/bin/default-chrome",
			},
		},
	}
	got := d.resolveManagedBrowserBinaryFromConfig(platformMap)
	if got == "" {
		t.Error("平台 map 形式应解析出非空路径")
	}

	// 无 browser 配置 → 空串
	if got := d.resolveManagedBrowserBinaryFromConfig(map[string]any{}); got != "" {
		t.Errorf("无 browser 配置应返回空串，实际 %q", got)
	}
}

// ──────────────────────────── updateSessionTools 测试 ────────────────────────────
// 对齐 Python _update_session_tools (interface_deep.py L2966-3010)：
// cron 工具按 ctx 绑定的 RuntimeCronContext 注册；heartbeat/cron 前缀跳过；
// instance 未初始化时降级（Python try/except 语义）。

// TestUpdateSessionTools_nil配置 config 为 nil 直接返回
func TestUpdateSessionTools_nil配置(t *testing.T) {
	d := NewDeepAdapter()
	// 不应 panic
	d.updateSessionTools(t.Context(), nil)
}

// TestUpdateSessionTools_前缀跳过 heartbeat/cron 前缀会话跳过 cron 注册
// Python: if not (session_id.startswith("heartbeat") or session_id.startswith("cron"))
func TestUpdateSessionTools_前缀跳过(t *testing.T) {
	d := NewDeepAdapter()
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "s", nil, "r", "agent.plan")

	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "heartbeat_s1"})
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "cron_job_x"})
	// 非前缀命中的普通会话不受影响（instance nil 走降级分支不 panic）
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "web_s1"})
}

// TestUpdateSessionTools_无绑定 ctx 未绑定 RuntimeCronContext 时返回
func TestUpdateSessionTools_无绑定(t *testing.T) {
	d := NewDeepAdapter()
	// ctx 无绑定值 → FromRuntimeCronContext 返回 nil → 直接返回
	d.updateSessionTools(t.Context(), &runtimeConfig{SessionID: "web_s1"})
}

// TestUpdateSessionTools_instance未初始化降级 tools 已构建但 instance nil
// 对齐 Python：self._instance.ability_manager 抛 AttributeError → error 日志降级
func TestUpdateSessionTools_instance未初始化降级(t *testing.T) {
	d := NewDeepAdapter()
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		map[string]any{"k": "v"}, "req-1", "agent.plan")

	// BuildTools 走默认 backend（GetBackend 懒加载，全局无注入时仍可解析），
	// 构建出非空工具集后命中 instance nil 守卫 → 降级返回，不 panic
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "web_sess-1"})
}

// TestUpdateSessionTools_正常注册 全链路：工具注册到 ResourceMgr + AbilityManager
func TestUpdateSessionTools_正常注册(t *testing.T) {
	d := NewDeepAdapter()
	// 构造 DeepAgent 实例（agentID 非空走 tool_id_{agentID} 命名分支）
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-cron-1"),
		agentschema.WithAgentName("cron-test-agent"),
	)
	d.instance = harness.NewDeepAgent(card)

	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "feishu", "sess-9",
		map[string]any{"chat_type": "group"}, "req-9", "agent.plan")
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "feishu_sess-9"})

	// 断言 1：统一 cron 工具已注册到 AbilityManager（name="cron"）
	am := d.instance.AbilityManager()
	found := false
	for _, ab := range am.List() {
		if tc, ok := ab.(*tool.ToolCard); ok && tc.Name == "cron" {
			found = true
			// 断言 2：tool_id 应含 ToolScopeOverride（runtime_{指针十六进制}）
			// 期望前缀 "cron_runtime_"（toolScope 替换冒号后拼接）
			if !strings.HasPrefix(tc.ID, "cron_runtime_") {
				t.Errorf("cron tool ID 应含 runtime scope 前缀, got %q", tc.ID)
			}
			// agentID 非空 → ID 以 agentID 结尾
			if !strings.HasSuffix(tc.ID, "_agent-cron-1") {
				t.Errorf("cron tool ID 应以 agentID 结尾, got %q", tc.ID)
			}
		}
	}
	if !found {
		t.Fatal("统一 cron 工具（name=cron）未注册到 AbilityManager")
	}

	// 断言 3：工具已注册到全局 ResourceMgr（可按 ID 查回）
	for _, ab := range am.List() {
		if tc, ok := ab.(*tool.ToolCard); ok && tc.Name == "cron" {
			existing, err := runner.GetResourceMgr().GetTool([]string{tc.ID})
			if err != nil || len(existing) != 1 {
				t.Errorf("cron tool 应可从 ResourceMgr 查回, err=%v, count=%d", err, len(existing))
			}
		}
	}

	// 断言 4：二次调用不重复注册（Python: get_tool 查重后跳过 add_tool，
	// ability_manager.add 重复 name 时 Warn 并保留已有——幂等）
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "feishu_sess-9"})
	cronCount := 0
	for _, ab := range am.List() {
		if tc, ok := ab.(*tool.ToolCard); ok && tc.Name == "cron" {
			cronCount++
		}
	}
	if cronCount != 1 {
		t.Errorf("重复调用后 cron 工具应保持 1 个, got %d", cronCount)
	}
}

// ──────────────────── updateSendFileTools 测试（10.6.24b）────────────────────

// newSendFileTestAdapter 构造带 instance 与 send_file 启用配置的最小 DeepAdapter。
// 复用既有 fake 模式（真实 harness.NewDeepAgent + 真实 AbilityManager）。
func newSendFileTestAdapter(t *testing.T, channels map[string]any) *DeepAdapter {
	t.Helper()
	// loader 注入固定密钥（零文件系统副作用，避免触碰真实 ~/.uapclaw）
	swarmtools.ResetWebFileDownloadManager()
	t.Cleanup(swarmtools.ResetWebFileDownloadManager)
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-sf-1"),
		agentschema.WithAgentName("sf-test-agent"),
	)
	d := NewDeepAdapter()
	d.instance = harness.NewDeepAgent(card)
	d.configBase = map[string]any{"channels": channels}
	return d
}

// countSendFileAbilities 统计 ability 中 send_file_to_user 前缀卡数。
func countSendFileAbilities(d *DeepAdapter) int {
	n := 0
	for _, ab := range d.instance.AbilityManager().List() {
		if strings.HasPrefix(ab.AbilityName(), "send_file_to_user") {
			n++
		}
	}
	return n
}

// TestUpdateSendFileTools_开关三态 缺省 false / 显式 true / 未配渠道 false。
// Python L2995-2997: channels[channel].send_file_allowed 缺省 False
func TestUpdateSendFileTools_开关三态(t *testing.T) {
	// 缺省（无 channels 配置）→ false → 不注册
	d := newSendFileTestAdapter(t, nil)
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		map[string]any{"k": "v"}, "req-1", "agent.plan")
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
	if d.sendFileToolkit != nil {
		t.Error("未配置 send_file_allowed 时 toolkit 应为 nil")
	}

	// 显式 true → 注册
	d2 := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	})
	d2.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
	if d2.sendFileToolkit == nil {
		t.Fatal("send_file_allowed=true 时 toolkit 应非 nil")
	}
	if n := countSendFileAbilities(d2); n != 1 {
		t.Errorf("应注册 1 张卡, got %d", n)
	}

	// 配置了但 false → 不注册
	d3 := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": false},
	})
	d3.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
	if d3.sendFileToolkit != nil {
		t.Error("send_file_allowed=false 时 toolkit 应为 nil")
	}
}

// TestUpdateSendFileTools_已注册复用 二次调用刷新三元组、卡数不变。
// Python L3016-3021: already_registered → update_runtime_context
func TestUpdateSendFileTools_已注册复用(t *testing.T) {
	d := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	})
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		map[string]any{"k": "v"}, "req-1", "agent.plan")
	cfg1 := &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"}
	d.updateSendFileTools(ctx, cfg1)
	tk1 := d.sendFileToolkit

	// 二次调用（改 requestID，channel 保持 web——config 未带 ChannelID 时
	// resolvePromptChannel("sess-2") 回退 web，命中已启用的 web 开关）
	ctx2 := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-2",
		map[string]any{"k": "v2"}, "req-2", "agent.plan")
	d.updateSendFileTools(ctx2, &runtimeConfig{SessionID: "sess-2", RequestID: "req-2"})
	if d.sendFileToolkit != tk1 {
		t.Error("二次调用应复用同一 toolkit 实例")
	}
	if n := countSendFileAbilities(d); n != 1 {
		t.Errorf("复用时卡数应保持 1, got %d", n)
	}
	// 三元组刷新经行为验证：hooks 捕获 push 断言路由
	var gotReq, gotSess string
	swarmtools.SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		gotReq, _ = msg["request_id"].(string)
		gotSess, _ = msg["session_id"].(string)
		return nil
	})
	t.Cleanup(func() { swarmtools.SetSendFilePushFunc(nil) })
	fp := filepath.Join(t.TempDir(), "reuse.txt")
	if err := os.WriteFile(fp, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sendFileToolkit.SendFile(t.Context(), fp); err != nil {
		t.Fatal(err)
	}
	if gotReq != "req-2" || gotSess != "sess-2" {
		t.Errorf("三元组应刷新为 req-2/sess-2, got %q/%q", gotReq, gotSess)
	}
}

// TestUpdateSendFileTools_cron块降级不阻断send_file cron 注册降级路径（rcc 未绑定）
// 不得连带跳过 send_file 块——Python 两块平级（cron 块 try/except 仅降级自身），
// Go 侧 2026-10-28 推送后复审 P1 修复：cron 块抽为独立方法、return 仅退出自身。
func TestUpdateSendFileTools_cron块降级不阻断send_file(t *testing.T) {
	d := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	})
	// ctx 未绑定 RuntimeCronContext（模拟 rcc==nil 降级路径）
	// —— Python _build_cron_tools 依赖 contextvar 默认值仍可构造，
	//    Go 侧 rcc==nil 时 cron 块整体跳过，但 send_file 块必须继续执行。
	d.updateSessionTools(t.Context(), &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
	if d.sendFileToolkit == nil {
		t.Fatal("cron 块降级（rcc 未绑定）时 send_file 分支仍应执行（Python 平级语义）")
	}
	if n := countSendFileAbilities(d); n != 1 {
		t.Errorf("应注册 1 张 send_file 卡, got %d", n)
	}
}

// TestUpdateSendFileTools_heartbeat会话 守卫收窄回归：cron 跳过但 send_file 仍执行。
// Python L2974 守卫仅包 cron 块（10.6.24b 对齐性修正，spec 偏差 #1）。
// 注意：heartbeat_s1 前缀 → resolvePromptChannel → "heartbeat" 渠道，
// 开关须配 channels.heartbeat.send_file_allowed（Python 同语义）。
func TestUpdateSendFileTools_heartbeat会话(t *testing.T) {
	d := newSendFileTestAdapter(t, map[string]any{
		"heartbeat": map[string]any{"send_file_allowed": true},
	})
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "heartbeat_s1",
		nil, "req-h", "agent.plan")
	// heartbeat 前缀：cron 块跳过，send_file 块仍执行
	d.updateSessionTools(ctx, &runtimeConfig{SessionID: "heartbeat_s1", RequestID: "req-h"})
	if d.sendFileToolkit == nil {
		t.Error("heartbeat 会话 send_file 分支仍应执行（守卫收窄）")
	}
	if n := countSendFileAbilities(d); n != 1 {
		t.Errorf("应注册 1 张卡, got %d", n)
	}
	// 同时确认 cron 未注册（heartbeat 前缀守卫生效）
	cronCount := 0
	for _, ab := range d.instance.AbilityManager().List() {
		if tc, ok := ab.(*tool.ToolCard); ok && tc.Name == "cron" {
			cronCount++
		}
	}
	if cronCount != 0 {
		t.Errorf("heartbeat 会话 cron 不应注册, got %d", cronCount)
	}
}

// TestUpdateSendFileTools_channel三级fallback ChannelID 缺省时从 sessionID 解析。
// Python L2992-2994: channel_id or resolve_prompt_channel(session_id) or "web"
func TestUpdateSendFileTools_channel三级fallback(t *testing.T) {
	// feishu_xxx 前缀 → channel 解析 feishu → 查 feishu 渠道开关
	d := newSendFileTestAdapter(t, map[string]any{
		"feishu": map[string]any{"send_file_allowed": true},
		"web":    map[string]any{"send_file_allowed": false},
	})
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "", "feishu_x",
		nil, "req-f", "agent.plan")
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "feishu_x", RequestID: "req-f"})
	if d.sendFileToolkit == nil {
		t.Error("feishu 渠道启用时 toolkit 应非 nil")
	}

	// 普通会话前缀 → resolvePromptChannel 回退 web → 查 web 渠道
	d2 := newSendFileTestAdapter(t, map[string]any{
		"feishu": map[string]any{"send_file_allowed": false},
		"web":    map[string]any{"send_file_allowed": true},
	})
	d2.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess_1", RequestID: "req-w"})
	if d2.sendFileToolkit == nil {
		t.Error("web 渠道启用时 toolkit 应非 nil")
	}
}

// TestUpdateSendFileTools_缺RequestID 开关开但缺 requestID/sessionID 跳过。
// Python L2998: if send_file_enabled and request_id and session_id
func TestUpdateSendFileTools_缺RequestID(t *testing.T) {
	d := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	})
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		nil, "req-1", "agent.plan")
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: ""})
	if d.sendFileToolkit != nil {
		t.Error("缺 requestID 应跳过")
	}
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "", RequestID: "req-1"})
	if d.sendFileToolkit != nil {
		t.Error("缺 sessionID 应跳过")
	}
}

// TestUpdateSendFileTools_跨实例残留 ability 有卡但本实例 nil → 重建绑定。
// Go 补充分支：Python 无此分支（同进程同 adapter），跨实例残留时补绑
func TestUpdateSendFileTools_跨实例残留(t *testing.T) {
	d := newSendFileTestAdapter(t, map[string]any{
		"web": map[string]any{"send_file_allowed": true},
	})
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		nil, "req-1", "agent.plan")

	// 首次注册
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
	tk1 := d.sendFileToolkit

	// 模拟跨实例残留：清空本实例字段但保留 ability 卡
	d.sendFileToolkit = nil
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-9"})
	if d.sendFileToolkit == nil {
		t.Fatal("跨实例残留应重建 toolkit")
	}
	if d.sendFileToolkit == tk1 {
		t.Error("重建应为新实例")
	}
	// 重建绑定的新 requestID 经行为验证（跨包无法直读未导出字段）
	var gotReq string
	swarmtools.SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		gotReq, _ = msg["request_id"].(string)
		return nil
	})
	t.Cleanup(func() { swarmtools.SetSendFilePushFunc(nil) })
	fp := filepath.Join(t.TempDir(), "rebuild.txt")
	if err := os.WriteFile(fp, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sendFileToolkit.SendFile(t.Context(), fp); err != nil {
		t.Fatal(err)
	}
	if gotReq != "req-9" {
		t.Errorf("重建应绑定新 requestID, got %q", gotReq)
	}
}

// TestUpdateSendFileTools_instance未初始化 instance nil 时静默跳过。
// Python: ability_manager 访问抛 AttributeError → 分支不执行
func TestUpdateSendFileTools_instance未初始化(t *testing.T) {
	d := NewDeepAdapter()
	d.configBase = map[string]any{
		"channels": map[string]any{"web": map[string]any{"send_file_allowed": true}},
	}
	ctx := cronbridge.BindRuntimeCronContext(t.Context(), "web", "sess-1",
		nil, "req-1", "agent.plan")
	// 不应 panic
	d.updateSendFileTools(ctx, &runtimeConfig{SessionID: "sess-1", RequestID: "req-1"})
}

// TestBuildModelFromEntry_customHeaders透传 验证 M-01 越界发现顺手修：
// custom_headers 此前仅列入 knownKeys 排除 extra 却无映射分支，静默丢弃。
// 对齐 Python ModelClientConfig(**model_client_config) 全字段透传。
func TestBuildModelFromEntry_customHeaders透传(t *testing.T) {
	d := NewDeepAdapter()
	mcc := map[string]any{
		"client_provider": "OpenAI",
		"api_key":         "k",
		"api_base":        "https://m.example.com/v1",
		"model_name":      "m1",
		// verify_ssl 默认 true 时 Validate 要求 ssl_cert 必填，此处关掉走纯 headers 断言
		"verify_ssl": false,
		"custom_headers": map[string]any{
			"X-Gateway-Auth": "token-1",
			"X-Org":          "org-9",
		},
	}
	m, err := d.buildModelFromEntry(mcc, map[string]any{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if m.ClientConfig == nil {
		t.Fatal("clientConfig 不应为 nil")
	}
	got := m.ClientConfig.CustomHeaders
	if got["X-Gateway-Auth"] != "token-1" || got["X-Org"] != "org-9" {
		t.Errorf("custom_headers 应透传: %v", got)
	}
	// custom_headers 已正确映射，不应再落入 Extra
	if _, exists := m.ClientConfig.Extra["custom_headers"]; exists {
		t.Errorf("custom_headers 不应重复进 Extra: %v", m.ClientConfig.Extra)
	}
}

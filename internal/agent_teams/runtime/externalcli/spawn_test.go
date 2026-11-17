package externalcli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// externalCliRtc 构造最小合法的 TeamRuntimeContext（member/team_spec 齐备）。
func externalCliRtc(cliAgent string) *schema.TeamRuntimeContext {
	return &schema.TeamRuntimeContext{
		Role:       schema.TeamRoleTeammate,
		MemberName: "beta",
		TeamSpec:   &schema.TeamSpec{TeamName: "pay"},
		CLIAgent:   cliAgent,
	}
}

// boolPtr 布尔指针助手。
func boolPtr(v bool) *bool { return &v }

// fPtr / iPtr 数值指针助手（Options 数值字段指针形态）。
func fPtr(v float64) *float64 { return &v }
func iPtr(v int) *int         { return &v }

// writeSpawnScript 在临时目录写可执行 shell 脚本（测试禁真实 /tmp，一律 t.TempDir）。
func writeSpawnScript(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// captureModelProvider 实现 ModelConfigProvider 的探测桩：记录 choice/fallback
// 并返回固定映射片段（memberModelConfig fallback 丢弃语义断言用）。
type captureModelProvider struct {
	gotChoice   *harnessprotocol.ModelChoice
	gotFallback *harnessprotocol.ModelChoice
	called      bool
}

func (p *captureModelProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Capabilities: map[harnessprotocol.HarnessCapability]bool{
			harnessprotocol.HarnessCapabilityEndpointModels: true,
			harnessprotocol.HarnessCapabilityAuthFallback:   true,
		},
	}
}

func (p *captureModelProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return &fakeHarnessProtocol{}, nil
}

func (p *captureModelProvider) ModelConfig(
	choice harnessprotocol.ModelChoice, fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	p.called = true
	copied := choice
	p.gotChoice = &copied
	p.gotFallback = fallback
	return harnessprotocol.JsonObject{
		"model":          map[string]any{"model": "mapped-primary"},
		"fallback_model": map[string]any{"model": "mapped-fallback"},
	}, nil
}

// noEndpointModelProvider card 不声明 ENDPOINT_MODELS 的探测桩（映射报错路用）。
type noEndpointModelProvider struct{}

func (p *noEndpointModelProvider) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{}
}

func (p *noEndpointModelProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return &fakeHarnessProtocol{}, nil
}

func (p *noEndpointModelProvider) ModelConfig(
	choice harnessprotocol.ModelChoice, fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	return harnessprotocol.JsonObject{}, nil
}

// mcpCardProtocol 带可配置 MCP 能力卡的 harness 桩。
type mcpCardProtocol struct {
	fakeHarnessProtocol
	mcpTools bool
}

func (h *mcpCardProtocol) Card() harnessprotocol.HarnessCard {
	return harnessprotocol.HarnessCard{
		Name: "fake-prov-harness",
		Capabilities: map[harnessprotocol.HarnessCapability]bool{
			harnessprotocol.HarnessCapabilityMcpTools: h.mcpTools,
		},
	}
}

// ──────────────────────────── 入口守卫（D-15.13-x） ────────────────────────────

// TestBuildCliRuntime_claude守卫pending 对齐 D-15.13-x：Python spawn.py:308-350
// claude 分支体 ⤵️15.7，入口守卫报 AGENT_TEAM_CONFIG_INVALID 含 pending 15.7/15.8。
func TestBuildCliRuntime_claude守卫pending(t *testing.T) {
	ctx := newSessionCtx(t, "sess-1")
	_, err := BuildCliRuntime(ctx, externalCliRtc("claude"), Options{})
	if err == nil || !strings.Contains(err.Error(), "pending 15.7/15.8") {
		t.Fatalf("got %v", err)
	}
}

// TestBuildCliRuntime_codex守卫pending 对齐 D-15.13-x：Python spawn.py:351-404
// codex 分支体 ⤵️15.8。
func TestBuildCliRuntime_codex守卫pending(t *testing.T) {
	ctx := newSessionCtx(t, "sess-1")
	_, err := BuildCliRuntime(ctx, externalCliRtc("codex"), Options{})
	if err == nil || !strings.Contains(err.Error(), "pending 15.7/15.8") {
		t.Fatalf("got %v", err)
	}
}

// TestBuildCliRuntime_cliAgent判空 对齐 spawn.py:297-301。
func TestBuildCliRuntime_cliAgent判空(t *testing.T) {
	ctx := newSessionCtx(t, "sess-1")
	_, err := BuildCliRuntime(ctx, externalCliRtc(""), Options{})
	if err == nil || !strings.Contains(err.Error(), "without ctx.cli_agent set") {
		t.Fatalf("got %v", err)
	}
}

// ──────────────────────────── provider 路 7 项互斥 ────────────────────────────

// TestBuildCliRuntime_provider分支_互斥参数校验 对齐 spawn.py:405-441：7 项
// provider 不支持的配置逐条收集，报文 "does not support: <sorted names>"。
func TestBuildCliRuntime_provider分支_互斥参数校验(t *testing.T) {
	name := "testcli-guard-prov"
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &captureModelProvider{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)
	ctx := newSessionCtx(t, "sess-1")

	cases := []struct {
		field string
		opts  Options
	}{
		{"command_override", Options{CommandOverride: []string{"x"}}},
		{"codex_bin", Options{CodexBin: "/usr/bin/codex"}},
		{"ssh_transport", Options{SshTransport: &schema.SshTransportConfig{Host: "h"}}},
		{"claude_turn_idle_timeout_s", Options{ClaudeTurnIdleTimeoutS: fPtr(1)}},
		{"claude_max_buffer_size", Options{ClaudeMaxBufferSize: iPtr(1)}},
		{"codex_turn_idle_timeout_s", Options{CodexTurnIdleTimeoutS: fPtr(1)}},
		{"codex_turn_idle_retries", Options{CodexTurnIdleRetries: iPtr(1)}},
	}
	for _, tc := range cases {
		_, err := BuildCliRuntime(ctx, externalCliRtc(name), tc.opts)
		if err == nil || !strings.Contains(err.Error(), "does not support: "+tc.field) {
			t.Fatalf("%s: got %v", tc.field, err)
		}
	}
	// 多项同时配置：sorted 后逗号连接
	_, err := BuildCliRuntime(ctx, externalCliRtc(name), Options{
		CommandOverride: []string{"x"}, CodexBin: "/bin/codex",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support: codex_bin, command_override") {
		t.Fatalf("多项 sorted: got %v", err)
	}
}

// ──────────────────────────── adapter 路五项前置校验 ────────────────────────────

// TestBuildCliRuntime_adapter路_五项前置校验 对齐 spawn.py:442-466 报文逐字。
func TestBuildCliRuntime_adapter路_五项前置校验(t *testing.T) {
	ctx := newSessionCtx(t, "sess-1")
	cases := []struct {
		want string
		opts Options
	}{
		{"ssh transport is only supported for claude SDK external CLI members",
			Options{SshTransport: &schema.SshTransportConfig{Host: "h"}}},
		{"codex_bin is only supported for Codex SDK members",
			Options{CodexBin: "/bin/codex"}},
		{"cli_path is only supported for Claude and Codex SDK members",
			Options{CLIPath: "/bin/claude"}},
		{"claude_turn_idle_timeout_s is only supported for Claude SDK members",
			Options{ClaudeTurnIdleTimeoutS: fPtr(1)}},
		{"claude_max_buffer_size is only supported for Claude SDK members",
			Options{ClaudeMaxBufferSize: iPtr(1)}},
	}
	for _, tc := range cases {
		_, err := BuildCliRuntime(ctx, externalCliRtc("openclaw"), tc.opts)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("got %v want %q", err, tc.want)
		}
	}
}

// ──────────────────────────── env 三级合成 ────────────────────────────

// TestBuildCliRuntime_env三级合成与strip前缀 对齐 spawn.py:469-478：
// base_env（剥 strip 前缀）→ extra_env → descriptor.to_env() 最后覆盖 →
// _with_home_env setdefault。
func TestBuildCliRuntime_env三级合成与strip前缀(t *testing.T) {
	// 测试注入 strip 前缀（内置 adapter 全部为空，Python 同；运行后恢复）
	builtin := builtinAdapters["openclaw"]
	orig := builtin.EnvStripPrefixes
	builtin.EnvStripPrefixes = []string{"UAPCLAW_STRIP_"}
	defer func() { builtin.EnvStripPrefixes = orig }()
	t.Setenv("UAPCLAW_STRIP_FOO", "stripped")
	t.Setenv("UAPCLAW_KEEP", "kept")

	ctx := newSessionCtx(t, "sess-1")
	rtc := externalCliRtc("openclaw")
	opts := Options{
		InjectMCP: boolPtr(false),
		ExtraEnv: map[string]string{
			"UAPCLAW_EXTRA": "extra",
			TeamJoinEnv:     "bogus-override",
		},
	}
	rt, err := BuildCliRuntime(ctx, rtc, opts)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	reinvoke, ok := rt.(*ReinvokeCliRuntime)
	if !ok {
		t.Fatalf("openclaw 是 one-shot，应返回 ReinvokeCliRuntime: %T", rt)
	}
	env := reinvoke.env
	if _, ok := env["UAPCLAW_STRIP_FOO"]; ok {
		t.Fatal("strip 前缀环境变量必须被剥除")
	}
	if env["UAPCLAW_KEEP"] != "kept" {
		t.Fatal("非前缀继承环境必须保留")
	}
	if env["UAPCLAW_EXTRA"] != "extra" {
		t.Fatal("extra_env 必须并入")
	}
	if env[TeamJoinEnv] == "bogus-override" || !strings.HasPrefix(env[TeamJoinEnv], "{") {
		t.Fatal("descriptor.to_env() 最后覆盖——extra_env 不能遮蔽 join")
	}
	if env[OpenjiuwenHomeEnv] == "" {
		t.Fatal("WithHomeEnv 必须注入 OPENJIUWEN_HOME")
	}
}

// TestBuildCliRuntime_oneshot与流式两形态 对齐 spawn.py:502-537：one-shot 返回
// ReinvokeCliRuntime（canonical UUID 会话 id），流式（generic + 假脚本）返回
// ExternalCliRuntime 并真实起进程。
func TestBuildCliRuntime_oneshot与流式两形态(t *testing.T) {
	ctx := newSessionCtx(t, "sess-1")
	// one-shot：openclaw 不抢先启动
	rt, err := BuildCliRuntime(ctx, externalCliRtc("openclaw"), Options{InjectMCP: boolPtr(false)})
	if err != nil {
		t.Fatalf("openclaw: %v", err)
	}
	reinvoke, ok := rt.(*ReinvokeCliRuntime)
	if !ok {
		t.Fatalf("openclaw: %T", rt)
	}
	if len(reinvoke.cliSessionID) != 36 || strings.Count(reinvoke.cliSessionID, "-") != 4 {
		t.Fatalf("canonical UUID 会话 id（str(uuid4) 带连字符）: %q", reinvoke.cliSessionID)
	}
	// 流式：generic + command_override 指向假脚本
	script := writeSpawnScript(t, "fake_stream_cli.sh",
		`while IFS= read -r line; do echo "ack<<END_OF_TURN>>"; done`)
	rt2, err := BuildCliRuntime(ctx, externalCliRtc("generic"), Options{
		InjectMCP:       boolPtr(false),
		CommandOverride: []string{script},
	})
	if err != nil {
		t.Fatalf("generic: %v", err)
	}
	stream, ok := rt2.(*ExternalCliRuntime)
	if !ok {
		t.Fatalf("generic: %T", rt2)
	}
	if stream.process == nil {
		t.Fatal("流式路必须已启动子进程")
	}
	stream.Stop() // 收尾回收子进程
}

// ──────────────────────────── MemberContext ────────────────────────────

// TestMemberContext_agentID回落 对齐 spawn.py:651-668：member_agent_id 缺失时
// 回落 "{team}_{member}"；metadata 带 team_name/role。
func TestMemberContext_agentID回落(t *testing.T) {
	rtc := externalCliRtc("dsh")
	d := &TeamJoinDescriptor{SessionID: "sess-1", TeamName: "pay", MemberName: "beta"}
	// 无 member_agent_id → 回落
	ctxValue, err := MemberContext(rtc, d, "", "sp", "/w")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if ctxValue.AgentID != "pay_beta" {
		t.Fatalf("agent_id 回落: %q", ctxValue.AgentID)
	}
	if ctxValue.AgentName != "beta" || ctxValue.HostSessionID != "sess-1" ||
		ctxValue.SystemPrompt != "sp" || ctxValue.Cwd == nil || *ctxValue.Cwd != "/w" {
		t.Fatalf("字段: %+v", ctxValue)
	}
	if ctxValue.Metadata["team_name"] != "pay" || ctxValue.Metadata["role"] != string(schema.TeamRoleTeammate) {
		t.Fatalf("metadata: %v", ctxValue.Metadata)
	}
	// 显式 member_agent_id 优先
	ctxValue2, err := MemberContext(rtc, d, "pay_beta", "", "")
	if err != nil {
		t.Fatalf("context2: %v", err)
	}
	if ctxValue2.AgentID != "pay_beta" || ctxValue2.Cwd != nil {
		t.Fatalf("显式 agent_id: %+v", ctxValue2)
	}
}

// ──────────────────────────── IsNativeEndpoint / ModelChoice ────────────────────────────

// TestIsNativeEndpoint_三态 对齐 spawn.py:671-678：nil 或 api_base/provider 均空
// 为 native。
func TestIsNativeEndpoint_三态(t *testing.T) {
	if !IsNativeEndpoint(nil) {
		t.Fatal("nil 应为 native")
	}
	if !IsNativeEndpoint(&schema.ExternalCliModelConfig{Model: "m"}) {
		t.Fatal("仅 model 应为 native（内置模型）")
	}
	if IsNativeEndpoint(&schema.ExternalCliModelConfig{APIBase: "http://x"}) {
		t.Fatal("有 api_base 非 native")
	}
	if IsNativeEndpoint(&schema.ExternalCliModelConfig{Provider: "ark"}) {
		t.Fatal("有 provider 非 native")
	}
}

// TestModelChoice_三态 对齐 spawn.py:681-692：nil → nil；native → endpoint nil；
// 端点 → endpoint 三键齐。
func TestModelChoice_三态(t *testing.T) {
	if ModelChoice(nil) != nil {
		t.Fatal("nil config → nil choice")
	}
	native := ModelChoice(&schema.ExternalCliModelConfig{Model: "sonnet", Effort: "high"})
	if native == nil || native.Endpoint != nil || native.Model == nil || *native.Model != "sonnet" ||
		native.Effort == nil || *native.Effort != "high" {
		t.Fatalf("native choice: %+v", native)
	}
	endpoint := ModelChoice(&schema.ExternalCliModelConfig{
		APIBase: "http://x", APIKey: "k", Provider: "ark", Model: "m",
	})
	if endpoint == nil || endpoint.Endpoint == nil {
		t.Fatalf("endpoint choice: %+v", endpoint)
	}
	if endpoint.Endpoint.APIBase == nil || *endpoint.Endpoint.APIBase != "http://x" ||
		endpoint.Endpoint.APIKey == nil || *endpoint.Endpoint.APIKey != "k" ||
		endpoint.Endpoint.Provider == nil || *endpoint.Endpoint.Provider != "ark" {
		t.Fatalf("endpoint 三键: %+v", endpoint.Endpoint)
	}
}

// ──────────────────────────── memberModelConfig ────────────────────────────

// TestMemberModelConfig_fallback丢弃_端点成员 对齐 spawn.py:695-720：已在端点上
// 的成员（pool 分配或静态端点）fallback 丢弃——provider 只见 fallback=nil。
func TestMemberModelConfig_fallback丢弃_端点成员(t *testing.T) {
	name := "testcli-model-prov"
	probe := &captureModelProvider{}
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return probe, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)

	primary := &schema.ExternalCliModelConfig{APIBase: "http://x", Provider: "ark", Model: "m"}
	fallback := &schema.ExternalCliModelConfig{Provider: "ark", Model: "lite"}
	mapped, err := memberModelConfig(name, primary, fallback)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if !probe.called || probe.gotFallback != nil {
		t.Fatalf("端点成员 fallback 必须丢弃: choice=%+v fallback=%+v", probe.gotChoice, probe.gotFallback)
	}
	if probe.gotChoice == nil || probe.gotChoice.Model == nil || *probe.gotChoice.Model != "m" {
		t.Fatalf("primary choice: %+v", probe.gotChoice)
	}
	if mapped["model"] == nil {
		t.Fatalf("端点成员 model 键必须保留: %v", mapped)
	}
}

// TestMemberModelConfig_fallback丢弃_native成员且popModel 对齐 ：717-719：仅
// fallback 被选中时 pop 掉 model 键（成员保持 CLI 默认模型）。
func TestMemberModelConfig_fallback丢弃_native成员且popModel(t *testing.T) {
	name := "testcli-model-prov-native"
	probe := &captureModelProvider{}
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return probe, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)

	fallback := &schema.ExternalCliModelConfig{Provider: "ark", Model: "lite"}
	mapped, err := memberModelConfig(name, nil, fallback)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	// Python: choice or ModelChoice()——native 侧传空 ModelChoice 进 provider
	if probe.gotChoice == nil || probe.gotChoice.Model != nil || probe.gotChoice.Endpoint != nil {
		t.Fatalf("native 无 primary 端点选择（空 ModelChoice 占位）: %+v", probe.gotChoice)
	}
	if probe.gotFallback == nil {
		t.Fatal("native 成员 fallback 必须传入 provider")
	}
	if _, ok := mapped["model"]; ok {
		t.Fatalf("仅回退被选中时必须 pop model 键: %v", mapped)
	}
}

// TestMemberModelConfig_双nil_不触provider 对齐 ：711-712：双 nil 返回空映射。
func TestMemberModelConfig_双nil_不触provider(t *testing.T) {
	name := "testcli-model-prov-none"
	probe := &captureModelProvider{}
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return probe, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)
	mapped, err := memberModelConfig(name, nil, nil)
	if err != nil || len(mapped) != 0 {
		t.Fatalf("双 nil 应返回空映射: %v %v", mapped, err)
	}
	if probe.called {
		t.Fatal("双 nil 不得触达 provider")
	}
}

// TestMemberModelConfig_provider拒绝_配置无效 对齐 ：713-716：provider 拒绝映射
// → AGENT_TEAM_CONFIG_INVALID（cause 链）。
func TestMemberModelConfig_provider拒绝_配置无效(t *testing.T) {
	name := "testcli-model-prov-reject"
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &noEndpointModelProvider{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)
	primary := &schema.ExternalCliModelConfig{APIBase: "http://x", Provider: "ark", Model: "m"}
	_, err := memberModelConfig(name, primary, nil)
	if err == nil || !strings.Contains(err.Error(), "does not take endpoint models") {
		t.Fatalf("got %v", err)
	}
}

// ──────────────────────────── RegisterMCPOutOfBand ────────────────────────────

// TestRegisterMCPOutOfBand_失败带stderr尾 对齐 spawn.py:151-198：注册命令退出
// 非零 → Warn（尾 500 字节）；启动失败（二进制不存在）→ Warn；无注册机制 →
// 响亮告警后返回。
func TestRegisterMCPOutOfBand_失败带stderr尾(t *testing.T) {
	// 非 0 退出 + stderr：hermes 经 command_override 指向假脚本（注册命令以
	// adapter.Command[0] 为 CLI 名）
	script := writeSpawnScript(t, "fake_register_fail.sh", `echo boom-stderr >&2; exit 3`)
	adapter, err := BuildAdapter("hermes", []string{script})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	RegisterMCPOutOfBand(adapter, "openjiuwen-team", []string{"openjiuwen-team-mcp"},
		map[string]string{"K": "V"}, "", "beta")
	// 启动失败：hermes 默认命令二进制不存在
	adapter2, _ := BuildAdapter("hermes", nil)
	RegisterMCPOutOfBand(adapter2, "openjiuwen-team", []string{"openjiuwen-team-mcp"},
		nil, "", "beta")
	// 无注册机制：openclaw（MCPInjectNone）→ 响亮告警并返回
	adapter3, _ := BuildAdapter("openclaw", nil)
	RegisterMCPOutOfBand(adapter3, "openjiuwen-team", []string{"openjiuwen-team-mcp"},
		nil, "", "beta")
}

// ──────────────────────────── BuildProviderMemberRuntime ────────────────────────────

// TestBuildProviderMemberRuntime_MCP能力两分支 对齐 spawn.py:968-984：card 声明
// MCP_TOOLS → BindMCPServers（env 只传 MCP_SERVER_ENV_VARS 交集）；不声明 →
// 不绑定。inject_mcp=false 两者皆不绑定。
func TestBuildProviderMemberRuntime_MCP能力两分支(t *testing.T) {
	name := "testcli-mcp-prov"
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return &mcpProvider{protocol: &mcpCardProtocol{mcpTools: true}}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)
	rtc := externalCliRtc(name)
	d := &TeamJoinDescriptor{SessionID: "sess-1", TeamName: "pay", MemberName: "beta"}

	// 有 MCP 能力：绑定 stdio server，env 交集
	rt, err := BuildProviderMemberRuntime(rtc, d, Options{
		InjectMCP:    boolPtr(true),
		ExtraEnv:     map[string]string{"NOISE": "x"},
		SystemPrompt: "team policy",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rt.ReliabilityAgentKind() != name {
		t.Fatalf("agent_kind=provider 名: %q", rt.ReliabilityAgentKind())
	}
	servers := mcpServersOf(rt)
	if len(servers) != 1 {
		t.Fatalf("声明 MCP_TOOLS 须绑定 1 个 server: %d", len(servers))
	}
	if servers[0].Name != TeamMCPServerName || servers[0].Transport != harnessprotocol.McpTransportStdio {
		t.Fatalf("server 形状: %+v", servers[0])
	}
	if _, ok := servers[0].Env[TeamJoinEnv]; !ok {
		t.Fatalf("MCP env 必须携带 OPENJIUWEN_TEAM_JOIN 交集: %v", servers[0].Env)
	}
	if _, ok := servers[0].Env["NOISE"]; ok {
		t.Fatalf("MCP env 只传 MCP_SERVER_ENV_VARS 交集: %v", servers[0].Env)
	}

	// 无 MCP 能力：不绑定（Warn）
	name2 := "testcli-mcp-prov-nocap"
	if err := harnessproviders.RegisterHarnessProvider(name2, func() (harnessprotocol.HarnessProvider, error) {
		return &mcpProvider{protocol: &mcpCardProtocol{mcpTools: false}}, nil
	}); err != nil {
		t.Fatalf("register2: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name2)
	rt2, err := BuildProviderMemberRuntime(externalCliRtc(name2), d, Options{InjectMCP: boolPtr(true)})
	if err != nil {
		t.Fatalf("build2: %v", err)
	}
	if len(mcpServersOf(rt2)) != 0 {
		t.Fatal("card 无 MCP_TOOLS 不得绑定 server")
	}
}

// mcpServersOf 读绑定的 MCP servers（同包访问非导出字段）。
func mcpServersOf(rt *ExternalHarnessMemberRuntime) []harnessprotocol.McpServerConfig {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.extraMCPServers
}

// mcpProvider 卡片可配的 provider 桩。
type mcpProvider struct {
	protocol harnessprotocol.HarnessProtocol
}

func (p *mcpProvider) Card() harnessprotocol.HarnessCard { return p.protocol.Card() }

func (p *mcpProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return p.protocol, nil
}

// ──────────────────────────── BuildCliRuntime provider 全链 ────────────────────────────

// TestBuildCliRuntime_provider路全链 对齐 spawn.py:405-441 → :897-985：经既有
// provider 注册缝构造 ExternalHarnessMemberRuntime。
func TestBuildCliRuntime_provider路全链(t *testing.T) {
	probe := &captureModelProvider{}
	name := "testcli-full-prov"
	if err := harnessproviders.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return probe, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer harnessproviders.UnregisterHarnessProvider(name)
	ctx := newSessionCtx(t, "sess-1")
	primary := &schema.ExternalCliModelConfig{APIBase: "http://x", Provider: "ark", Model: "m"}
	rt, err := BuildCliRuntime(ctx, externalCliRtc(name), Options{
		ExternalModelConfig: primary,
		InjectMCP:           boolPtr(false),
		SystemPrompt:        "sp",
		MemberAgentID:       "pay_beta",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	harnessRT, ok := rt.(*ExternalHarnessMemberRuntime)
	if !ok {
		t.Fatalf("provider 路应返回 ExternalHarnessMemberRuntime: %T", rt)
	}
	if harnessRT.ProviderName() != "fake-provider" {
		t.Fatalf("harness 卡名: %q", harnessRT.ProviderName())
	}
	// 端点成员 → fallback 丢弃（captureModelProvider 记录）
	if probe.gotFallback != nil {
		t.Fatal("端点成员 fallback 必须丢弃")
	}
}

// ──────────────────────────── BuildTeamContextTracker ────────────────────────────

// TestBuildTeamContextTracker_workspace两分支 对齐
// spawn/external_cli_spawn.py:194-236：workspace enabled 传 root_path，未启用
// 传 None（Go 空串）。
func TestBuildTeamContextTracker_workspace两分支(t *testing.T) {
	rtc := externalCliRtc("openclaw")
	// 审查修正：原断言 `x != "" && x != rtc.Prompt` 在 rtc.Prompt 为空串时恒假，
	// 改喂非空 prompt 后断言透传（Python member_prompt=ctx.prompt or ""，
	// external_cli_spawn.py:231）。
	rtc.Prompt = "beta-私有约定"
	root := filepath.Join(t.TempDir(), "team-ws")
	enabled := &schema.TeamAgentSpec{
		Workspace: &team_workspace.TeamWorkspaceConfig{Enabled: true, RootPath: root},
	}
	tr := BuildTeamContextTracker(nil, enabled, rtc, "beta", "pay")
	if tr == nil {
		t.Fatal("tracker 必须构造成功")
	}
	if got := unexportedString(tr, "teamWorkspacePath"); got != root {
		t.Fatalf("enabled → workspace_path=root_path: %q", got)
	}
	disabled := &schema.TeamAgentSpec{Workspace: &team_workspace.TeamWorkspaceConfig{Enabled: false}}
	tr2 := BuildTeamContextTracker(nil, disabled, rtc, "beta", "pay")
	if got := unexportedString(tr2, "teamWorkspacePath"); got != "" {
		t.Fatalf("disabled → 空串（Python None）: %q", got)
	}
	if got := unexportedString(tr, "memberPrompt"); got != rtc.Prompt {
		t.Fatalf("member_prompt 应透传 ctx.prompt: %q", got)
	}
}

// unexportedString 反射读 tracker 非导出 string 字段（测试断言专用）。
func unexportedString(v any, field string) string {
	rv := reflect.ValueOf(v).Elem().FieldByName(field)
	if !rv.IsValid() || rv.Kind() != reflect.String {
		return "<missing>"
	}
	return rv.String()
}

// ──────────────────────────── 进程环境助手 ────────────────────────────

// TestClaudeProcessEnv_ssh清空 对齐 spawn.py:723-728：ssh_transport 非 nil 时
// base_env 清空（仅 extra/join/home）。strip_parent_claude_env ⤴️15.7。
func TestClaudeProcessEnv_ssh清空(t *testing.T) {
	env := claudeProcessEnv(
		map[string]string{"EXTRA": "1"},
		map[string]string{TeamJoinEnv: "join"},
		&schema.SshTransportConfig{Host: "h"},
	)
	if env["EXTRA"] != "1" || env[TeamJoinEnv] != "join" || env[OpenjiuwenHomeEnv] == "" {
		t.Fatalf("ssh 路 env: %v", env)
	}
	if len(env) != 3 {
		t.Fatalf("ssh_transport 非 nil 时 base_env 清空，仅 extra/join/home: %v", env)
	}
}

// TestCodexProcessEnv_全量继承 对齐 spawn.py:731-733：继承 os.environ + extra
// + join。
func TestCodexProcessEnv_全量继承(t *testing.T) {
	t.Setenv("UAPCLAW_CODEX_PARENT", "p")
	env := codexProcessEnv(map[string]string{"EXTRA": "1"}, map[string]string{TeamJoinEnv: "join"})
	if env["UAPCLAW_CODEX_PARENT"] != "p" || env["EXTRA"] != "1" || env[TeamJoinEnv] != "join" {
		t.Fatalf("codex env: %v", env)
	}
	if env[OpenjiuwenHomeEnv] == "" {
		t.Fatal("必须注入 OPENJIUWEN_HOME")
	}
}

// ──────────────────────────── tools 包引用守卫 ────────────────────────────

// TestTeamBackend类型面 编译期确认 tracker 桥接的 TeamBackend 类型可用
// （BuildTeamContextTracker 的 team_backend 参数）。
func TestTeamBackend类型面(t *testing.T) {
	var backend *tools.TeamBackend
	if backend != nil {
		t.Fatal("nil 后端合法（外部 CLI 成员可无 DB 后端构造 tracker）")
	}
}

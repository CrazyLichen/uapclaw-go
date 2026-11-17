//go:build integration

package externalcli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/dsh"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DshProviderSuite 外部 CLI provider 路（dsh 栈）集成套件。
//
// 跨子系统：spawn（externalcli.BuildCliRuntime provider 分支 + memberModelConfig）
// → externalcli（ExternalHarnessMemberRuntime）→ harness_providers（第三方注册缝）
// → harness_providers/dsh（DshHarnessConfigFromMapping 校验 + ModelConfig 拒绝
// 语义 + SDKFactory 缝）→ harness_protocol（HarnessContext/ModelChoice）。
// SDK 边界经 15.9 的 SDKFactory 缝注入 fake，绝不触达真实 deepseek-harness 子进程。
//
// 注册缝说明：dsh 与 native/claudecode/codex 同属内置 provider 名闭集
// （factory.go:110，Python factory.py:62 PROVIDER_NAMES），第三方成员路按
// backends.py:114 的 third_party_providers 语义排除内置名——外部 CLI 成员要
// 跑 dsh 栈，须经第三方 provider 注册缝承载（对齐上游「已安装发行版」集成
// 形态）。本套件注册 dsh 透传 wrapper（Card/ModelConfig/Create 全部委托真实
// dsh provider），被测链路里除注册缝外全部是真实 dsh 代码。
type DshProviderSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDshProviderSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestDshProviderSuite(t *testing.T) {
	suite.Run(t, new(DshProviderSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestProvider路_端点映射与fallback丢弃 provider 路全链：
// BuildCliRuntime(provider名) → memberModelConfig → 真实 dsh provider
// ModelConfig 映射 → DshHarnessConfigFromMapping → ExternalHarnessMemberRuntime。
// 断言四件事：
//  1. external_model_config 经 memberModelConfig → dsh.ModelConfig 映射进
//     provider config（model/base_url/api_key 落在 Create 收到的 config）；
//  2. fallback 在端点配置下被丢弃——dsh.ModelConfig 对非 nil fallback 报
//     "DSH has no authentication fallback"（provider.py:44-48 能力面），
//     本用例同时配置 primary 端点 + fallback 且构建成功，即 fallback 已丢弃
//     的反证；
//  3. spawn 公共键契约：cwd/env/inherit_process_env/skills/skill_conflict
//     全量到达 provider config，join 描述符经 env 注入；
//  4. SDKFactory 缝承载 fake：Create 出的 harness 持 fake 工厂。
//
// 对齐 spec §4 用例③；Python: _build_provider_member_runtime (spawn.py:897-985)
// + _member_model_config (:695-720)。
func (s *DshProviderSuite) TestProvider路_端点映射与fallback丢弃() {
	wrapper := &dshMemberWrapper{t: s.T()}
	name := registerDshMemberWrapper(s.T(), wrapper)

	rtc := &schema.TeamRuntimeContext{
		Role:        schema.TeamRoleExternalCLI,
		MemberName:  "beta",
		DisplayName: "Beta",
		TeamSpec:    &schema.TeamSpec{TeamName: "pay", Language: "cn"},
		CLIAgent:    name,
	}
	state := &sessionctx.SessionState{}
	state.SetSessionID("sess-dsh-provider")
	ctxValue := sessionctx.WithSessionState(s.Ctx, state)

	primary := &schema.ExternalCliModelConfig{
		APIBase: "http://dsh-endpoint.test", APIKey: "k-primary",
		Provider: "deepseek-official", Model: "deepseek-v4",
	}
	// fallback 同时给出：端点成员必须被丢弃，否则 dsh.ModelConfig 直接报错
	fallback := &schema.ExternalCliModelConfig{Provider: "deepseek-official", Model: "lite"}

	rt, err := externalcli.BuildCliRuntime(ctxValue, rtc, externalcli.Options{
		ExternalModelConfig:         primary,
		FallbackExternalModelConfig: fallback,
		InjectMCP:                   boolPtr(true),
		MemberAgentID:               "pay_beta",
		Cwd:                         s.T().TempDir(),
	})
	s.Require().NoError(err, "provider 路构建应成功（fallback 已被丢弃，否则 dsh 拒绝映射）")
	harnessRT, ok := rt.(*externalcli.ExternalHarnessMemberRuntime)
	s.Require().True(ok, "provider 路应返回 ExternalHarnessMemberRuntime，got %T", rt)
	s.Equal("deepseek-harness", harnessRT.ProviderName(), "真实 dsh 卡名经透传 wrapper 透出")
	s.True(harnessRT.InjectMCP, "inject_mcp 透传")
	s.Equal(externalcli.TeamMCPServerName, harnessRT.MCPServerName, "团队 MCP server 逻辑名")

	// provider config 断言（Create 收到的映射；JsonObject 冻结形态经 JSON
	// 规范化后断言，避免 map 键形态差异）
	normalized := normalizeJsonObject(s.T(), wrapper.gotCreateConfig)
	s.Equal("deepseek-v4", normalized["model"], "external_model_config.model 经 dsh.ModelConfig 映射")
	s.Equal("http://dsh-endpoint.test", normalized["base_url"], "api_base → base_url")
	s.Equal("k-primary", normalized["api_key"], "api_key 透传")
	s.Equal(false, normalized["inherit_process_env"], "spawn 公共键 inherit_process_env")
	s.Equal("skip", normalized["skill_conflict"], "skill_conflict 默认 skip")
	s.NotEmpty(normalized["cwd"], "cwd 公共键")
	env, ok := normalized["env"].(map[string]any)
	s.Require().True(ok, "env 公共键形状: %T", normalized["env"])
	s.NotEmpty(env[externalcli.TeamJoinEnv], "join 描述符经 OPENJIUWEN_TEAM_JOIN 注入 provider env")
	s.NotEmpty(env[externalcli.OpenjiuwenHomeEnv], "OPENJIUWEN_HOME 注入 provider env")

	// SDKOptions 读回：真实 dsh harness 的配置面（Python _sdk_options 测试面）
	dshHarness, ok := harnessRT.Harness().(*dsh.DshHarness)
	s.Require().True(ok, "harness 应为 *dsh.DshHarness，got %T", harnessRT.Harness())
	opts := dshHarness.SDKOptions(&harnessprotocol.HarnessContext{})
	s.Equal("deepseek-v4", opts.Model, "config.model → SDK 选项")
	s.Equal("http://dsh-endpoint.test", opts.BaseURL, "config.base_url → SDK 选项")
	s.Equal("k-primary", opts.APIKey, "config.api_key → SDK 选项")
	s.Equal("deepseek-official", opts.Provider, "config.provider → SDK 选项")
}

// TestProvider路_dsh能力面拒绝 dsh 的 ModelConfig 拒绝语义（provider.py:24-41）：
// effort 显式报错；native 成员 + fallback 报错（认证回退能力面缺失）。
// 这两条同时是「fallback 只在端点配置下被丢弃」的正向证据。
func (s *DshProviderSuite) TestProvider路_dsh能力面拒绝() {
	name := registerDshMemberWrapper(s.T(), &dshMemberWrapper{t: s.T()})
	state := &sessionctx.SessionState{}
	state.SetSessionID("sess-dsh-reject")
	ctxValue := sessionctx.WithSessionState(s.Ctx, state)
	rtc := &schema.TeamRuntimeContext{
		Role:       schema.TeamRoleExternalCLI,
		MemberName: "beta",
		TeamSpec:   &schema.TeamSpec{TeamName: "pay"},
		CLIAgent:   name,
	}

	// effort：DSH has no reasoning effort setting
	_, err := externalcli.BuildCliRuntime(ctxValue, rtc, externalcli.Options{
		ExternalModelConfig: &schema.ExternalCliModelConfig{
			APIBase: "http://x", Model: "deepseek-v4", Effort: "high",
		},
	})
	s.Require().Error(err, "effort 必须被 dsh 拒绝")
	s.True(strings.Contains(err.Error(), "DSH has no reasoning effort setting"), "报文对齐 provider.py:39-43: %v", err)

	// native 成员 + fallback：能力面拒绝。dsh 卡不声明 AUTH_FALLBACK，报错
	// 在 factory 能力守卫层（"has no authentication fallback"）；若卡声明了
	// 该能力，provider.ModelConfig（provider.py:44-48）同样报
	// "DSH has no authentication fallback"——两处语义一致。
	_, err = externalcli.BuildCliRuntime(ctxValue, rtc, externalcli.Options{
		FallbackExternalModelConfig: &schema.ExternalCliModelConfig{Model: "lite"},
	})
	s.Require().Error(err, "native 成员 fallback 必须被拒绝")
	s.True(strings.Contains(err.Error(), "authentication fallback"), "认证回退拒绝报文: %v", err)
}

// TestProvider路_fakeSDK生命周期 经 SDKFactory 缝注入 fake SDK，走
// Start（OpenSession：配置校验 + overlay 判定 + 工厂调用 + StartSession）→
// Stop（CloseSession：Close + 清场）全周期，并从工厂收到的 GoSDKOptions 断言
// provider config 到 SDK 选项的传播。
//
// Python: harness.py:92-147 _open_session/_close_session。
func (s *DshProviderSuite) TestProvider路_fakeSDK生命周期() {
	fake := &fakeDshSDK{}
	factory := func(options dsh.GoSDKOptions) (dsh.DshSDKHarness, error) {
		fake.options = options
		return fake, nil
	}
	cfg, err := dsh.DshHarnessConfigFromMapping(harnessprotocol.JsonObject{
		"env": map[string]any{"FAKE_KEY": "V"},
	})
	s.Require().NoError(err)
	harness, err := dsh.NewDshHarnessWithFactory(cfg, factory)
	s.Require().NoError(err)

	harnessCtx, err := harnessprotocol.NewHarnessContext("beta", "pay_beta", "sess-dsh-fake", "",
		harnessprotocol.ContextOptions{})
	s.Require().NoError(err)
	s.Require().NoError(harness.Start(s.Ctx, harnessCtx))
	sessionID := harness.ProviderSessionID()
	s.Require().NotNil(sessionID, "OpenSession 应产出 provider 会话 id")
	s.True(strings.HasPrefix(*sessionID, "dsh-"), "会话 id 形状 dsh-<hex>: %q", *sessionID)

	s.Equal("deepseek-official", fake.options.Provider, "SDK 选项 provider 默认值")
	s.Equal("deepseek-v4-flash", fake.options.Model, "SDK 选项 model 默认值")
	s.Equal("V", fake.options.Env["FAKE_KEY"], "config.env 传播进 SDK 选项")

	s.Require().NoError(harness.Stop(s.Ctx))
	s.True(fake.closed, "Stop 必须 CloseSession 释放 fake SDK 句柄")
}

// ──────────────────────────── 非导出函数（本目录共享测试助手） ────────────────────────────

// normalizeJsonObject JsonObject 冻结形态 → 普通 map（断言用）。
func normalizeJsonObject(t *testing.T, obj harnessprotocol.JsonObject) map[string]any {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return out
}

// dshMemberWrapper 真实 dsh provider 的成员路透传 wrapper：Card/ModelConfig/
// Create 全部委托真实 dsh 实现，仅捕获 Create 收到的 provider config 供断言。
// 对齐上游「已安装第三方 provider 发行版」集成形态（backends.py:114 内置名
// 闭集排除 dsh，成员路须经第三方注册缝）。
type dshMemberWrapper struct {
	t               *testing.T
	inner           dsh.DshHarnessProvider
	gotCreateConfig harnessprotocol.JsonObject
}

func (w *dshMemberWrapper) Card() harnessprotocol.HarnessCard { return w.inner.Card() }

func (w *dshMemberWrapper) ModelConfig(
	choice harnessprotocol.ModelChoice, fallback *harnessprotocol.ModelChoice,
) (harnessprotocol.JsonObject, error) {
	return w.inner.ModelConfig(choice, fallback)
}

func (w *dshMemberWrapper) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	w.gotCreateConfig = config
	return w.inner.Create(config)
}

// registerDshMemberWrapper 把 wrapper 注册进第三方 provider 注册缝并登记清理。
func registerDshMemberWrapper(t *testing.T, wrapper *dshMemberWrapper) string {
	t.Helper()
	name := "dsh-member-itest"
	if err := harness_providers.RegisterHarnessProvider(name, func() (harnessprotocol.HarnessProvider, error) {
		return wrapper, nil
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	t.Cleanup(func() { harness_providers.UnregisterHarnessProvider(name) })
	return name
}

// fakeDshSDK SDKFactory 缝的 fake 句柄（SDK 边界 mock；对齐 harness_test.go
// installFakeSDK 模式）。
type fakeDshSDK struct {
	options dsh.GoSDKOptions
	closed  bool
}

func (f *fakeDshSDK) StartSession(sessionID string) (dsh.DshSDKSession, error) {
	return &fakeDshSession{sessionID: sessionID}, nil
}

func (f *fakeDshSDK) Close() error {
	f.closed = true
	return nil
}

// fakeDshSession fake 会话句柄（本用例只走 Start/Stop，Run 不触达）。
type fakeDshSession struct {
	sessionID string
}

func (f *fakeDshSession) Run(_ any, _ func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	return &deepseekharness.RunResult{}, nil
}

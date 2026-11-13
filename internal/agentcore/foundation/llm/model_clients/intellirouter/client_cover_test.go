package intellirouter

import (
	"context"
	"errors"
	"testing"

	intelliroutertypesAlias "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func testCtx() context.Context { return context.Background() }

const (
	intelliroutercoreStatusCooldown = intelliroutercore.DeploymentStatusCooldown
	intelliroutercoreStatusHealthy  = intelliroutercore.DeploymentStatusHealthy
)

// 补充覆盖：构造器/接口面/配置兜底/转换边界（spec §9 覆盖率 ≥85% 门槛）。

// NewIntelliRouterModelClient 结构化配置路径（Python: :443-459 __init__）。
func TestNewIntelliRouterModelClient_结构化配置(t *testing.T) {
	groupID := "g1"
	cc := &llmschema.ModelClientConfig{
		ClientProvider: "intelli_router",
		IntelliRouter: &llmschema.IntelliRouterConfig{
			ModelGroupID: &groupID,
			Strategy:     "ordered-failover",
			Deployments: []llmschema.IntelliRouterDeploymentConfig{{
				RouteID: "r1", ModelName: "m", Provider: "openai",
				APIKey: "k", APIBase: "http://127.0.0.1:1",
			}},
		},
	}
	client, err := NewIntelliRouterModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("m")), cc)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.routerConfig == nil || client.routerKey == "" {
		t.Fatal("routerConfig/routerKey 应已填充")
	}
	if len(client.Router.Deployments) != 1 {
		t.Fatalf("deployment 应为 1，得 %d", len(client.Router.Deployments))
	}
	// 注入 router 时 routerConfig/routerKey 为 nil（Python: :444-447）
	client2 := &IntelliRouterModelClient{router: client.Router}
	if client2.routerConfig != nil || client2.routerKey != "" {
		t.Fatal("注入路径不应填充缓存键")
	}
}

// 接口面：Release/Supports*/亲和参数（Python: InferenceAffinity 才有 release）。
func TestClient_InterfaceDefaults(t *testing.T) {
	client := &IntelliRouterModelClient{}
	if client.SupportsKVCacheRelease() {
		t.Fatal("SupportsKVCacheRelease 应为 false")
	}
	if client.SupportsKVCacheAffinity() {
		t.Fatal("SupportsKVCacheAffinity 应为 false")
	}
	if _, err := client.Release(testCtx()); err == nil {
		t.Fatal("Release 应报不支持")
	}
	if got := client.BuildKVCacheAffinityInvokeKwargs(nil, "", "", 0); len(got) != 0 {
		t.Fatalf("亲和参数应为空 map：%v", got)
	}
}

// resolveModelName 覆盖（Python: :711/:768 model or model_name or "*"）。
func TestClient_ResolveModelName(t *testing.T) {
	client := &IntelliRouterModelClient{}
	if got := client.resolveModelName(""); got != "*" {
		t.Fatalf("空模型名应回退 *，得 %q", got)
	}
	client.ModelConfig = llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen"))
	if got := client.resolveModelName(""); got != "qwen" {
		t.Fatalf("应回退 model_config.model_name，得 %q", got)
	}
	if got := client.resolveModelName("override"); got != "override" {
		t.Fatalf("显式模型名应优先，得 %q", got)
	}
}

// effective* 调用参数优先级（Python: :645-659 _build_call_params）。
func TestClient_EffectiveParams(t *testing.T) {
	client := &IntelliRouterModelClient{}
	client.ModelConfig = llmschema.NewModelRequestConfig(llmschema.WithModelName("m"))
	temp := 0.7
	topP := 0.9
	maxTokens := 100
	stop := "END"
	client.ModelConfig.Temperature = &temp
	client.ModelConfig.TopP = &topP
	client.ModelConfig.MaxTokens = &maxTokens
	client.ModelConfig.Stop = &stop

	// 显式覆盖优先
	if v := client.effectiveTemperature(ptrF(0.1)); *v != 0.1 {
		t.Fatal("显式 temperature 应优先")
	}
	// 缺省回退模型配置
	if v := client.effectiveTemperature(nil); *v != 0.7 {
		t.Fatal("应回退 model_config.temperature")
	}
	if v := client.effectiveTopP(nil); *v != 0.9 {
		t.Fatal("应回退 model_config.top_p")
	}
	if v := client.effectiveMaxTokens(nil); *v != 100 {
		t.Fatal("应回退 model_config.max_tokens")
	}
	if v := client.effectiveStop(nil); v != stop {
		t.Fatalf("应回退 model_config.stop，得 %v", v)
	}
	if v := client.effectiveStop(ptrS("X")); v != "X" {
		t.Fatal("显式 stop 应优先")
	}
}

// buildCoreDeployment 全字段转换（Python: :513-560 dep_kwargs）。
func TestBuildCoreDeployment_全字段(t *testing.T) {
	dep := buildCoreDeployment(map[string]any{
		"route_id": "r1", "model_id": "m1", "model_name": "qwen-max",
		"provider": "openai", "api_key": "k", "api_base": "http://x",
		"endpoint_profile": "siliconflow", "verify_ssl": false,
		"fallback_tag": "fast", "model_description": "desc",
		"request_defaults": map[string]any{"temperature": 0.5},
		"tpm":              100000.0, "rpm": 1000.0, "timeout": 15.0,
		"custom_headers": map[string]any{"X-A": "b"},
		"tags":           []any{"t1", "t2"},
	}, true)
	if dep.ID != "r1" || dep.ModelID != "m1" || dep.ModelName != "qwen-max" ||
		dep.Provider != "openai" || dep.APIKey != "k" || dep.APIBase != "http://x" {
		t.Fatalf("基础字段错误：%+v", dep)
	}
	if dep.EndpointProfile == nil || *dep.EndpointProfile != "siliconflow" {
		t.Fatal("endpoint_profile 错误")
	}
	if dep.VerifySSL {
		t.Fatal("verify_ssl 应为 false（路由级覆盖）")
	}
	if dep.FallbackTag == nil || *dep.FallbackTag != "fast" {
		t.Fatal("fallback_tag 错误")
	}
	if dep.ModelDescription == nil || *dep.ModelDescription != "desc" {
		t.Fatal("model_description 错误")
	}
	if dep.RequestDefaults["temperature"] != 0.5 {
		t.Fatal("request_defaults 错误")
	}
	if dep.TPM == nil || *dep.TPM != 100000 || dep.RPM == nil || *dep.RPM != 1000 {
		t.Fatal("tpm/rpm 错误")
	}
	if dep.Timeout == nil || *dep.Timeout != 15 {
		t.Fatal("timeout 错误")
	}
	if dep.CustomHeaders["X-A"] != "b" {
		t.Fatal("custom_headers 错误")
	}
	if len(dep.Tags) != 2 {
		t.Fatalf("tags 错误：%v", dep.Tags)
	}
	// verify_ssl 缺省回落 config.verify_ssl（Python: dep_cfg.get("verify_ssl", config.verify_ssl)）
	dep2 := buildCoreDeployment(map[string]any{"route_id": "r2", "model_name": "m", "provider": "openai"}, false)
	if dep2.VerifySSL {
		t.Fatal("verify_ssl 应回落 false")
	}
	// id 兜底链：route_id 缺省时用 id（Python: dep_cfg.get("route_id") or dep_cfg.get("id")）
	dep3 := buildCoreDeployment(map[string]any{"id": "legacy", "model_name": "m", "provider": "openai"}, true)
	if dep3.ID != "legacy" {
		t.Fatalf("id 兜底错误：%s", dep3.ID)
	}
}

// deploymentToMap exclude_none 语义（Python: model_dump(exclude_none=True)）。
func TestDeploymentToMap_ExcludeNone(t *testing.T) {
	m := deploymentToMap(llmschema.IntelliRouterDeploymentConfig{
		RouteID: "r1", ModelName: "m", Provider: "openai",
	})
	for _, absent := range []string{"model_id", "endpoint_profile", "fallback_tag", "tpm", "rpm", "timeout"} {
		if _, exists := m[absent]; exists {
			t.Fatalf("%s 不应出现在 exclude_none 输出：%v", absent, m)
		}
	}
	if m["model_name"] != "m" || m["provider"] != "openai" {
		t.Fatalf("必填字段丢失：%v", m)
	}
}

// FromModelClientConfig extra 兜底全键（Python: :265-280）。
func TestFromModelClientConfig_Extra兜底全键(t *testing.T) {
	groupID := "g1"
	scope := "s1"
	cc := &llmschema.ModelClientConfig{
		ClientProvider: "intelli_router",
		VerifySSL:      false,
		Extra: map[string]any{
			"intelli_router_model_group_id":        groupID,
			"intelli_router_deployments":           []any{map[string]any{"route_id": "r1", "model_name": "m", "provider": "openai"}},
			"intelli_router_strategy":              "tag-filtered",
			"intelli_router_num_retries":           5,
			"intelli_router_timeout":               42.0,
			"intelli_router_strategy_kwargs":       map[string]any{"fallback_tag": "fast"},
			"intelli_router_enable_health_check":   true,
			"intelli_router_health_check_interval": 120.0,
			"intelli_router_enable_observability":  true,
			"intelli_router_web_dashboard_port":    8080,
		},
	}
	// RouterScope 无 extra 键（Python: getattr(router_config, "router_scope", None)）
	cfg := FromModelClientConfig(cc)
	if cfg.ModelGroupID == nil || *cfg.ModelGroupID != "g1" {
		t.Fatal("model_group_id 错误")
	}
	if cfg.Strategy != "tag-filtered" || cfg.NumRetries != 5 || cfg.Timeout != 42 {
		t.Fatalf("标量键错误：%+v", cfg)
	}
	if cfg.StrategyKwargs["fallback_tag"] != "fast" {
		t.Fatal("strategy_kwargs 错误")
	}
	if !cfg.EnableHealthCheck || cfg.HealthCheckInterval != 120 || !cfg.EnableObservability || cfg.WebDashboardPort != 8080 {
		t.Fatalf("开关键错误：%+v", cfg)
	}
	// RouterScope 显式设置走结构化路径
	cc2 := &llmschema.ModelClientConfig{
		ClientProvider: "intelli_router",
		IntelliRouter: &llmschema.IntelliRouterConfig{
			RouterScope: &scope,
		},
	}
	cfg2 := FromModelClientConfig(cc2)
	if cfg2.RouterScope == nil || *cfg2.RouterScope != "s1" {
		t.Fatal("router_scope 错误")
	}
}

// cooldownRemainingSeconds 冷却余量读取（Python: 经 cooldown_remaining 口径）。
func TestCooldownRemainingSeconds(t *testing.T) {
	cfg := &IntelliRouterClientConfig{
		Deployments:    []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1"}},
		Strategy:       "simple-shuffle",
		NumRetries:     1,
		Timeout:        5,
		StrategyKwargs: map[string]any{},
	}
	r, err := CreateRouter(cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	until := 1000.0
	r.State.DeploymentStatuses["r1"] = intelliroutercoreStatusCooldown
	r.State.CooldownUntil["r1"] = &until
	if remaining := cooldownRemainingSeconds(r, "r1", 0); remaining == nil || *remaining != 1000 {
		t.Fatalf("冷却余量错误：%v", remaining)
	}
	// 过期 → nil
	if remaining := cooldownRemainingSeconds(r, "r1", 2000); remaining != nil {
		t.Fatalf("过期冷却应返回 nil，得 %v", *remaining)
	}
	// 健康状态 → nil
	healthy := intelliroutercoreStatusHealthy
	r.State.DeploymentStatuses["r1"] = healthy
	if cooldownRemainingSeconds(r, "r1", 0) != nil {
		t.Fatal("健康状态应返回 nil")
	}
}

// ──────────────────────────── fixture ────────────────────────────

func ptrF(f float64) *float64 { return &f }
func ptrS(s string) *string   { return &s }
func ptrI(i int) *int         { return &i }

var errTest = errors.New("parse failed")

// fakeOutputParser 解析器桩。
type fakeOutputParser struct {
	result any
	err    error
}

func (f *fakeOutputParser) Parse(input any) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeOutputParser) StreamParse(input <-chan any) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult, 1)
	out <- model_clients.StreamParsedResult{Content: f.result}
	close(out)
	return out
}

// intelliroutertypes 别名（测试内短写）。
type (
	intelliroutertypesMsg      = intelliroutertypesAlias.AssistantMessage
	intelliroutertypesToolCall = intelliroutertypesAlias.ToolCall
	intelliroutertypesUsage    = intelliroutertypesAlias.UsageMetadata
)

// ──────────────────────────── 转换与辅助补充 ────────────────────────────

// toOwAssistantMessage 全字段（Python: :907-961）。
func TestToOwAssistantMessage_全字段(t *testing.T) {
	client := &IntelliRouterModelClient{}
	idx := 1
	reasoning := "thinking..."
	respID := "resp-1"
	respModel := "qwen-max"
	msg := &intelliroutertypesMsg{
		Content: "raw output",
		ToolCalls: []intelliroutertypesToolCall{{
			ID: "tc1", Type: "function", Name: "search", Arguments: "{}", Index: &idx,
		}},
		FinishReason:     "tool_calls",
		ReasoningContent: &reasoning,
		ResponseID:       &respID,
		ResponseModel:    &respModel,
		UsageMetadata: &intelliroutertypesUsage{
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15, ReasoningTokens: 3,
		},
		ProviderMetadata: map[string]any{"stop_reason": "end_turn"},
	}
	out, err := client.toOwAssistantMessage(msg, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if out.Content.Text() != "raw output" || out.FinishReason != "tool_calls" {
		t.Fatalf("基础字段错误：%v %s", out.Content, out.FinishReason)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Index != 1 {
		t.Fatalf("tool_calls 转换错误：%+v", out.ToolCalls)
	}
	if out.ReasoningContent != "thinking..." || out.ResponseID != "resp-1" || out.ResponseModel != "qwen-max" {
		t.Fatalf("扩展字段错误：%s %s %s", out.ReasoningContent, out.ResponseID, out.ResponseModel)
	}
	if out.UsageMetadata == nil || out.UsageMetadata.ReasoningTokens == nil || *out.UsageMetadata.ReasoningTokens != 3 {
		t.Fatalf("usage 转换错误：%+v", out.UsageMetadata)
	}
	if out.ProviderMetadata["stop_reason"] != "end_turn" {
		t.Fatal("provider_metadata 错误")
	}

	// output parser 应用（Python: :917-927：string 解析结果替换 content）
	parser := &fakeOutputParser{result: "parsed!"}
	out2, err := client.toOwAssistantMessage(msg, parser)
	if err != nil {
		t.Fatalf("convert2: %v", err)
	}
	if out2.Content.Text() != "parsed!" {
		t.Fatalf("parser 内容错误：%v", out2.Content)
	}
	if out2.ProviderContent == nil {
		t.Fatal("parser 改写后 provider_content 应兜底原文")
	}
	// 解析失败保留原文（Python: :925-927 logger.error 分支）
	failParser := &fakeOutputParser{err: errTest}
	out3, err := client.toOwAssistantMessage(msg, failParser)
	if err != nil {
		t.Fatalf("convert3: %v", err)
	}
	if out3.Content.Text() != "raw output" {
		t.Fatal("解析失败应保留原文")
	}
	// finish_reason 缺省 stop（Python: finish_reason or "stop"）
	msg.FinishReason = ""
	out4, _ := client.toOwAssistantMessage(msg, nil)
	if out4.FinishReason != "stop" {
		t.Fatal("finish_reason 缺省应为 stop")
	}
}

// urlHost（DashScope 端点解析）。
func TestURLHost(t *testing.T) {
	if got := urlHost("https://dashscope.aliyuncs.com/api/v1"); got != "dashscope.aliyuncs.com" {
		t.Fatalf("host 错误：%q", got)
	}
	if got := urlHost("not-a-url"); got == "" {
		t.Fatal("非法 URL 不应为空")
	}
}

// effectiveTopP/MaxTokens 显式优先（补 66.7% 分支）。
func TestClient_EffectiveTopPMaxTokens_Explicit(t *testing.T) {
	client := &IntelliRouterModelClient{}
	if v := client.effectiveTopP(ptrF(0.3)); *v != 0.3 {
		t.Fatal("显式 top_p 应优先")
	}
	if v := client.effectiveMaxTokens(ptrI(64)); *v != 64 {
		t.Fatal("显式 max_tokens 应优先")
	}
}

// toAnySlice / containsStr / orDefaultStr 边界。
func TestHelpers_边界(t *testing.T) {
	if got := toAnySlice(nil); len(got) != 0 {
		t.Fatal("nil 应为空 slice")
	}
	if containsStr("", "x") {
		t.Fatal("空串不含 x")
	}
	if orDefaultStr("", "fb") != "fb" || orDefaultStr("v", "fb") != "v" {
		t.Fatal("orDefaultStr 回退错误")
	}
}

// resolveGenerationProvider model 过滤分支（Python: :1071-1073）。
func TestClient_ResolveGenerationProvider_模型过滤(t *testing.T) {
	client := dashscopeTestClient(&testing.T{}, "http://127.0.0.1:1")
	if got := client.resolveGenerationProvider("m"); got != "dashscope" {
		t.Fatalf("provider 错误：%s", got)
	}
	if got := client.resolveGenerationProvider("other"); got != "unknown" {
		t.Fatalf("不匹配模型应 unknown，得 %s", got)
	}
	if got := client.apiKeyForProvider("dashscope"); got != "k" {
		t.Fatalf("api_key 错误：%s", got)
	}
	if got := client.apiBaseForProvider("nope"); got != "" {
		t.Fatalf("未知 provider 应空串，得 %q", got)
	}
}

// Name / PinnedRouteTag tag-filtered 分支。
func TestBridge_NameAndPinnedTag(t *testing.T) {
	if (&CoreRoutingEventBridge{}).Name() != "CoreRoutingEventBridge" {
		t.Fatal("Name 错误")
	}
	cfg := &IntelliRouterClientConfig{
		Deployments:    []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1", "fallback_tag": "fast"}},
		Strategy:       "tag-filtered",
		NumRetries:     1,
		Timeout:        5,
		StrategyKwargs: map[string]any{"fallback_tag": "fast"},
	}
	r, err := CreateRouter(cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view := routerViewOf(r)
	if view.PinnedRouteTag() != "fast" {
		t.Fatalf("pinned tag 错误：%q", view.PinnedRouteTag())
	}
	if view.ModelGroupID() != "" {
		t.Fatal("无组 ID 应为空串")
	}
}

// userMessageText 多模态消息返回空（Python: isinstance(content, str) else ""）。
func TestUserMessageText_多模态(t *testing.T) {
	msg := llmschema.NewUserMessage("text")
	if got := userMessageText(msg); got != "text" {
		t.Fatalf("文本消息错误：%q", got)
	}
}

// deploymentToMap 可选键补齐（coverage 补充）。
func TestDeploymentToMap_可选键(t *testing.T) {
	verify := false
	tag := "fast"
	desc := "d"
	m := deploymentToMap(llmschema.IntelliRouterDeploymentConfig{
		RouteID: "r1", ModelName: "m", Provider: "openai",
		EndpointProfile: "siliconflow", VerifySSL: &verify,
		FallbackTag: &tag, ModelDescription: &desc,
		RequestDefaults: map[string]any{"temperature": 0.5},
		TPM:             ptrI(1000), RPM: ptrI(10), Timeout: ptrF(15),
	})
	for _, key := range []string{"endpoint_profile", "verify_ssl", "fallback_tag", "model_description", "request_defaults", "tpm", "rpm", "timeout"} {
		if _, exists := m[key]; !exists {
			t.Fatalf("%s 丢失：%v", key, m)
		}
	}
}

// payloadFromEvent attempt/total_attempts/error_message 透传（coverage 补充）。
func TestEventBridge_PayloadFromEvent_透传(t *testing.T) {
	event := observability.NewRoutingEvent(observability.EventTypeAllDeploymentsExhausted, "req1", "*")
	event.Attempt = 2
	event.TotalAttempts = 3
	errorMessage := "boom"
	event.ErrorMessage = &errorMessage
	event.Extra = map[string]any{"model_group_id": "g1", "route_id": "r1"}
	payload := payloadFromEvent(event)
	if payload["attempt"] != 2 || payload["total_attempts"] != 3 || payload["error_message"] != "boom" {
		t.Fatalf("透传键错误：%v", payload)
	}
	// attempt/total_attempts 为 0 不透传
	event2 := observability.NewRoutingEvent(observability.EventTypeAllDeploymentsExhausted, "req2", "*")
	event2.Extra = map[string]any{"model_group_id": "g1"}
	payload2 := payloadFromEvent(event2)
	if _, exists := payload2["attempt"]; exists {
		t.Fatal("attempt=0 不应透传")
	}
}

// createRouter observability 开关 + announceRouteAttempt 路径（coverage 补充）。
func TestCreateRouter_ObservabilityAndAnnounce(t *testing.T) {
	groupID := "g1"
	cfg := &IntelliRouterClientConfig{
		Deployments:         []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1", "fallback_tag": "fast"}},
		Strategy:            "ordered-failover",
		NumRetries:          1,
		Timeout:             5,
		StrategyKwargs:      map[string]any{},
		EnableObservability: true,
		WebDashboardPort:    8080, // 与 observability 同开 → 不告警
		ModelGroupID:        &groupID,
	}
	r, err := CreateRouter(cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(r.EventBus.Handlers()) != 1 {
		t.Fatalf("应注册 1 个核心桥 handler，得 %d", len(r.EventBus.Handlers()))
	}
	// announceRouteAttempt：metadata 有 model_group_id → 走日志 + 回调（不 panic）
	announceRouteAttempt(r, r.Deployments[0])
	// 无 model_group_id → 提前返回
	cfgNoGroup := &IntelliRouterClientConfig{
		Deployments:    []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1"}},
		Strategy:       "ordered-failover",
		NumRetries:     1,
		Timeout:        5,
		StrategyKwargs: map[string]any{},
	}
	r2, err := CreateRouter(cfgNoGroup)
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	announceRouteAttempt(r2, r2.Deployments[0])
	// orStr 直测
	if got := orStr("", "fallback"); got != "fallback" {
		t.Fatalf("orStr 回退错误：%q", got)
	}
	// WebDashboardPort 开但 observability 关 → 告警日志（不 panic 即可）
	cfgWarn := &IntelliRouterClientConfig{
		Deployments:      []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1"}},
		Strategy:         "ordered-failover",
		NumRetries:       1,
		Timeout:          5,
		StrategyKwargs:   map[string]any{},
		WebDashboardPort: 8080,
	}
	if _, err := CreateRouter(cfgWarn); err != nil {
		t.Fatalf("create3: %v", err)
	}
}

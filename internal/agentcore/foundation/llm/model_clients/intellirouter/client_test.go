package intellirouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	intelliroutertypescore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	intelliroutertypesprovider "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 以下用例对齐上游 test_intelli_router_routed_model.py 12 例中的
// 占位符/标签部分与旧 2.12 客户端迁移清单（spec §6）。

// ──────────────────────────── routed_model ────────────────────────────

// 对齐 upstream::test_routed_model_label_prefers_fallback_tag。
func TestRoutedModelLabel_PrefersFallbackTag(t *testing.T) {
	d := newTestDeployment("d1", "qwen-max", "official-name")
	d.FallbackTag = strPtr("official-name")
	if got := RoutedModelLabel(d); got != "official-name" {
		t.Fatalf("fallback_tag 优先，得 %q", got)
	}
}

// 对齐 upstream::test_routed_model_label_falls_back_to_model_name。
func TestRoutedModelLabel_FallsBackToModelName(t *testing.T) {
	d := newTestDeployment("d1", "qwen-max", "")
	if got := RoutedModelLabel(d); got != "qwen-max" {
		t.Fatalf("model_name 回退，得 %q", got)
	}
}

// 对齐 upstream::test_placeholder_filled_in_string_content。
func TestFillMessagePlaceholder_StringContent(t *testing.T) {
	msg := map[string]any{"role": "user", "content": "use " + RoutedModelPlaceholder + " please"}
	fillMessagePlaceholder(msg, "qwen-max")
	if msg["content"] != "use qwen-max please" {
		t.Fatalf("占位符未回填：%v", msg["content"])
	}
}

// 对齐 upstream::test_placeholder_filled_in_multimodal_parts。
func TestFillMessagePlaceholder_MultimodalParts(t *testing.T) {
	msg := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "look at " + RoutedModelPlaceholder},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://x"}},
	}}
	fillMessagePlaceholder(msg, "m1")
	parts := msg["content"].([]any)
	text := parts[0].(map[string]any)["text"]
	if text != "look at m1" {
		t.Fatalf("多模态占位符未回填：%v", text)
	}
	if parts[1].(map[string]any)["image_url"] == nil {
		t.Fatal("非文本 part 不得被改写")
	}
}

// 对齐 upstream::test_messages_without_placeholder_untouched。
func TestFillMessagePlaceholder_NoPlaceholder(t *testing.T) {
	msg := map[string]any{"role": "user", "content": "hello"}
	fillMessagePlaceholder(msg, "m1")
	if msg["content"] != "hello" {
		t.Fatal("无占位符消息不得改写")
	}
}

// 对齐 upstream::test_adapter_wraps_transform_request。
func TestRoutedModelAdapter_WrapsTransformRequest(t *testing.T) {
	inner := &fakeProviderAdapter{
		transformRequest: func(model string, messages []map[string]any, deployment *intelliroutertypescore.Deployment, kwargs map[string]any) map[string]any {
			return map[string]any{"model": model, "messages": messages}
		},
	}
	adapter := &routedModelAdapter{BaseProviderAdapter: inner}
	d := newTestDeployment("d1", "qwen-max", "tag-a")
	out := adapter.TransformRequest("m", []map[string]any{{"content": RoutedModelPlaceholder}}, d, nil)
	msgs := out["messages"].([]map[string]any)
	if msgs[0]["content"] != "tag-a" {
		t.Fatalf("适配器未回填：%v", msgs[0]["content"])
	}
}

// 对齐 upstream::test_adapter_label_falls_back_to_unknown（畸形 deployment）。
func TestRoutedModelAdapter_LabelFallbackUnknown(t *testing.T) {
	inner := &fakeProviderAdapter{
		transformRequest: func(model string, messages []map[string]any, deployment *intelliroutertypescore.Deployment, kwargs map[string]any) map[string]any {
			return map[string]any{"messages": messages}
		},
	}
	adapter := &routedModelAdapter{BaseProviderAdapter: inner}
	d := &intelliroutertypescore.Deployment{} // 空名
	out := adapter.TransformRequest("m", []map[string]any{{"content": RoutedModelPlaceholder}}, d, nil)
	msgs := out["messages"].([]map[string]any)
	if msgs[0]["content"] != "unknown" {
		t.Fatalf("畸形 deployment 应回退 unknown，得 %v", msgs[0]["content"])
	}
}

// ──────────────────────────── config ────────────────────────────

// 对齐旧 TestFromModelClientConfig_DefaultValues + 上游 from_model_client_config。
func TestFromModelClientConfig_Extra兜底缺省(t *testing.T) {
	cc := &llmschema.ModelClientConfig{ClientProvider: "intelli_router", VerifySSL: false}
	cfg := FromModelClientConfig(cc)
	if cfg.Strategy != "simple-shuffle" || cfg.NumRetries != 3 || cfg.Timeout != 30.0 {
		t.Fatalf("缺省值漂移：%+v", cfg)
	}
	if cfg.HealthCheckInterval != 300.0 || cfg.EnableHealthCheck {
		t.Fatalf("健康检查缺省漂移：%+v", cfg)
	}
	if cfg.VerifySSL {
		t.Fatal("verify_ssl 应透传 false")
	}
}

// 对齐旧 TestFromModelClientConfig_WithDeployments（结构化路径优先）。
func TestFromModelClientConfig_结构化路径(t *testing.T) {
	numRetries := 2
	groupID := "g1"
	tag := "fast"
	cc := &llmschema.ModelClientConfig{
		ClientProvider: "intelli_router",
		VerifySSL:      true,
		IntelliRouter: &llmschema.IntelliRouterConfig{
			ModelGroupID: &groupID,
			Strategy:     "ordered-failover",
			NumRetries:   &numRetries,
			Timeout:      45,
			Deployments: []llmschema.IntelliRouterDeploymentConfig{{
				RouteID: "r1", ModelName: "m", Provider: "openai",
				FallbackTag: &tag, RequestDefaults: map[string]any{"temperature": 0.5},
			}},
		},
	}
	cfg := FromModelClientConfig(cc)
	if cfg.ModelGroupID == nil || *cfg.ModelGroupID != "g1" {
		t.Fatal("model_group_id 丢失")
	}
	if cfg.NumRetries != 2 || cfg.Strategy != "ordered-failover" || cfg.Timeout != 45 {
		t.Fatalf("字段丢失：%+v", cfg)
	}
	if len(cfg.Deployments) != 1 || cfg.Deployments[0]["fallback_tag"] != "fast" {
		t.Fatalf("deployment 转换错误：%v", cfg.Deployments)
	}
	if _, ok := cfg.Deployments[0]["request_defaults"]; !ok {
		t.Fatal("request_defaults 丢失")
	}
}

// 对齐旧 TestFromModelClientConfig_DeploymentDefaults（num_retries nil 兜底）。
func TestFromModelClientConfig_NumRetries缺省(t *testing.T) {
	cc := &llmschema.ModelClientConfig{
		ClientProvider: "intelli_router",
		IntelliRouter: &llmschema.IntelliRouterConfig{
			Deployments: []llmschema.IntelliRouterDeploymentConfig{
				{RouteID: "r1", ModelName: "m", Provider: "openai"},
				{RouteID: "r2", ModelName: "m", Provider: "openai"},
				{RouteID: "r3", ModelName: "m", Provider: "openai"},
			},
		},
	}
	cfg := FromModelClientConfig(cc)
	if cfg.NumRetries != 2 { // max(len-1, 0)
		t.Fatalf("num_retries 缺省应为 len-1=2，得 %d", cfg.NumRetries)
	}
}

// 对齐旧 TestRouterCache_SameConfig/DifferentConfig + MakeRouterKey_*。
func TestMakeRouterKey_确定性与区分度(t *testing.T) {
	base := &IntelliRouterClientConfig{
		Deployments:    []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://x"}},
		Strategy:       "ordered-failover",
		NumRetries:     1,
		Timeout:        30,
		StrategyKwargs: map[string]any{},
	}
	k1 := MakeRouterKey(base)
	k2 := MakeRouterKey(base)
	if k1 != k2 {
		t.Fatal("同配置键应相同")
	}
	other := *base
	other.Strategy = "tag-filtered"
	if MakeRouterKey(&other) == k1 {
		t.Fatal("不同配置键应不同")
	}
	other2 := *base
	other2.NumRetries = 2
	if MakeRouterKey(&other2) == k1 {
		t.Fatal("num_retries 影响缓存键")
	}
}

// 对齐旧 TestRouterCache_SameConfig（同配置共享实例）。
func TestGetOrCreateRouter_同配置共享实例(t *testing.T) {
	cfg := &IntelliRouterClientConfig{
		Deployments:    []map[string]any{{"route_id": "r1", "model_name": "m", "provider": "openai", "api_key": "k", "api_base": "http://127.0.0.1:1"}},
		Strategy:       "simple-shuffle",
		NumRetries:     1,
		Timeout:        5,
		StrategyKwargs: map[string]any{},
	}
	r1, err := GetOrCreateRouter(cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r2, err := GetOrCreateRouter(cfg)
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	if r1 != r2 {
		t.Fatal("同配置应共享 router 实例（冷却状态共享）")
	}
}

// ──────────────────────────── Invoke/Stream（httptest 假上游）────────────────────────────

// 对齐旧 TestInvoke_Success / TestInvoke_RetryOnFailure。
func TestClient_Invoke_RetryOnFailure(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "model": "m",
			"choices": []map[string]any{{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "hello"}}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL, "m", "m2")
	msgs := model_clients.NewMessagesParam(llmschema.NewUserMessage("hi"))
	result, err := client.Invoke(context.Background(), msgs)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.Content.Text() != "hello" {
		t.Fatalf("内容错误：%v", result.Content)
	}
	if callCount < 2 {
		t.Fatalf("应重试到第二个 deployment，得 %d", callCount)
	}
}

// 对齐旧 TestInvoke_AllEndpointsFail → 全耗尽走 ToModelError（181010）。
func TestClient_Invoke_AllEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL, "m", "m2")
	msgs := model_clients.NewMessagesParam(llmschema.NewUserMessage("hi"))
	_, err := client.Invoke(context.Background(), msgs)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "[181010]") {
		t.Fatalf("应为 MODEL_GROUP_NO_AVAILABLE_ROUTE/181010，得 %v", err)
	}
	// 上游 issue #5489 断言：用户文本不含内部术语
	assertNoJargon(t, err.Error())
}

// 对齐旧 TestInvoke_NoAvailableEndpoints → request_rejected 不升级。
func TestClient_Invoke_RequestRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 4096 tokens"}}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL, "m")
	msgs := model_clients.NewMessagesParam(llmschema.NewUserMessage("hi"))
	_, err := client.Invoke(context.Background(), msgs)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "exhausted_request_rejected") &&
		!strings.Contains(err.Error(), "maximum context length") {
		t.Fatalf("应保留 provider 文本：%v", err)
	}
}

// 对齐旧 TestStream_Success（SSE → chunk 转换）。
func TestClient_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt_Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		_, _ = fmt_Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6,`+
			`"completion_tokens_details":{"reasoning_tokens":1}}}`+"\n\n")
		flusher.Flush()
		_, _ = fmt_Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL, "m")
	ch, err := client.Stream(context.Background(), model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var content string
	var usage *llmschema.UsageMetadata
	for chunk := range ch {
		if chunk.StreamError != nil {
			t.Fatalf("流错误：%v", chunk.StreamError)
		}
		content += chunk.Content.Text()
		if chunk.UsageMetadata != nil {
			usage = chunk.UsageMetadata
		}
	}
	if content != "ok" {
		t.Fatalf("内容错误：%q", content)
	}
	// 对齐 _to_ow_usage_metadata reasoning_tokens（Python: :1033）
	if usage == nil || usage.ReasoningTokens == nil || *usage.ReasoningTokens != 1 {
		t.Fatalf("reasoning_tokens 应为 1：%v", usage)
	}
}

// 对齐旧 TestStream_Error（流中失败 → StreamError 哨兵 + output_emitted）。
func TestClient_Stream_ErrorAfterVisibleChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt_Fprint(w, `data: {"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		// 非 SSE 响应 → 解析中断
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt_Fprint(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL, "m", "m2")
	ch, err := client.Stream(context.Background(), model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var sawContent bool
	var streamErr error
	for chunk := range ch {
		if chunk.StreamError != nil {
			streamErr = chunk.StreamError
			break
		}
		if chunk.Content.Text() != "" {
			sawContent = true
		}
	}
	if !sawContent {
		t.Fatal("应先产出可见 chunk")
	}
	if streamErr == nil {
		t.Fatal("应有 StreamError 哨兵")
	}
	base, ok := streamErr.(*exception.BaseError)
	if !ok {
		t.Fatalf("应为 BaseError，得 %T", streamErr)
	}
	details, _ := base.Details().(map[string]any)
	if v, ok := details["output_emitted"].(bool); !ok || !v {
		t.Fatalf("output_emitted 应为 true：%v", details)
	}
	if details["route_case"] != "switchable" || details["reason"] != "stream_interrupted" {
		t.Fatalf("route_case/reason 错误：%v/%v", details["route_case"], details["reason"])
	}
}

// ──────────────────────────── fixture ────────────────────────────

func fmt_Fprint(w http.ResponseWriter, s string) (int, error) {
	return w.Write([]byte(s))
}

func newTestDeployment(id, name, tag string) *intelliroutertypescore.Deployment {
	d := &intelliroutertypescore.Deployment{ID: id, ModelName: name, Provider: "openai"}
	if tag != "" {
		d.FallbackTag = &tag
	}
	return d
}

func strPtr(s string) *string { return &s }

// newTestClient 构造走 httptest 上游的客户端（两 deployment 触发 failover）。
func newTestClient(t *testing.T, apiBase string, modelNames ...string) *IntelliRouterModelClient {
	t.Helper()
	deps := []map[string]any{}
	for i, name := range modelNames {
		deps = append(deps, map[string]any{
			"route_id": string(rune('a' + i)), "model_name": name,
			"provider": "openai", "api_key": "k", "api_base": apiBase,
		})
	}
	cfg := &IntelliRouterClientConfig{
		Deployments:    deps,
		Strategy:       "ordered-failover",
		NumRetries:     len(modelNames) - 1,
		Timeout:        10,
		StrategyKwargs: map[string]any{},
	}
	r, err := CreateRouter(cfg)
	if err != nil {
		t.Fatalf("create router: %v", err)
	}
	embed, err := model_clients.NewBaseClientEmbed(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("*")),
		&llmschema.ModelClientConfig{ClientProvider: "intelli_router"},
	)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	return &IntelliRouterModelClient{BaseClientEmbed: *embed, router: r}
}

// fakeProviderAdapter 测试桩适配器（内嵌 OpenAI 适配器补齐其余方法）。
type fakeProviderAdapter struct {
	intelliroutertypesprovider.BaseProviderAdapter
	transformRequest func(model string, messages []map[string]any, deployment *intelliroutertypescore.Deployment, kwargs map[string]any) map[string]any
}

func (f *fakeProviderAdapter) TransformRequest(model string, messages []map[string]any, deployment *intelliroutertypescore.Deployment, kwargs map[string]any) map[string]any {
	return f.transformRequest(model, messages, deployment, kwargs)
}

// assertNoJargon 内部标识不出现在用户可见文本（上游 _FORBIDDEN_TEXT）。
func assertNoJargon(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"'*'", "dpl_", "route", "pinned", "cooldown"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("%q 泄漏进 %q", forbidden, text)
		}
	}
}

// errorDetails 从 BaseError 提取 details。
func errorDetails(err error) map[string]any {
	type detailsGetter interface{ Details() map[string]any }
	if g, ok := err.(detailsGetter); ok {
		return g.Details()
	}
	return nil
}

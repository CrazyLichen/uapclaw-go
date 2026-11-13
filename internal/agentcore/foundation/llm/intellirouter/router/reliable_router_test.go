package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/strategy"
)

// sseResponse OpenAI 兼容 SSE 响应体。
func sseResponse(content string) string {
	var b strings.Builder
	b.WriteString("data: " + chunkJSON(map[string]any{"role": "assistant"}) + "\n\n")
	b.WriteString("data: " + chunkJSON(map[string]any{"content": content}) + "\n\n")
	b.WriteString("data: " + chunkJSON(map[string]any{}, "stop") + "\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// chunkJSON 构造流 chunk JSON（delta 形态 + 可选 finish_reason）。
func chunkJSON(delta map[string]any, finish ...string) string {
	choice := map[string]any{"index": 0, "delta": delta}
	if len(finish) > 0 {
		choice["finish_reason"] = finish[0]
	}
	body, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "m", "choices": []any{choice}})
	return string(body)
}

// completionJSON 非流式响应。
func completionJSON(content string, completionTokens int) string {
	body, _ := json.Marshal(map[string]any{
		"id": "resp-1", "object": "chat.completion", "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": content}}},
		"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": completionTokens, "total_tokens": 3 + completionTokens},
	})
	return string(body)
}

func dep(t *testing.T, id, url string) *intelliroutercore.Deployment {
	t.Helper()
	d, err := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: url, ID: id, Provider: "openai",
	})
	require.NoError(t, err)
	return d
}

// captureEmitHandler 捕获事件的 handler。
type captureEmitHandler struct {
	events []observability.RoutingEvent
}

func (h *captureEmitHandler) HandleEvent(e observability.RoutingEvent) {
	h.events = append(h.events, e)
}
func (h *captureEmitHandler) Name() string { return "capture" }

func eventTypes(h *captureEmitHandler) []string {
	types := make([]string, len(h.events))
	for i, e := range h.events {
		types[i] = string(e.EventType)
	}
	return types
}

// TestReliableRouter_Completion_Failover 上游 A 500 → B 200；冷却写入；事件序列。
func TestReliableRouter_Completion_Failover(t *testing.T) {
	var aCalls, bCalls atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aCalls.Add(1)
		w.WriteHeader(500)
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bCalls.Add(1)
		w.Write([]byte(completionJSON("from-b", 7)))
	}))
	defer srvB.Close()

	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL), dep(t, "B", srvB.URL)},
		StrategyType: "ordered-failover",
		NumRetries:   3,
		EventBus:     bus,
	})
	resp, err := r.Completion(context.Background(), "m", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, int(bCalls.Load()))
	// deployment_id 标识实际服务的部署（base_router.py:187-190）
	require.Equal(t, "B", resp["deployment_id"])

	// state 记账：A 失败 1 次并进入冷却；B 成功清零（py:523-529/499）
	require.Equal(t, 1, r.State.ConsecutiveFailures["A"])
	require.Equal(t, intelliroutercore.DeploymentStatusCooldown, r.State.DeploymentStatuses["A"])
	require.Equal(t, 0, r.State.ConsecutiveFailures["B"])
	require.Equal(t, 7, r.State.TotalTokens["B"]) // usage.completion_tokens

	// 事件序列：STARTED → RETRIED → SUCCEEDED（py:431-558）
	types := eventTypes(handler)
	require.Equal(t, []string{"request_started", "request_retried", "request_succeeded"}, types)

	// GetStats 导出 consecutive_failures（py:1712，23.4b 错误翻译层契约）
	stats := r.GetStats()
	require.Equal(t, 1, stats["consecutive_failures"].(map[string]any)["A"])
}

// TestReliableRouter_NoDeploymentAvailable 全冷却 → NoDeploymentAvailable（py:429-439）。
func TestReliableRouter_NoDeploymentAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, StrategyType: "ordered-failover"})
	// A 进入冷却
	r.State.OnFailure("A", nil, 60.0)
	_, err := r.Completion(context.Background(), "m", nil, nil, nil)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "No available deployment for model 'm'. No available deployments"))
}

// TestReliableRouter_RequestSideError_NotFallbackable 请求被拒不冷却不计数（review 7）。
func TestReliableRouter_RequestSideError_NotFallbackable(t *testing.T) {
	var calls atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(400) // 请求侧错误：4xx
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("ok", 1)))
	}))
	defer srvB.Close()
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	// 注意策略用显式非 strict 策略：ordered-failover 是 strict 策略
	//（strict_fallback_errors=True），non-fallbackable 的 400 会 fail-fast
	// 直接抛（py:531-543）——那是另一条路径，由 StrictFailFast 专测覆盖
	r := NewReliableRouter(RouterParams{
		Deployments: []*intelliroutercore.Deployment{dep(t, "A", srvA.URL), dep(t, "B", srvB.URL)},
		Strategy:    &seqStrategy{order: []string{"A", "B"}}, // 确定性：先 A 后 B
		NumRetries:  3,
		EventBus:    bus,
	})
	// 400 请求被拒：不 on_failure（不计失败、不冷却）但仍 fallback（py:523-529）
	resp, err := r.Completion(context.Background(), "m", nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"])
	require.NotContains(t, r.State.ConsecutiveFailures, "A") // 不计数
	require.NotContains(t, r.State.DeploymentStatuses, "A")  // 不冷却
	require.Equal(t, 1, int(calls.Load()))

	// reset 契约：ReleaseCooldown 后立即可选（23.4b _release_cooldown 目标）
	r.State.OnFailure("B", nil, 60.0)
	require.Equal(t, intelliroutercore.DeploymentStatusCooldown, r.State.DeploymentStatuses["B"])
	r.State.ResetDeployment("B")
	require.Equal(t, intelliroutercore.DeploymentStatusHealthy, r.State.DeploymentStatuses["B"])
}

// deltaOf 提取 OpenAI chunk 的 choices[0].delta。
func deltaOf(chunk map[string]any) map[string]any {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}
	delta, _ := choice["delta"].(map[string]any)
	return delta
}

// seqStrategy 按固定顺序选择部署的测试策略（非 strict，对齐 simple-shuffle 旗标）。
type seqStrategy struct {
	order []string
	idx   int
}

func (s *seqStrategy) SelectDeployment(deployments []*intelliroutercore.Deployment, _ *intelliroutercore.RoutingContext) *intelliroutercore.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	// 优先按 order 顺序取仍可用的第一个
	for _, id := range s.order {
		for _, d := range deployments {
			if d.ID == id {
				return d
			}
		}
	}
	return deployments[0]
}
func (s *seqStrategy) OnSuccess(*intelliroutercore.Deployment, float64, int) {}
func (s *seqStrategy) OnFailure(*intelliroutercore.Deployment, error)        {}
func (s *seqStrategy) StrictFallbackErrors() bool                            { return false }
func (s *seqStrategy) FallbackToFirstOnEmptySelection() bool                 { return true }

// TestReliableRouter_StrictFailFast ordered-failover strict 策略下 400 fail-fast（py:531-543）。
func TestReliableRouter_StrictFailFast(t *testing.T) {
	var calls atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(400) // non-fallbackable：请求侧 4xx
	}))
	defer srvA.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL)},
		StrategyType: "ordered-failover", // strict_fallback_errors=True（py:17）
	})
	_, err := r.Completion(context.Background(), "m", nil, nil, nil)
	require.Error(t, err)
	require.Equal(t, 1, int(calls.Load()))                   // fail-fast 不重试
	require.NotContains(t, r.State.ConsecutiveFailures, "A") // 请求侧不计数
}

// TestReliableRouter_StreamCompletion_FailoverBeforeFirstChunk 首可见前断连换部署。
func TestReliableRouter_StreamCompletion_FailoverBeforeFirstChunk(t *testing.T) {
	var calls atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500) // 请求建立即失败，未吐可见内容（py:385 fallbackable）
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sseResponse("hello")))
	}))
	defer srvB.Close()
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL), dep(t, "B", srvB.URL)},
		StrategyType: "ordered-failover",
		NumRetries:   3,
		EventBus:     bus,
	})
	var contents []string
	for sc := range r.StreamCompletion(context.Background(), "m", nil, nil) {
		if sc.Err != nil {
			t.Fatalf("unexpected error: %v", sc.Err)
		}
		if delta := deltaOf(sc.Chunk); delta != nil {
			if c, ok := delta["content"].(string); ok && c != "" {
				contents = append(contents, c)
			}
		}
	}
	t.Logf("events=%v", eventTypes(handler))
	require.Equal(t, 1, int(calls.Load()))
	require.Equal(t, []string{"hello"}, contents)
	require.Equal(t, 1, r.State.ConsecutiveFailures["A"]) // A 冷却
	require.Equal(t, 0, r.State.ConsecutiveFailures["B"])
}

// TestReliableRouter_StreamCompletion_NoFailoverAfterVisibleOutput 可见输出后断连不换部署（py:386/399）。
func TestReliableRouter_StreamCompletion_NoFailoverAfterVisibleOutput(t *testing.T) {
	var calls atomic.Int64
	// 上游吐可见内容后断流（无 [DONE]、无终结信号 → SSE 断流检测报错）
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte("data: " + chunkJSON(map[string]any{"role": "assistant"}) + "\n\n"))
		w.Write([]byte("data: " + chunkJSON(map[string]any{"content": "visible"}) + "\n\n"))
		// 连接在此关闭：断流检测触发（base_provider.py:197-204）
	}))
	defer srvA.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL)},
		StrategyType: "ordered-failover",
		NumRetries:   3,
	})
	var contents []string
	var streamErr error
	for sc := range r.StreamCompletion(context.Background(), "m", nil, nil) {
		if sc.Err != nil {
			streamErr = sc.Err
			break
		}
		if delta := deltaOf(sc.Chunk); delta != nil {
			if c, ok := delta["content"].(string); ok && c != "" {
				contents = append(contents, c)
			}
		}
	}
	require.Equal(t, 1, int(calls.Load()))          // 只尝试一次
	require.Equal(t, []string{"visible"}, contents) // 可见内容已收到
	require.Error(t, streamErr)                     // 断流错误上抛
	require.True(t, strings.Contains(streamErr.Error(), "stream ended without [DONE]"))
}

// TestReliableRouter_StreamQuotaAndEvents 流式用量收集与事件（py:837-843/898-903）。
func TestReliableRouter_StreamQuotaAndEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("data: " + chunkJSON(map[string]any{"role": "assistant"}) + "\n\n")
		b.WriteString("data: " + chunkJSON(map[string]any{"content": "hi"}) + "\n\n")
		b.WriteString(`data: {"id":"c1","model":"m","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":5}}` + "\n\n")
		b.WriteString("data: " + chunkJSON(map[string]any{}, "stop") + "\n\n")
		b.WriteString("data: [DONE]\n\n")
		w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srv.URL)},
		StrategyType: "ordered-failover",
		EventBus:     bus,
	})
	count := 0
	for sc := range r.StreamCompletion(context.Background(), "m", nil, nil) {
		require.NoError(t, sc.Err)
		count++
	}
	// STREAM_STARTED → STREAM_ROUTE_SELECTED → STREAM_SUCCEEDED
	require.Equal(t, []string{"stream_started", "stream_route_selected", "stream_succeeded"}, eventTypes(handler))
	require.Equal(t, 5, r.State.TotalTokens["A"]) // usage 收集（py:898-903 review 8）
	require.Equal(t, 1, r.State.TotalRequests["A"])
	_ = count
}

// TestReliableRouter_TagFilteredRouting tag-filtered 策略与 fallback_tag（py:35-52）。
func TestReliableRouter_TagFilteredRouting(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("from-a", 1)))
	}))
	defer srvA.Close()
	tag := "qwen-plus"
	dA, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: srvA.URL, ID: "A", Provider: "openai", FallbackTag: &tag,
	})
	dB, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: srvA.URL, ID: "B", Provider: "openai",
	})
	r := NewReliableRouter(RouterParams{
		Deployments:    []*intelliroutercore.Deployment{dA, dB},
		StrategyType:   "tag-filtered",
		StrategyKwargs: map[string]any{"fallback_tag": "qwen-plus"},
	})
	// 只命中带 tag 的部署（py:43-45）
	resp, err := r.Completion(context.Background(), "m", nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "A", resp["deployment_id"])
	// GetStats 显示 B 未被调度
	require.Equal(t, 0, r.State.TotalRequests["B"])
}

// TestReliableRouter_ReservedRequestDefaults request_defaults 保留键跳过部署（py:473-481）。
func TestReliableRouter_ReservedRequestDefaults(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srvA.Close()
	dA, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: srvA.URL, ID: "A", Provider: "openai",
		RequestDefaults: map[string]any{"model": "forbidden"}, // 保留键（py:225-231）
	})
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dA},
		StrategyType: "ordered-failover",
	})
	_, err := r.Completion(context.Background(), "m", nil, nil, nil)
	// 全部失败 → AllDeploymentsFailed（RouterError 子类，py:575）
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "reserved keys"))
}

// TestReliableRouter_QuotaSync tpm/rpm 配额接入（py:146-179）。
func TestReliableRouter_QuotaSync(t *testing.T) {
	tpm, rpm := 1000, 10
	d, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: "http://x", ID: "A", Provider: "openai", TPM: &tpm, RPM: &rpm,
	})
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{d}, StrategyType: "ordered-failover"})
	require.Equal(t, 1000, r.State.TokenUsage["A"].Limit)
	require.Equal(t, 10, r.State.RPMTrackers["A"].RPMLimit)
	// 配额移除 → 回 inf 语义（py:158-161）
	d2, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: "http://x", ID: "A", Provider: "openai",
	})
	r.UpdateDeployments([]*intelliroutercore.Deployment{d2})
	require.NotContains(t, r.State.TokenUsage, "A")
	require.Equal(t, 0, r.State.TotalTokens["A"]) // 累计值随 remove 清理（py:1083-1085）
}

// TestInvokeAndConverters invoke 类型化响应与转换器（py:1127-1186/1586-1699）。
func TestInvokeAndConverters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("answer", 4)))
	}))
	defer srv.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srv.URL)},
		StrategyType: "ordered-failover",
	})
	msg, err := r.Invoke(context.Background(), []map[string]any{{"role": "user", "content": "q"}}, InvokeParams{Model: "m"})
	require.NoError(t, err)
	require.Equal(t, "answer", msg.Content)
	require.Equal(t, "stop", msg.FinishReason)
	require.NotNil(t, msg.UsageMetadata)
	require.Equal(t, 4, msg.UsageMetadata.OutputTokens) // py:1553 completion_tokens
	require.Equal(t, 3, msg.UsageMetadata.InputTokens)
	require.Equal(t, "resp-1", *msg.ResponseID)
	// provider_content 保留原始响应（py:1626）
	require.Equal(t, "resp-1", msg.ProviderContent.(map[string]any)["id"])
}

// TestChunkConverters 流 chunk 转换与空白过滤（py:1634-1699）。
func TestChunkConverters(t *testing.T) {
	// 空白 chunk（无 content/finish/tool/usage）→ nil（py:1668-1675）
	require.Nil(t, chunkToAssistantMessageChunk(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}}},
	}))
	// usage-only chunk（无 choices）→ usage chunk（py:1637-1649）
	got := chunkToAssistantMessageChunk(map[string]any{
		"model": "m", "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 2},
	})
	require.NotNil(t, got)
	require.NotNil(t, got.UsageMetadata)
	require.Equal(t, 3, got.UsageMetadata.TotalTokens)
	// content chunk
	got = chunkToAssistantMessageChunk(map[string]any{
		"id": "c1", "model": "m",
		"choices": []any{map[string]any{"delta": map[string]any{"content": "x"}, "finish_reason": nil}},
	})
	require.Equal(t, "x", got.Content)
	require.Equal(t, "c1", *got.ResponseID)
	require.Nil(t, got.FinishReason)
	// tool_call 增量 chunk index 保留（py:1688 无兜底）
	got = chunkToAssistantMessageChunk(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{
			"tool_calls": []any{map[string]any{"index": 2, "id": "t", "function": map[string]any{"name": "f", "arguments": "{}"}}},
		}}},
	})
	require.NotNil(t, got.ToolCalls)
	require.Equal(t, 2, *got.ToolCalls[0].Index)
}

// TestBatchAndFallback 钩子覆写验证（py:206-212 review 16）。
func TestBatchAndFallback(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("b", 1)))
	}))
	defer srvB.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL), dep(t, "B", srvB.URL)},
		StrategyType: "ordered-failover",
	})
	// CompletionWithFallback：候选经 _available_deployments_for_model（state 过滤）
	resp, err := r.CompletionWithFallback(context.Background(), "m", nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "B", resp["deployment_id"])
}

// TestCreateStrategy 策略工厂与未知策略（strategy/__init__.py）。
func TestCreateStrategy(t *testing.T) {
	s, err := createStrategy("ordered-failover", nil)
	require.NoError(t, err)
	require.True(t, s.StrictFallbackErrors())
	_, err = createStrategy("adaptive", nil) // D-1 裁剪
	require.ErrorContains(t, err, "Unknown strategy: adaptive")
}

// TestSessionIdNotLeaked 路由专用参数不进 provider body（py:60-68 review 9）。
func TestSessionIdNotLeaked(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(completionJSON("ok", 1)))
	}))
	defer srv.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srv.URL)},
		StrategyType: "ordered-failover",
	})
	_, err := r.Completion(context.Background(), "m", nil, nil,
		map[string]any{"session_id": "s1", "tags": []string{"x"}, "temperature": 0.5})
	require.NoError(t, err)
	_, hasSession := body["session_id"]
	require.False(t, hasSession) // 路由专用键剔除（py:221-224/232-235）
	_, hasTags := body["tags"]
	require.False(t, hasTags)
	require.Equal(t, 0.5, body["temperature"]) // 普通参数透传
	_ = strategy.NewOrderedFailover
}

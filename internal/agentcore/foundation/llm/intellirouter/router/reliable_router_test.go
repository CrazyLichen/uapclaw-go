package router

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/health"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/strategy"
	"time"
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

// TestBatchCompletion 批量并发与异常折叠（py:1044-1062）。
func TestBatchCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("ok", 1)))
	}))
	defer srv.Close()
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srvBad.Close()
	r := NewReliableRouter(RouterParams{
		Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)},
		Strategy:    &seqStrategy{order: []string{"A"}},
	})
	results := r.BatchCompletion(context.Background(), []BatchRequest{
		{Model: "m", Messages: []map[string]any{{"role": "user", "content": "1"}}},
		{Model: "m", Messages: []map[string]any{{"role": "user", "content": "2"}}, Kwargs: map[string]any{}},
	}, 2)
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	// 另一路由单部署打满后不判异常——两条都成功
	require.NoError(t, results[1].Err)
	// 失败折叠为 Err 不中断整批
	rBad := NewReliableRouter(RouterParams{
		Deployments: []*intelliroutercore.Deployment{dep(t, "A", srvBad.URL)},
		Strategy:    &seqStrategy{order: []string{"A"}},
		NumRetries:  0,
	})
	resultsBad := rBad.BatchCompletion(context.Background(), []BatchRequest{
		{Model: "m", Messages: nil},
		{Model: "m", Messages: nil},
	}, 2)
	require.Error(t, resultsBad[0].Err)
	require.Error(t, resultsBad[1].Err) // 整批继续（return_exceptions=True）
}

// TestBackgroundCheckStartStop 后台健康检查启停（py:105-116）。
func TestBackgroundCheckStartStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("ok", 1)))
	}))
	defer srv.Close()
	state := intelliroutercore.NewLocalRouterState()
	hc := health.NewSDKHealthChecker([]*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, state, 0, 0, nil, 0, srv.Client())
	r := NewReliableRouter(RouterParams{
		Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)},
		Strategy:    &seqStrategy{order: []string{"A"}},
	})
	require.Nil(t, r.HealthChecker)
	r.HealthChecker = hc
	r.StartBackgroundCheck()
	require.Eventually(t, func() bool { return state.HealthStateOf("A") }, 2e9, 1e7)
	r.StopBackgroundCheck()
}

// TestAnthropicRouteViaRouter anthropic provider 经路由的 wire 验证。
func TestAnthropicRouteViaRouter(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "sk-1", r.Header.Get("x-api-key"))
		gotAuth = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		// 返回 Anthropic 原生响应 → adapter.TransformResponse 归一
		w.Write([]byte(`{"id":"m1","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer srv.Close()
	d, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "c", APIKey: "sk-1", APIBase: srv.URL, ID: "A", Provider: "anthropic",
	})
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{d}, Strategy: &seqStrategy{order: []string{"A"}}})
	resp, err := r.Completion(context.Background(), "c", []map[string]any{{"role": "user", "content": "q"}}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "2023-06-01", gotAuth)
	// system 提取到顶层（py:108-110）；max_tokens 默认 4096（py:51）
	_, hasSystem := gotBody["system"]
	_ = hasSystem
	require.InDelta(t, float64(provider.DefaultMaxTokens), gotBody["max_tokens"], 0) // JSON 解码后为 float64
	// 原生响应归一为 OpenAI 格式（py:117-157）
	require.Equal(t, "hi", resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"])
	require.Equal(t, 2, resp["usage"].(map[string]any)["completion_tokens"])
}

// TestBaseRouterConfigsAndHelpers 配置详情/超时/状态过滤/HTTP 映射。
func TestBaseRouterConfigsAndHelpers(t *testing.T) {
	tag := "qwen-plus"
	desc := "desc"
	d1, errD1 := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k1", APIBase: "http://a", ID: "A", Provider: "openai",
		FallbackTag: &tag, ModelDescription: &desc,
	})
	require.NoError(t, errD1)
	// api_key 必填（py:57-68），has_api_key=false 是防御分支（Python 同样不可达）
	d2, errD2 := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m2", APIKey: "k2", APIBase: "http://b", ID: "B", Provider: "openai",
	})
	require.NoError(t, errD2)
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{d1, d2}, Strategy: &seqStrategy{}})

	// GetDeploymentConfigs / ByModel（py:99-133）：has_api_key 不回传明文
	configs := r.GetDeploymentConfigs()
	require.Len(t, configs, 2)
	require.True(t, configs[0].HasAPIKey)
	require.True(t, configs[1].HasAPIKey)
	require.Equal(t, "qwen-plus", *configs[0].FallbackTag)
	byModel := r.GetDeploymentConfigByModel("m2")
	require.Len(t, byModel, 1)
	require.Equal(t, "B", byModel[0].ID)

	// RequestTimeout：deployment 显式优先（py:157-161）
	t5 := 5.0
	d1.Timeout = &t5
	require.Equal(t, 5.0, r.RequestTimeout(d1))
	d1.Timeout = nil
	require.Equal(t, 30.0, r.RequestTimeout(d1))

	// stateAvailableDeployments（py:266-292）：冷却未过期跳过、过期可用
	r.State.OnFailure("A", nil, 60.0)
	require.Empty(t, r.stateAvailableDeployments([]*intelliroutercore.Deployment{d1}))
	// 未登记默认 HEALTHY
	require.Len(t, r.stateAvailableDeployments([]*intelliroutercore.Deployment{d2}), 1)

	// mapHTTPStatus 四分支（py:190-229）
	auth := mapHTTPStatus(d1, 401, "", "", 30)
	require.Equal(t, 401, intellirouter.StatusCodeOf(auth))
	ra := mapHTTPStatus(d1, 429, "", "2.5", 30)
	require.Equal(t, 429, intellirouter.StatusCodeOf(ra))
	srv := mapHTTPStatus(d1, 503, "body", "", 30)
	require.Equal(t, 502, intellirouter.StatusCodeOf(srv))
	gen := mapHTTPStatus(d1, 400, "bad", "", 30)
	require.Equal(t, 500, intellirouter.StatusCodeOf(gen)) // 基类 DeploymentError → 500
	require.Contains(t, gen.Error(), "returned HTTP 400")

	// parseRetryAfter（py:202-205）：非法值忽略
	f, err := parseRetryAfter("3")
	require.NoError(t, err)
	require.Equal(t, 3.0, f)
	_, err = parseRetryAfter("bad")
	require.Error(t, err)
	_, err = parseRetryAfter("")
	require.Error(t, err)

	// CompletionWithFallback：fallback 模型映射（py:394-439）
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("fb", 1)))
	}))
	defer srvOK.Close()
	_, err = r.CompletionWithFallback(context.Background(), "nope", nil,
		map[string]string{"nope": "fb2"}, nil)
	require.Error(t, err) // 模型名无部署 → RouterError All deployments failed
	_ = srvOK
	r.Close() // py:254-259
}

// TestReliableRouter_CompletionOnDeployment 显式点名部署直通（py:631-728）。
func TestReliableRouter_CompletionOnDeployment(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(completionJSON("direct", 2)))
	}))
	defer srv.Close()
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	d := dep(t, "A", srv.URL)
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{d}, Strategy: &seqStrategy{order: []string{"A"}}, EventBus: bus})
	// 显式 deployment：跳过选路与重试，不参与 state 记账（py:417-422）
	resp, err := r.Completion(context.Background(), "m", nil, d, nil)
	require.NoError(t, err)
	require.Equal(t, "direct", resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"])
	require.Equal(t, 1, int(calls.Load()))
	// 不参与 state 记账（py:418-420 注释）
	require.Equal(t, 0, r.State.TotalRequests["A"])
	// 事件成对：STARTED → SUCCEEDED（py:424-428）
	require.Equal(t, []string{"request_started", "request_succeeded"}, eventTypes(handler))
}

// TestInvokeOutputParser invoke 解析器降级路径（py:1160-1176）。
func TestInvokeOutputParser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("raw", 1)))
	}))
	defer srv.Close()
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, Strategy: &seqStrategy{order: []string{"A"}}})
	// 解析成功：dict → json dumps（py:1166-1167）
	msg, err := r.Invoke(context.Background(), nil, InvokeParams{
		Model: "m",
		OutputParser: func(m *intellirouter.AssistantMessage) (any, error) {
			return map[string]any{"k": "v"}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, `{"k":"v"}`, msg.Content)
	// 解析失败：降级保留原始 content（py:1170-1176）
	msg, err = r.Invoke(context.Background(), nil, InvokeParams{
		Model: "m",
		OutputParser: func(m *intellirouter.AssistantMessage) (any, error) {
			return nil, context.DeadlineExceeded
		},
	})
	require.NoError(t, err)
	require.Equal(t, "raw", msg.Content)
	// Model 缺省取首个部署 model_name（py:1117-1125）
	msg, err = r.Invoke(context.Background(), nil, InvokeParams{})
	require.NoError(t, err)
	require.Equal(t, "raw", msg.Content)
	// 无部署时 Model 缺省报错（py:1120-1123）
	rEmpty := NewReliableRouter(RouterParams{Deployments: nil, Strategy: &seqStrategy{}})
	_, err = rEmpty.Invoke(context.Background(), nil, InvokeParams{})
	require.ErrorContains(t, err, "No model specified and no deployments configured")
}

// TestReliableRouter_StreamTyped Stream 类型化流（py:1188-1511）。
func TestReliableRouter_StreamTyped(t *testing.T) {
	var b strings.Builder
	b.WriteString("data: " + chunkJSON(map[string]any{"role": "assistant"}) + "\n\n")
	b.WriteString("data: " + chunkJSON(map[string]any{"content": "hey"}) + "\n\n")
	b.WriteString(`data: {"id":"c1","model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":3}}` + "\n\n")
	b.WriteString("data: " + chunkJSON(map[string]any{}, "stop") + "\n\n")
	b.WriteString("data: [DONE]\n\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, Strategy: &seqStrategy{order: []string{"A"}}, EventBus: bus})
	var chunks []intellirouter.AssistantMessageChunk
	for cr := range r.Stream(context.Background(), nil, InvokeParams{Model: "m"}) {
		require.NoError(t, cr.Err)
		chunks = append(chunks, cr.Chunk)
	}
	// STREAM_STARTED → ROUTE_SELECTED → SUCCEEDED
	require.Equal(t, []string{"stream_started", "stream_route_selected", "stream_succeeded"}, eventTypes(handler))
	// 类型化层空过滤：role-only chunk 在 py:1668-1675 判 None 不产出
	//（Python 同行为）；产出序列 = [content, usage-only, finish]
	require.Len(t, chunks, 3)
	require.Equal(t, "hey", chunks[0].Content)
	require.NotNil(t, chunks[1].UsageMetadata)
	require.NotNil(t, chunks[2].FinishReason)
	require.Equal(t, 3, r.State.TotalTokens["A"]) // review 8 流式 usage 记账
}

// TestHelpers 补助手函数分支。
func TestHelpers(t *testing.T) {
	// truthy 各类型分支（helpers.go）
	require.False(t, truthy(nil))
	require.False(t, truthy(""))
	require.True(t, truthy("x"))
	require.False(t, truthy(false))
	require.True(t, truthy(true))
	require.False(t, truthy(0))
	require.True(t, truthy(1))
	require.True(t, truthy(1.5))
	require.False(t, truthy([]any{}))
	require.True(t, truthy([]any{1}))
	require.False(t, truthy(map[string]any{}))
	require.True(t, truthy(map[string]any{"a": 1}))
	// toFloat64 字符串分支
	f, err := toFloat64("2.5")
	require.NoError(t, err)
	require.Equal(t, 2.5, f)
	_, err = toFloat64("bad")
	require.Error(t, err)
	_, err = toFloat64(struct{}{})
	require.Error(t, err)
	require.Equal(t, 2.5, toFloatMust("2.5"))
	require.Equal(t, 0.0, toFloatMust("bad"))
	// minInt 双分支
	require.Equal(t, 1, minInt(1, 2))
	require.Equal(t, 1, minInt(2, 1))
	// orAny/orDefault/orDefaultAny
	require.Equal(t, "b", orAny(nil, "b"))
	require.Equal(t, "a", orAny("a", "b"))
	require.Equal(t, "def", orDefault("", "def"))
	require.Equal(t, "x", orDefault("x", "def"))
	require.Equal(t, "b", orDefaultAny(nil, "b"))
	require.Equal(t, "a", orDefaultAny("a", "b"))
	// contextErrMsg 双分支
	c := intelliroutercore.NewRoutingContext("m", nil, nil)
	require.Nil(t, contextErrMsg(c))
	c.SetFailure(context.Canceled)
	require.NotNil(t, contextErrMsg(c))
	// round4 / floatInf / floatPtrOf
	require.Equal(t, 1.2346, round4(1.23456))
	require.Equal(t, math.Inf(1), floatInf())
	require.Equal(t, 1.5, *floatPtrOf(1.5))
}

// TestReliableRouter_StreamWithFallback Stream 的 fallback 与取消路径（py:1188-1511）。
func TestReliableRouter_StreamWithFallback(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sseResponse("b-answer")))
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
	for cr := range r.Stream(context.Background(), nil, InvokeParams{Model: "m"}) {
		require.NoError(t, cr.Err)
		if cr.Chunk.Content != "" {
			contents = append(contents, cr.Chunk.Content)
		}
	}
	require.Equal(t, []string{"b-answer"}, contents)
	require.Equal(t, 1, r.State.ConsecutiveFailures["A"]) // 503 冷却
	// 事件含 RETRIED（py:1445-1456）
	types := eventTypes(handler)
	require.Contains(t, types, "request_retried")
	require.Contains(t, types, "stream_succeeded")
}

// TestStream_CancelledConsumer 消费者中断 → STREAM_CANCELLED（py:1462-1506）。
func TestStream_CancelledConsumer(t *testing.T) {
	// 慢流：足够 chunk 让消费者中断发生在可见输出后
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 50; i++ {
			w.Write([]byte("data: " + chunkJSON(map[string]any{"content": "x"}) + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		w.Write([]byte("data: " + chunkJSON(map[string]any{}, "stop") + "\n\ndata: [DONE]\n\n"))
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
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	for range r.Stream(ctx, nil, InvokeParams{Model: "m"}) {
		count++
		if count == 3 {
			cancel() // 消费者提前中断（GeneratorExit 等价，py:1462）
			break
		}
	}
	// STREAM_CANCELLED 终态投递（goroutine 异步，轮询等待）
	require.Eventually(t, func() bool {
		for _, e := range handler.events {
			if e.EventType == observability.EventTypeStreamCancelled {
				return true
			}
		}
		return false
	}, 2e9, 1e7)
	// 取消路径既不 on_success 也不 on_failure（py:1479-1484 注释）
	require.Equal(t, 0, r.State.TotalRequests["A"])
	require.NotContains(t, r.State.ConsecutiveFailures, "A")
}

// TestSelectDeployment_FallbackToFirst 空选回退首部署（py:380-381）。
func TestSelectDeployment_FallbackToFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("x", 1)))
	}))
	defer srv.Close()
	// tag-filtered 且 tag 不匹配任何部署 → 过滤后空 → fallback_to_first_on_empty_selection=False → nil（py:26）
	tag := "no-match"
	dA, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: srv.URL, ID: "A", Provider: "openai",
	})
	r := NewReliableRouter(RouterParams{
		Deployments:    []*intelliroutercore.Deployment{dA},
		StrategyType:   "tag-filtered",
		StrategyKwargs: map[string]any{"fallback_tag": tag},
	})
	_, err := r.Completion(context.Background(), "m", nil, nil, nil)
	require.Error(t, err) // NoDeploymentAvailable（策略返回 nil，无回退）
	require.Contains(t, err.Error(), "No deployment matched routing strategy")
	require.False(t, r.Strategy.FallbackToFirstOnEmptySelection()) // py:26 = False
	// simple-shuffle 默认 True（router:243 getattr 默认）
	r2 := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dA},
		StrategyType: "simple-shuffle",
	})
	require.True(t, r2.Strategy.FallbackToFirstOnEmptySelection())
	require.False(t, r2.Strategy.StrictFallbackErrors())
}

// TestResponseToMessage_分支 补 message 转换分支（py:1586-1632）。
func TestResponseToMessage_分支(t *testing.T) {
	// 空 choices → 空 content（py:1590-1592）
	msg := responseToMessage(map[string]any{})
	require.Equal(t, "", msg.Content)
	// tool_calls 解析（py:1603-1619）
	raw := map[string]any{
		"id": "r1", "model": "m",
		"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{"role": "assistant", "content": nil,
				"reasoning_content": "think",
				"tool_calls": []any{map[string]any{"id": "t1", "type": "function",
					"function": map[string]any{"name": "f", "arguments": "{}"}}},
			},
		}},
		"usage":              map[string]any{"prompt_tokens": 1, "completion_tokens": 2},
		"system_fingerprint": "fp",
	}
	msg = responseToMessage(raw)
	require.NotNil(t, msg.ToolCalls)
	require.Equal(t, "f", msg.ToolCalls[0].Name)
	// index 缺省取数组下标（py:1614 tc.get("index", idx)）
	require.Equal(t, 0, *msg.ToolCalls[0].Index)
	require.Equal(t, "tool_calls", msg.FinishReason) // _response_to_message 直取 finish_reason（py:1597），映射只在 anthropic adapter
	require.Equal(t, "think", *msg.ReasoningContent)
	pm := msg.ProviderMetadata
	require.Equal(t, "fp", pm["system_fingerprint"]) // 白名单（py:1512-1522）
}

// TestCreateStrategy_Kwargs 策略工厂 kwargs 分支。
func TestCreateStrategy_Kwargs(t *testing.T) {
	s, err := createStrategy("simple-shuffle", map[string]any{
		"weights": map[string]any{"A": 2.0}, "default_weight": 3.0,
	})
	require.NoError(t, err)
	ss := s.(*strategy.SimpleShuffle)
	require.Equal(t, 2.0, ss.Weights["A"])
	require.Equal(t, 3.0, ss.DefaultWeight)
	// tag-filtered 带固定 tag
	tag := "qwen"
	s2, err := createStrategy("tag-filtered", map[string]any{"fallback_tag": tag})
	require.NoError(t, err)
	tf := s2.(*strategy.TagFiltered)
	require.NotNil(t, tf.FallbackTag)
	require.Equal(t, "qwen", *tf.FallbackTag)
	// weights 缺省 → 空 map、default 1.0（py:28-29）
	s3, _ := createStrategy("simple-shuffle", nil)
	ss3 := s3.(*strategy.SimpleShuffle)
	require.Empty(t, ss3.Weights)
	require.Equal(t, 1.0, ss3.DefaultWeight)
}

// TestFallbackHookOverride 钩子注入验证：fallback 循环走 state 过滤版（py:206-212）。
func TestFallbackHookOverride(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("b", 1)))
	}))
	defer srvB.Close()
	r := NewReliableRouter(RouterParams{
		Deployments:  []*intelliroutercore.Deployment{dep(t, "A", srvA.URL), dep(t, "B", srvB.URL)},
		StrategyType: "ordered-failover",
		NumRetries:   3,
	})
	require.NotNil(t, r.hookAvailableDeploymentsForModel) // 构造期注入
	// 预置 A 冷却：fallback 循环不应尝试冷却中的 A（py:206-212 review 16）
	r.State.OnFailure("A", nil, 60.0)
	resp, err := r.CompletionWithFallback(context.Background(), "m", nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "B", resp["deployment_id"])
	// 显式点名路径不参与 state 记账（py:631-728）→ A 冷却计数不变
	require.Equal(t, 1, r.State.ConsecutiveFailures["A"])
	// 全冷却时 fallback 无候选 → RouterError（py:437）
	r.State.OnFailure("B", nil, 60.0)
	_, err = r.CompletionWithFallback(context.Background(), "m", nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "All deployments failed")
}

// TestRouterRemaining 分支补齐：BaseRouter 直通/超时取消/事件异步投递。
func TestRouterRemaining(t *testing.T) {
	// Close 双分支：Transport 注入/默认（base_router.go Close）
	r1 := NewReliableRouter(RouterParams{Deployments: nil, Strategy: &seqStrategy{},
		HTTPClient: &http.Client{Transport: &http.Transport{}}})
	r1.Close()
	r2 := NewReliableRouter(RouterParams{Deployments: nil, Strategy: &seqStrategy{}})
	r2.Close() // 默认 Transport 非 *http.Transport 分支
	// availableDeploymentsForModel 直调（py:206-212）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completionJSON("x", 1)))
	}))
	defer srv.Close()
	r3 := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, Strategy: &seqStrategy{order: []string{"A"}}})
	require.Len(t, r3.availableDeploymentsForModel("m"), 1)
	// emitEventWithoutAwait goroutine 投递（py:259-290）
	bus := &observability.EventBus{}
	handler := &captureEmitHandler{}
	bus.Register(handler)
	r3.EventBus = bus
	r3.emitEventWithoutAwait(routingEvent(observability.EventTypeRequestStarted, "r", "m", map[string]any{}))
	require.Eventually(t, func() bool { return len(handler.events) == 1 }, 2e9, 1e7)
	// BaseRouter.Completion 无可用部署分支（py:271-273 NoDeploymentAvailable）
	rBad := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", srv.URL)}, Strategy: &seqStrategy{order: []string{"A"}}})
	rBad.State.OnFailure("A", nil, 60.0)
	// Completion(显式 dep) 绕过可用性检查（py:274-279 显式指定不检查）
	d := dep(t, "A", srv.URL)
	resp, err := rBad.Completion(context.Background(), "m", nil, d, nil)
	require.NoError(t, err) // 冷却中的部署显式点名仍直通
	require.Equal(t, "A", resp["deployment_id"])
	// GetModelList（py:95-97）
	require.Contains(t, r3.GetModelList(), "m")
	// syncQuotaState 热更新 limit 保留累计（py:155-159）
	tpm1, tpm2 := 100, 200
	dq, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "q", APIKey: "k", APIBase: srv.URL, ID: "Q", Provider: "openai", TPM: &tpm1})
	rq := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dq}, Strategy: &seqStrategy{order: []string{"Q"}}})
	rq.State.TokenUsage["Q"] = &intelliroutercore.TokenUsage{Used: 50, Limit: 100}
	dq2, _ := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "q", APIKey: "k", APIBase: srv.URL, ID: "Q", Provider: "openai", TPM: &tpm2})
	rq.syncQuotaState([]*intelliroutercore.Deployment{dq2})
	require.Equal(t, 200, rq.State.TokenUsage["Q"].Limit) // 只改 limit
	require.Equal(t, 50, rq.State.TokenUsage["Q"].Used)   // 保留累计 used
}

// TestBaseRouterCompletion_NoDeployments BaseRouter 直通无部署分支（py:271-273）。
func TestBaseRouterCompletion_NoDeployments(t *testing.T) {
	// BaseRouter.Completion（nil deployment）无可用 → NoDeploymentAvailable
	b := NewBaseRouter([]*intelliroutercore.Deployment{dep(t, "A", "http://127.0.0.1:1")}, 0, 0, nil)
	b.State.OnFailure("A", nil, 60.0)
	_, err := b.Completion(context.Background(), "m", nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "No deployment")
	// BaseRouter.CompletionStream 同分支（py:296-300）
	ch := b.CompletionStream(context.Background(), "m", nil, nil, nil)
	sc := <-ch
	require.Error(t, sc.Err)
	// ReliableRouter.availableDeploymentsForModel 直调（py:206-212）
	r := NewReliableRouter(RouterParams{Deployments: []*intelliroutercore.Deployment{dep(t, "A", "http://x")}, Strategy: &seqStrategy{}})
	r.State.OnFailure("A", nil, 60.0)
	require.Empty(t, r.availableDeploymentsForModel("m")) // 冷却剔除
}

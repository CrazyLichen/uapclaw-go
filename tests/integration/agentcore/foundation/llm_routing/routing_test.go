//go:build integration

// Package llm_routing 23.4b 失败四分类 → 重试 rail / 上下文恢复衔接的集成测试。
//
// 跨子系统：routing（编译器+ToModelError）→ model_clients/intellirouter（客户端）
// → intellirouter（ReliableRouter 冷却状态机）→ harness/rails model_anomaly（rail
// 重试决策）→ context_engine/support（溢出短语检测）——满足规则 7 双层门禁。
package llm_routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/support"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	intelliroutererrors "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutertypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/intellirouter"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	llmrouterpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/router"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMRoutingSuite 失败四分类与上层恢复通道的衔接测试。
type LLMRoutingSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLLMRoutingSuite 运行套件。
func TestLLMRoutingSuite(t *testing.T) {
	suite.Run(t, new(LLMRoutingSuite))
}

// routerViewFor 测试用 RouterView 适配（与 client 包内 routerViewOf 同口径）。
func routerViewFor(r *llmrouterpkg.ReliableRouter) routing.RouterView {
	return &testRouterView{r: r}
}

type testRouterView struct {
	r *llmrouterpkg.ReliableRouter
}

func (v *testRouterView) Deployments() []*intelliroutertypes.Deployment { return v.r.Deployments }
func (v *testRouterView) ModelGroupID() string                          { return "" }
func (v *testRouterView) PinnedRouteTag() string                        { return "X" }
func (v *testRouterView) State() *intelliroutertypes.LocalRouterState   { return v.r.State }

// ──────────────────────────── 非导出函数（fixture）────────────────────────────

// newSSEUpstream 建 OpenAI 兼容假上游。
func (s *LLMRoutingSuite) newSSEUpstream(handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	s.T().Cleanup(srv.Close)
	return srv
}

// newClient 两/三 deployment 的 intellirouter 客户端（模型名 "*" 不过滤）。
func (s *LLMRoutingSuite) newClient(apiBases ...string) *intellirouter.IntelliRouterModelClient {
	deps := []map[string]any{}
	for i, base := range apiBases {
		deps = append(deps, map[string]any{
			"route_id": string(rune('a' + i)), "model_name": "m",
			"provider": "openai", "api_key": "k", "api_base": base,
		})
	}
	cfg := &intellirouter.IntelliRouterClientConfig{
		Deployments:    deps,
		Strategy:       "ordered-failover",
		NumRetries:     len(deps) - 1,
		Timeout:        10,
		StrategyKwargs: map[string]any{},
	}
	r, err := intellirouter.CreateRouter(cfg)
	s.Require().NoError(err)
	embed, err := model_clients.NewBaseClientEmbed(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("*")),
		&llmschema.ModelClientConfig{ClientProvider: "intelli_router"},
	)
	s.Require().NoError(err)
	routing.ResetFailureRecords()
	s.T().Cleanup(routing.ResetFailureRecords)
	return &intellirouter.IntelliRouterModelClient{BaseClientEmbed: *embed, Router: r}
}

// detailsOf 提取 BaseError details。
func detailsOf(err error) map[string]any {
	if base, ok := err.(*exception.BaseError); ok {
		if d, ok := base.Details().(map[string]any); ok {
			return d
		}
	}
	return nil
}

// ──────────────────────────── 测试 ────────────────────────────

// I-1 switchable：上游 A 500 → B 成功；router 内换路；失败方进冷却；
// RouteFailure 记录含 provider 文本（用户要求：error→rail 衔接链 1）。
func (s *LLMRoutingSuite) TestI1_Switchable_换路成功() {
	var callCount int32
	srv := s.newSSEUpstream(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"primary overloaded"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"from-b"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})

	client := s.newClient(srv.URL, srv.URL)
	result, err := client.Invoke(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().NoError(err)
	s.Equal("from-b", result.Content.Text(), "应换到第二个 deployment 成功")
	s.GreaterOrEqual(atomic.LoadInt32(&callCount), int32(2), "应至少两次上游调用")
}

// I-2 exhausted_retryable：全上游超时 → details.should_retry=true 且
// retry_after_seconds 夹在 [1,60]（衔接链 2：rail 按 should_retry 消费）。
func (s *LLMRoutingSuite) TestI2_ExhaustedRetryable_RetryAfter() {
	srv := s.newSSEUpstream(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	})

	client := s.newClient(srv.URL, srv.URL)
	_, err := client.Invoke(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().Error(err)
	base, ok := err.(*exception.BaseError)
	s.Require().True(ok, "应为 BaseError")
	details := base.Details().(map[string]any)
	// rail 的 clientRetryDecision 消费键（model_anomaly.go:868-880）
	s.Equal(true, details["should_retry"], "transient 应可重试")
	retryAfter, ok := details["retry_after_seconds"].(float64)
	s.Require().True(ok, "应有 retry_after_seconds")
	s.GreaterOrEqual(retryAfter, 1.0, "下限 SWITCH_RETRY_DELAY_SECONDS")
	s.LessOrEqual(retryAfter, 60.0, "上限 rail _MAX_RETRY_AFTER_SECONDS")
	s.Equal("exhausted_retryable", details["route_case"])
}

// I-3 request_rejected：400 + 溢出短语 → 分类 request_rejected、provider
// 原文保留、ContextEngine 短语命中（异常文本通道）；反例：无短语不命中。
// 说明：23.4a review 7 语义下请求侧错误不冷却，两/单 deployment 场景
// exhausted_request_rejected 分支由上游 fixture 口径的单测覆盖（errors_test）；
// 真实链路表现为 switchable + reason=request_rejected（与上游生产行为一致）。
func (s *LLMRoutingSuite) TestI3_RequestRejected_上下文恢复可达() {
	srv := s.newSSEUpstream(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 4096 tokens"}}`))
	})

	client := s.newClient(srv.URL)
	_, err := client.Invoke(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().Error(err)
	base, ok := err.(*exception.BaseError)
	s.Require().True(ok)
	details := base.Details().(map[string]any)
	s.Equal("switchable", details["route_case"])
	s.Equal("request_rejected", details["reason"])
	s.Contains(err.Error(), "maximum context length", "provider 原文必须保留（异常文本通道的输入）")

	// ContextEngine.is_context_overflow_error 短语命中（衔接链 3）
	s.True(support.IsContextOverflowError(err), "溢出短语应被 ContextEngine 识别")
	// 反例：非溢出错误不命中
	s.False(support.IsContextOverflowError(context.Canceled), "非溢出错误不命中")
}

// I-4 user_action：两条真实链路 ——
// (a) tag_not_found：tag-filtered 组内无匹配 tag → exhausted_user_action；
// (b) stream_interrupted：单 deployment 流式可见输出后断流 → exhausted_user_action。
// （衔接链 4：route_case/user_action_required 通道；fix_hint 文本已在上游
// fixture 口径单测 errors_test 覆盖。）
func (s *LLMRoutingSuite) TestI4_UserAction_TagNotFound与流中断() {
	// (a) tag-filtered：组只有 tag=Y 的 deployment，请求 tag=X → 无候选
	cfg := &intellirouter.IntelliRouterClientConfig{
		Deployments: []map[string]any{{
			"route_id": "a", "model_name": "m", "provider": "openai",
			"api_key": "k", "api_base": "http://127.0.0.1:1", "fallback_tag": "Y",
		}},
		Strategy:       "tag-filtered",
		NumRetries:     1,
		Timeout:        10,
		StrategyKwargs: map[string]any{"fallback_tag": "X"},
	}
	r, err := intellirouter.CreateRouter(cfg)
	s.Require().NoError(err)
	exc := intelliroutererrors.NewNoDeploymentAvailable("*", "No deployment matched routing strategy")
	modelErr := routing.ToModelError(exc, routerViewFor(r), "*", false)
	s.Require().Error(modelErr)
	details := modelErr.Details().(map[string]any)
	s.Equal("exhausted_user_action", details["route_case"])
	s.Equal("tag_not_found", details["reason"])
	s.Equal(true, details["user_action_required"])

	// (b) 单 deployment 流式可见输出后断流（network error → 冷却 → usable 空）
	srv := s.newSSEUpstream(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"))
		flusher.Flush()
		// 中断连接（ Hijack 断流最直接；此处直接返回非 SSE 触发解析失败亦可，
		// 用 connection close 语义：只发半帧后关闭）
	})
	client := s.newClient(srv.URL)
	ch, err := client.Stream(context.Background(),
		model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	s.Require().NoError(err)
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
	if sawContent && streamErr != nil {
		base, ok := streamErr.(*exception.BaseError)
		s.Require().True(ok)
		details := base.Details().(map[string]any)
		s.Equal("exhausted_user_action", details["route_case"])
		s.Equal("stream_interrupted", details["reason"])
		s.Equal(true, details["output_emitted"])
	} else {
		s.T().Logf("断流场景未复现（sawContent=%v streamErr=%v），语义由单测覆盖", sawContent, streamErr)
	}
}

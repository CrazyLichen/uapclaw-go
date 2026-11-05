package rails

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewModelAnomalyDetectionRail_默认值 校验默认参数与上游一致（L329-391）。
func TestNewModelAnomalyDetectionRail_默认值(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if r.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", r.Priority())
	}
	if r.maxRetries != 2 || r.repeatMinPatternChars != 2 || r.repeatMaxPatternChars != 64 ||
		r.repeatMinCount != 6 || r.repeatMinTotalChars != 160 || r.repeatWindowChars != 1024 ||
		r.singleCharRepeatCount != 100 {
		t.Fatalf("默认参数与上游不一致: %+v", r)
	}
	if r.transientMaxRetries != 3 || r.transientBaseDelay != 2.0 {
		t.Fatalf("瞬时重试默认值与上游不一致: %d / %v", r.transientMaxRetries, r.transientBaseDelay)
	}
	if len(r.backoffSeconds) != 3 || r.backoffSeconds[0] != 0.5 || r.backoffSeconds[2] != 2.0 {
		t.Fatalf("退避表默认值应为 0.5/1/2，实际 %v", r.backoffSeconds)
	}
	if r.toolLoopCompact.Enabled {
		t.Fatal("工具循环压缩默认必须关闭（对齐上游 enabled=False）")
	}
	if r.toolLoopCompact.ConsecutiveThreshold != 4 || r.toolLoopCompact.BailoutThreshold != 3 {
		t.Fatalf("压缩阈值默认值应为 4/3，实际 %d/%d",
			r.toolLoopCompact.ConsecutiveThreshold, r.toolLoopCompact.BailoutThreshold)
	}
}

// TestNewModelAnomalyDetectionRail_十条校验 逐条覆盖上游 10 个 ValueError。
func TestNewModelAnomalyDetectionRail_十条校验(t *testing.T) {
	cases := []struct {
		name string
		opt  ModelAnomalyOption
		want string
	}{
		{"max_retries 为负", WithMaxRetries(-1), "max_retries must be >= 0"},
		{"transient_max_retries 为负", WithTransientMaxRetries(-1), "transient_max_retries must be >= 0"},
		{"base_delay 为负", WithTransientBaseDelaySeconds(-0.1), "transient_base_delay_seconds must be >= 0"},
		{"退避表含负数", WithBackoffSeconds([]float64{0.5, -1}), "backoff_seconds entries must be >= 0"},
		{"重复片段下限为 0", WithRepeatMinPatternChars(0), "repeat_min_pattern_chars must be >= 1"},
		{"上限小于下限", WithRepeatParams(8, 4, 6, 160, 1024), "repeat_max_pattern_chars must be >= repeat_min_pattern_chars"},
		{"重复次数为 1", WithRepeatMinCount(1), "repeat_min_count must be >= 2"},
		{"最小总长为 0", WithRepeatMinTotalChars(0), "repeat_min_total_chars must be >= 1"},
		{"单字符阈值为 1", WithSingleCharRepeatCount(1), "single_char_repeat_count must be >= 2"},
		{"窗口过小", WithRepeatWindowChars(100), "repeat_window_chars is too small"},
	}
	for _, c := range cases {
		_, err := NewModelAnomalyDetectionRail(c.opt)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 期望错误含 %q，实际 %v", c.name, c.want, err)
		}
	}
}

// TestNewModelAnomalyDetectionRail_退避表越界取末项 验证 backoff 语义（L408-415）。
func TestNewModelAnomalyDetectionRail_退避表越界取末项(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail(WithBackoffSeconds([]float64{0.5, 1.0}))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := r.BackoffDelay(0); got != 0.5 {
		t.Fatalf("第 0 次退避应为 0.5，实际 %v", got)
	}
	if got := r.BackoffDelay(5); got != 1.0 {
		t.Fatalf("越界应取末项 1.0，实际 %v", got)
	}
}

// ──────────────────────────── 重复检测测试辅助 ────────────────────────────

// newTestCBC 构造不带会话的空回调上下文。
func newTestCBC() *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
}

// feedChunk 向检查器投递一个分片。
func feedChunk(r *ModelAnomalyDetectionRail, cbc *agentinterfaces.AgentCallbackContext, reasoning, content string) error {
	return r.InspectStreamChunk(context.Background(), cbc,
		&llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{
			DefaultMessage:   llmschema.DefaultMessage{Content: llmschema.NewTextContent(content)},
			ReasoningContent: reasoning,
		}})
}

// noiseText 生成不含重复后缀的噪声文本，用于验证尾窗滚动。
func noiseText(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString(strconv.Itoa(i*7919 + 13))
		b.WriteByte(';')
	}
	return b.String()
}

// TestInspectStreamChunk_多字符重复触发 54 次 abc → 命中多字符检测（上游默认阈值）。
func TestInspectStreamChunk_多字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	var err error
	for i := 0; i < 54 && err == nil; i++ {
		err = feedChunk(r, cbc, "abc", "")
	}
	if err == nil {
		t.Fatal("54 次 abc 应触发重复检测")
	}
	msg := err.Error()
	for _, want := range []string{repeatErrorMarker, "field=reasoning_content", "unit='abc'", "repeat_count="} {
		if !strings.Contains(msg, want) {
			t.Errorf("文案缺少 %q: %s", want, msg)
		}
	}
}

// TestInspectStreamChunk_短重复不触发 5 次 abc（15 字符）不达总长阈值。
func TestInspectStreamChunk_短重复不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 5; i++ {
		if err := feedChunk(r, cbc, "", "abc"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
}

// TestInspectStreamChunk_单字符重复触发 100 个 '-' 命中单字符阈值。
func TestInspectStreamChunk_单字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	var err error
	for i := 0; i < 100 && err == nil; i++ {
		err = feedChunk(r, cbc, "", "-")
	}
	if err == nil {
		t.Fatal("100 个 '-' 应触发单字符重复检测")
	}
	if !strings.Contains(err.Error(), "unit='-'") {
		t.Fatalf("文案应含 unit='-'，实际 %s", err.Error())
	}
}

// TestInspectStreamChunk_空白后缀不触发 空白字符不参与重复判定。
func TestInspectStreamChunk_空白后缀不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 200; i++ {
		if err := feedChunk(r, cbc, "", " "); err != nil {
			t.Fatalf("空白重复不应触发: %v", err)
		}
	}
}

// TestInspectStreamChunk_两字段独立窗口 两个字段各自累计、互不影响。
func TestInspectStreamChunk_两字段独立窗口(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 3; i++ {
		if err := feedChunk(r, cbc, "xy", "zz"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
	raw, _ := cbc.GetExtra(modelAnomalyStateKey)
	state := raw.(*anomalyState)
	if state.content != "zzzzzz" || state.reasoningContent != "xyxyxy" {
		t.Fatalf("两字段应各自累计，实际 content=%q reasoning=%q", state.content, state.reasoningContent)
	}
}

// TestInspectStreamChunk_尾窗滚动仍能检出 噪声超过窗口后仍能检出尾部重复。
func TestInspectStreamChunk_尾窗滚动仍能检出(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	if err := feedChunk(r, cbc, "", noiseText(2000)); err != nil {
		t.Fatalf("噪声不应触发: %v", err)
	}
	var err error
	for i := 0; i < 60 && err == nil; i++ {
		err = feedChunk(r, cbc, "", "abc")
	}
	if err == nil {
		t.Fatal("滚动窗口内仍应检出")
	}
}

// ──────────────────────────── 瞬时失败分类测试 ────────────────────────────

// httpErr 构造带状态码与头部的 provider 错误。
func httpErr(status int, headers map[string]any, msg string) error {
	params := map[string]any{"http_status": status}
	for key, value := range headers {
		params[key] = value
	}
	return exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(msg), exception.WithParams(params))
}

// TestClassifyTransientFailure 覆盖上游分类矩阵（L195-242）。
func TestClassifyTransientFailure(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantRetry      bool
		wantStatus     int
		wantRetryAfter float64
	}{
		{"429 可重试", httpErr(429, nil, "too many requests"), true, 429, 0},
		{"408 可重试", httpErr(408, nil, "timeout"), true, 408, 0},
		{"409 可重试", httpErr(409, nil, "conflict"), true, 409, 0},
		{"500 可重试", httpErr(500, nil, "boom"), true, 500, 0},
		{"503 可重试", httpErr(503, nil, "unavailable"), true, 503, 0},
		{"400 不重试", httpErr(400, nil, "bad request"), false, 400, 0},
		{"401 不重试", httpErr(401, nil, "unauthorized"), false, 401, 0},
		{"无状态码（连接断）可重试", errors.New("connection reset by peer"), true, 0, 0},
		{"配额文本不重试", errors.New("Error: insufficient_quota"), false, 0, 0},
		{"上下文过长不重试", errors.New("maximum context length exceeded"), false, 0, 0},
		{"x-should-retry false 挡住 429", httpErr(429, map[string]any{"x_should_retry": "false"}, "429"), false, 429, 0},
		{"x-should-retry true 放行 400", httpErr(400, map[string]any{"x_should_retry": "true"}, "400"), true, 400, 0},
		{"retry-after 数字", httpErr(429, map[string]any{"retry_after": "3"}, "429"), true, 429, 3},
		{"retry-after-ms", httpErr(429, map[string]any{"retry_after_ms": "1500"}, "429"), true, 429, 1.5},
		{"retry-after 超上限不重试", httpErr(429, map[string]any{"retry_after": "120"}, "429"), false, 429, 120},
	}
	for _, c := range cases {
		decision := classifyTransientFailure(c.err)
		if decision.retryable != c.wantRetry {
			t.Errorf("%s: retryable 期望 %v，实际 %v", c.name, c.wantRetry, decision.retryable)
		}
		if c.wantStatus != 0 && (decision.statusCode == nil || *decision.statusCode != c.wantStatus) {
			t.Errorf("%s: status 期望 %d，实际 %v", c.name, c.wantStatus, decision.statusCode)
		}
		if c.wantRetryAfter != 0 {
			if decision.retryAfterSeconds == nil || *decision.retryAfterSeconds != c.wantRetryAfter {
				t.Errorf("%s: retryAfter 期望 %v，实际 %v", c.name, c.wantRetryAfter, decision.retryAfterSeconds)
			}
		}
	}
}

// TestClassifyTransientFailure_客户端决策优先 details.should_retry 覆盖文本与状态码判定。
func TestClassifyTransientFailure_客户端决策优先(t *testing.T) {
	disabled := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("connection reset"),
		exception.WithDetails(map[string]any{"should_retry": false}))
	if decision := classifyTransientFailure(disabled); decision.retryable {
		t.Fatal("details.should_retry=false 应优先")
	}

	enabled := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("boom"),
		exception.WithDetails(map[string]any{"should_retry": true, "retry_after_seconds": 1.5}))
	decision := classifyTransientFailure(enabled)
	if !decision.retryable || decision.retryAfterSeconds == nil || *decision.retryAfterSeconds != 1.5 {
		t.Fatalf("details.should_retry=true 应被采纳，实际 %+v", decision)
	}
}

// TestParseRetryAfterSeconds_HTTP日期 HTTP-date 形态换算为剩余秒数。
func TestParseRetryAfterSeconds_HTTP日期(t *testing.T) {
	when := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfterSeconds(map[string]string{"retry-after": when})
	if got == nil || *got < 3 || *got > 6 {
		t.Fatalf("HTTP-date 应解析为约 5s，实际 %v", got)
	}
}

// TestExceptionChain_沿Cause展开 BaseError.Cause 必须被 Unwrap 展开。
func TestExceptionChain_沿Cause展开(t *testing.T) {
	inner := errors.New("inner")
	middle := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("mid"), exception.WithCause(inner))
	outer := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("outer"), exception.WithCause(middle))
	chain := exceptionChain(outer)
	if len(chain) != 3 {
		t.Fatalf("异常链应为 3 层，实际 %d", len(chain))
	}
}

// ──────────────────────────── 重试记账测试 ────────────────────────────

// withException 把异常塞进回调上下文（OnModelException 从 ctx.Exception() 读取）。
func withException(cbc *agentinterfaces.AgentCallbackContext, err error) *agentinterfaces.AgentCallbackContext {
	cbc.SetException(err)
	return cbc
}

// newTestCBCWithSession 构造带会话 id 的回调上下文。
func newTestCBCWithSession(id string) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, newFakeSession(id))
}

// repeatErr 构造重复输出异常。
func repeatErr() error {
	return exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(repeatErrorMarker+": field=content"))
}

// TestOnModelException_重复异常按退避表重试 两次预算、耗尽归零后可重新获得。
func TestOnModelException_重复异常按退避表重试(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithBackoffSeconds([]float64{0.5, 1.0}))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	req := cbc.ConsumeRetryRequest()
	if req == nil || req.DelaySeconds != 0.5 {
		t.Fatalf("第 1 次应请求 0.5s 重试，实际 %+v", req)
	}

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	req = cbc.ConsumeRetryRequest()
	if req == nil || req.DelaySeconds != 1.0 {
		t.Fatalf("第 2 次应请求 1.0s 重试，实际 %+v", req)
	}

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	if req = cbc.ConsumeRetryRequest(); req != nil {
		t.Fatalf("超过 max_retries 后不应再重试，实际 %+v", req)
	}

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	if req = cbc.ConsumeRetryRequest(); req == nil {
		t.Fatal("计数归零后应重新重试")
	}
}

// TestOnModelException_流超时与重复各自计数 两类异常不共享预算。
func TestOnModelException_流超时与重复各自计数(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithMaxRetries(1), WithBackoffSeconds([]float64{0}))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	timeoutErr := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("LLM stream timeout: stream frame timeout: stage=idle_token"))

	_ = r.OnModelException(context.Background(), withException(cbc, timeoutErr))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("流超时第 1 次应重试")
	}
	_ = r.OnModelException(context.Background(), withException(cbc, timeoutErr))
	if cbc.ConsumeRetryRequest() != nil {
		t.Fatal("max_retries=1 时第 2 次不应重试")
	}

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("重复异常应使用独立计数")
	}
}

// TestOnModelException_已输出可见内容时不做瞬时重试 闸门只挡瞬时，不挡重复。
func TestOnModelException_已输出可见内容时不做瞬时重试(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)
	cbc.SetModelVisibleOutputEmitted(true)

	_ = r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if cbc.ConsumeRetryRequest() != nil {
		t.Fatal("已输出内容后不应做瞬时重试")
	}

	_ = r.OnModelException(context.Background(), withException(cbc, repeatErr()))
	if cbc.ConsumeRetryRequest() == nil {
		t.Fatal("重复异常不受可见输出闸门限制（对齐上游判定顺序）")
	}
}

// TestOnModelException_瞬时重试指数退避与耗尽 2s/4s 后耗尽。
func TestOnModelException_瞬时重试指数退避与耗尽(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithTransientMaxRetries(2), WithTransientBaseDelaySeconds(2))
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	_ = r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 2 {
		t.Fatalf("第 1 次应为 2s，实际 %+v", req)
	}
	_ = r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 4 {
		t.Fatalf("第 2 次应为 4s，实际 %+v", req)
	}
	_ = r.OnModelException(context.Background(), withException(cbc, httpErr(429, nil, "429")))
	if req := cbc.ConsumeRetryRequest(); req != nil {
		t.Fatal("耗尽后不应重试")
	}
}

// TestOnModelException_RetryAfter优先 服务端建议时长覆盖指数退避。
func TestOnModelException_RetryAfter优先(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	r.BeforeInvoke(context.Background(), cbc)

	_ = r.OnModelException(context.Background(), withException(cbc, httpErr(429, map[string]any{"retry_after": "7"}, "429")))
	if req := cbc.ConsumeRetryRequest(); req == nil || req.DelaySeconds != 7 {
		t.Fatalf("应使用 Retry-After=7，实际 %+v", req)
	}
}

// TestRetryCounters_按会话隔离且归零即删 分桶互不影响，AfterInvoke 释放条目。
func TestRetryCounters_按会话隔离且归零即删(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail(WithMaxRetries(1), WithBackoffSeconds([]float64{0}))
	cbcA := newTestCBCWithSession("session-a")
	cbcB := newTestCBCWithSession("session-b")
	r.BeforeInvoke(context.Background(), cbcA)
	r.BeforeInvoke(context.Background(), cbcB)

	_ = r.OnModelException(context.Background(), withException(cbcA, repeatErr()))
	cbcA.ConsumeRetryRequest()

	if r.counterValue("session-b", "repeat") != 0 {
		t.Fatal("会话 A 的计数不应影响会话 B")
	}

	r.AfterInvoke(context.Background(), cbcA)
	if _, ok := r.counters["session-a"]; ok {
		t.Fatal("AfterInvoke 应删除该会话条目")
	}
}

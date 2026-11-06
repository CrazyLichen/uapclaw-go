package rails

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
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
	_ = r.BeforeInvoke(context.Background(), cbc)

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
	_ = r.BeforeInvoke(context.Background(), cbc)

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
	_ = r.BeforeInvoke(context.Background(), cbc)
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
	_ = r.BeforeInvoke(context.Background(), cbc)

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
	_ = r.BeforeInvoke(context.Background(), cbc)

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
	_ = r.BeforeInvoke(context.Background(), cbcA)
	_ = r.BeforeInvoke(context.Background(), cbcB)

	_ = r.OnModelException(context.Background(), withException(cbcA, repeatErr()))
	cbcA.ConsumeRetryRequest()

	if r.counterValue("session-b", "repeat") != 0 {
		t.Fatal("会话 A 的计数不应影响会话 B")
	}

	_ = r.AfterInvoke(context.Background(), cbcA)
	if _, ok := r.counters["session-a"]; ok {
		t.Fatal("AfterInvoke 应删除该会话条目")
	}
}

// ──────────────────────────── 工具循环压缩测试 ────────────────────────────

// fakeToolLoopContext 最小 ModelContext，只实现 GetMessages/SetMessages。
type fakeToolLoopContext struct {
	ceinterface.ModelContext
	// msgs 当前上下文消息
	msgs []llmschema.BaseMessage
}

// GetMessages 返回全量消息（对齐 Python context.get_messages()）。
func (f *fakeToolLoopContext) GetMessages(int, bool) ([]llmschema.BaseMessage, error) {
	return f.msgs, nil
}

// SetMessages 覆盖消息列表。
func (f *fakeToolLoopContext) SetMessages(msgs []llmschema.BaseMessage, _ bool) {
	f.msgs = msgs
}

// toolRoundMessages 构造 rounds 轮相同工具调用（含结果）。
func toolRoundMessages(args string, rounds int) []llmschema.BaseMessage {
	messages := make([]llmschema.BaseMessage, 0, rounds*2)
	for i := 0; i < rounds; i++ {
		call := llmschema.NewToolCall(fmt.Sprintf("id-%d", i), "read_file", args)
		messages = append(messages,
			llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{call})),
			llmschema.NewToolMessage(fmt.Sprintf("id-%d", i), "content"))
	}
	return messages
}

// newCompactRail 构造启用压缩的 rail 与上下文。
func newCompactRail(t *testing.T, threshold, bailout int) (*ModelAnomalyDetectionRail, *agentinterfaces.AgentCallbackContext, *fakeToolLoopContext) {
	t.Helper()
	r, err := NewModelAnomalyDetectionRail(WithToolLoopCompact(ToolLoopCompactConfig{
		Enabled:              true,
		ConsecutiveThreshold: threshold,
		BailoutThreshold:     bailout,
	}))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	cbc := newTestCBC()
	mc := &fakeToolLoopContext{}
	cbc.SetModelContext(mc)
	return r, cbc, mc
}

// TestBeforeModelCall_安装检查器且幂等 防"嵌入 no-op 遮蔽"回归。
func TestBeforeModelCall_安装检查器且幂等(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got := len(cbc.StreamChunkInspectors()); got != 1 {
		t.Fatalf("应安装 1 个检查器，实际 %d", got)
	}
	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("第二次不应报错: %v", err)
	}
	if got := len(cbc.StreamChunkInspectors()); got != 1 {
		t.Fatalf("重复调用应幂等（仍 1 个），实际 %d", got)
	}
	if _, ok := cbc.GetExtra(modelAnomalyStateKey); !ok {
		t.Fatal("应重置重复检测尾窗状态")
	}
}

// TestToolLoopCompact_三轮相同折叠 3 轮相同调用折叠为 1 条中文警告。
func TestToolLoopCompact_三轮相同折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	mc.msgs = toolRoundMessages(`{"path":"a.txt"}`, 3)

	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(mc.msgs) != 1 {
		t.Fatalf("3 轮应折叠为 1 条警告，实际 %d 条", len(mc.msgs))
	}
	userMessage, ok := mc.msgs[0].(*llmschema.UserMessage)
	if !ok {
		t.Fatalf("折叠结果应为 UserMessage，实际 %T", mc.msgs[0])
	}
	if !strings.Contains(userMessage.Content.Text(), "请跳出重复工具调用") {
		t.Fatalf("应使用中文模板（默认 cn），实际 %s", userMessage.Content.Text())
	}
}

// TestToolLoopCompact_参数不同不折叠 指纹不同即断。
func TestToolLoopCompact_参数不同不折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	mc.msgs = append(toolRoundMessages(`{"path":"a"}`, 1), toolRoundMessages(`{"path":"b"}`, 2)...)

	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 6 {
		t.Fatalf("参数不同不应折叠，实际 %d 条", len(mc.msgs))
	}
}

// TestToolLoopCompact_返回值不同仍折叠 返回值不参与匹配。
func TestToolLoopCompact_返回值不同仍折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 3, 0)
	for i := 0; i < 3; i++ {
		call := llmschema.NewToolCall(fmt.Sprintf("id-%d", i), "read_file", `{"path":"a"}`)
		mc.msgs = append(mc.msgs,
			llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{call})),
			llmschema.NewToolMessage(fmt.Sprintf("id-%d", i), fmt.Sprintf("result-%d", i)))
	}
	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 1 {
		t.Fatalf("返回值不参与匹配，应折叠，实际 %d 条", len(mc.msgs))
	}
}

// TestToolLoopCompact_用户输入隔开不折叠 A-09 核心护栏：只折背靠背尾段。
func TestToolLoopCompact_用户输入隔开不折叠(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 2, 0)
	mc.msgs = toolRoundMessages(`{"path":"a"}`, 1)
	mc.msgs = append(mc.msgs, llmschema.NewUserMessage("继续"))
	mc.msgs = append(mc.msgs, toolRoundMessages(`{"path":"a"}`, 2)...)

	_ = r.BeforeModelCall(context.Background(), cbc)
	// 折叠后：轮1(2 条) + 用户输入 + 警告 = 4 条
	if len(mc.msgs) != 4 {
		t.Fatalf("只应折叠背靠背尾段，实际 %d 条", len(mc.msgs))
	}
	userMessage, ok := mc.msgs[2].(*llmschema.UserMessage)
	if !ok || userMessage.Content.Text() != "继续" {
		t.Fatal("用户输入必须保留（A-09 回归护栏）")
	}
}

// TestToolLoopCompact_第三次命中bailout 第 3 次命中抛 AbortError 且 cause 为兜底错误。
func TestToolLoopCompact_第三次命中bailout(t *testing.T) {
	r, cbc, mc := newCompactRail(t, 2, 3)
	for i := 0; i < 3; i++ {
		mc.msgs = toolRoundMessages(`{"path":"a"}`, 2)
		err := r.BeforeModelCall(context.Background(), cbc)
		if i < 2 && err != nil {
			t.Fatalf("第 %d 次不应中止: %v", i+1, err)
		}
		if i < 2 {
			continue
		}
		if err == nil {
			t.Fatal("第 3 次应 bailout")
		}
		var abortErr *cb.AbortError
		if !errors.As(err, &abortErr) {
			t.Fatalf("应返回 *AbortError，实际 %T", err)
		}
		if abortErr.Cause == nil || !strings.Contains(abortErr.Cause.Error(), "automatically aborted") {
			t.Fatalf("cause 应为兜底错误，实际 %v", abortErr.Cause)
		}
		if !exception.IsControlFlowAbort(abortErr.Cause) {
			t.Fatal("bailout 的 cause 必须带控制流中止标记（重试/切模型据此跳过）")
		}
	}
}

// TestToolLoopCompact_默认关闭不动作 默认 enabled=false 时不动消息。
func TestToolLoopCompact_默认关闭不动作(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()
	mc := &fakeToolLoopContext{msgs: toolRoundMessages(`{"path":"a"}`, 5)}
	cbc.SetModelContext(mc)

	_ = r.BeforeModelCall(context.Background(), cbc)
	if len(mc.msgs) != 10 {
		t.Fatalf("默认关闭时不应改动消息，实际 %d 条", len(mc.msgs))
	}
}

// ──────────────────────────── 边界与分支补测 ────────────────────────────

// TestGetCallbacks_注册四个事件 验证事件映射与 BeforeModelCall 解析到本 rail。
func TestGetCallbacks_注册四个事件(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	callbacks := r.GetCallbacks()
	for _, event := range []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackAfterInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackOnModelException,
	} {
		if callbacks[event] == nil {
			t.Fatalf("事件 %s 未注册", event)
		}
	}

	cbc := newTestCBC()
	if err := callbacks[agentinterfaces.CallbackBeforeModelCall](context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 回调失败: %v", err)
	}
	if len(cbc.StreamChunkInspectors()) != 1 {
		t.Fatal("回调必须命中共体方法（嵌入 no-op 不得遮蔽）")
	}
	if err := callbacks[agentinterfaces.CallbackAfterInvoke](context.Background(), cbc); err != nil {
		t.Fatalf("AfterInvoke 回调失败: %v", err)
	}
}

// TestNormalizeLanguage 覆盖 zh* / cn / en / 其余 / 空 五种输入。
func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"":      "cn",
		"zh-CN": "cn",
		"CN":    "cn",
		"en":    "en",
		"fr":    "cn",
	}
	for input, want := range cases {
		if got := normalizeLanguage(input); got != want {
			t.Errorf("%q: 期望 %q，实际 %q", input, want, got)
		}
	}
}

// TestBuildToolLoopWarningContent_英文模板 en 使用英文模板。
func TestBuildToolLoopWarningContent_英文模板(t *testing.T) {
	content := buildToolLoopWarningContent("en", "[]", "[]")
	if !strings.Contains(content, "Please break out of the repeated tool calls") {
		t.Fatalf("应使用英文模板，实际 %s", content)
	}
	if strings.Contains(content, "{tool_calls}") {
		t.Fatal("占位符必须被替换")
	}
}

// TestPyReprString_转义 单引号与反斜杠按 Python repr 语义转义。
func TestPyReprString_转义(t *testing.T) {
	if got := pyReprString(`a'b\c`); got != `'a\'b\\c'` {
		t.Fatalf("转义结果不符，实际 %s", got)
	}
	if got := pyReprString("tab\there"); got != `'tab\there'` {
		t.Fatalf("制表符应转义，实际 %s", got)
	}
}

// TestFuncPtrEqual 同一方法相等、不同方法不等、nil 一律不等。
func TestFuncPtrEqual(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	other := func(context.Context, *agentinterfaces.AgentCallbackContext, *llmschema.AssistantMessageChunk) error {
		return nil
	}
	if !funcPtrEqual(r.InspectStreamChunk, r.InspectStreamChunk) {
		t.Fatal("同一方法应相等")
	}
	if funcPtrEqual(r.InspectStreamChunk, other) {
		t.Fatal("不同方法不应相等")
	}
	if funcPtrEqual(nil, r.InspectStreamChunk) || funcPtrEqual(r.InspectStreamChunk, nil) {
		t.Fatal("nil 不应相等")
	}
}

// TestToFloat_五种形态 兼容 int/int64/float64/字符串/其他。
func TestToFloat_五种形态(t *testing.T) {
	cases := []struct {
		value any
		want  float64
		ok    bool
	}{
		{3, 3, true},
		{int64(4), 4, true},
		{1.5, 1.5, true},
		{" 2.5 ", 2.5, true},
		{"abc", 0, false},
		{true, 0, false},
	}
	for _, c := range cases {
		got, ok := toFloat(c.value)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%v: 期望 (%v,%v)，实际 (%v,%v)", c.value, c.want, c.ok, got, ok)
		}
	}
}

// TestStatusCodeOf_ProviderCode details.provider_code 也应被识别为状态码。
func TestStatusCodeOf_ProviderCode(t *testing.T) {
	err := exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg("provider response error"),
		exception.WithDetails(map[string]any{"provider_code": 500}))
	code := statusCodeOf(err)
	if code == nil || *code != 500 {
		t.Fatalf("应取 details.provider_code=500，实际 %v", code)
	}
}

// TestCoerceStatusCode_布尔与字符串 bool 不参与、数字字符串可解析。
func TestCoerceStatusCode_布尔与字符串(t *testing.T) {
	if code := coerceStatusCode(true); code != nil {
		t.Fatalf("bool 应视为无状态码，实际 %v", *code)
	}
	if code := coerceStatusCode("429"); code == nil || *code != 429 {
		t.Fatalf("数字字符串应解析为 429，实际 %v", code)
	}
	if code := coerceStatusCode(0); code != nil {
		t.Fatalf("0 应视为无状态码，实际 %v", *code)
	}
}

// TestBackoffDelay_负索引取首项 对齐 Python min(retry_index, len-1) 的下界保护。
func TestBackoffDelay_负索引取首项(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	if got := r.BackoffDelay(-3); got != 0.5 {
		t.Fatalf("负索引应取首项 0.5，实际 %v", got)
	}
	empty, _ := NewModelAnomalyDetectionRail(WithBackoffSeconds(nil))
	if got := empty.BackoffDelay(0); got != 0 {
		t.Fatalf("空退避表应返回 0，实际 %v", got)
	}
}

// TestParseToolArguments_三种输入 空串→{}、非法 JSON→原样、合法→解析。
func TestParseToolArguments_三种输入(t *testing.T) {
	if _, ok := parseToolArguments("").(map[string]any); !ok {
		t.Fatal("空串应返回空 map")
	}
	if got := parseToolArguments("not-json"); got != "not-json" {
		t.Fatalf("非法 JSON 应原样返回，实际 %v", got)
	}
	parsed, ok := parseToolArguments(`{"b":1,"a":2}`).(map[string]any)
	if !ok || len(parsed) != 2 {
		t.Fatalf("合法 JSON 应解析为 map，实际 %v", parsed)
	}
}

// TestCollectToolRounds_不完整轮次跳过 有 call 无 result 不算一轮。
func TestCollectToolRounds_不完整轮次跳过(t *testing.T) {
	callA := llmschema.NewToolCall("id-a", "read_file", `{"p":1}`)
	callB := llmschema.NewToolCall("id-b", "read_file", `{"p":2}`)
	messages := []llmschema.BaseMessage{
		llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{callA, callB})),
		llmschema.NewToolMessage("id-a", "only one"),
	}
	if rounds := collectToolRounds(messages); len(rounds) != 0 {
		t.Fatalf("不完整轮次应被跳过，实际 %d 轮", len(rounds))
	}
}

// fakeAgent 最小 BaseAgent 包装，仅提供 SystemPromptBuilder。
type fakeAgent struct {
	agentinterfaces.BaseAgent
	// builder 提示词构建器
	builder saprompt.SystemPromptBuilderInterface
}

// SystemPromptBuilder 返回注入的构建器。
func (f *fakeAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return f.builder }

// fakePromptBuilder 最小提示词构建器，仅提供语言。
type fakePromptBuilder struct {
	saprompt.SystemPromptBuilderInterface
	// lang 语言标记
	lang string
}

// Language 返回注入的语言。
func (f *fakePromptBuilder) Language() string { return f.lang }

// TestResolveToolLoopWarningLanguage_三分支 builder/nil 与语言归一化。
func TestResolveToolLoopWarningLanguage_三分支(t *testing.T) {
	if got := resolveToolLoopWarningLanguage(newTestCBC()); got != "cn" {
		t.Fatalf("无 agent 时应为 cn，实际 %q", got)
	}

	withAgentNoBuilder := agentinterfaces.NewAgentCallbackContext(&fakeAgent{}, nil, nil)
	if got := resolveToolLoopWarningLanguage(withAgentNoBuilder); got != "cn" {
		t.Fatalf("builder 为 nil 时应为 cn，实际 %q", got)
	}

	english := agentinterfaces.NewAgentCallbackContext(&fakeAgent{builder: &fakePromptBuilder{lang: "en-US"}}, nil, nil)
	if got := resolveToolLoopWarningLanguage(english); got != "cn" {
		t.Fatalf("en-US 非 en 精确匹配，应为 cn，实际 %q", got)
	}

	englishExact := agentinterfaces.NewAgentCallbackContext(&fakeAgent{builder: &fakePromptBuilder{lang: "en"}}, nil, nil)
	if got := resolveToolLoopWarningLanguage(englishExact); got != "en" {
		t.Fatalf("en 应为 en，实际 %q", got)
	}
}

// TestWithRepeatMaxPatternChars_生效 选项可独立设置上限。
func TestWithRepeatMaxPatternChars_生效(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail(WithRepeatMaxPatternChars(16))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if r.repeatMaxPatternChars != 16 {
		t.Fatalf("上限应为 16，实际 %d", r.repeatMaxPatternChars)
	}
}

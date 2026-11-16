package duplex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestBriefMarkers 对齐 video_search.py:32 _brief_markers。
func TestBriefMarkers_协议标记(t *testing.T) {
	begin, end := BriefMarkers("abc123")
	if begin != "[[JIUWEN_BRIEF_BEGIN:abc123]]" || end != "[[JIUWEN_BRIEF_END:abc123]]" {
		t.Fatalf("标记不符: %q %q", begin, end)
	}
}

// TestResultKind 对齐 video_search.py:60-77。
func TestResultKind_research_code_calc分型(t *testing.T) {
	if resultKind("q", "a", []string{"web_search"}) != "research" {
		t.Fatal("搜索工具应 research")
	}
	if resultKind("q", "```python\nx=1\n```", nil) != "code" {
		t.Fatal("代码块应 code")
	}
	if resultKind("计算 3 + 4", "7", nil) != "calculation" {
		t.Fatal("算式应 calculation")
	}
	if resultKind("帮我写一封信", "好的", nil) != "action" {
		t.Fatal("默认 action")
	}
}

// TestSafeBrief 对齐 video_search.py:79-92。
func TestSafeBrief_边界(t *testing.T) {
	if safeBrief("任务完成，结果已展示") != "任务完成，结果已展示" {
		t.Fatal("正常简报应保留")
	}
	if safeBrief("看 https://x.com") != "" {
		t.Fatal("含 URL 应拒")
	}
	if safeBrief("看\n两行") != "" {
		t.Fatal("含换行应拒")
	}
	if safeBrief(`{"status":"ok"}`) != "" {
		t.Fatal("JSON 应拒")
	}
	if safeBrief("[[JIUWEN_BRIEF_BEGIN:x]]") != "" {
		t.Fatal("协议标记应拒")
	}
	if safeBrief(strings.Repeat("字", 181)) != "" {
		t.Fatal("超 180 字应拒")
	}
}

// TestFallbackRealtimeBrief 对齐 video_search.py:94-117。
func TestFallbackRealtimeBrief_降级链(t *testing.T) {
	summary, source := fallbackRealtimeBrief(strings.Repeat("短", 50), "research")
	if summary != strings.Repeat("短", 50) || source != "derived" {
		t.Fatalf("短结果应 derived: %s %s", summary, source)
	}
	summary, source = fallbackRealtimeBrief(strings.Repeat("长", 200), "code")
	if source != "fallback" || summary != "代码已经生成，完整内容已经显示在界面中。" {
		t.Fatalf("长结果应 fallback 分型文案: %s %s", summary, source)
	}
	summary, source = fallbackRealtimeBrief(strings.Repeat("长", 200), "未知分型")
	if summary != "任务已经完成，完整结果已经显示在界面中。" {
		t.Fatalf("未知分型回 generic: %s", summary)
	}
}

// TestPresentCoreAgentResult 对齐 video_search.py:119-156。
func TestPresentCoreAgentResult_brief提取与降级(t *testing.T) {
	begin, end := BriefMarkers("n1")
	raw := "完整答案内容。\n" + begin + "\n口头回执。\n" + end + "\n"
	got := PresentCoreAgentResult(raw, "n1", "问题", nil)
	if got["display_result"] != "完整答案内容。" {
		t.Fatalf("标记段应剥离: %q", got["display_result"])
	}
	brief := got["realtime_brief"].(map[string]any)
	if brief["summary"] != "口头回执。" || brief["source"] != "core_agent" {
		t.Fatalf("模型简报应提取: %v", brief)
	}
	// 无标记 → fallback
	got = PresentCoreAgentResult("只有完整答案", "n2", "问题", nil)
	brief = got["realtime_brief"].(map[string]any)
	if brief["source"] != "derived" {
		t.Fatalf("无标记应降级 derived: %v", brief)
	}
}

// TestDelegationContextText 对齐 video_search.py:162-184。
func TestDelegationContextText_上限截断(t *testing.T) {
	if delegationContextText(nil) != "None" {
		t.Fatal("空上下文应 None")
	}
	items := []map[string]any{
		{"question": "q1", "query": "g1", "result": "r1", "files": []any{map[string]any{"name": "f"}}},
	}
	got := delegationContextText(items)
	if !strings.Contains(got, "[1] User instruction: q1") || !strings.Contains(got, "Core Agent result: r1") {
		t.Fatalf("块结构不符: %s", got)
	}
	// 超长结果截断
	huge := strings.Repeat("字", MaxDelegationContextChars+100)
	got = delegationContextText([]map[string]any{{"question": "q", "result": huge}})
	if len(got) > MaxDelegationContextChars+10 {
		t.Fatalf("总长应受 8000 上限: %d", len(got))
	}
	// 只取最近 6 条
	many := make([]map[string]any, 8)
	for i := range many {
		many[i] = map[string]any{"question": fmt.Sprintf("q%d", i), "result": "r"}
	}
	got = delegationContextText(many)
	if strings.Contains(got, "q0 ") || strings.Contains(got, "q1 ") || !strings.Contains(got, "q2") {
		t.Fatalf("应只含最近 6 条: %s", got)
	}
}

// TestFrameMediaItem 对齐 video_search.py:186-201。
func TestFrameMediaItem(t *testing.T) {
	if frameMediaItem("not-a-data-url") != nil {
		t.Fatal("非 dataURL 应 nil")
	}
	if frameMediaItem("data:image/gif;base64,AA") != nil {
		t.Fatal("gif 不在白名单应 nil")
	}
	got := frameMediaItem("data:image/png;base64,QUJD")
	if got == nil || got["filename"] != "video-search-frame.png" || got["base64Data"] != "QUJD" {
		t.Fatalf("png 帧条目不符: %v", got)
	}
}

// TestCoreAgentText 对齐 video_search.py:203-217。
func TestCoreAgentText_归一与截断(t *testing.T) {
	if CoreAgentText(nil, 280) != "" {
		t.Fatal("nil 应空")
	}
	if CoreAgentText("多  行\n文本", 280) != "多 行 文本" {
		t.Fatalf("空白应压缩: %q", CoreAgentText("多  行\n文本", 280))
	}
	long := strings.Repeat("字", 300)
	got := CoreAgentText(long, 280)
	if len([]rune(got)) != 283 || !strings.HasSuffix(got, "...") {
		t.Fatalf("应截断至 280+3: %d", len([]rune(got)))
	}
}

// TestCoreAgentProgress 事件映射逐条对照 video_search.py:219-364。
func TestCoreAgentProgress_事件映射(t *testing.T) {
	if CoreAgentProgress(map[string]any{"event_type": "chat.file", "files": []any{map[string]any{"name": "f", "path": "/f"}}}) == nil {
		t.Fatal("chat.file 应映射 file 阶段")
	}
	if CoreAgentProgress(map[string]any{"event_type": "chat.reasoning", "content": ""}) != nil {
		t.Fatal("空 reasoning 应 nil")
	}
	got := CoreAgentProgress(map[string]any{"event_type": "chat.tool_call", "tool_call": map[string]any{"display_name": "搜索", "arguments": map[string]any{"q": "x"}, "id": "t1"}})
	if got["title"] != "调用工具：搜索" || got["tool_call_id"] != "t1" {
		t.Fatalf("tool_call 映射不符: %v", got)
	}
	got = CoreAgentProgress(map[string]any{"event_type": "chat.tool_result", "tool_result": map[string]any{"tool_name": "搜索", "success": false, "error": "boom"}})
	if got["status"] != "failed" || got["title"] != "搜索执行失败" {
		t.Fatalf("失败 tool_result 不符: %v", got)
	}
	got = CoreAgentProgress(map[string]any{"event_type": "todo.updated", "todos": []any{map[string]any{"id": "1", "content": "a", "status": "completed"}, map[string]any{"id": "2", "content": "b", "status": "pending"}}})
	if got["detail"] != "1/2 项已完成" {
		t.Fatalf("todo 映射不符: %v", got)
	}
	if CoreAgentProgress(map[string]any{"event_type": "chat.delta", "content": "片段"}) == nil {
		t.Fatal("非空 delta 应 answer 阶段")
	}
	got = CoreAgentProgress(map[string]any{"event_type": "chat.error", "error": "出错"})
	if got["stage"] != "error" || got["status"] != "failed" {
		t.Fatalf("error 映射不符: %v", got)
	}
	if CoreAgentProgress(map[string]any{"event_type": "unknown"}) != nil {
		t.Fatal("未知事件应 nil")
	}
}

// fakeAgentClient 测试桩。
type fakeAgentClient struct {
	responses []*schema.AgentResponse
	stream    []*schema.AgentResponse
	noStream  bool
	sent      []*e2a.E2AEnvelope
}

func (f *fakeAgentClient) SendRequest(_ context.Context, env *e2a.E2AEnvelope) *schema.AgentResponse {
	f.sent = append(f.sent, env)
	if len(f.responses) == 0 {
		return &schema.AgentResponse{OK: false, Payload: map[string]any{"error": "no more responses"}}
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp
}

func (f *fakeAgentClient) SendRequestStream(_ context.Context, env *e2a.E2AEnvelope) <-chan *schema.AgentResponse {
	f.sent = append(f.sent, env)
	if f.noStream {
		return nil
	}
	ch := make(chan *schema.AgentResponse, len(f.stream)+1)
	for _, resp := range f.stream {
		ch <- resp
	}
	close(ch)
	return ch
}

// TestExecuteCoreAgent_非流式 对齐 video_search.py:414-432。
func TestExecuteCoreAgent_非流式_假AgentServer(t *testing.T) {
	client := &fakeAgentClient{responses: []*schema.AgentResponse{{
		OK: true,
		Payload: map[string]any{
			"content": "答案正文\n" + "[[JIUWEN_BRIEF_BEGIN:n]]\n口头回执\n[[JIUWEN_BRIEF_END:n]]",
		},
	}}, noStream: true}
	opts := CoreAgentOptions{Question: "q", Query: "", BriefNonce: "n"}
	got := recoverResult(func() *CoreAgentResult { return ExecuteCoreAgent(nil, client, opts) })
	if got == nil {
		t.Fatal("应成功返回")
	}
	// raw_answer 含标记原文（对齐 Python：strip 后整段计数）
	rawWithMarkers := "答案正文\n[[JIUWEN_BRIEF_BEGIN:n]]\n口头回执\n[[JIUWEN_BRIEF_END:n]]"
	if got.Answer != "答案正文" || got.RawAnswerChars != len([]rune(rawWithMarkers)) {
		t.Fatalf("答案与字数不符: %q %d (期望 %d)", got.Answer, got.RawAnswerChars, len([]rune(rawWithMarkers)))
	}
	if got.RealtimeBrief["source"] != "core_agent" {
		t.Fatalf("简报来源: %v", got.RealtimeBrief)
	}
	if got.ToolsUsed == nil || len(got.ToolsUsed) != 0 {
		t.Fatal("非流式 tools_used 应空")
	}
	sent := client.sent[0]
	if sent.Method != "chat.send" || sent.Channel != "video_tool" {
		t.Fatalf("请求形态不符: %v", sent)
	}
	if sent.Params["mode"] != "agent" || sent.Params["work_mode"] != "work" || sent.Params["supports_user_interaction"] != true {
		t.Fatal("params 契约不符")
	}
	if !strings.Contains(sent.Params["query"].(string), "User task requirements (including user changes in chronological order): q") {
		t.Fatal("prompt 应含任务要求段")
	}
	// 失败响应
	client = &fakeAgentClient{responses: []*schema.AgentResponse{{OK: false, Payload: map[string]any{"error": "Agent 炸了"}}}, noStream: true}
	err := recoverErr(func() { ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q"}) })
	if err == nil || err.Error() != "Agent 炸了" {
		t.Fatalf("失败响应应上抛: %v", err)
	}
	// 空输出
	client = &fakeAgentClient{responses: []*schema.AgentResponse{{OK: true, Payload: map[string]any{}}}, noStream: true}
	err = recoverErr(func() { ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q"}) })
	if err == nil || err.Error() != "Jiuwen Core Agent returned empty output" {
		t.Fatalf("空输出应报错: %v", err)
	}
}

// TestExecuteCoreAgent_流式事件全链 对齐 video_search.py:434-539。
func TestExecuteCoreAgent_流式事件全链(t *testing.T) {
	client := &fakeAgentClient{stream: []*schema.AgentResponse{
		{OK: true, Payload: map[string]any{"event_type": "chat.delta", "content": "答案"}},
		{OK: true, Payload: map[string]any{"event_type": "chat.final", "content": "答案正文"}},
	}}
	opts := CoreAgentOptions{Question: "q", BriefNonce: "n"}
	got := recoverResult(func() *CoreAgentResult { return ExecuteCoreAgent(nil, client, opts) })
	if got == nil || got.Answer != "答案正文" {
		t.Fatalf("流式末帧应作答案: %v", got)
	}

	// chat.error 上抛
	client = &fakeAgentClient{stream: []*schema.AgentResponse{{OK: true, Payload: map[string]any{"event_type": "chat.error", "error": "执行失败"}}}}
	err := recoverErr(func() { ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q"}) })
	if err == nil || err.Error() != "执行失败" {
		t.Fatalf("chat.error 应上抛: %v", err)
	}

	// 无末帧 → ExecutionUncertain
	client = &fakeAgentClient{stream: []*schema.AgentResponse{{OK: true, Payload: map[string]any{"event_type": "chat.delta", "content": "片段"}}}}
	err = recoverErr(func() { ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q"}) })
	var uncertain *ExecutionUncertainAlias
	if !errors.As(err, &uncertain) {
		t.Fatalf("无末帧应 ExecutionUncertain: %v", err)
	}

	// 交互提问 → InteractionPending
	client = &fakeAgentClient{stream: []*schema.AgentResponse{
		{OK: true, Payload: map[string]any{"event_type": "chat.ask_user_question", "source": "ask_user_interrupt", "request_id": "r1", "questions": []any{map[string]any{"question": "预算多少？"}}}},
	}}
	var progress []map[string]any
	err = recoverErr(func() {
		ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q", OnProgress: func(p map[string]any) error {
			progress = append(progress, p)
			return nil
		}})
	})
	var pending *InteractionPendingAlias
	if !errors.As(err, &pending) {
		t.Fatalf("ask_user_interrupt 应 InteractionPending: %v", err)
	}
	if len(progress) != 1 || progress[0]["stage"] != "interaction" {
		t.Fatalf("交互应推送进度: %v", progress)
	}

	// ask_user（非 interrupt）源不挂起 → 无末帧 → ExecutionUncertain
	client = &fakeAgentClient{stream: []*schema.AgentResponse{
		{OK: true, Payload: map[string]any{"event_type": "chat.ask_user_question", "source": "ask_user", "request_id": "r1", "questions": []any{map[string]any{"question": "确认？"}}}},
	}}
	err = recoverErr(func() { ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q"}) })
	var uncertain2 *ExecutionUncertainAlias
	if !errors.As(err, &uncertain2) {
		t.Fatalf("非 interrupt 源不挂起: %v", err)
	}
}

// TestExecuteCoreAgent_附件归一化被调 对齐 video_search.py:383-387。
func TestExecuteCoreAgent_附件归一化被调(t *testing.T) {
	client := &fakeAgentClient{responses: []*schema.AgentResponse{{OK: true, Payload: map[string]any{"content": "ok"}}}, noStream: true}
	called := false
	opts := CoreAgentOptions{
		Question: "q", FrameDataURL: "data:image/png;base64,QUJD",
		NormalizeMediaAttachments: func(params map[string]any, coreSessionID string) error {
			called = true
			return nil
		},
	}
	got := recoverResult(func() *CoreAgentResult { return ExecuteCoreAgent(nil, client, opts) })
	if got == nil || !called {
		t.Fatal("归一化服务应被调")
	}
	mediaItems := client.sent[0].Params["media_items"].([]map[string]any)
	if mediaItems[0]["filename"] != "video-search-frame.png" {
		t.Fatalf("media_items 不符: %v", mediaItems)
	}
	// 无归一化服务时报错
	err := recoverErr(func() {
		ExecuteCoreAgent(nil, &fakeAgentClient{}, CoreAgentOptions{Question: "q", FrameDataURL: "data:image/png;base64,QUJD"})
	})
	if err == nil || err.Error() != "Core media attachment service is unavailable" {
		t.Fatalf("缺归一化服务应报错: %v", err)
	}
}

// TestExecuteCoreAgent_interaction_answer分支 对齐 video_search.py:389-391。
func TestExecuteCoreAgent_interaction_answer分支(t *testing.T) {
	client := &fakeAgentClient{responses: []*schema.AgentResponse{{OK: true, Payload: map[string]any{"content": "ok"}}}, noStream: true}
	opts := CoreAgentOptions{
		Question:          "q",
		InteractionAnswer: map[string]any{"params": map[string]any{"answers": []any{}}, "req_method": "chat.answer"},
	}
	got := recoverResult(func() *CoreAgentResult { return ExecuteCoreAgent(nil, client, opts) })
	if got == nil {
		t.Fatal("answer 分支应成功")
	}
	if client.sent[0].Method != "chat.answer" {
		t.Fatalf("answer 分支应换请求方法: %s", client.sent[0].Method)
	}
	if client.sent[0].Params["log_as_user"] != false {
		t.Fatal("log_as_user 应强制 false")
	}
}

// TestExecuteCoreAgent_managed_task_binding透传 对齐 video_search.py:393-394。
func TestExecuteCoreAgent_managed_task_binding透传(t *testing.T) {
	client := &fakeAgentClient{responses: []*schema.AgentResponse{{OK: true, Payload: map[string]any{"content": "ok"}}}, noStream: true}
	binding := map[string]any{"job_id": "j1"}
	ExecuteCoreAgent(nil, client, CoreAgentOptions{Question: "q", ManagedTaskBinding: binding})
	if gotBinding, ok := client.sent[0].Params["managed_task_binding"].(map[string]any); !ok || gotBinding["job_id"] != "j1" {
		t.Fatal("binding 应透传")
	}
}

// ───────────── 别名与辅助（测试侧错误类型断言） ─────────────

// ExecutionUncertainAlias tasks.ExecutionUncertain 的本包别名。
type ExecutionUncertainAlias = tasks.ExecutionUncertain

// InteractionPendingAlias tasks.InteractionPending 的本包别名。
type InteractionPendingAlias = tasks.InteractionPending

// recoverResult 捕获 panic(error) 取结果。
func recoverResult(fn func() *CoreAgentResult) (result *CoreAgentResult) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				result = nil
				panicErrSlot = e
				return
			}
			panic(r)
		}
	}()
	return fn()
}

// recoverErr 捕获 panic(error) 取错误。
func recoverErr(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				panicErrSlot = e
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

// panicErrSlot recover 辅助的错误槽位。
var panicErrSlot error

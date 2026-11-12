package processcli

import (
	"strings"
	"testing"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// TestEventRenderer_JSONL逐行 jsonl 模式每条事件一行。
// Python: render.py:141-145
func TestEventRenderer_JSONL逐行(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputJSONL, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		RequestID: "r1", ChannelID: "process_cli", SessionID: "s1",
		Payload: map[string]any{"event_type": "chat.delta", "delta": "hi"}, OK: true,
	}, "")
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("行数 = %d：%q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"chat.delta"`) {
		t.Errorf("缺少事件类型：%q", lines[0])
	}
	if !strings.Contains(lines[0], `"hi"`) {
		t.Errorf("缺少 delta 内容：%q", lines[0])
	}
}

// TestEventRenderer_JSONL不转义HTML ensure_ascii=False 与 SetEscapeHTML(false) 对齐。
func TestEventRenderer_JSONL不转义HTML(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputJSONL, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.delta", "delta": "<a&b>"}, OK: true,
	}, "")
	if strings.Contains(out.String(), `\u003c`) {
		t.Errorf("HTML 被转义：%q", out.String())
	}
	if !strings.Contains(out.String(), "<a&b>") {
		t.Errorf("原文丢失：%q", out.String())
	}
}

// TestEventRenderer_JSON聚合 json 模式只在 Finish 时输出一次。
// Python: render.py:146-147 + :158-170
func TestEventRenderer_JSON聚合(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputJSON, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.delta", "delta": "x"}, OK: true,
	}, "")
	if out.Len() != 0 {
		t.Errorf("json 模式 Render 不应立即输出：%q", out.String())
	}
	renderer.Finish("s1", "r1", false)
	if !strings.Contains(out.String(), `"events"`) || !strings.Contains(out.String(), "chat.delta") {
		t.Errorf("Finish 输出不完整：%q", out.String())
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Errorf("缺少 ok 字段：%q", out.String())
	}
}

// TestEventRenderer_failed判定 三类错误事件置 failed。
// Python: render.py:127-135
func TestEventRenderer_failed判定(t *testing.T) {
	for _, eventType := range []string{"chat.error", "runtime.error", "team.error"} {
		renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{}, false, false, false)
		renderer.Render(&swruntime.RuntimeEvent{
			Payload: map[string]any{"event_type": eventType}, OK: false,
		}, "")
		if !renderer.Failed() {
			t.Errorf("%s 应置 failed", eventType)
		}
	}
	// OK=false 但事件类型正常也置 failed
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{}, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.final", "content": "ok"}, OK: false,
	}, "")
	if !renderer.Failed() {
		t.Error("OK=false 应置 failed")
	}
	// 正常事件不置 failed
	renderer = NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{}, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.final", "content": "ok"}, OK: true,
	}, "")
	if renderer.Failed() {
		t.Error("正常事件不应置 failed")
	}
}

// TestEventRenderer_humanDelta human 模式增量写 stdout。
// Python: render.py:186-200
func TestEventRenderer_humanDelta(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputHuman, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.delta", "delta": "hello"}, OK: true,
	}, "")
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("增量文本未输出：%q", out.String())
	}
}

// TestEventRenderer_teammate过滤 团队成员流不拼进单一答案。
// Python: render.py:190-194
func TestEventRenderer_teammate过滤(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputHuman, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type": "chat.delta", "delta": "member-text", "role": "teammate",
		}, OK: true,
	}, "")
	if strings.Contains(out.String(), "member-text") {
		t.Errorf("teammate 文本不应输出：%q", out.String())
	}
}

// TestEventRenderer_skills视图 skills.list 视图渲染技能。
// Python: render.py:148-153
func TestEventRenderer_skills视图(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputHuman, &out, &out, false, false, false)
	renderer.Render(&swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type": "skills.list",
			"skills": []any{
				map[string]any{"name": "s1", "installed": true},
			},
		}, OK: true,
	}, "skills.list")
	if !strings.Contains(out.String(), "s1") {
		t.Errorf("技能未渲染：%q", out.String())
	}
	if !strings.Contains(out.String(), "已安装技能") {
		t.Errorf("分组标题缺失：%q", out.String())
	}
}

// TestHumanErrorSummary_240字截断。
// Python: render.py:32-52
func TestHumanErrorSummary_240字截断(t *testing.T) {
	got := humanErrorSummary(strings.Repeat("x", 300))
	if displayWidth(got) > 241 {
		t.Errorf("未截断：长度 %d", displayWidth(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("应加省略号：%q", got)
	}
	if got := humanErrorSummary("short error"); got != "short error" {
		t.Errorf("短消息被改：%q", got)
	}
	if got := humanErrorSummary(""); got != "未知错误" {
		t.Errorf("空消息应返回未知错误，得 %q", got)
	}
}

// TestHumanErrorSummary_已知翻译 两条固定文案。
// Python: render.py:14-19
func TestHumanErrorSummary_已知翻译(t *testing.T) {
	if got := humanErrorSummary("process CLI execution timed out"); got != "进程式 CLI 执行超时" {
		t.Errorf("超时文案 = %q", got)
	}
	if got := humanErrorSummary(
		"process CLI received an interaction request but interactive input is unavailable",
	); got != "当前输出模式无法接收交互输入，请使用交互式 CLI 完成此操作" {
		t.Errorf("交互文案 = %q", got)
	}
}

// TestHumanErrorSummary_模型失败分支 model call failed 提取 HTTP 状态。
// Python: render.py:38-47
func TestHumanErrorSummary_模型失败分支(t *testing.T) {
	got := humanErrorSummary("model call failed: Access denied. Error code: 403")
	if !strings.Contains(got, "403") || !strings.Contains(got, "拒绝访问") {
		t.Errorf("未识别 403 access denied：%q", got)
	}
	got = humanErrorSummary("model call failed: something long " + strings.Repeat("y", 300))
	if !strings.Contains(got, "模型调用失败") || !strings.Contains(got, "诊断日志") {
		t.Errorf("超长模型失败文案未收敛：%q", got)
	}
}

// TestEventText_五键优先 依次取 delta/content/text/message/answer。
// Python: render.py:22-28
func TestEventText_五键优先(t *testing.T) {
	if got := eventText(map[string]any{"delta": "d", "content": "c"}); got != "d" {
		t.Errorf("delta 应优先，得 %q", got)
	}
	if got := eventText(map[string]any{"content": "c", "text": "t"}); got != "c" {
		t.Errorf("content 应优先于 text，得 %q", got)
	}
	if got := eventText(map[string]any{"answer": "a"}); got != "a" {
		t.Errorf("answer 兜底失败，得 %q", got)
	}
	if got := eventText(map[string]any{"delta": ""}); got != "" {
		t.Errorf("空串应跳过，得 %q", got)
	}
}

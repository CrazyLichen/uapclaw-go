package processcli

import (
	"strings"
	"testing"
)

// ──────────────────────────── HumanRunUI 渲染 ────────────────────────────
//
// 被测方法大多以 u.enhanced / u.unicode / u.color 三个布尔为门卫。
// strings.Builder 不是终端，构造函数算出的三个标记都是 false，渲染会被短路。
// 这里直接构造结构体把标记打开，测「开启装饰后」的渲染分支。

// newEnhancedHumanUI 构造三个标记全开的装饰器（写入 builder）。
func newEnhancedHumanUI() (*HumanRunUI, *strings.Builder, *strings.Builder) {
	out := &strings.Builder{}
	errOut := &strings.Builder{}
	ui := &HumanRunUI{
		stdout:          out,
		stderr:          errOut,
		enhanced:        true,
		unicode:         true,
		color:           false, // 无色输出更易断言
		reasoningAtLine: true,
	}
	return ui, out, errOut
}

// TestHumanRunUI_构造函数标记 strings.Builder 下三个标记均为假。
func TestHumanRunUI_构造函数标记(t *testing.T) {
	ui := NewHumanRunUI(&strings.Builder{}, &strings.Builder{})
	if ui.enhanced {
		t.Error("非终端不应 enhanced")
	}
	if ui.color {
		t.Error("非终端不应 color")
	}
}

// TestHumanRunUI_Start静默 start 不产出任何输出。
// Python: ui.py:406-408
func TestHumanRunUI_Start静默(t *testing.T) {
	ui, out, errOut := newEnhancedHumanUI()
	ui.Start()
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("start 应静默，得 %q / %q", out.String(), errOut.String())
	}
}

// TestHumanRunUI_Working无enhanced不输出 非装饰模式 working 不写状态。
// Python: ui.py:410-415
func TestHumanRunUI_Working无enhanced不输出(t *testing.T) {
	out := &strings.Builder{}
	ui := NewHumanRunUI(out, &strings.Builder{})
	ui.Working()
	if out.Len() != 0 {
		t.Errorf("非装饰模式不应输出，得 %q", out.String())
	}
}

// TestHumanRunUI_Working输出状态 装饰模式下写出「正在处理」。
func TestHumanRunUI_Working输出状态(t *testing.T) {
	ui, out, _ := newEnhancedHumanUI()
	ui.Working()
	if !strings.Contains(out.String(), "正在处理") {
		t.Errorf("缺处理状态：%q", out.String())
	}
}

// TestHumanRunUI_BeginAssistant 首次输出助手标记，第二次不再重复。
// Python: ui.py:417-424
func TestHumanRunUI_BeginAssistant(t *testing.T) {
	ui, out, _ := newEnhancedHumanUI()
	ui.BeginAssistant()
	first := out.String()
	if !strings.Contains(first, "UAPClaw") {
		t.Errorf("缺助手标记：%q", first)
	}
	ui.BeginAssistant()
	if out.String() != first {
		t.Errorf("第二次不应重复输出：%q", out.String())
	}
}

// TestHumanRunUI_Completed 完成标记含会话短 ID。
// Python: ui.py:526-539
func TestHumanRunUI_Completed(t *testing.T) {
	ui, out, _ := newEnhancedHumanUI()
	ui.Completed("0123456789abcdef")
	text := out.String()
	if !strings.Contains(text, "执行完成") {
		t.Errorf("缺完成文案：%q", text)
	}
	if !strings.Contains(text, "0123456789ab…") {
		t.Errorf("缺短会话 ID：%q", text)
	}
}

// TestHumanRunUI_SessionEvent四种 四种会话事件各一条文案，未知事件不输出。
// Python: ui.py:541-570
func TestHumanRunUI_SessionEvent四种(t *testing.T) {
	cases := map[string]string{
		"session.created": "已创建并切换到会话",
		"session.switched": "已恢复会话",
		"session.forked":  "已创建并切换到会话分支",
		"session.deleted": "已删除会话",
	}
	for eventType, want := range cases {
		ui, out, _ := newEnhancedHumanUI()
		ui.SessionEvent(eventType, map[string]any{"session_id": "s-1"})
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s → %q, 期望含 %q", eventType, out.String(), want)
		}
	}

	ui, out, _ := newEnhancedHumanUI()
	ui.SessionEvent("session.unknown", map[string]any{"session_id": "s-1"})
	if out.Len() != 0 {
		t.Errorf("未知事件不应输出：%q", out.String())
	}
}

// TestHumanRunUI_SessionEvent_forked带标题 标题非空时追加。
func TestHumanRunUI_SessionEvent_forked带标题(t *testing.T) {
	ui, out, _ := newEnhancedHumanUI()
	ui.SessionEvent("session.forked", map[string]any{
		"session_id": "s-1", "title": "分支标题",
	})
	if !strings.Contains(out.String(), "分支标题") {
		t.Errorf("缺标题：%q", out.String())
	}
}

// TestHumanRunUI_Failed写stderr 失败写 stderr，含标记与消息。
// Python: ui.py:572-583
func TestHumanRunUI_Failed(t *testing.T) {
	ui, out, errOut := newEnhancedHumanUI()
	ui.Failed("出错了")
	text := errOut.String()
	if !strings.Contains(text, "执行失败") || !strings.Contains(text, "出错了") {
		t.Errorf("失败输出 = %q", text)
	}
	if out.Len() != 0 {
		t.Errorf("失败信息不应写 stdout：%q", out.String())
	}
}

// TestHumanRunUI_Interrupted 中断写 stderr。
// Python: ui.py:585-591
func TestHumanRunUI_Interrupted(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.Interrupted()
	if !strings.Contains(errOut.String(), "已中断") {
		t.Errorf("中断输出 = %q", errOut.String())
	}
}

// TestHumanRunUI_LiveInputReady 实时输入就绪写 stderr 三段布局。
// Python: ui.py:593-599
func TestHumanRunUI_LiveInputReady(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.LiveInputReady("干点活")
	text := errOut.String()
	for _, want := range []string{"请求", "处理", "模型输出", "干点活"} {
		if !strings.Contains(text, want) {
			t.Errorf("缺 %q：%q", want, text)
		}
	}
}

// TestHumanRunUI_LiveInputReceipt三种 三种回执标记。
// Python: ui.py:601-626
func TestHumanRunUI_LiveInputReceipt三种(t *testing.T) {
	cases := map[string]string{
		"accepted": "已接收",
		"rejected": "被拒绝",
		"unknown":  "状态未知",
	}
	for status, message := range cases {
		ui, _, errOut := newEnhancedHumanUI()
		ui.LiveInputReceipt(status, message)
		if !strings.Contains(errOut.String(), message) {
			t.Errorf("%s → %q", status, errOut.String())
		}
	}
}

// TestHumanRunUI_Reasoning 思考块写 stderr 与缩进前缀。
// Python: ui.py:628-662
func TestHumanRunUI_Reasoning(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.Reasoning("第一行\n第二行")
	text := errOut.String()
	if !strings.Contains(text, "思考") {
		t.Errorf("缺思考标题：%q", text)
	}
	if !strings.Contains(text, "第一行") || !strings.Contains(text, "第二行") {
		t.Errorf("缺思考内容：%q", text)
	}
}

// TestHumanRunUI_Reasoning空串 空串不输出。
func TestHumanRunUI_Reasoning空串(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.Reasoning("")
	if errOut.Len() != 0 {
		t.Errorf("空思考不应输出：%q", errOut.String())
	}
}

// TestHumanRunUI_EndReasoning补换行 未闭合时补一个换行。
// Python: ui.py:664-675
func TestHumanRunUI_EndReasoning补换行(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.Reasoning("x")
	before := errOut.Len()
	ui.EndReasoning()
	if errOut.Len() == before {
		t.Error("未闭合的思考块应收尾（补换行）")
	}
	// 重复调用无副作用
	after := errOut.Len()
	ui.EndReasoning()
	if errOut.Len() != after {
		t.Error("重复 EndReasoning 不应再输出")
	}
}

// TestHumanRunUI_Tool 工具标签与内容写 stderr。
// Python: ui.py:677-686
func TestHumanRunUI_Tool(t *testing.T) {
	ui, _, errOut := newEnhancedHumanUI()
	ui.Tool("工具", "read_file")
	text := errOut.String()
	if !strings.Contains(text, "工具") || !strings.Contains(text, "read_file") {
		t.Errorf("工具输出 = %q", text)
	}
}

// TestHumanRunUI_ClearStatus无状态无输出 未显示状态时清屏不写。
// Python: ui.py:511-524
func TestHumanRunUI_ClearStatus无状态无输出(t *testing.T) {
	ui, out, _ := newEnhancedHumanUI()
	ui.ClearStatus()
	if out.Len() != 0 {
		t.Errorf("无状态时不应输出：%q", out.String())
	}
}

// ──────────────────────────── ProcessCliUI 渲染 ────────────────────────────

// TestProcessCliUI_Startup宽终端 卡片含模型/目录/模式/会话四项。
// Python: ui.py:179-249
func TestProcessCliUI_Startup宽终端(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.Startup("gpt-4", "agent.code", "/work", "s-1")
	text := out.String()
	for _, want := range []string{"UAPClaw", "gpt-4", "/work", "agent.code", "s-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("启动卡片缺 %q：%q", want, text)
		}
	}
}

// TestProcessCliUI_Startup窄终端 窄终端走无边框分支。
func TestProcessCliUI_Startup窄终端(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 40)
	ui.Startup("gpt-4", "agent.code", "/work", "")
	text := out.String()
	if !strings.Contains(text, "UAPClaw") {
		t.Errorf("窄终端缺标题：%q", text)
	}
	if !strings.Contains(text, "尚未创建") {
		t.Errorf("空会话应显示「尚未创建」：%q", text)
	}
}

// TestProcessCliUI_Status 状态行含模型/模式/会话/目录。
// Python: ui.py:251-292
func TestProcessCliUI_Status(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.Status("gpt-4", "agent.code", "/work", "0123456789abcdef")
	text := out.String()
	if !strings.Contains(text, "gpt-4") || !strings.Contains(text, "agent.code") {
		t.Errorf("状态行 = %q", text)
	}
	if !strings.Contains(text, "0123456789ab…") {
		t.Errorf("会话应短形式：%q", text)
	}
}

// TestProcessCliUI_Status空会话 无会话时显示「尚未创建会话」。
func TestProcessCliUI_Status空会话(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.Status("gpt-4", "agent.code", "/work", "")
	if !strings.Contains(out.String(), "尚未创建会话") {
		t.Errorf("状态行 = %q", out.String())
	}
}

// TestProcessCliUI_Notice 通知带感叹号前缀。
// Python: ui.py:294-296
func TestProcessCliUI_Notice(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Notice("注意")
	if !strings.Contains(out.String(), "注意") {
		t.Errorf("通知 = %q", out.String())
	}
}

// TestProcessCliUI_Diagnostics 诊断块含全部行。
// Python: ui.py:302-306
func TestProcessCliUI_Diagnostics(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Diagnostics([]string{"line-1", "line-2"})
	text := out.String()
	if !strings.Contains(text, "line-1") || !strings.Contains(text, "line-2") {
		t.Errorf("诊断 = %q", text)
	}
}

// TestProcessCliUI_BlankLine 输出一个换行。
func TestProcessCliUI_BlankLine(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.BlankLine()
	if out.String() != "\n" {
		t.Errorf("空行 = %q", out.String())
	}
}

// TestProcessCliUI_Session 有/无会话两种文案。
// Python: ui.py:318-323
func TestProcessCliUI_Session(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Session("s-1")
	if !strings.Contains(out.String(), "s-1") {
		t.Errorf("有会话 = %q", out.String())
	}

	out.Reset()
	ui = NewProcessCliUI(out, 80)
	ui.Session("")
	if !strings.Contains(out.String(), "尚未创建") {
		t.Errorf("无会话 = %q", out.String())
	}
}

// TestProcessCliUI_Skills两组 已安装与可安装分组渲染。
// Python: ui.py:289-316
func TestProcessCliUI_Skills两组(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Skills([]any{
		map[string]any{"name": "installed-skill", "installed": true, "is_builtin": true},
		map[string]any{"name": "available-skill", "installed": false, "source": "project"},
	})
	text := out.String()
	if !strings.Contains(text, "已安装技能") {
		t.Errorf("缺已安装分组：%q", text)
	}
	if !strings.Contains(text, "可安装技能") {
		t.Errorf("缺可安装分组：%q", text)
	}
	if !strings.Contains(text, "内置") {
		t.Errorf("内置来源应标注：%q", text)
	}
}

// TestProcessCliUI_Skills全空 无技能时提示。
func TestProcessCliUI_Skills全空(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Skills(nil)
	if !strings.Contains(out.String(), "暂无已安装技能") {
		t.Errorf("空技能 = %q", out.String())
	}
}

// TestProcessCliUI_Details 标题与正文缩进渲染。
// Python: ui.py:298-300
func TestProcessCliUI_Details(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.Details("会话", []string{"a", "b"})
	text := out.String()
	for _, want := range []string{"会话", "a", "b"} {
		if !strings.Contains(text, want) {
			t.Errorf("缺 %q：%q", want, text)
		}
	}
}

// TestProcessCliUI_Help两列 宽终端两列排布，含全部命令名。
// Python: ui.py:203-232
func TestProcessCliUI_Help两列(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.Help(false)
	text := out.String()
	if !strings.Contains(text, "可用命令") {
		t.Errorf("缺标题：%q", text)
	}
	if !strings.Contains(text, "/help") {
		t.Errorf("缺 /help：%q", text)
	}
}

// TestProcessCliUI_Help窄终端 窄终端一列。
func TestProcessCliUI_Help窄终端(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 50)
	ui.Help(false)
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("窄终端缺 /help：%q", out.String())
	}
}

// TestProcessCliUI_LiveExecutionLayout 三段布局。
// Python: ui.py:341-360
func TestProcessCliUI_LiveExecutionLayout(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.LiveExecutionLayout("任务文本")
	text := out.String()
	for _, want := range []string{"请求", "处理", "模型输出", "任务文本"} {
		if !strings.Contains(text, want) {
			t.Errorf("缺 %q：%q", want, text)
		}
	}
}

// TestProcessCliUI_LiveExecutionLayout空请求 空请求显示占位。
func TestProcessCliUI_LiveExecutionLayout空请求(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.LiveExecutionLayout("   ")
	if !strings.Contains(out.String(), "空请求") {
		t.Errorf("空请求应显示占位：%q", out.String())
	}
}

// TestProcessCliUI_Card边框 卡片用边框字符包裹。
// Python: ui.py:386-406
func TestProcessCliUI_Card边框(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.card([][2]string{{"inside", ""}})
	text := out.String()
	if !strings.Contains(text, "inside") {
		t.Errorf("缺内容：%q", text)
	}
	if !strings.ContainsAny(text, "╭╮╰╯") {
		t.Errorf("缺 Unicode 边框：%q", text)
	}
}

// TestProcessCliUI_CardASCII降级 不支持 Unicode 时用 ASCII 边框。
func TestProcessCliUI_CardASCII降级(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 100)
	ui.unicode = false
	ui.card([][2]string{{"inside", ""}})
	text := out.String()
	if !strings.Contains(text, "+") || !strings.Contains(text, "|") {
		t.Errorf("ASCII 边框缺失：%q", text)
	}
}

// TestProcessCliUI_Styled颜色开关 color 关闭时原样返回。
// Python: ui.py:412-415
func TestProcessCliUI_Styled颜色开关(t *testing.T) {
	out := &strings.Builder{}
	ui := NewProcessCliUI(out, 80)
	ui.color = false
	if got := ui.styled("text", "\033[1m"); got != "text" {
		t.Errorf("无色应原样返回：%q", got)
	}
	ui.color = true
	if got := ui.styled("text", "\033[1m"); !strings.Contains(got, "\033[1m") {
		t.Errorf("有色应包转义：%q", got)
	}
}


package processcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// 本文件是 channels/process_cli/render.py 的 Go 对应物。
//
// Python: channels/process_cli/render.py（214 行）
//
// 职责：渲染 Runtime 事件流而不影响执行。三态输出（human / json / jsonl）
// 共用同一条事件流；human 模式的八个分支顺序不可调换。

// ──────────────────────────── 结构体 ────────────────────────────

// EventRenderer 渲染事件而不影响 Runtime 执行。
// Python: render.py:56-208 EventRenderer
type EventRenderer struct {
	// outputFormat è¾åºæ ¼å¼ï¼human / json / jsonl
	outputFormat string
	// stdout 用户可见输出
	stdout io.Writer
	// stderr 用户可见错误
	stderr io.Writer
	// showReasoning 是否显示思考过程
	showReasoning bool
	// showTools 是否显示工具调用与结果
	showTools bool
	// conciseErrors 是否精简错误文案（非 worker 命令用）
	conciseErrors bool
	// events 累积事件（json 模式 Finish 时一次性输出）
	events []map[string]any
	// wroteDelta 是否已写出过增量文本
	wroteDelta bool
	// pendingBreak 下一次输出前是否先空一行
	pendingBreak bool
	// failed 是否发生过失败事件
	failed bool
	// humanUI 人类模式装饰器
	humanUI *HumanRunUI
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OutputHuman 人类可读输出
	OutputHuman = "human"
	// OutputJSON 单份 JSON 文档（累积到 Finish）
	OutputJSON = "json"
	// OutputJSONL 逐行 JSONL
	OutputJSONL = "jsonl"
	// conciseErrorMaxRunes 精简错误文案的单行上限
	// Python: render.py:48 len(translated) > 240
	conciseErrorMaxRunes = 240
)

// ──────────────────────────── 全局变量 ────────────────────────────

// humanErrorTranslations 固定错误文案的中文翻译。
// Python: render.py:14-19 _HUMAN_ERROR_TRANSLATIONS
var humanErrorTranslations = map[string]string{
	"process CLI execution timed out":                                                  "进程式 CLI 执行超时",
	"process CLI received an interaction request but interactive input is unavailable": "当前输出模式无法接收交互输入，请使用交互式 CLI 完成此操作",
}

// httpStatusPattern 从错误文本提取 HTTP 状态码。
// Python: render.py:39 re.search(r"Error code:\s*(\d{3})", translated)
var httpStatusPattern = regexp.MustCompile(`Error code:\s*(\d{3})`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventRenderer 构造渲染器。
// Python: render.py:68-95 EventRenderer.__init__
func NewEventRenderer(
	outputFormat string, stdout, stderr io.Writer,
	showReasoning, showTools, conciseErrors bool,
) *EventRenderer {
	return &EventRenderer{
		outputFormat:  outputFormat,
		stdout:        stdout,
		stderr:        stderr,
		showReasoning: showReasoning,
		showTools:     showTools,
		conciseErrors: conciseErrors,
	}
}

// Start 初始化输出。
// Python: render.py:97-99
func (r *EventRenderer) Start() {
	if r.outputFormat == OutputHuman {
		r.ensureHumanUI().Start()
	}
}

// Working 显示处理中状态。
// Python: render.py:101-103
func (r *EventRenderer) Working() {
	if r.outputFormat == OutputHuman {
		r.ensureHumanUI().Working()
	}
}

// Interrupted 显示中断状态。
// Python: render.py:105-107
func (r *EventRenderer) Interrupted() {
	if r.outputFormat == OutputHuman {
		r.ensureHumanUI().Interrupted()
	}
}

// PrepareInteraction 清掉瞬时状态，准备让 Runtime 提问。
// Python: render.py:109-113
func (r *EventRenderer) PrepareInteraction() {
	if r.outputFormat == OutputHuman {
		ui := r.ensureHumanUI()
		ui.EndReasoning()
		ui.ClearStatus()
	}
}

// Render 渲染一条事件。
// Python: render.py:123-155 EventRenderer.render
func (r *EventRenderer) Render(event *swruntime.RuntimeEvent, view string) {
	if event == nil {
		return
	}
	data := event.ToDict()
	r.events = append(r.events, data)

	eventType := event.EventType()
	if !event.OK || eventType == "chat.error" || eventType == "runtime.error" || eventType == "team.error" {
		r.failed = true
	}

	switch r.outputFormat {
	case OutputJSONL:
		// Python: :141-145 json.dumps(data, ensure_ascii=False) + "\n"
		// 必须禁用 HTML 转义，否则 < > & 变 \u003c 等，与上游字节不一致。
		line, err := marshalNoEscape(data)
		if err != nil {
			return
		}
		fmt.Fprint(r.stdout, line)
		return
	case OutputJSON:
		// Python: :146-147 json 模式只累积
		return
	}

	// Python: :148-153 skills 视图优先
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if view == "skills.list" && event.OK {
		skills, _ := payload["skills"].([]any)
		r.ensureHumanUI().Skills(skills)
		return
	}
	r.renderHuman(event)
}

// Finish 收尾输出。
// Python: render.py:157-182 EventRenderer.finish
func (r *EventRenderer) Finish(sessionID, requestID string, showCompletion bool) {
	if r.outputFormat == OutputHuman {
		r.ensureHumanUI().EndReasoning()
	}
	switch r.outputFormat {
	case OutputJSON:
		document := map[string]any{
			"ok":         !r.failed,
			"session_id": sessionID,
			"request_id": requestID,
			"events":     r.events,
		}
		line, err := marshalNoEscape(document)
		if err != nil {
			return
		}
		fmt.Fprint(r.stdout, line)
	case OutputHuman:
		// Python: :175-176 human 模式补一个换行
		if r.wroteDelta {
			fmt.Fprint(r.stdout, "\n")
		}
	}
	// Python: :178-179 仅在成功且有完成提示时打完成行
	if r.outputFormat == OutputHuman && !r.failed && showCompletion {
		r.ensureHumanUI().Completed(sessionID)
	}
}

// Failed 是否发生过失败事件。
func (r *EventRenderer) Failed() bool { return r.failed }

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalNoEscape 序列化为紧凑 JSON 并追加换行（不转义 HTML）。
// Python: json.dumps(..., ensure_ascii=False, separators=(",", ":")) + "\n"
func marshalNoEscape(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ensureHumanUI 惰性构造人类模式装饰器。
func (r *EventRenderer) ensureHumanUI() *HumanRunUI {
	if r.humanUI == nil {
		r.humanUI = NewHumanRunUI(r.stdout, r.stderr)
	}
	return r.humanUI
}

// renderHuman human 模式的事件渲染。
// Python: render.py:184-208 _render_human（八分支顺序不可调换）
func (r *EventRenderer) renderHuman(event *swruntime.RuntimeEvent) {
	eventType := event.EventType()
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	ui := r.ensureHumanUI()

	// 分支 1：团队成员流不拼进单一可读答案
	role, _ := payload["role"].(string)
	teammate := strings.EqualFold(strings.TrimSpace(role), "teammate")
	if teammate && (eventType == "chat.delta" || eventType == "chat.final" || eventType == "chat.reasoning") {
		return
	}

	text := eventText(payload)

	// 分支 2：steering 分代开始 → 后续输出前空一行
	if steering, ok := payload["steering_generation_start"].(bool); ok && steering && r.wroteDelta {
		r.pendingBreak = true
	}

	switch {
	case eventType == "chat.delta":
		ui.EndReasoning()
		if r.pendingBreak {
			fmt.Fprint(r.stdout, "\n\n")
			r.pendingBreak = false
		}
		ui.BeginAssistant()
		fmt.Fprint(r.stdout, text)
		r.wroteDelta = true
	case eventType == "chat.final":
		ui.EndReasoning()
		if text != "" && !r.wroteDelta {
			ui.BeginAssistant()
			fmt.Fprint(r.stdout, text)
			r.wroteDelta = true
		}
		if r.wroteDelta {
			r.pendingBreak = true
		}
	case eventType == "chat.reasoning":
		if r.wroteDelta {
			r.pendingBreak = true
		}
		if r.showReasoning && text != "" {
			ui.BeginAssistant()
			ui.Reasoning(text)
		}
	case eventType == "chat.tool_call" || eventType == "chat.tool_result":
		if !teammate {
			ui.EndReasoning()
		}
		if r.wroteDelta && !teammate {
			r.pendingBreak = true
		}
		if r.showTools {
			ui.BeginAssistant()
			label := "工具"
			if eventType == "chat.tool_result" {
				label = "工具结果"
			}
			if text == "" {
				text = fmt.Sprint(payload)
			}
			ui.Tool(label, text)
		}
	case !event.OK || eventType == "chat.error" || eventType == "runtime.error" || eventType == "team.error":
		message := text
		if message == "" {
			if raw, ok := payload["error"].(string); ok {
				message = raw
			} else {
				message = fmt.Sprint(payload)
			}
		}
		if r.conciseErrors {
			message = humanErrorSummary(message)
		} else if translated, ok := humanErrorTranslations[message]; ok {
			message = translated
		}
		ui.Failed(message)
	case eventType == "plan.mode_exited":
		// Python: :203-207
		ui.ClearStatus()
		mode, _ := payload["mode"].(string)
		if mode == "" {
			mode = "normal"
		}
		fmt.Fprintf(r.stderr, "\n! 计划模式已退出，当前模式：%s\n", mode)
	case strings.HasPrefix(eventType, "session."):
		ui.SessionEvent(eventType, payload)
	}
}

// eventText 依次取 delta/content/text/message/answer 第一个非空字符串。
// Python: render.py:22-28 _event_text
func eventText(payload map[string]any) string {
	for _, key := range []string{"delta", "content", "text", "message", "answer"} {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// humanErrorSummary 生成人类可读的错误摘要（240 字上限）。
// Python: render.py:32-52 _human_error_summary
func humanErrorSummary(message string) string {
	translated, ok := humanErrorTranslations[message]
	if !ok {
		translated = message
	}
	lowered := strings.ToLower(translated)
	if strings.Contains(lowered, "model call failed") {
		suffix := ""
		if match := httpStatusPattern.FindStringSubmatch(translated); match != nil {
			suffix = fmt.Sprintf("（HTTP %s）", match[1])
		}
		if strings.Contains(lowered, "access denied") {
			return fmt.Sprintf("模型服务拒绝访问%s；请检查模型账号或服务权限。", suffix)
		}
		if len([]rune(translated)) > conciseErrorMaxRunes || strings.Contains(translated, "\n") {
			return fmt.Sprintf("模型调用失败%s；详细原因见诊断日志。", suffix)
		}
	}
	trimmed := strings.TrimSpace(translated)
	if trimmed == "" {
		return "未知错误"
	}
	firstLine := strings.SplitN(trimmed, "\n", 2)[0]
	if len([]rune(firstLine)) > conciseErrorMaxRunes || strings.Contains(translated, "\n") {
		runes := []rune(firstLine)
		if len(runes) > conciseErrorMaxRunes {
			runes = runes[:conciseErrorMaxRunes]
		}
		return strings.TrimRight(string(runes), " ") + "…"
	}
	return firstLine
}

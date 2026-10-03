package monitor

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sourceKey 每源键（member, role）。
// 对齐 Python: _runs 的键类型 tuple[str | None, str | None]
type sourceKey struct {
	member string
	role   string
}

// run 每个 source 的累积中 run。
// 对齐 Python: _Run dataclass
type run struct {
	category string
	buf      []string
}

// TeamStreamLogger 可选的处理对象，将聚合的团队流记录写入文件。
// 对齐 Python: TeamStreamLogger (openjiuwen/agent_teams/monitor/stream_logger.py)
//
// 块分为两类：
//   - 累积型（llm_output/llm_reasoning）：按 (member, role) 缓冲，同一 source 切换 category 时刷新
//   - 离散型（所有其他类型）：立即写入，写入前先刷新该 source 的待处理 run
//
// answer chunk 被 llmOutputSeen 去重。
// Feed 和 Flush 绝不 panic（recover 兜底）。
type TeamStreamLogger struct {
	file          *os.File
	mu            sync.Mutex
	runs          map[sourceKey]*run
	llmOutputSeen map[sourceKey]bool
	chunkCount    int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// 块类型常量，对齐 Python _CHUNK_*
	chunkLLMOutput       = "llm_output"
	chunkLLMReasoning    = "llm_reasoning"
	chunkAnswer          = "answer"
	chunkInteraction     = "__interaction__"
	chunkMessage         = "message"
	chunkToolCall        = "tool_call"
	chunkToolResult      = "tool_result"
	chunkToolUpdate      = "tool_update"
	chunkTodoUpdated     = "todo.updated"
	chunkControllerOutput = "controller_output"

	// runtime_ready 事件类型
	runtimeReadyEvent = "team.runtime_ready"

	// 截断限制
	toolResultCap = 2000
	toolArgsCap   = 500
	genericCap    = 2000

	// 未知占位符
	unknown = "<unknown>"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// accumulatingTypes 累积型块类型集合
	// 对齐 Python: _ACCUMULATING_TYPES = frozenset({_CHUNK_LLM_OUTPUT, _CHUNK_LLM_REASONING})
	accumulatingTypes = map[string]bool{
		chunkLLMOutput:    true,
		chunkLLMReasoning: true,
	}

	// categoryLevel 类别→日志级别映射
	// 对齐 Python: _CATEGORY_LEVEL
	categoryLevel = map[string]string{
		"text":             "INFO",
		"reasoning":        "DEBUG",
		"tool_call":        "DEBUG",
		"tool_result":      "DEBUG",
		"tool_update":      "DEBUG",
		"interaction":      "WARN",
		"controller_output": "WARN",
		"runtime_ready":    "INFO",
		"message":          "INFO",
		"todo":             "INFO",
		"other":            "INFO",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamStreamLogger 创建 TeamStreamLogger。
// 对齐 Python: TeamStreamLogger.__init__(file_path)
func NewTeamStreamLogger(filePath string) (*TeamStreamLogger, error) {
	if err := os.MkdirAll(filePath[:strings.LastIndex(filePath, "/")], 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	return &TeamStreamLogger{
		file:          f,
		runs:          make(map[sourceKey]*run),
		llmOutputSeen: make(map[sourceKey]bool),
	}, nil
}

// Feed 消费一个流块。绝不 panic。
// 对齐 Python: TeamStreamLogger.feed(chunk)
func (l *TeamStreamLogger) Feed(chunk map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			l.safeWrite(fmt.Sprintf("[WARN] stream logger feed error: %v", r))
		}
	}()
	l.feed(chunk)
}

// Flush 刷新所有待处理 run 并关闭文件。绝不 panic。
// 对齐 Python: TeamStreamLogger.flush()
func (l *TeamStreamLogger) Flush() {
	defer func() {
		if r := recover(); r != nil {
			l.safeWrite(fmt.Sprintf("[WARN] stream logger flush error: %v", r))
		}
	}()
	l.mu.Lock()
	defer l.mu.Unlock()

	for key := range l.runs {
		l.flushKey(key)
	}
	if l.chunkCount > 0 {
		l.safeWrite(fmt.Sprintf("[INFO] stream end, %d chunks", l.chunkCount))
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// feed 核心处理逻辑。
// 对齐 Python: TeamStreamLogger._feed(chunk)
func (l *TeamStreamLogger) feed(chunk map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.chunkCount++

	ctype, _ := chunk["type"].(string)
	if ctype == "" {
		return
	}
	payload := chunk["payload"]
	member, _ := chunk["source_member"].(string)
	role := renderRole(chunk["role"])
	key := sourceKey{member: member, role: role}

	// answer 重复了已流式传输的 llm_output；去重
	if ctype == chunkAnswer && l.llmOutputSeen[key] {
		return
	}

	category := classify(ctype, payload)

	if accumulatingTypes[ctype] {
		content := extractContent(payload)
		if content == "" {
			return
		}
		r := l.runs[key]
		if r != nil && r.category != category {
			l.flushKey(key)
			r = nil
		}
		if r == nil {
			r = &run{category: category}
			l.runs[key] = r
		}
		r.buf = append(r.buf, content)
		if ctype == chunkLLMOutput {
			l.llmOutputSeen[key] = true
		}
		return
	}

	// 离散型：先刷新该 source 的待处理 run，再立即写入
	l.flushKey(key)
	summary := discreteSummary(category, payload)
	l.emit(category, member, role, summary)
}

// flushKey 刷新指定 source 的待处理 run。
func (l *TeamStreamLogger) flushKey(key sourceKey) {
	r, ok := l.runs[key]
	if !ok || r == nil {
		return
	}
	delete(l.runs, key)
	if len(r.buf) > 0 {
		content := strings.Join(r.buf, "")
		l.emit(r.category, key.member, key.role, content)
	}
}

// classify 块分类。
// 对齐 Python: _classify(ctype, payload)
func classify(ctype string, payload any) string {
	switch ctype {
	case chunkLLMOutput, chunkAnswer:
		return "text"
	case chunkLLMReasoning:
		return "reasoning"
	case chunkToolCall:
		return "tool_call"
	case chunkToolResult:
		return "tool_result"
	case chunkToolUpdate:
		return "tool_update"
	case chunkInteraction:
		return "interaction"
	case chunkControllerOutput:
		return "controller_output"
	case chunkMessage:
		if m, ok := payload.(map[string]any); ok {
			if et, _ := m["event_type"].(string); et == runtimeReadyEvent {
				return "runtime_ready"
			}
		}
		return "message"
	case chunkTodoUpdated:
		return "todo"
	default:
		return "other"
	}
}

// emit 写入格式化日志行。
// 对齐 Python: _emit(category, member, role, content)
func (l *TeamStreamLogger) emit(category, member, role, content string) {
	if content == "" {
		return
	}
	level, ok := categoryLevel[category]
	if !ok {
		level = "INFO"
	}
	if member == "" {
		member = unknown
	}
	if role == "" {
		role = unknown
	}
	prefixed := strings.Join(strings.Split(content, "\n"), "\n  | ")
	prefixed = "  | " + prefixed
	header := fmt.Sprintf("[%s] member=%s role=%s category=%s", level, member, role, category)
	l.safeWrite(fmt.Sprintf("%s %s\n%s", time.Now().Format("2006-01-02 15:04:05.000"), header, prefixed))
}

// safeWrite 安全写入文件行。
func (l *TeamStreamLogger) safeWrite(line string) {
	if l.file == nil {
		return
	}
	_, _ = l.file.WriteString(line + "\n")
}

// discreteSummary 离散型块摘要。
// 对齐 Python: TeamStreamLogger._discrete_summary(category, payload)
func discreteSummary(category string, payload any) string {
	switch category {
	case "tool_call":
		return toolCallSummary(payload)
	case "tool_result":
		return toolResultSummary(payload)
	case "tool_update":
		return toolUpdateSummary(payload)
	case "controller_output":
		return controllerOutputSummary(payload)
	case "runtime_ready":
		return runtimeReadySummary(payload)
	case "interaction":
		return interactionSummary(payload)
	default:
		return genericSummary(payload)
	}
}

// toolCallSummary 工具调用摘要。
// 对齐 Python: _tool_call_summary(payload)
func toolCallSummary(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	name, _ := m["tool_name"].(string)
	argsRaw := fmt.Sprintf("%v", m["tool_args"])
	if name == "" && argsRaw == "" {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	args := capStr(argsRaw, toolArgsCap)
	return fmt.Sprintf("tool_name=%s tool_args=%s", name, args)
}

// toolResultSummary 工具结果摘要。
// 对齐 Python: _tool_result_summary(payload)
func toolResultSummary(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	name, _ := m["tool_name"].(string)
	argsRaw := fmt.Sprintf("%v", m["tool_args"])
	resultRaw := fmt.Sprintf("%v", m["tool_result"])
	if name == "" && argsRaw == "" && resultRaw == "" {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	args := capStr(argsRaw, toolArgsCap)
	result := capStr(resultRaw, toolResultCap)
	return fmt.Sprintf("tool_name=%s tool_args=%s\nresult: %s", name, args, result)
}

// toolUpdateSummary 工具更新摘要。
// 对齐 Python: _tool_update_summary(payload)
func toolUpdateSummary(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	update, _ := m["tool_update"].(map[string]any)
	if update == nil {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	name, _ := update["tool_name"].(string)
	status, _ := update["status"].(string)
	callID, _ := update["tool_call_id"].(string)
	args := capStr(fmt.Sprintf("%v", update["arguments"]), toolArgsCap)
	return fmt.Sprintf("tool_name=%s status=%s tool_call_id=%s arguments=%s", name, status, callID, args)
}

// controllerOutputSummary 控制器输出摘要。
// 对齐 Python: _controller_output_summary(payload)
func controllerOutputSummary(payload any) string {
	m, ok := payload.(map[string]any)
	if ok {
		payloadType := strings.ToLower(fmt.Sprintf("%v", m["type"]))
		if strings.Contains(payloadType, "task_failed") {
			data, _ := m["data"].([]any)
			var texts []string
			for _, item := range data {
				if im, ok := item.(map[string]any); ok {
					text := strings.TrimSpace(fmt.Sprintf("%v", im["text"]))
					if text != "" {
						texts = append(texts, text)
					}
				}
			}
			if len(texts) > 0 {
				return strings.Join(texts, "\n")
			}
		}
	}
	return capStr(fmt.Sprintf("%v", payload), genericCap)
}

// runtimeReadySummary 运行时就绪摘要。
// 对齐 Python: _runtime_ready_summary(payload)
func runtimeReadySummary(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return capStr(fmt.Sprintf("%v", payload), genericCap)
	}
	return fmt.Sprintf("team=%v session=%v activation=%v",
		m["team_name"], m["session_id"], m["activation_kind"])
}

// interactionSummary 交互摘要。
// 对齐 Python: _interaction_summary(payload)
func interactionSummary(payload any) string {
	iid := "unknown"
	if m, ok := payload.(map[string]any); ok {
		if v, ok := m["interaction_id"].(string); ok {
			iid = v
		}
	}
	return fmt.Sprintf("interaction_id=%s\n%s", iid, capStr(fmt.Sprintf("%v", payload), genericCap))
}

// genericSummary 通用摘要。
// 对齐 Python: _generic_summary(payload)
func genericSummary(payload any) string {
	content := extractContent(payload)
	if content != "" {
		return content
	}
	return capStr(fmt.Sprintf("%v", payload), genericCap)
}

// extractContent 从 payload 中提取内容。
// 对齐 Python: _extract_content(payload)
func extractContent(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if c, _ := m["content"].(string); c != "" {
			return c
		}
		if o, _ := m["output"].(string); o != "" {
			return o
		}
	}
	if s, ok := payload.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", payload)
}

// capStr 截断字符串。
// 对齐 Python: _cap(text, limit)
func capStr(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "… (truncated)"
}

// renderRole 渲染角色值。
// 对齐 Python: _render_role(role)
func renderRole(role any) string {
	if role == nil {
		return ""
	}
	return fmt.Sprintf("%v", role)
}

// ensure logComponent is used
var _ = logger.ComponentChannel

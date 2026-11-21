package agent_teams

// fork_compact.go — fork 感知的上下文压缩（注入前对旧消息做模型摘要）。
//
// fork 上下文过大时，compact_context 把 split 点某一侧压缩成模型生成的摘要、
// 另一侧逐字保留。
//
// 对应 Python 代码：openjiuwen/agent_teams/fork_compact.py（148 行，一比一）。

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

const logComponent = logger.ComponentTeam

// ──────────────────────────── 常量 ────────────────────────────

// compactionPrompt 压缩提示词（英文原文一比一复刻，不翻译）。
// Python: _COMPACTION_PROMPT (fork_compact.py:21-32)
const compactionPrompt = "Your task is to create a detailed summary of the conversation history below. " +
	"Pay close attention to every user request, file read, search result, " +
	"analysis conclusion, and technical decision that was made.\n\n" +
	"The summary must retain:\n" +
	"- Every file path that was read or written\n" +
	"- Key code patterns and architectural decisions\n" +
	"- Errors encountered and how they were resolved\n" +
	"- User feedback and changes of direction\n\n" +
	"Summarise concisely but do not omit any fact that would be necessary " +
	"to continue development work without losing context.\n\n" +
	"=== BEGIN CONVERSATION HISTORY ===\n"

// compactionMaxTokens 摘要调用输出上限。
// Python: max_tokens=4096（_run_compaction, fork_compact.py:128）
const compactionMaxTokens = 4096

// ──────────────────────────── 结构体 ────────────────────────────

// CompactContextTarget compact_context 的目标窄接口——Python duck-typing
// deep_agent 的收窄（agent_teams 根包不能 import harness 包，依赖倒置）。
// Python: compact_context(deep_agent, ...) 直接调 deep_agent 的三个属性。
type CompactContextTarget interface {
	// GetCurrentContext 读取当前上下文消息
	// Python: deep_agent.get_current_context(session_id)
	GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error)
	// ContextEngineForCheckpoint 取目标 context engine（回写压缩结果）。
	// 复用 25.1f checkpoint 捕获的既有访问器（Python: react.context_engine）。
	ContextEngineForCheckpoint() ceinterface.ContextEngine
	// CompactionModel 摘要模型（Python: deep_agent.deep_config.model）。
	CompactionModel() *llm.Model
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CompactContext 把 fork 上下文在 splitAt 处的一侧压缩成摘要。
//
// direction="before"（默认）：压缩头部 msgs[:splitAt] 成摘要、尾部逐字保留；
// direction="after"：头部逐字保留、压缩尾部 msgs[splitAt:]。
// 压缩前剥掉全部 SystemMessage。
//
// Python: compact_context (fork_compact.py:36-95)
func CompactContext(ctx context.Context, target CompactContextTarget, splitAt int, sessionID string, direction string) {
	// Python: msgs = deep_agent.get_current_context(session_id=session_id)
	msgs, err := target.GetCurrentContext(ctx, sessionID, "")
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("[fork] compact_context: read current context failed")
		return
	}
	// Python: [m for m in msgs if not isinstance(m, SystemMessage)]（:61-62）
	filtered := make([]llmschema.BaseMessage, 0, len(msgs))
	for _, m := range msgs {
		if _, isSystem := m.(*llmschema.SystemMessage); isSystem {
			continue
		}
		filtered = append(filtered, m)
	}
	msgs = filtered

	// Python: if split_at <= 0 or split_at >= len(msgs): skip（info 日志）
	if splitAt <= 0 || splitAt >= len(msgs) {
		logger.Info(logComponent).
			Int("split_at", splitAt).
			Int("len", len(msgs)).
			Msg("[fork] compact_context: skipping")
		return
	}

	var keptMsgs, toCompress []llmschema.BaseMessage
	if direction == "after" {
		// 保留头部逐字，压缩尾部。头部必须延伸过 checkpoint 调用的闭合
		// ToolMessage 块——否则它以悬挂 tool call 结尾（其 result 落在被压缩的
		// 尾部里），context rail 会把它标成「工具执行被中断」。
		// Python: fork_compact.py:73-79
		headEnd := splitAt
		for headEnd < len(msgs) && isToolMessage(msgs[headEnd]) {
			headEnd++
		}
		keptMsgs = msgs[:headEnd]
		toCompress = msgs[headEnd:]
	} else {
		// Python: kept_msgs = msgs[split_at:]; to_compress = msgs[:split_at]
		keptMsgs = msgs[splitAt:]
		toCompress = msgs[:splitAt]
	}

	logger.Info(logComponent).
		Int("compressing", len(toCompress)).
		Int("keeping", len(keptMsgs)).
		Msg("[fork] compact_context: compressing")

	summary := runCompaction(ctx, target, toCompress)
	summaryMsg := llmschema.NewUserMessage(fmt.Sprintf(
		"[Compacted context — earlier conversation summarised]\n\n%s", summary))

	var compacted []llmschema.BaseMessage
	if direction == "after" {
		compacted = append(append([]llmschema.BaseMessage{}, keptMsgs...), summaryMsg)
	} else {
		compacted = append([]llmschema.BaseMessage{summaryMsg}, keptMsgs...)
	}

	// 直接替换既有 context engine 里的消息——create_new_context_engine 会新建
	// 一个 Session，其 get_state() 返回 None，_load_state_from_session 会静默
	// 跳过新消息，所以必须原地回写。
	// Python: fork_compact.py:88-98（react.context_engine.get_context(...).set_messages）
	engine := target.ContextEngineForCheckpoint()
	if engine != nil {
		resolvedSession := sessionID
		if resolvedSession == "" {
			// Python: session_id or "default_session_id"
			resolvedSession = "default_session_id"
		}
		context := engine.GetContext("default_context_id", resolvedSession)
		if context != nil {
			// Python: context.set_messages(compacted)——全量替换（含历史）
			context.SetMessages(compacted, true)
		}
	}

	logger.Info(logComponent).
		Int("after", len(compacted)).
		Msg("[fork] compact_context: done")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runCompaction 用 agent 自身模型跑单轮摘要。
// 模型 KV cache 里仍有 fork 上下文，摘要调用只付出输出 token 成本。
// Python: _run_compaction (fork_compact.py:116-137)
func runCompaction(ctx context.Context, target CompactContextTarget, oldMsgs []llmschema.BaseMessage) string {
	model := target.CompactionModel()
	if model == nil {
		// Python: config is None or config.model is None → "[compaction skipped: no model available]"
		return "[compaction skipped: no model available]"
	}
	prompt := compactionPrompt + renderMessageTexts(oldMsgs)
	response, err := model.Invoke(ctx,
		model_clients.NewTextMessagesParam(prompt),
		model_clients.WithInvokeMaxTokens(compactionMaxTokens))
	if err != nil {
		// Python: except Exception → warning + f"[compaction failed: {exc}]"
		logger.Warn(logComponent).
			Err(err).
			Msg("[fork] compaction failed")
		return fmt.Sprintf("[compaction failed: %v]", err)
	}
	if response == nil {
		// Python: return response.content if response else "[compaction returned empty response]"
		return "[compaction returned empty response]"
	}
	return response.GetContent().Text()
}

// renderMessageTexts 把消息列表渲染成压缩提示词用的纯文本。
// Python: _render_message_texts (fork_compact.py:140-147)
func renderMessageTexts(messages []llmschema.BaseMessage) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := msg.GetContent().Text()
		if content != "" {
			parts = append(parts, fmt.Sprintf("[%s]\n%s", msg.GetRole(), content))
		}
	}
	return strings.Join(parts, "\n\n")
}

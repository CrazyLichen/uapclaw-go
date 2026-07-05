package context

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMemoryUpdater 会话记忆更新器接口。
// direct_replace 模式由 SessionMemoryDirectUpdater 实现；
// agent_edit 模式 ⤵️ 6.x 回填，由 SessionMemoryAgentUpdater 实现。
type SessionMemoryUpdater interface {
	// Invoke 执行记忆更新
	Invoke(ctx context.Context, opts SessionMemoryUpdateOptions) error
	// BindModelDefaults 绑定默认模型配置
	BindModelDefaults(modelConfig *llm_schema.ModelRequestConfig, clientConfig *llm_schema.ModelClientConfig)
	// SetInheritedSystemPrompt 设置继承的系统提示词
	SetInheritedSystemPrompt(prompt string)
}

// SessionMemoryConfig 会话记忆配置。
//
// 对应 Python: openjiuwen/core/context_engine/context/session_memory_manager.py (SessionMemoryConfig)
type SessionMemoryConfig struct {
	// TriggerTokens 首次触发更新的 token 阈值
	TriggerTokens int
	// TriggerAddTokens 增量触发更新的 token 阈值
	TriggerAddTokens int
	// ToolMin 最少工具调用次数阈值
	ToolMin int
	// Model 模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// UpdateMode 更新模式："agent_edit" | "direct_replace"
	UpdateMode string
	// DirectReplaceMaxRetries direct_replace 模式最大重试次数
	DirectReplaceMaxRetries int
}

// SessionMemoryUpdateOptions 记忆更新选项
type SessionMemoryUpdateOptions struct {
	// FullContextMessages 完整上下文消息列表
	FullContextMessages []llm_schema.BaseMessage
	// NotesPath 笔记文件路径
	NotesPath string
	// CurrentNotes 当前笔记内容
	CurrentNotes string
}

// SessionMemoryDirectUpdater direct_replace 模式的会话记忆更新器。
// 通过直接调用 LLM 生成完整笔记内容并写入文件。
//
// 对应 Python: SessionMemoryUpdateAgent._invoke_direct_replace()
type SessionMemoryDirectUpdater struct {
	config                SessionMemoryConfig
	model                 *llm.Model
	inheritedSystemPrompt string
}

// SessionMemoryAgentUpdater agent_edit 模式的会话记忆更新器（骨架）。
// ⤵️ 6.x 回填：完整实现需要 ReActAgent
type SessionMemoryAgentUpdater struct {
	config SessionMemoryConfig
}

// SessionMemoryManager 会话记忆管理器，协调何时触发更新、后台任务调度和文件管理。
//
// 对应 Python: openjiuwen/core/context_engine/context/session_memory_manager.py (SessionMemoryManager)
type SessionMemoryManager struct {
	config  SessionMemoryConfig
	updater SessionMemoryUpdater
	mu      sync.Mutex
	tasks   map[string]context.CancelFunc
}

// ──────────────────────────── 常量 ────────────────────────────

// sessionMemoryStateKey 会话记忆运行时状态键
const sessionMemoryStateKey = "__session_memory__"

// defaultSessionMemoryTemplate 默认会话记忆 Markdown 模板。
//
// 对应 Python: DEFAULT_SESSION_MEMORY_TEMPLATE
const defaultSessionMemoryTemplate = `# Session Title
_A short and distinctive 5-10 word descriptive title for the session. Super info dense, no filler_

# Current State
_What is actively being worked on right now? Pending tasks not yet completed. Immediate next steps._

# Task specification
_What did the user ask to build? Any design decisions or other explanatory context_

# Files and Functions
_What are the important files? In short, what do they contain and why are they relevant?_

# Workflow
_What bash commands are usually run and in what order? How to interpret their output if not obvious?_

# Errors & Corrections
_Errors encountered and how they were fixed.
What did the user correct? What approaches failed and should not be tried again?_

# Codebase and System Documentation
_What are the important system components? How do they work/fit together?_

# Learnings
_What has worked well? What has not? What to avoid? Do not duplicate items from other sections_

# Key results
_If the user asked a specific output such as an answer to a question,
a table, or other document, repeat the exact result here_

# Worklog
_Step by step, what was attempted, done? Very terse summary for each step_
`

// defaultSessionMemoryPrompt agent_edit 模式提示词模板。
// 占位符 {{notesPath}} 和 {{currentNotes}} 在运行时替换。
//
// 对应 Python: DEFAULT_SESSION_MEMORY_PROMPT
const defaultSessionMemoryPrompt = `IMPORTANT: This message and these instructions are NOT part of the actual user conversation. Do NOT include any references to "note-taking", "session notes extraction", or these update instructions in the notes content.

Based on the user conversation above
(EXCLUDING this note-taking instruction message as well as system prompt,
or any past session summaries), update the session notes file.

The file {{notesPath}} has already been read for you. Here are its current contents:
<current_notes_content>
{{currentNotes}}
</current_notes_content>

Your ONLY task is to use the edit_file to update the notes file, then stop.
You can make multiple edits (update every section as needed) - make all
edit_file calls in parallel in a single message. Do not call any other tools.

CRITICAL RULES FOR EDITING:
- The file must maintain its exact structure with all sections, headers, and italic descriptions intact
-- NEVER modify, delete, or add section headers (the lines starting with '#' like # Task specification)
-- NEVER modify or delete the italic _section description_ lines
(these are the lines in italics immediately following each header -
they start and end with underscores)
-- The italic _section descriptions_ are TEMPLATE INSTRUCTIONS
that must be preserved exactly as-is - they guide what content belongs
in each section
-- ONLY update the actual content that appears BELOW the italic
_section descriptions_ within each existing section
-- Do NOT add any new sections, summaries, or information outside the existing structure
- Do NOT reference this note-taking process or instructions anywhere in the notes
- It's OK to skip updating a section if there are no substantial new insights
to add. Do not add filler content like "No info yet", just leave sections
blank/unedited if appropriate.
- Write DETAILED, INFO-DENSE content for each section - include specifics
like file paths, function names, error messages, exact commands,
technical details, etc.
- For "Key results", include the complete, exact output the user requested (e.g., full table, full answer, etc.)
- Do not include information that's already in the CLAUDE.md files included in the context
- Keep each section under ~500 tokens/words - if a section is approaching this limit,
condense it by cycling out less important details while preserving the most critical information
- Focus on actionable, specific information that would help someone
understand or recreate the work discussed in the conversation
- IMPORTANT: Always update "Current State" to reflect the most recent work -
this is critical for continuity after compaction

Use the edit_file with file_path: {{notesPath}}

STRUCTURE PRESERVATION REMINDER:
Each section has TWO parts that must be preserved exactly as they appear
in the current file:
1. The section header (line starting with #)
2. The italic description line
(the _italicized text_ immediately after the header -
this is a template instruction)

You ONLY update the actual content that comes AFTER these two preserved lines.
The italic description lines starting and ending with underscores are part of
the template structure, NOT content to be edited or removed.

REMEMBER: Use the edit_file in parallel and stop. Do not continue after
the edits. Only include insights from the actual user conversation,
never from these note-taking instructions. Do not delete or change
section headers or italic _section descriptions_.
`

// directSessionMemoryPrompt direct_replace 模式提示词模板。
// 占位符 {{notesPath}} 和 {{currentNotes}} 在运行时替换。
//
// 对应 Python: DIRECT_SESSION_MEMORY_PROMPT
const directSessionMemoryPrompt = `IMPORTANT: This message and these instructions are NOT part of the actual user conversation. Do NOT include any references to "note-taking", "session notes extraction", or these update instructions in the notes content.

Based on the user conversation above
(EXCLUDING this note-taking instruction message as well as system prompt,
or any past session summaries), update the session notes file.

The file {{notesPath}} has already been read for you. Here are its current contents:

// ──────────────────────────── 导出函数 ────────────────────────────

<current_notes_content>
{{currentNotes}}
</current_notes_content>

Your ONLY task is to return the COMPLETE updated notes file content, then stop. Do not call any tools.

CRITICAL RULES FOR EDITING:
- The file must maintain its exact structure with all sections, headers, and italic descriptions intact
-- NEVER modify, delete, or add section headers (the lines starting with '#' like # Task specification)
-- NEVER modify or delete the italic _section description_ lines
(these are the lines in italics immediately following each header -
they start and end with underscores)
-- The italic _section descriptions_ are TEMPLATE INSTRUCTIONS
that must be preserved exactly as-is - they guide what content belongs
in each section
-- ONLY update the actual content that appears BELOW the italic
_section descriptions_ within each existing section
-- Do NOT add any new sections, summaries, or information outside the existing structure
- Do NOT reference this note-taking process or instructions anywhere in the notes
- It's OK to skip updating a section if there are no substantial new insights
to add. Do not add filler content like "No info yet", just leave sections
blank/unedited if appropriate.
- Write DETAILED, INFO-DENSE content for each section - include specifics
like file paths, function names, error messages, exact commands,
technical details, etc.
- For "Key results", include the complete, exact output the user requested (e.g., full table, full answer, etc.)
- Do not include information that's already in the CLAUDE.md files included in the context
- Keep each section under ~500 tokens/words - if a section is approaching this limit,
condense it by cycling out less important details while preserving the most critical information
- Focus on actionable, specific information that would help someone
understand or recreate the work discussed in the conversation
- IMPORTANT: Always update "Current State" to reflect the most recent work -
this is critical for continuity after compaction
- Output plain markdown only
- Do NOT wrap the result in code fences

STRUCTURE PRESERVATION REMINDER:
Each section has TWO parts that must be preserved exactly as they appear
in the current file:
1. The section header (line starting with #)
2. The italic description line
(the _italicized text_ immediately after the header -
this is a template instruction)

You ONLY update the actual content that comes AFTER these two preserved lines.
The italic description lines starting and ending with underscores are part of
the template structure, NOT content to be edited or removed.
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionMemoryConfig 创建默认会话记忆配置。
//
// 默认值：TriggerTokens=10000, TriggerAddTokens=5000, ToolMin=3,
// 更新模式="direct_replace", 直接替换最大重试次数=2
func NewSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{
		TriggerTokens:           10000,
		TriggerAddTokens:        5000,
		ToolMin:                 3,
		UpdateMode:              "direct_replace",
		DirectReplaceMaxRetries: 2,
	}
}

// Validate 校验会话记忆配置。
//
// TriggerTokens/TriggerAddTokens/ToolMin 必须 > 0，
// UpdateMode 必须是 "agent_edit" 或 "direct_replace"。
func (c SessionMemoryConfig) Validate() error {
	if c.TriggerTokens <= 0 {
		return fmt.Errorf("TriggerTokens 必须 > 0，当前值: %d", c.TriggerTokens)
	}
	if c.TriggerAddTokens <= 0 {
		return fmt.Errorf("TriggerAddTokens 必须 > 0，当前值: %d", c.TriggerAddTokens)
	}
	if c.ToolMin <= 0 {
		return fmt.Errorf("ToolMin 必须 > 0，当前值: %d", c.ToolMin)
	}
	if c.UpdateMode != "agent_edit" && c.UpdateMode != "direct_replace" {
		return fmt.Errorf("UpdateMode 必须是 \"agent_edit\" 或 \"direct_replace\"，当前值: %q", c.UpdateMode)
	}
	return nil
}

// NewSessionMemoryDirectUpdater 创建 direct_replace 模式的会话记忆更新器。
func NewSessionMemoryDirectUpdater(config SessionMemoryConfig) *SessionMemoryDirectUpdater {
	return &SessionMemoryDirectUpdater{
		config: config,
	}
}

// BindModelDefaults 绑定默认模型配置，当 config 中 Model/ModelClient 为 nil 时设置。
func (u *SessionMemoryDirectUpdater) BindModelDefaults(
	modelConfig *llm_schema.ModelRequestConfig,
	clientConfig *llm_schema.ModelClientConfig,
) {
	if u.config.Model == nil {
		u.config.Model = modelConfig
	}
	if u.config.ModelClient == nil {
		u.config.ModelClient = clientConfig
	}
}

// SetInheritedSystemPrompt 设置继承的系统提示词。
func (u *SessionMemoryDirectUpdater) SetInheritedSystemPrompt(prompt string) {
	u.inheritedSystemPrompt = prompt
}

// Invoke 执行记忆更新：构建提示消息 → 调用 LLM → 规范化输出 → 写入文件。
//
// 对应 Python: SessionMemoryUpdateAgent._invoke_direct_replace()
func (u *SessionMemoryDirectUpdater) Invoke(ctx context.Context, opts SessionMemoryUpdateOptions) error {
	// 对齐 Python: if self._config.model is None or self._config.model_client is None: raise RuntimeError(...)
	if u.config.Model == nil || u.config.ModelClient == nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "SessionMemoryDirectUpdater.Invoke").
			Msg("模型配置缺失，无法创建模型实例")
		return fmt.Errorf("模型配置缺失: Model=%v, ModelClient=%v", u.config.Model, u.config.ModelClient)
	}

	// 确保 model 已创建（延迟初始化）
	if u.model == nil {
		model, err := llm.NewModel(u.config.ModelClient, u.config.Model)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "SessionMemoryDirectUpdater.Invoke").
				Str("model_provider", providerName(u.config.ModelClient)).
				Err(err).
				Msg("创建模型实例失败")
			return fmt.Errorf("创建模型实例失败: %w", err)
		}
		u.model = model
	}

	// 构建提示消息列表
	var messages []llm_schema.BaseMessage

	// 如果有 inheritedSystemPrompt，添加 SystemMessage
	inheritedPrompt := strings.TrimSpace(u.inheritedSystemPrompt)
	if inheritedPrompt != "" {
		messages = append(messages, llm_schema.NewSystemMessage(inheritedPrompt))
	}

	// 添加完整上下文消息
	messages = append(messages, opts.FullContextMessages...)

	// 添加 direct_replace 模式提示 UserMessage
	prompt := buildDirectSessionMemoryPrompt(opts.NotesPath, opts.CurrentNotes)
	messages = append(messages, llm_schema.NewUserMessage(prompt))

	// 带重试调用模型
	attempts := u.config.DirectReplaceMaxRetries + 1
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		response, err := u.model.Invoke(
			ctx,
			model_clients.NewMessagesParam(messages...),
			model_clients.WithInvokeModel(u.config.Model.ModelName),
		)
		if err != nil {
			lastErr = err
			if attempt < attempts {
				logger.Warn(logComponent).
					Str("event_type", "SESSION_MEMORY_RETRY").
					Int("attempt", attempt).
					Int("max_attempts", attempts).
					Err(err).
					Msg("direct_replace 模型调用失败，正在重试")
				continue
			}
			logger.Error(logComponent).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "SessionMemoryDirectUpdater.Invoke").
				Str("model_provider", providerName(u.config.ModelClient)).
				Err(err).
				Msg("direct_replace 模型调用失败，重试耗尽")
			return fmt.Errorf("direct_replace 模型调用失败（重试 %d 次）: %w", attempts, err)
		}

		// 规范化输出：去掉 markdown 代码块包裹
		content := normalizeDirectResponseContent(response.GetContent().Text())
		if content == "" {
			return fmt.Errorf("direct_replace 返回内容为空")
		}

		// 写入文件
		if err := os.WriteFile(opts.NotesPath, []byte(content), 0644); err != nil {
			logger.Error(logComponent).
				Str("event_type", "SESSION_MEMORY_WRITE_ERROR").
				Str("notes_path", opts.NotesPath).
				Err(err).
				Msg("写入会话记忆文件失败")
			return fmt.Errorf("写入会话记忆文件失败: %w", err)
		}

		return nil
	}

	return fmt.Errorf("direct_replace 模型调用失败（重试 %d 次）: %w", attempts, lastErr)
}

// NewSessionMemoryAgentUpdater 创建 agent_edit 模式的会话记忆更新器骨架。
func NewSessionMemoryAgentUpdater(config SessionMemoryConfig) *SessionMemoryAgentUpdater {
	return &SessionMemoryAgentUpdater{
		config: config,
	}
}

// Invoke 执行记忆更新（agent_edit 模式尚未实现）。
func (u *SessionMemoryAgentUpdater) Invoke(_ context.Context, _ SessionMemoryUpdateOptions) error {
	return fmt.Errorf("agent_edit 模式尚未实现（⤵️ 6.x 回填）")
}

// BindModelDefaults 绑定默认模型配置（agent_edit 模式尚未实现，空操作）。
func (u *SessionMemoryAgentUpdater) BindModelDefaults(
	_ *llm_schema.ModelRequestConfig,
	_ *llm_schema.ModelClientConfig,
) {
}

// SetInheritedSystemPrompt 设置继承的系统提示词（agent_edit 模式尚未实现，空操作）。
func (u *SessionMemoryAgentUpdater) SetInheritedSystemPrompt(_ string) {
}

// NewSessionMemoryManager 创建会话记忆管理器。
// 根据 UpdateMode 创建 DirectUpdater 或 AgentUpdater。
//
// 对应 Python: SessionMemoryManager.__init__()
func NewSessionMemoryManager(config SessionMemoryConfig) *SessionMemoryManager {
	var updater SessionMemoryUpdater
	switch config.UpdateMode {
	case "agent_edit":
		updater = NewSessionMemoryAgentUpdater(config)
	default:
		updater = NewSessionMemoryDirectUpdater(config)
	}

	return &SessionMemoryManager{
		config:  config,
		updater: updater,
		tasks:   make(map[string]context.CancelFunc),
	}
}

// BindModelDefaults 绑定默认模型配置，透传给 updater。
func (m *SessionMemoryManager) BindModelDefaults(
	modelConfig *llm_schema.ModelRequestConfig,
	clientConfig *llm_schema.ModelClientConfig,
) {
	m.updater.BindModelDefaults(modelConfig, clientConfig)
}

// MaybeScheduleUpdate 判断是否需要触发更新，如果需要则创建后台任务。
//
// 对应 Python: SessionMemoryManager.maybe_schedule_update()
func (m *SessionMemoryManager) MaybeScheduleUpdate(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	mc iface.ModelContext,
	workspaceDir string,
) {
	if sess == nil || workspaceDir == "" {
		return
	}

	sessionID := sess.GetSessionID()

	// 已有在运行的任务 → 跳过
	m.mu.Lock()
	if cancel, exists := m.tasks[sessionID]; exists {
		m.mu.Unlock()
		_ = cancel
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SKIP").
			Str("session_id", sessionID).
			Msg("跳过调度：已有运行中的任务")
		return
	}

	// 收集上下文窗口
	contextWindow := m.CollectContextWindow(mc)
	completedContextWindow := truncateContextWindowToCompletedAPIRound(contextWindow)

	notesPath := GetSessionMemoryPath(workspaceDir, sessionID)
	pendingNotesPath := getPendingSessionMemoryPath(notesPath)

	// 更新运行时状态
	updateSessionMemoryRuntime(sess, map[string]any{
		"session_id":          sessionID,
		"memory_path":         notesPath,
		"pending_memory_path": pendingNotesPath,
	})

	// ShouldUpdate 判断
	if !m.ShouldUpdate(sess, mc, completedContextWindow) {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SKIP").
			Str("session_id", sessionID).
			Msg("跳过调度：should_update 返回 false")
		m.mu.Unlock()
		return
	}

	// 标记正在提取
	runtime := GetSessionMemoryRuntime(sess)
	runtime["is_extracting"] = true
	updateSessionMemoryRuntime(sess, runtime)

	// 创建后台 goroutine 执行更新
	bgCtx, cancel := context.WithCancel(context.Background())
	m.tasks[sessionID] = cancel
	m.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "SESSION_MEMORY_SCHEDULE").
		Str("session_id", sessionID).
		Str("notes_path", notesPath).
		Int("message_count", len(completedContextWindow.ContextMessages)).
		Msg("调度会话记忆更新")

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.tasks, sessionID)
			m.mu.Unlock()
		}()

		m.updateBackground(bgCtx, sess, mc, completedContextWindow, notesPath, pendingNotesPath)
	}()
}

// ShouldUpdate 基于阈值判断是否需要更新会话记忆。
//
// 对应 Python: SessionMemoryManager.should_update()
func (m *SessionMemoryManager) ShouldUpdate(
	sess sessioninterfaces.SessionFacade,
	mc iface.ModelContext,
	window *iface.ContextWindow,
) bool {
	messages := window.ContextMessages
	if sess == nil || mc == nil || len(messages) == 0 {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SHOULD_UPDATE").
			Bool("session_exists", sess != nil).
			Bool("context_exists", mc != nil).
			Int("message_count", len(messages)).
			Msg("跳过更新判断：条件不满足")
		return false
	}

	runtime := getRuntimeState(sess)
	currentTokens := countTokens(mc, window)

	// 首次触发
	if !getBoolFromMap(runtime, "initialized") {
		if currentTokens >= m.config.TriggerTokens {
			logger.Info(logComponent).
				Str("event_type", "SESSION_MEMORY_TRIGGER").
				Int("current_tokens", currentTokens).
				Int("threshold", m.config.TriggerTokens).
				Msg("首次触发更新")
			runtime["initialized"] = true
			setRuntimeState(sess, runtime)
			return true
		}
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SKIP").
			Int("current_tokens", currentTokens).
			Int("threshold", m.config.TriggerTokens).
			Msg("未达到首次触发阈值")
		return false
	}

	// 增量触发
	totalToolCalls := countToolCalls(messages)
	tokensAtLastUpdate := getIntFromMap(runtime, "tokens_at_last_update")
	toolCallsAtLastUpdate := getIntFromMap(runtime, "tool_calls_at_last_update")

	// 基线重置检测
	baselineReset := false
	if currentTokens < tokensAtLastUpdate {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_BASELINE_RESET").
			Int("current_tokens", currentTokens).
			Int("previous_tokens", tokensAtLastUpdate).
			Msg("token 基线重置：上下文收缩")
		runtime["tokens_at_last_update"] = 0
		baselineReset = true
	}
	if totalToolCalls < toolCallsAtLastUpdate {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_BASELINE_RESET").
			Int("current_tool_calls", totalToolCalls).
			Int("previous_tool_calls", toolCallsAtLastUpdate).
			Msg("tool-call 基线重置：上下文收缩")
		runtime["tool_calls_at_last_update"] = 0
		baselineReset = true
	}
	if baselineReset {
		setRuntimeState(sess, runtime)
	}

	tokensSinceLast := currentTokens - getIntFromMap(runtime, "tokens_at_last_update")
	if tokensSinceLast < m.config.TriggerAddTokens {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SKIP").
			Int("delta_tokens", tokensSinceLast).
			Int("threshold", m.config.TriggerAddTokens).
			Msg("未达到增量 token 阈值")
		return false
	}

	toolCallsSinceLast := totalToolCalls - getIntFromMap(runtime, "tool_calls_at_last_update")
	if toolCallsSinceLast < m.config.ToolMin {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_SKIP").
			Int("delta_tool_calls", toolCallsSinceLast).
			Int("threshold", m.config.ToolMin).
			Msg("未达到增量 tool_call 阈值")
		return false
	}

	return true
}

// CollectContextWindow 从 ModelContext 获取消息构建 ContextWindow。
//
// 对应 Python: SessionMemoryManager.collect_context_window()
func (m *SessionMemoryManager) CollectContextWindow(mc iface.ModelContext) *iface.ContextWindow {
	if mc == nil {
		return iface.NewContextWindow()
	}
	messages, _ := mc.GetMessages(0, false)
	window := iface.NewContextWindow()
	window.ContextMessages = messages
	return window
}

// UpdateInheritedSystemPrompt 从消息列表提取系统提示词，设置到 updater。
//
// 对应 Python: SessionMemoryManager.update_inherited_system_prompt()
func (m *SessionMemoryManager) UpdateInheritedSystemPrompt(messages []llm_schema.BaseMessage) {
	inheritedSystemPrompt := buildSystemPromptText(messages)
	m.updater.SetInheritedSystemPrompt(inheritedSystemPrompt)
}

// Shutdown 取消所有后台任务。
//
// 对应 Python: SessionMemoryManager.shutdown()
func (m *SessionMemoryManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for sessionID, cancel := range m.tasks {
		cancel()
		delete(m.tasks, sessionID)
	}
}

// GetSessionMemoryRuntime 从 session state 获取 "__session_memory__" 键的值。
//
// 对应 Python: get_session_memory_runtime()
func GetSessionMemoryRuntime(sess sessioninterfaces.SessionFacade) map[string]any {
	if sess == nil {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_RUNTIME").
			Msg("会话记忆运行时状态为空：session 为 nil")
		return buildSessionMemoryRuntime()
	}
	val, _ := sess.GetState(state.StringKey(sessionMemoryStateKey))
	if val == nil {
		return buildSessionMemoryRuntime()
	}
	stateMap, ok := val.(map[string]any)
	if !ok {
		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_RUNTIME").
			Msg("会话记忆运行时状态非 dict，返回初始状态")
		return buildSessionMemoryRuntime()
	}
	result := make(map[string]any, len(stateMap))
	for k, v := range stateMap {
		result[k] = v
	}
	return result
}

// InvalidateSessionMemoryAnchor 重置基线。
//
// 对应 Python: invalidate_session_memory_anchor()
func InvalidateSessionMemoryAnchor(sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	updateSessionMemoryRuntime(sess, map[string]any{
		"tokens_at_last_update":         0,
		"last_summarized_message_count": 0,
		"notes_upto_message_id":         "",
	})
}

// GetSessionMemoryPath 返回会话记忆文件路径。
// 格式：{workspaceDir}/context/{sessionID}_context/session_memory/session_context.md
//
// 对应 Python: SessionMemoryManager._get_session_memory_path()
func GetSessionMemoryPath(workspaceDir, sessionID string) string {
	return filepath.Join(workspaceDir, "context", sessionID+"_context", "session_memory", "session_context.md")
}

// GetContextMessageID 从消息 metadata 获取 context_message_id。
//
// 对应 Python: get_context_message_id()
func GetContextMessageID(msg llm_schema.BaseMessage) string {
	metadata := msg.GetMetadata()
	if metadata == nil {
		return ""
	}
	id, ok := metadata[ContextMessageIDKey]
	if !ok {
		return ""
	}
	s, ok := id.(string)
	if !ok || s == "" {
		return ""
	}
	return s
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateSessionMemoryRuntime 更新运行时状态。
//
// 对应 Python: update_session_memory_runtime()
func updateSessionMemoryRuntime(sess sessioninterfaces.SessionFacade, st map[string]any) {
	if sess == nil {
		return
	}
	existing := GetSessionMemoryRuntime(sess)
	merged := make(map[string]any, len(existing)+len(st))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range st {
		merged[k] = v
	}

	sessionID := sess.GetSessionID()
	logger.Info(logComponent).
		Str("event_type", "SESSION_MEMORY_UPDATE_RUNTIME").
		Str("session_id", sessionID).
		Str("memory_path", getStringFromMap(merged, "memory_path")).
		Str("pending_memory_path", getStringFromMap(merged, "pending_memory_path")).
		Bool("initialized", getBoolFromMap(merged, "initialized")).
		Bool("is_extracting", getBoolFromMap(merged, "is_extracting")).
		Int("tokens_at_last_update", getIntFromMap(merged, "tokens_at_last_update")).
		Int("tool_calls_at_last_update", getIntFromMap(merged, "tool_calls_at_last_update")).
		Int("last_summarized_message_count", getIntFromMap(merged, "last_summarized_message_count")).
		Str("notes_upto_message_id", getStringFromMap(merged, "notes_upto_message_id")).
		Msg("更新会话记忆运行时状态")

	sess.UpdateState(map[string]any{sessionMemoryStateKey: merged})
}

// getPendingSessionMemoryPath 返回待提交路径。
// 格式：{stem}.pending{ext}（如 session_context.pending.md）
//
// 对应 Python: SessionMemoryManager._get_pending_session_memory_path()
func getPendingSessionMemoryPath(path string) string {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	return filepath.Join(filepath.Dir(path), stem+".pending"+ext)
}

// readOrInitSessionMemory 读取文件或初始化默认模板。
//
// 对应 Python: SessionMemoryManager._read_or_init_session_memory()
func readOrInitSessionMemory(path string) string {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data)
	}

	// 目录不存在则创建
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_MEMORY_INIT").
			Str("path", path).
			Err(err).
			Msg("创建会话记忆目录失败")
		return defaultSessionMemoryTemplate
	}

	// 检查模板文件
	templatePath := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "session_memory.md")
	if tmplData, err := os.ReadFile(templatePath); err == nil {
		_ = os.WriteFile(path, tmplData, 0644)
		return string(tmplData)
	}

	// 写入默认模板
	_ = os.WriteFile(path, []byte(defaultSessionMemoryTemplate), 0644)
	return defaultSessionMemoryTemplate
}

// buildSessionMemoryPrompt 替换 agent_edit 提示词模板中的占位符。
//
// 对应 Python: build_session_memory_prompt()
func buildSessionMemoryPrompt(notesPath, currentNotes string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(defaultSessionMemoryPrompt, "{{notesPath}}", notesPath),
		"{{currentNotes}}", currentNotes,
	)
}

// buildDirectSessionMemoryPrompt 替换 direct_replace 提示词模板中的占位符。
//
// 对应 Python: build_direct_session_memory_prompt()
func buildDirectSessionMemoryPrompt(notesPath, currentNotes string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(directSessionMemoryPrompt, "{{notesPath}}", notesPath),
		"{{currentNotes}}", currentNotes,
	)
}

// buildSystemPromptText 提取第一条 SystemMessage 的 content。
//
// 对应 Python: build_system_prompt_text()
func buildSystemPromptText(messages []llm_schema.BaseMessage) string {
	if len(messages) == 0 {
		return ""
	}
	first := messages[0]
	if first.GetRole() != llm_schema.RoleTypeSystem {
		return ""
	}
	return first.GetContent().Text()
}

// groupCompletedAPIRounds 委托给 processor.GroupCompletedAPIRounds。
//
// 对应 Python: group_completed_api_rounds()
func groupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][2]int {
	return processor.GroupCompletedAPIRounds(messages)
}

// findLastCompletedAPIRoundEnd 找到最后完成轮次结束索引。
//
// 对应 Python: find_last_completed_api_round_end()
func findLastCompletedAPIRoundEnd(messages []llm_schema.BaseMessage) int {
	rounds := groupCompletedAPIRounds(messages)
	if len(rounds) == 0 {
		return 0
	}
	return rounds[len(rounds)-1][1]
}

// buildSessionMemoryRuntime 构建初始运行时状态。
func buildSessionMemoryRuntime() map[string]any {
	return map[string]any{
		"memory_path":                   "",
		"pending_memory_path":           "",
		"initialized":                   false,
		"is_extracting":                 false,
		"tokens_at_last_update":         0,
		"tool_calls_at_last_update":     0,
		"last_summarized_message_count": 0,
		"notes_upto_message_id":         "",
	}
}

// normalizeDirectResponseContent 规范化 direct_replace 模式返回内容，去掉 markdown 代码块包裹。
//
// 对应 Python: SessionMemoryUpdateAgent._normalize_direct_response_content()
func normalizeDirectResponseContent(content string) string {
	normalized := strings.TrimSpace(content)
	if strings.HasPrefix(normalized, "```") {
		lines := strings.Split(normalized, "\n")
		if len(lines) >= 3 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			normalized = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	return normalized
}

// getRuntimeState 获取运行时状态（带默认值填充）。
//
// 对应 Python: SessionMemoryManager._get_runtime_state()
func getRuntimeState(sess sessioninterfaces.SessionFacade) map[string]any {
	st := GetSessionMemoryRuntime(sess)
	return map[string]any{
		"memory_path":                   getStringFromMap(st, "memory_path"),
		"pending_memory_path":           getStringFromMap(st, "pending_memory_path"),
		"initialized":                   getBoolFromMap(st, "initialized"),
		"is_extracting":                 getBoolFromMap(st, "is_extracting"),
		"tokens_at_last_update":         getIntFromMap(st, "tokens_at_last_update"),
		"tool_calls_at_last_update":     getIntFromMap(st, "tool_calls_at_last_update"),
		"last_summarized_message_count": getIntFromMap(st, "last_summarized_message_count"),
		"notes_upto_message_id":         getStringFromMap(st, "notes_upto_message_id"),
	}
}

// setRuntimeState 设置运行时状态。
func setRuntimeState(sess sessioninterfaces.SessionFacade, st map[string]any) {
	updateSessionMemoryRuntime(sess, st)
}

// countTokens 统计上下文窗口的 token 数。
//
// 对应 Python: SessionMemoryManager._count_tokens()
func countTokens(mc iface.ModelContext, window *iface.ContextWindow) int {
	tokenCounter := mc.TokenCounter()
	allMessages := make([]llm_schema.BaseMessage, 0, len(window.SystemMessages)+len(window.ContextMessages))
	allMessages = append(allMessages, window.SystemMessages...)
	allMessages = append(allMessages, window.ContextMessages...)

	// 尝试使用 token 计数器
	if tokenCounter != nil {
		modelName := ""
		if counter, ok := tokenCounter.(interface{ ModelName() string }); ok {
			modelName = counter.ModelName()
		}
		count, err := tokenCounter.CountMessages(allMessages, modelName)
		if err == nil {
			return count
		}
		logger.Debug(logComponent).
			Str("event_type", "SESSION_MEMORY_TOKEN_COUNT").
			Err(err).
			Msg("token 计数器统计失败，使用估算")
	}

	total := 0
	for _, msg := range allMessages {
		total += processor.EstimateMessageTokens(msg)
	}
	return total
}

// countToolCalls 统计消息列表中 AssistantMessage 的 tool_calls 总数。
//
// 对应 Python: SessionMemoryManager._count_tool_calls()
func countToolCalls(messages []llm_schema.BaseMessage) int {
	total := 0
	for _, msg := range messages {
		am, ok := msg.(*llm_schema.AssistantMessage)
		if ok && am.ToolCalls != nil {
			total += len(am.ToolCalls)
		}
	}
	return total
}

// truncateContextWindowToCompletedAPIRound 截断上下文窗口至最后一个完成的 API 轮次。
//
// 对应 Python: SessionMemoryManager._truncate_context_window_to_completed_api_round()
func truncateContextWindowToCompletedAPIRound(window *iface.ContextWindow) *iface.ContextWindow {
	completedEnd := findLastCompletedAPIRoundEnd(window.ContextMessages)
	if completedEnd <= 0 {
		return iface.NewContextWindow()
	}
	result := iface.NewContextWindow()
	result.SystemMessages = make([]llm_schema.BaseMessage, len(window.SystemMessages))
	copy(result.SystemMessages, window.SystemMessages)
	result.ContextMessages = make([]llm_schema.BaseMessage, completedEnd)
	copy(result.ContextMessages, window.ContextMessages[:completedEnd])
	result.Tools = make([]commonschema.ToolInfoInterface, len(window.Tools))
	copy(result.Tools, window.Tools)
	return result
}

// updateBackground 后台更新会话记忆。
//
// 对应 Python: SessionMemoryManager._update_background()
func (m *SessionMemoryManager) updateBackground(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	mc iface.ModelContext,
	contextWindow *iface.ContextWindow,
	notesPath string,
	pendingNotesPath string,
) {
	if sess == nil {
		return
	}

	messages := contextWindow.ContextMessages
	sessionID := sess.GetSessionID()
	runtime := getRuntimeState(sess)
	currentNotes := readOrInitSessionMemory(notesPath)

	// 准备 pending 文件
	preparePendingSessionMemory(notesPath, pendingNotesPath, currentNotes)

	logger.Info(logComponent).
		Str("event_type", "SESSION_MEMORY_UPDATE_START").
		Str("session_id", sessionID).
		Str("mode", m.config.UpdateMode).
		Str("notes_path", notesPath).
		Str("pending_notes_path", pendingNotesPath).
		Int("message_count", len(messages)).
		Msg("后台更新开始")

	err := m.updater.Invoke(ctx, SessionMemoryUpdateOptions{
		FullContextMessages: contextWindow.GetMessages(),
		NotesPath:           pendingNotesPath,
		CurrentNotes:        currentNotes,
	})

	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_MEMORY_UPDATE_FAILED").
			Str("session_id", sessionID).
			Str("notes_path", notesPath).
			Str("pending_notes_path", pendingNotesPath).
			Err(err).
			Msg("后台更新失败")
	} else {
		// 提交 pending 文件
		if commitErr := commitPendingSessionMemory(pendingNotesPath, notesPath); commitErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "SESSION_MEMORY_COMMIT_FAILED").
				Str("session_id", sessionID).
				Err(commitErr).
				Msg("提交 pending 文件失败")
		}

		// 更新运行时状态
		if mc != nil {
			runtime["tokens_at_last_update"] = countTokens(mc, contextWindow)
		}
		runtime["tool_calls_at_last_update"] = countToolCalls(messages)
		runtime["last_summarized_message_count"] = len(messages)
		if len(messages) > 0 {
			runtime["notes_upto_message_id"] = GetContextMessageID(messages[len(messages)-1])
		} else {
			runtime["notes_upto_message_id"] = ""
		}
		runtime["initialized"] = true

		logger.Info(logComponent).
			Str("event_type", "SESSION_MEMORY_UPDATE_COMPLETE").
			Str("notes_upto_message_id", getStringFromMap(runtime, "notes_upto_message_id")).
			Int("last_summarized_message_count", getIntFromMap(runtime, "last_summarized_message_count")).
			Int("tokens_at_last_update", getIntFromMap(runtime, "tokens_at_last_update")).
			Int("tool_calls_at_last_update", getIntFromMap(runtime, "tool_calls_at_last_update")).
			Msg("后台更新完成")
	}

	// 无论成功失败，重置提取标记
	runtime["is_extracting"] = false
	setRuntimeState(sess, runtime)
}

// preparePendingSessionMemory 准备 pending 文件。
//
// 对应 Python: SessionMemoryManager._prepare_pending_session_memory()
func preparePendingSessionMemory(activePath, pendingPath, currentNotes string) {
	dir := filepath.Dir(pendingPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_MEMORY_PREPARE").
			Str("pending_path", pendingPath).
			Err(err).
			Msg("创建 pending 目录失败")
		return
	}

	// 如果 active 文件存在，复制到 pending
	if data, err := os.ReadFile(activePath); err == nil {
		_ = os.WriteFile(pendingPath, data, 0644)
		return
	}

	// 否则写入当前笔记内容
	_ = os.WriteFile(pendingPath, []byte(currentNotes), 0644)
}

// commitPendingSessionMemory 提交 pending 文件为正式文件。
//
// 对应 Python: SessionMemoryManager._commit_pending_session_memory()
func commitPendingSessionMemory(pendingPath, activePath string) error {
	if _, err := os.Stat(pendingPath); os.IsNotExist(err) {
		return fmt.Errorf("pending 会话记忆文件不存在: %s", pendingPath)
	}
	return os.Rename(pendingPath, activePath)
}

// getIntFromMap 从 map 中安全获取 int 值。
func getIntFromMap(m map[string]any, key string) int {
	val, ok := m[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	default:
		return 0
	}
}

// getBoolFromMap 从 map 中安全获取 bool 值。
func getBoolFromMap(m map[string]any, key string) bool {
	val, ok := m[key]
	if !ok || val == nil {
		return false
	}
	b, ok := val.(bool)
	if !ok {
		return false
	}
	return b
}

// getStringFromMap 从 map 中安全获取 string 值。
func getStringFromMap(m map[string]any, key string) string {
	val, ok := m[key]
	if !ok || val == nil {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

// providerName 从 ModelClientConfig 安全获取 ClientProvider 名称。
func providerName(cfg *llm_schema.ModelClientConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.ClientProvider
}

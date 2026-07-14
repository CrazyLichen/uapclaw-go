package context

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionModelContext 上下文引擎的核心运行时实现，管理对话消息和上下文窗口。
//
// 对应 Python: openjiuwen/core/context_engine/context/context.py (SessionModelContext)
type SessionModelContext struct {
	// contextID 上下文唯一标识
	contextID string
	// sessionID 会话唯一标识
	sessionID string
	// messageBuffer 消息缓冲区
	messageBuffer *ContextMessageBuffer
	// defaultWindowSize 默认窗口大小
	defaultWindowSize int
	// enableReload 是否启用 offload 重载功能
	enableReload bool
	// contextWindowTokens 上下文窗口 token 数
	contextWindowTokens int
	// modelName 模型名称
	modelName string
	// modelContextWindowTokens 各模型上下文窗口 token 映射
	modelContextWindowTokens map[string]int
	// workspace 工作空间
	workspace *hworkspace.Workspace
	// sysOperation 系统操作接口
	sysOperation sysop.SysOperation
	// sessionRef 会话引用
	sessionRef sessioninterfaces.SessionFacade
	// defaultDialogueRound 默认对话轮数
	defaultDialogueRound int
	// tokenCounter Token 计数器
	tokenCounter token.TokenCounter
	// processors 处理器列表
	processors []iface.ContextProcessor
	// stateRecorder 压缩状态记录器
	stateRecorder *ProcessorStateRecorder
	// processorLock 处理器互斥锁，对齐 Python asyncio.Lock
	processorLock sync.Mutex
	// activeCompressionInProgress 主动压缩进行中标志
	// Python 依赖 asyncio 单线程安全，Go 使用 atomic.Bool 保证多 goroutine 可见性
	activeCompressionInProgress atomic.Bool
	// kvCacheManager KV 缓存管理器
	kvCacheManager *KVCacheManager
	// offloadMessageBuffer 卸载消息缓冲区
	offloadMessageBuffer *OffloadMessageBuffer
}

// reloaderToolInput 重载工具输入参数结构体。
type reloaderToolInput struct {
	// OffloadHandle 卸载内容的唯一标识
	OffloadHandle string `json:"offload_handle" jsonschema:"description=A unique identifier or file path pointing to the offloaded content. Accepts either a UUID string (e.g. abc123-def456) for memory-based storage, or a file path for filesystem-based storage."`
	// OffloadType 卸载内容的存储类型
	OffloadType string `json:"offload_type" jsonschema:"description=The storage backend used when the content was offloaded. Must be one of: in_memory (session cache, handle is UUID), filesystem (disk file, handle is file path)."`
}

// ──────────────────────────── 常量 ────────────────────────────

// reloaderSystemPrompt reload 工具的系统提示词，告知 LLM 如何使用 reloader_tool。
//
// 对应 Python: openjiuwen/core/context_engine/context/context.py (_RELOADER_SYSTEM_PROMPT)
const reloaderSystemPrompt = `You may see offloaded content markers in your context: [[OFFLOAD: handle=<id>, type=<type>]].

When you see an offloaded-content marker and believe retrieving it will help your answer, 
feel free to call reload_original_context_messages:
- Call reload_original_context_messages(offload_handle="<id>", offload_type="<type>") with the exact values from the marker
- Do not guess or make up the missing content

Storage types: "in_memory" (session cache), "filesystem" (disk file).`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionModelContext 创建 SessionModelContext 实例。
//
// 流程：
//  1. 校验历史消息 + 确保 ID
//  2. 创建消息缓冲区
//  3. 从配置读取窗口参数
//  4. 创建状态记录器
//  5. 条件创建 KVCacheManager
//  6. 创建卸载缓冲区
//
// 对应 Python: SessionModelContext.__init__
func NewSessionModelContext(
	contextID string,
	sessionID string,
	config ceschema.ContextEngineConfig,
	historyMessages []llm_schema.BaseMessage,
	processors []iface.ContextProcessor,
	tokenCounter token.TokenCounter,
	sessionRef sessioninterfaces.SessionFacade,
	workspace *hworkspace.Workspace,
	sysOperation sysop.SysOperation,
) *SessionModelContext {
	// 1. 校验历史消息
	if err := ValidateMessages(historyMessages); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "CONTEXT_HISTORY_VALIDATE_ERROR").
			Str("context_id", contextID).
			Str("session_id", sessionID).
			Err(err).
			Msg("历史消息校验失败")
	}
	// 2. 确保消息 ID
	EnsureContextMessageIDs(historyMessages)

	// 3. 创建消息缓冲区
	messageBuffer := NewContextMessageBuffer(historyMessages, config.MaxContextMessageNum)

	// 4. 从配置读取窗口参数
	mc := &SessionModelContext{
		contextID:                contextID,
		sessionID:                sessionID,
		messageBuffer:            messageBuffer,
		defaultWindowSize:        config.DefaultWindowMessageNum,
		defaultDialogueRound:     config.DefaultWindowRoundNum,
		enableReload:             config.EnableReload,
		contextWindowTokens:      config.ContextWindowTokens,
		modelName:                config.ModelName,
		modelContextWindowTokens: config.ModelContextWindowTokens,
		workspace:                workspace,
		sysOperation:             sysOperation,
		sessionRef:               sessionRef,
		tokenCounter:             tokenCounter,
		processors:               processors,
	}

	// 5. 创建状态记录器
	mc.stateRecorder = NewProcessorStateRecorder(
		sessionID,
		contextID,
		func() sessioninterfaces.SessionFacade { return mc.sessionRef },
		tokenCounter,
		100, // 历史限制
	)

	// 6. 条件创建 KVCacheManager
	if config.EnableKVCacheRelease {
		mc.kvCacheManager = NewKVCacheManager(sessionID)
	}

	// 7. 创建卸载消息缓冲区
	mc.offloadMessageBuffer = NewOffloadMessageBuffer(nil)
	mc.offloadMessageBuffer.SetSysOperation(sysOperation)
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir != "" {
		mc.offloadMessageBuffer.SetWorkspaceInfo(workspaceDir, sessionID)
	}

	// 8. reloaderToolCard 不再预构造，由 ReloaderTool() 调用 NewTool 时从
	//    reloaderToolInput 反射自动生成 schema + ToolCard

	logger.Info(logComponent).
		Str("event_type", "SESSION_MODEL_CONTEXT_CREATED").
		Str("context_id", contextID).
		Str("session_id", sessionID).
		Int("history_message_count", len(historyMessages)).
		Int("processor_count", len(processors)).
		Bool("enable_reload", mc.enableReload).
		Bool("enable_kv_cache_release", mc.kvCacheManager != nil).
		Msg("SessionModelContext 已创建")

	return mc
}

// Len 返回上下文消息数量。
//
// 对应 Python: SessionModelContext.len()
func (mc *SessionModelContext) Len() int {
	return mc.messageBuffer.Size()
}

// GetMessages 获取消息列表。
//
// size ≤ 0 表示不限制；size < 0 返回错误；withHistory 控制是否包含历史消息。
// 对应 Python: SessionModelContext.get_messages()
func (mc *SessionModelContext) GetMessages(size int, withHistory bool) ([]llm_schema.BaseMessage, error) {
	if size < 0 {
		return nil, fmt.Errorf("get messages size 应大于等于 0，当前值: %d", size)
	}
	return mc.messageBuffer.GetBack(size, withHistory), nil
}

// SetMessages 替换消息列表。
//
// 对应 Python: SessionModelContext.set_messages()
func (mc *SessionModelContext) SetMessages(messages []llm_schema.BaseMessage, withHistory bool) {
	if err := ValidateMessages(messages); err != nil {
		logger.Error(logComponent).
			Str("event_type", "CONTEXT_SET_MESSAGES_VALIDATE_ERROR").
			Str("context_id", mc.contextID).
			Err(err).
			Msg("设置消息校验失败")
		return
	}
	EnsureContextMessageIDs(messages)
	mc.messageBuffer.SetMessages(messages, withHistory)
}

// PopMessages 从尾部弹出消息。
//
// size < 0 返回错误；withHistory 控制是否从历史消息中弹出。
// 对应 Python: SessionModelContext.pop_messages()
func (mc *SessionModelContext) PopMessages(size int, withHistory bool) []llm_schema.BaseMessage {
	if size < 0 {
		logger.Warn(logComponent).
			Str("event_type", "CONTEXT_POP_MESSAGES_INVALID_SIZE").
			Str("context_id", mc.contextID).
			Int("size", size).
			Msg("PopMessages size 不能为负数")
		return nil
	}
	return mc.messageBuffer.PopBack(size, withHistory)
}

// ClearMessages 清空消息，重置卸载缓冲区，触发 ContextCleared 事件。
//
// 对应 Python: SessionModelContext.clear_messages()
func (mc *SessionModelContext) ClearMessages(ctx context.Context, withHistory bool, opts ...iface.Option) error {
	// 弹出全部消息
	totalSize := mc.messageBuffer.Size()
	if totalSize > 0 {
		mc.messageBuffer.PopBack(totalSize, withHistory)
	}

	// 重置卸载缓冲区
	mc.offloadMessageBuffer = NewOffloadMessageBuffer(nil)
	mc.offloadMessageBuffer.SetSysOperation(mc.sysOperation)
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir != "" {
		mc.offloadMessageBuffer.SetWorkspaceInfo(workspaceDir, mc.sessionID)
	}

	// 触发 ContextCleared 事件
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextCleared,
		SessionID: mc.sessionID,
		ContextID: mc.contextID,
		Context:   mc,
	})

	logger.Info(logComponent).
		Str("event_type", "CONTEXT_CLEARED").
		Str("context_id", mc.contextID).
		Str("session_id", mc.sessionID).
		Int("cleared_count", totalSize).
		Msg("上下文消息已清空")

	return nil
}

// AddMessages 添加消息，执行处理器管线，触发 ContextUpdated 事件。
//
// 核心方法：
//   - 快速路径：activeCompressionInProgress && !processorLock.TryLock() → 仅入队
//   - 正常路径：processorLock.Lock() → runAddProcessors → 入队 → Unlock
//
// 对应 Python: SessionModelContext.add_messages()
func (mc *SessionModelContext) AddMessages(ctx context.Context, message llm_schema.BaseMessage, opts ...iface.Option) ([]llm_schema.BaseMessage, error) {
	// 将单条消息包装为列表
	messages := []llm_schema.BaseMessage{message}
	EnsureContextMessageIDs(messages)

	locked := false
	// 快速路径：主动压缩进行中，尝试非阻塞获取锁
	if mc.activeCompressionInProgress.Load() {
		if !mc.processorLock.TryLock() {
			// 无法获取锁，仅入队不执行处理器
			mc.messageBuffer.AddBack(messages)
			logger.Info(logComponent).
				Str("event_type", "CONTEXT_ADD_MESSAGES_FAST_PATH").
				Str("context_id", mc.contextID).
				Int("message_count", len(messages)).
				Msg("主动压缩进行中，消息仅入队")
		} else {
			locked = true
		}
	} else {
		mc.processorLock.Lock()
		locked = true
	}

	if locked {
		// 执行处理器
		messages, _ = mc.runAddProcessors(ctx, messages, false, nil, false, ceschema.PhaseAddMessages, opts...)
		// 入队
		mc.messageBuffer.AddBack(messages)
		mc.processorLock.Unlock()
	}

	// 触发 ContextUpdated 事件
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextUpdated,
		SessionID: mc.sessionID,
		ContextID: mc.contextID,
		Context:   mc,
	})

	return messages, nil
}

// GetContextWindow 构建上下文窗口供模型推理使用。
//
// 核心方法：
//  1. 参数校验
//  2. 获取锁
//  3. enableReload → 追加 reloaderSystemPrompt
//  4. getWindowMessages
//  5. 遍历处理器：trigger + on_get_context_window
//  6. ValidateAndFixContextWindow
//  7. kvCacheManager.Release（条件）
//  8. statContextWindow
//  9. 触发 ContextRetrieved 事件
//
// 对应 Python: SessionModelContext.get_context_window()
func (mc *SessionModelContext) GetContextWindow(
	ctx context.Context,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	windowSize int,
	dialogueRound int,
	opts ...iface.Option,
) (*iface.ContextWindow, error) {
	// 1. 参数校验
	effectiveWindowSize := mc.defaultWindowSize
	if windowSize > 0 {
		effectiveWindowSize = windowSize
	} else if windowSize < 0 {
		return nil, exception.NewBaseError(exception.StatusContextMessageInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("windowSize 不能为负数，当前值: %d", windowSize)),
		)
	}

	effectiveDialogueRound := mc.defaultDialogueRound
	if dialogueRound > 0 {
		effectiveDialogueRound = dialogueRound
	} else if dialogueRound < 0 {
		return nil, exception.NewBaseError(exception.StatusContextMessageInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("dialogueRound 不能为负数，当前值: %d", dialogueRound)),
		)
	}

	// 2. 获取锁
	mc.processorLock.Lock()
	defer mc.processorLock.Unlock()

	// 3. enableReload → 追加 reloaderSystemPrompt
	if mc.enableReload {
		reloaderMsg := llm_schema.NewSystemMessage(reloaderSystemPrompt)
		systemMessages = append(systemMessages, reloaderMsg)
	}

	// 4. getWindowMessages — 双截断逻辑
	systemMessages, contextMessages := mc.getWindowMessages(systemMessages, effectiveWindowSize, effectiveDialogueRound)

	// 构建 ContextWindow
	window := iface.NewContextWindow()
	window.SystemMessages = systemMessages
	window.ContextMessages = contextMessages
	window.Tools = tools

	// 5. 遍历处理器：trigger + on_get_context_window
	for _, proc := range mc.processors {
		triggered, err := proc.TriggerGetContextWindow(ctx, mc, *window, opts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "CONTEXT_PROCESSOR_TRIGGER_ERROR").
				Str("processor", proc.ProcessorType()).
				Str("context_id", mc.contextID).
				Err(err).
				Msg("处理器触发判断失败")
			continue
		}
		if !triggered {
			continue
		}

		event, newWindow, err := proc.OnGetContextWindow(ctx, mc, *window, opts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "CONTEXT_PROCESSOR_ERROR").
				Str("processor", proc.ProcessorType()).
				Str("context_id", mc.contextID).
				Err(err).
				Msg("处理器执行失败")
			continue
		}

		window = &newWindow

		// 记录处理器事件
		if event != nil {
			mc.stateRecorder.recordFromEvent(event)
		}
	}

	// 6. ValidateAndFixContextWindow
	ValidateAndFixContextWindow(window)

	// 7. kvCacheManager.Release（条件），Option 透传 model
	if mc.kvCacheManager != nil {
		if err := mc.kvCacheManager.Release(ctx, window, opts...); err != nil {
			logger.Error(logComponent).
				Str("event_type", "KV_CACHE_RELEASE_ERROR").
				Str("context_id", mc.contextID).
				Err(err).
				Msg("KV 缓存释放失败")
		}
	}

	// 8. statContextWindow
	mc.statContextWindow(window)

	// 9. 触发 ContextRetrieved 事件
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextRetrieved,
		SessionID: mc.sessionID,
		ContextID: mc.contextID,
		Context:   mc,
	})

	return window, nil
}

// Statistic 计算上下文统计信息。
//
// 对应 Python: SessionModelContext.statistic()
func (mc *SessionModelContext) Statistic() *iface.ContextStats {
	messages := mc.messageBuffer.GetBack(0, true)
	stat := &iface.ContextStats{}
	mc.statMessages(stat, messages)
	return stat
}

// SessionID 返回会话 ID。
func (mc *SessionModelContext) SessionID() string {
	return mc.sessionID
}

// ContextID 返回上下文 ID。
func (mc *SessionModelContext) ContextID() string {
	return mc.contextID
}

// TokenCounter 返回 Token 计数器。
func (mc *SessionModelContext) TokenCounter() token.TokenCounter {
	return mc.tokenCounter
}

// ReloaderTool 返回重载卸载消息的工具。
//
// NewTool 从 reloaderToolInput 的 jsonschema tag 反射提取 input schema，
// 内部自动生成 ToolCard，无需手动构造。
// 对应 Python: SessionModelContext.reloader_tool()
func (mc *SessionModelContext) ReloaderTool() tool.Tool {
	// 闭包捕获 offloadMessageBuffer 引用，对齐 Python @tool 装饰器
	reloadFn := func(ctx context.Context, input reloaderToolInput, _ ...tool.ToolOption) (string, error) {
		reloadedMessages := mc.offloadMessageBuffer.Reload(input.OffloadHandle, input.OffloadType)
		if len(reloadedMessages) == 0 {
			return fmt.Sprintf("Failed to reload messages with offload_handle=%s and offload_type=%s", input.OffloadHandle, input.OffloadType), nil
		}
		return FormatReloadedMessages(input.OffloadHandle, reloadedMessages), nil
	}

	t, err := tool.NewTool(reloadFn,
		tool.WithToolName("reload_original_context_messages"),
		tool.WithToolDescription("Retrieve messages that were previously offloaded from the context window. Provide the exact handle and storage type returned when the content was offloaded; the tool will fetch the complete original message list and inject it back into the conversation, allowing the model to see the full text as if it had never been removed."),
	)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "RELOADER_TOOL_CREATE_ERROR").
			Err(err).
			Msg("创建 reloader tool 失败")
		return nil
	}

	// 设置自定义 ID，对齐 Python: f"reload_{session_id}_{context_id}"
	t.Card().ID = fmt.Sprintf("reload_%s_%s", mc.sessionID, mc.contextID)

	return t
}

// WorkspaceDir 返回工作目录路径。
//
// 对应 Python: SessionModelContext.workspace_dir()
func (mc *SessionModelContext) WorkspaceDir() string {
	if mc.workspace == nil {
		return ""
	}
	return mc.workspace.RootPath
}

// SetSessionRef 设置会话引用。
//
// 对应 Python: SessionModelContext.set_session_ref()
func (mc *SessionModelContext) SetSessionRef(sess sessioninterfaces.SessionFacade) {
	mc.sessionRef = sess
}

// GetSessionRef 获取会话引用。
//
// 对应 Python: SessionModelContext.get_session_ref()
func (mc *SessionModelContext) GetSessionRef() sessioninterfaces.SessionFacade {
	return mc.sessionRef
}

// OffloadMessages 将消息卸载到内存缓冲区。
//
// 对应 Python: SessionModelContext.offload_messages()
func (mc *SessionModelContext) OffloadMessages(handle string, messages []llm_schema.BaseMessage) {
	mc.offloadMessageBuffer.Offload(handle, offloadTypeInMemory, messages)
}

// SaveState 保存上下文状态为 map。
//
// 对齐 Python: SessionModelContext.save_state() 返回扁平字典
// {"messages": ..., "offload_messages": ..., "processor_states": ..., "compression_history": ...}
func (mc *SessionModelContext) SaveState() map[string]any {
	allMessages := mc.messageBuffer.GetBack(0, true)
	offloadMessages := mc.offloadMessageBuffer.GetAll()
	processorStates := make(map[string]any)
	for _, proc := range mc.processors {
		processorStates[proc.ProcessorType()] = proc.SaveState()
	}

	return map[string]any{
		"messages":            allMessages,
		"offload_messages":    offloadMessages,
		"processor_states":    processorStates,
		"compression_history": mc.stateRecorder.History(),
	}
}

// LoadState 从 map 恢复上下文状态。
//
// 对齐 Python: SessionModelContext.load_state()，接收扁平字典
func (mc *SessionModelContext) LoadState(state map[string]any) {
	// 兼容旧格式：如果 state 仍含 contextID 嵌套键，先解嵌套
	stateMap := state
	if contextState, ok := state[mc.contextID]; ok {
		if inner, ok := contextState.(map[string]any); ok {
			stateMap = inner
		}
	}

	// 恢复消息
	if messagesVal, exists := stateMap["messages"]; exists {
		if messages, ok := messagesVal.([]llm_schema.BaseMessage); ok {
			if err := ValidateMessages(messages); err != nil {
				logger.Error(logComponent).
					Str("event_type", "CONTEXT_LOAD_STATE_VALIDATE_ERROR").
					Str("context_id", mc.contextID).
					Err(err).
					Msg("恢复消息校验失败")
				return
			}
			EnsureContextMessageIDs(messages)
			mc.messageBuffer.Rebuild(messages)
		}
	}

	// 恢复卸载消息缓冲区
	if offloadVal, exists := stateMap["offload_messages"]; exists {
		if offloadMessages, ok := offloadVal.(map[string][]llm_schema.BaseMessage); ok {
			mc.offloadMessageBuffer = NewOffloadMessageBuffer(offloadMessages)
			mc.offloadMessageBuffer.SetSysOperation(mc.sysOperation)
			workspaceDir := mc.WorkspaceDir()
			if workspaceDir != "" {
				mc.offloadMessageBuffer.SetWorkspaceInfo(workspaceDir, mc.sessionID)
			}
		}
	}

	// 恢复处理器状态
	if procStatesVal, exists := stateMap["processor_states"]; exists {
		if procStates, ok := procStatesVal.(map[string]any); ok {
			for _, proc := range mc.processors {
				if procState, exists := procStates[proc.ProcessorType()]; exists {
					if ps, ok := procState.(map[string]any); ok {
						proc.LoadState(ps)
					}
				}
			}
		}
	}

	// 恢复压缩历史
	if historyVal, exists := stateMap["compression_history"]; exists {
		if history, ok := historyVal.([]map[string]any); ok {
			mc.stateRecorder.LoadHistory(history)
		}
	}

	logger.Info(logComponent).
		Str("event_type", "CONTEXT_LOAD_STATE").
		Str("context_id", mc.contextID).
		Msg("上下文状态已恢复")
}

// CompressContext 主动压缩上下文。
//
// 返回 "busy"/"compressed"/"noop"。
// 对应 Python: SessionModelContext.compress_context()
func (mc *SessionModelContext) CompressContext(ctx context.Context, opts ...iface.CompressContextOption) (string, error) {
	// 尝试非阻塞获取锁
	if !mc.processorLock.TryLock() {
		logger.Warn(logComponent).
			Str("event_type", "CONTEXT_COMPRESS_BUSY").
			Str("context_id", mc.contextID).
			Msg("上下文压缩忙，跳过")
		return "busy", nil
	}

	mc.activeCompressionInProgress.Store(true)

	// 解析选项
	compOpts := iface.NewCompressContextOptions(opts...)

	// 按 compressionOnly 过滤处理器
	filteredProcessors := mc.selectProcessors(compOpts.ProcessorTypes, true)
	if len(filteredProcessors) == 0 {
		mc.activeCompressionInProgress.Store(false)
		mc.processorLock.Unlock()
		logger.Info(logComponent).
			Str("event_type", "CONTEXT_COMPRESS_NOOP").
			Str("context_id", mc.contextID).
			Msg("无压缩处理器，跳过主动压缩")
		return "noop", nil
	}

	// 执行处理器（force=true, phase=active_compress）
	if _, err := mc.runAddProcessors(ctx, nil, true, compOpts.ProcessorTypes, true, ceschema.PhaseActiveCompress, iface.WithSysOperation(compOpts.SysOperation)); err != nil {
		mc.activeCompressionInProgress.Store(false)
		mc.processorLock.Unlock()
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "CONTEXT_COMPRESS_ERROR").
			Str("context_id", mc.contextID).
			Msg("主动压缩执行处理器失败")
		return "error", err
	}

	mc.activeCompressionInProgress.Store(false)
	mc.processorLock.Unlock()

	logger.Info(logComponent).
		Str("event_type", "CONTEXT_COMPRESS_DONE").
		Str("context_id", mc.contextID).
		Msg("主动压缩完成")

	return "compressed", nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAddProcessors 遍历处理器执行 trigger + on_add_messages。
//
// 参数：
//   - messages: 待添加的消息列表
//   - force: 是否强制执行（主动压缩时为 true）
//   - processorTypes: 处理器类型过滤列表
//   - compressionOnly: 是否仅执行压缩类型处理器
//   - phase: 压缩阶段（PhaseAddMessages 或 PhaseActiveCompress）
//   - opts: 处理器选项
func (mc *SessionModelContext) runAddProcessors(
	ctx context.Context,
	messages []llm_schema.BaseMessage,
	force bool,
	processorTypes []string,
	compressionOnly bool,
	phase ceschema.CompressionPhase,
	opts ...iface.Option,
) ([]llm_schema.BaseMessage, error) {
	filteredProcessors := mc.selectProcessors(processorTypes, compressionOnly)

	for _, proc := range filteredProcessors {
		// 触发判断
		triggered := true
		if !force {
			var err error
			triggered, err = proc.TriggerAddMessages(ctx, mc, messages, opts...)
			if err != nil {
				logger.Error(logComponent).
					Str("event_type", "CONTEXT_PROCESSOR_TRIGGER_ERROR").
					Str("processor", proc.ProcessorType()).
					Str("context_id", mc.contextID).
					Err(err).
					Msg("处理器触发判断失败")
				continue
			}
		}

		if !triggered {
			continue
		}

		// 记录处理前状态
		beforeMessages := mc.messageBuffer.GetBack(0, true)
		operationID := uuid.New().String()
		startTime := time.Now()

		// 执行处理器
		event, newMessages, err := proc.OnAddMessages(ctx, mc, messages, opts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "CONTEXT_PROCESSOR_ERROR").
				Str("processor", proc.ProcessorType()).
				Str("context_id", mc.contextID).
				Err(err).
				Msg("处理器执行失败")

			// 构建失败状态
			mc.stateRecorder.Emit(ctx, mc.stateRecorder.BuildState(ProcessorStateInput{
				OperationID:    operationID,
				Status:         ceschema.CompressionFailed,
				Phase:          phase,
				Trigger:        "add_messages",
				Processor:      proc,
				BeforeMessages: beforeMessages,
				Force:          force,
				StartedAt:      startTime,
				EndedAt:        time.Now(),
				Error:          err.Error(),
				ContextMax:     mc.resolveContextMax(opts...),
			}))
			continue
		}

		// 处理器返回了新消息
		if newMessages != nil {
			messages = newMessages
		}

		// 记录处理器事件和状态
		if event != nil {
			afterMessages := mc.messageBuffer.GetBack(0, true)
			if len(messages) > 0 {
				afterMessages = append(afterMessages, messages...)
			}
			mc.stateRecorder.Emit(ctx, mc.stateRecorder.BuildState(ProcessorStateInput{
				OperationID:      operationID,
				Status:           ceschema.CompressionCompleted,
				Phase:            phase,
				Trigger:          "add_messages",
				Processor:        proc,
				Reason:           "处理器执行成功",
				BeforeMessages:   beforeMessages,
				AfterMessages:    afterMessages,
				Force:            force,
				StartedAt:        startTime,
				EndedAt:          time.Now(),
				MessagesToModify: event.MessagesToModify,
				CompactSummary:   event.CompactSummary,
				ContextMax:       mc.resolveContextMax(opts...),
			}))
		}
	}

	return messages, nil
}

// selectProcessors 按 processorTypes 和 compressionOnly 过滤处理器列表。
func (mc *SessionModelContext) selectProcessors(processorTypes []string, compressionOnly bool) []iface.ContextProcessor {
	if len(mc.processors) == 0 {
		return nil
	}

	var result []iface.ContextProcessor
	for _, proc := range mc.processors {
		// compressionOnly 过滤
		if compressionOnly && !IsCompressionProcessor(proc) {
			continue
		}

		// processorTypes 过滤
		if len(processorTypes) > 0 {
			matched := false
			for _, pt := range processorTypes {
				if proc.ProcessorType() == pt {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		result = append(result, proc)
	}
	return result
}

// getWindowMessages 先按 dialogueRound 截取 contextMessages，再按 windowSize 同时截断 systemMessages 和 contextMessages。
//
// 对齐 Python: _get_window_messages(self, system_messages, window_size, dialogue_round) -> (system_messages, context_messages)
// 双截断逻辑：systemSize = min(len(system), windowSize)，contextSize = windowSize - systemSize
func (mc *SessionModelContext) getWindowMessages(systemMessages []llm_schema.BaseMessage, windowSize, dialogueRound int) ([]llm_schema.BaseMessage, []llm_schema.BaseMessage) {
	// 获取全部消息（含历史）
	messages := mc.messageBuffer.GetBack(0, true)

	// 先按 dialogueRound 截取
	if dialogueRound > 0 {
		roundStartIdx := FindLastNDialogueRound(messages, dialogueRound)
		if roundStartIdx >= 0 && roundStartIdx < len(messages) {
			messages = messages[roundStartIdx:]
		}
	}

	// 按 windowSize 同时截断 systemMessages 和 contextMessages
	// 对齐 Python: system_messages_size = min(len(system_messages), window_size)
	if windowSize > 0 {
		systemSize := len(systemMessages)
		if systemSize > windowSize {
			systemSize = windowSize
		}
		systemMessages = systemMessages[len(systemMessages)-systemSize:]

		contextSize := windowSize - systemSize
		if contextSize > 0 && len(messages) > contextSize {
			messages = messages[len(messages)-contextSize:]
		} else if contextSize <= 0 {
			messages = nil
		}
	}

	return systemMessages, messages
}

// statContextWindow 调用 statMessages + statTools 填充 ContextWindow 的 Statistic。
// statMessages 已包含 TotalDialogues 计算，无需重复。
func (mc *SessionModelContext) statContextWindow(window *iface.ContextWindow) {
	allMessages := window.GetMessages()
	stat := &window.Statistic

	// 按角色统计消息 + token 计算 + 对话轮次
	mc.statMessages(stat, allMessages)

	// 工具 token 统计
	mc.statTools(stat, window.Tools)
}

// statMessages 按角色统计消息数量和 token 数。//
// 对齐 Python: _stat_messages(stat, messages)
func (mc *SessionModelContext) statMessages(stat *iface.ContextStats, messages []llm_schema.BaseMessage) {
	stat.TotalMessages = len(messages)
	// 对齐 Python: stat.total_dialogues = len(ContextUtils.find_all_dialogue_round(messages))
	stat.TotalDialogues = len(processor.FindAllDialogueRound(messages))

	// 按角色计数消息数量
	mc.countMessagesByRole(stat, messages)

	// 优先使用最后一条 AssistantMessage 的 usage_metadata.total_tokens
	// 对齐 Python: usage_tokens = self._get_last_assistant_usage_tokens(messages)
	// 对齐 Python: 如果usage_tokens不为空则直接设置总Token数
	for i := len(messages) - 1; i >= 0; i-- {
		if am, ok := messages[i].(*llm_schema.AssistantMessage); ok {
			if am.UsageMetadata != nil && am.UsageMetadata.TotalTokens > 0 {
				stat.TotalTokens = am.UsageMetadata.TotalTokens
				return
			}
		}
	}

	// 无 usage_metadata，逐条计算 token 并按角色累加
	mc.countMessagesTokensByRole(stat, messages)
}

// countMessagesByRole 按角色计数消息数量。
func (mc *SessionModelContext) countMessagesByRole(stat *iface.ContextStats, messages []llm_schema.BaseMessage) {
	for _, msg := range messages {
		switch msg.GetRole() {
		case llm_schema.RoleTypeSystem:
			stat.SystemMessages++
		case llm_schema.RoleTypeUser:
			stat.UserMessages++
		case llm_schema.RoleTypeAssistant:
			stat.AssistantMessages++
		case llm_schema.RoleTypeTool:
			stat.ToolMessages++
		}
	}
}

// countMessagesTokensByRole 逐条计算消息 token 并按角色累加。
func (mc *SessionModelContext) countMessagesTokensByRole(stat *iface.ContextStats, messages []llm_schema.BaseMessage) {
	var totalTokens int
	for _, msg := range messages {
		tokens := mc.countSingleMessageTokens(msg)
		totalTokens += tokens

		switch msg.GetRole() {
		case llm_schema.RoleTypeSystem:
			stat.SystemMessageTokens += tokens
		case llm_schema.RoleTypeUser:
			stat.UserMessageTokens += tokens
		case llm_schema.RoleTypeAssistant:
			stat.AssistantMessageTokens += tokens
		case llm_schema.RoleTypeTool:
			stat.ToolMessageTokens += tokens
		}
	}
	stat.TotalTokens = totalTokens
}

// statTools 统计工具 token 数。
func (mc *SessionModelContext) statTools(stat *iface.ContextStats, tools []schema.ToolInfoInterface) {
	if len(tools) == 0 {
		return
	}
	stat.Tools = len(tools)

	var toolTokens int
	for _, t := range tools {
		toolTokens += mc.countToolTokens(t)
	}
	stat.ToolTokens = toolTokens
	stat.TotalTokens += toolTokens
}

// countSingleMessageTokens 计算单条消息的 token 数。
//
// 对应 Python: SessionModelContext._count_single_message_tokens()
// tokenCounter 返回结果（含 0）直接使用，不再降级估算。
// fallback 使用 len/4 向下取整，对齐 Python len//4。
func (mc *SessionModelContext) countSingleMessageTokens(msg llm_schema.BaseMessage) int {
	content := msg.GetContent().Text()

	if mc.tokenCounter != nil {
		count, err := mc.tokenCounter.Count(content, mc.resolveContextModelName())
		if err == nil {
			return count
		}
	}

	// 降级：字符数/4 向下取整，对齐 Python len//4
	return len(content) / 4
}

// countToolTokens 计算单工具的 token 数。
//
// 对应 Python: SessionModelContext._count_tool_tokens()
// fallback 使用 json.Marshal 序列化整个 parameters dict + len/4 向下取整，对齐 Python json.dumps + len//4。
func (mc *SessionModelContext) countToolTokens(toolInfo schema.ToolInfoInterface) int {
	if mc.tokenCounter != nil {
		count, err := mc.tokenCounter.CountTools([]schema.ToolInfoInterface{toolInfo}, mc.resolveContextModelName())
		if err == nil {
			return count
		}
	}

	// 降级：拼接 name + description + json.dumps(parameters)，对齐 Python
	textContent := toolInfo.GetName()
	if toolInfo.GetDescription() != "" {
		textContent += toolInfo.GetDescription()
	}
	if toolInfo.GetParameters() != nil {
		if data, err := json.Marshal(toolInfo.GetParameters()); err == nil {
			textContent += string(data)
		}
	}
	return len(textContent) / 4
}

// countDialogueRounds 统计对话轮次数。
func (mc *SessionModelContext) countDialogueRounds(messages []llm_schema.BaseMessage) int {
	rounds := processor.FindAllDialogueRound(messages)
	return len(rounds)
}

// resolveContextModelName 从实例或选项解析模型名称。
//
// 对齐 Python: _resolve_context_model_name(self, kwargs) -> kwargs.get("model_name") or self._model_name
// 优先使用 opts 中的 ModelName，回退到实例字段 mc.modelName
func (mc *SessionModelContext) resolveContextModelName(opts ...iface.Option) string {
	o := iface.NewProcessorOption(opts...)
	if o.ModelName != "" {
		return o.ModelName
	}
	if mc.modelName != "" {
		return mc.modelName
	}
	return ""
}

// resolveContextMax 解析最大上下文 token 数。
func (mc *SessionModelContext) resolveContextMax(opts ...iface.Option) int {
	return ResolveContextMax(mc.modelName, mc.contextWindowTokens, mc.modelContextWindowTokens)
}

// getModelFromSession 尝试从 sessionRef 获取 llm.Model 实例。
func (mc *SessionModelContext) getModelFromSession() *llm.Model {
	if mc.sessionRef == nil {
		return nil
	}
	// Session 不直接持有 Model，返回 nil
	// 实际 Model 通过 Processor 的 option 传入
	return nil
}

// recordFromEvent 从 ContextEvent 记录简要信息到日志。
func (r *ProcessorStateRecorder) recordFromEvent(event *iface.ContextEvent) {
	if event == nil {
		return
	}
	logger.Info(logComponent).
		Str("event_type", event.EventType).
		Int("messages_to_modify_count", len(event.MessagesToModify)).
		Str("compact_summary", event.CompactSummary).
		Msg("处理器事件")
}

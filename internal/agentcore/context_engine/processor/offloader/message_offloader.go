package offloader

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/danwakefield/fnmatch"
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageOffloaderConfig 消息卸载器配置。
//
// 规则评估顺序：
//  1. messages_to_keep：最近 N 条消息始终保留
//  2. messages_threshold：总消息数超此值时触发卸载
//  3. tokens_threshold：总 Token 数超此值时触发卸载
//
// 仅角色在 offload_message_type 中且 Token 长度大于 large_message_threshold 的消息
// 才符合卸载条件。设置 keep_last_round=True 可独立保留最后一轮对话。
//
// 对应 Python: MessageOffloaderConfig (pydantic.BaseModel)
type MessageOffloaderConfig struct {
	// MessagesThreshold 消息数触发阈值，nil 表示不启用
	MessagesThreshold *int
	// TokensThreshold Token 数触发阈值（默认 20000）
	TokensThreshold int
	// LargeMessageThreshold 大消息判定阈值（默认 1000）
	LargeMessageThreshold int
	// OffloadMessageTypes 可卸载的消息角色列表（默认 ["tool"]）
	OffloadMessageTypes []string
	// ProtectedToolNames 受保护的工具名称列表（默认 ["reload_original_context_messages"]）
	ProtectedToolNames []string
	// TrimSize 裁剪保留 Token 数（默认 100）
	TrimSize int
	// MessagesToKeep 保留最近 N 条消息，nil 表示不保留
	MessagesToKeep *int
	// KeepLastRound 保留最后一轮完整对话（默认 true）
	KeepLastRound bool
}

// MessageOffloader 消息卸载器，基于消息数/Token 数阈值触发卸载。
//
// 当对话上下文超过安全限制时，对大消息执行裁剪并卸载到外部存储，
// 生成轻量占位符以减少 Token 消耗。
//
// 对应 Python: openjiuwen/core/context_engine/processor/offloader/message_offloader.py (MessageOffloader)
type MessageOffloader struct {
	*processor.BaseProcessor
	// config 具体配置（类型断言获取）
	config *MessageOffloaderConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// omitString 省略标记，等价 Python OMIT_STRING = "..."
	omitString = "..."
	// defaultTokensThreshold 默认 Token 触发阈值
	defaultTokensThreshold = 20000
	// defaultLargeMessageThreshold 默认大消息判定阈值
	defaultLargeMessageThreshold = 1000
	// defaultTrimSize 默认裁剪保留长度
	defaultTrimSize = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageOffloader 创建消息卸载器实例。
//
// 对应 Python: MessageOffloader.__init__(config)
func NewMessageOffloader(config *MessageOffloaderConfig) (*MessageOffloader, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)
	return &MessageOffloader{
		BaseProcessor: bp,
		config:        config,
	}, nil
}

// Validate 校验消息卸载器配置。
//
// 交叉校验：
//   - TrimSize < LargeMessageThreshold
//   - MessagesToKeep < MessagesThreshold（两者均非 nil 时）
//
// 对应 Python: MessageOffloader._validate_config()
func (c *MessageOffloaderConfig) Validate() error {
	// 应用默认值
	c.applyDefaults()

	if c.TrimSize >= c.LargeMessageThreshold {
		return fmt.Errorf("MessageOffloaderConfig.TrimSize(%d) 不能大于等于 LargeMessageThreshold(%d)",
			c.TrimSize, c.LargeMessageThreshold)
	}
	if c.MessagesThreshold != nil && c.MessagesToKeep != nil {
		if *c.MessagesToKeep >= *c.MessagesThreshold {
			return fmt.Errorf("MessageOffloaderConfig.MessagesToKeep(%d) 不能大于等于 MessagesThreshold(%d)",
				*c.MessagesToKeep, *c.MessagesThreshold)
		}
	}
	return nil
}

// ProcessorType 返回处理器类型标识。
func (mo *MessageOffloader) ProcessorType() string { return "MessageOffloader" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件（按顺序评估）：
//  1. MessagesToKeep != nil && 总消息数 <= MessagesToKeep → false
//  2. MessagesThreshold != nil && 总消息数 > MessagesThreshold → 检查候选 → true/false
//  3. 总 Token 数 > TokensThreshold → 检查候选 → true/false
//
// 对应 Python: MessageOffloader.trigger_add_messages()
func (mo *MessageOffloader) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	cfg := mo.config
	allMessages := append(mc.GetMessages(nil, true), messagesToAdd...)
	messageSize := len(allMessages)

	if cfg.MessagesToKeep != nil && messageSize <= *cfg.MessagesToKeep {
		return false, nil
	}

	if cfg.MessagesThreshold != nil && messageSize > *cfg.MessagesThreshold {
		if !mo.hasOffloadCandidate(allMessages, mc) {
			return false, nil
		}
		logger.Info(logger.ComponentAgentCore).
			Str("processor_type", mo.ProcessorType()).
			Int("message_size", messageSize).
			Int("threshold", *cfg.MessagesThreshold).
			Msg("上下文消息数超过阈值，触发卸载")
		return true, nil
	}

	// 计算 Token 数
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		contextTokens, _ := tokenCounter.CountMessages(mc.GetMessages(nil, true), "")
		addTokens, _ := tokenCounter.CountMessages(messagesToAdd, "")
		tokens := contextTokens + addTokens
		if tokens > cfg.TokensThreshold {
			if !mo.hasOffloadCandidate(allMessages, mc) {
				return false, nil
			}
			logger.Info(logger.ComponentAgentCore).
				Str("processor_type", mo.ProcessorType()).
				Int("tokens", tokens).
				Int("threshold", cfg.TokensThreshold).
				Msg("上下文 Token 数超过阈值，触发卸载")
			return true, nil
		}
	}

	return false, nil
}

// OnAddMessages 执行消息卸载。
//
// 对应 Python: MessageOffloader.on_add_messages()
func (mo *MessageOffloader) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	contextMessages := mc.GetMessages(nil, true)
	allMessages := append(contextMessages, messagesToAdd...)
	contextSize := len(contextMessages)

	event, processedMessages, err := mo.offloadLargeMessages(ctx, allMessages, mc, opts...)
	if err != nil {
		return nil, messagesToAdd, err
	}

	// 分离 contextMessages 和 messagesToAdd
	updatedContext := processedMessages[:contextSize]
	updatedToAdd := processedMessages[contextSize:]
	mc.SetMessages(updatedContext, true)

	return event, updatedToAdd, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (mo *MessageOffloader) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (mo *MessageOffloader) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyDefaults 应用默认值
func (c *MessageOffloaderConfig) applyDefaults() {
	if c.TokensThreshold == 0 {
		c.TokensThreshold = defaultTokensThreshold
	}
	if c.LargeMessageThreshold == 0 {
		c.LargeMessageThreshold = defaultLargeMessageThreshold
	}
	if len(c.OffloadMessageTypes) == 0 {
		c.OffloadMessageTypes = []string{"tool"}
	}
	if len(c.ProtectedToolNames) == 0 {
		c.ProtectedToolNames = []string{"reload_original_context_messages"}
	}
	if c.TrimSize == 0 {
		c.TrimSize = defaultTrimSize
	}
	// KeepLastRound 默认 true（零值为 false，需显式设置）
	// 注意：Go 零值为 false，但 Python 默认 true
	// 调用方应显式设置；此处不强制覆盖
}

// offloadLargeMessages 遍历卸载范围，逐条卸载大消息。
//
// 对应 Python: MessageOffloader._offload_large_messages()
func (mo *MessageOffloader) offloadLargeMessages(ctx context.Context, messages []llm_schema.BaseMessage, mc iface.ModelContext, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	processedMessages := make([]llm_schema.BaseMessage, len(messages))
	copy(processedMessages, messages)

	offloadRange := mo.getOffloadRange(messages)
	event := &iface.ContextEvent{
		EventType: mo.ProcessorType(),
	}

	for idx := offloadRange - 1; idx >= 0; idx-- {
		msg := processedMessages[idx]
		if !mo.shouldOffloadMessage(msg, processedMessages, mc) {
			continue
		}
		offloadMsg, err := mo.offloadMessage(ctx, msg, mc, opts...)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", mo.ProcessorType()).
				Int("message_idx", idx).
				Err(err).
				Msg("卸载消息失败，跳过")
			continue
		}
		if offloadMsg == nil {
			continue
		}
		processedMessages = processor.ReplaceMessages(processedMessages, []processor.Replacement{
			{StartIdx: idx, EndIdx: idx, Messages: []llm_schema.BaseMessage{offloadMsg}},
		})
		event.MessagesToModify = append(event.MessagesToModify, idx)
	}

	if len(event.MessagesToModify) == 0 {
		return nil, processedMessages, nil
	}
	return event, processedMessages, nil
}

// offloadMessage 卸载单条消息：裁剪内容 + 调用 BaseProcessor.OffloadMessages。
//
// 对应 Python: MessageOffloader._offload_message()
func (mo *MessageOffloader) offloadMessage(ctx context.Context, message llm_schema.BaseMessage, mc iface.ModelContext, opts ...iface.Option) (llm_schema.BaseMessage, error) {
	content := message.GetContent().Text()
	cfg := mo.config

	// 裁剪内容
	trimmedContent := content
	if len(content) > cfg.TrimSize {
		trimmedContent = content[:cfg.TrimSize] + omitString
	}

	// 生成 offload handle 和 path
	offloadHandle, offloadPath := mo.newOffloadHandleAndPath(mc)

	// 调用基类 OffloadMessages
	offloadMsg, err := mo.OffloadMessages(
		ctx, mc,
		message.GetRole().String(),
		trimmedContent,
		[]llm_schema.BaseMessage{message},
		iface.WithOffloadHandle(offloadHandle),
		iface.WithOffloadPath(offloadPath),
	)
	if err != nil {
		return nil, err
	}
	return offloadMsg, nil
}

// newOffloadHandleAndPath 生成卸载句柄和文件路径。
//
// 对应 Python: MessageOffloader._new_offload_handle_and_path()
//
// ⤵️ 5.31 回填：mc.WorkspaceDir() 方法
func (mo *MessageOffloader) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
	offloadHandle := uuid.New().String()
	sessionID := mc.SessionID()

	// ⤵️ 5.31 回填：使用 mc.WorkspaceDir() 获取工作目录
	// 当前 ModelContext 接口没有 WorkspaceDir() 方法，使用空字符串
	workspaceDir := mc.WorkspaceDir()

	fileName := fmt.Sprintf("%s_%s.json", mo.ProcessorType(), offloadHandle)
	if workspaceDir != "" {
		return offloadHandle, filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return offloadHandle, ""
}

// getOffloadRange 计算卸载范围（不在此范围内的消息不会被卸载）。
//
// 对应 Python: MessageOffloader._get_offload_range()
func (mo *MessageOffloader) getOffloadRange(messages []llm_schema.BaseMessage) int {
	keepIndex := len(messages)
	if mo.config.MessagesToKeep != nil {
		keepIndex = len(messages) - *mo.config.MessagesToKeep
	}

	if mo.config.KeepLastRound {
		lastAIMsgIdx := processor.FindLastFinalAssistantIdx(messages)
		if lastAIMsgIdx != -1 && lastAIMsgIdx < keepIndex {
			return lastAIMsgIdx
		}
	}
	return keepIndex
}

// hasOffloadCandidate 检查卸载范围内是否存在可卸载的候选消息。
//
// 对应 Python: MessageOffloader._has_offload_candidate()
func (mo *MessageOffloader) hasOffloadCandidate(messages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	offloadRange := mo.getOffloadRange(messages)
	for idx := offloadRange - 1; idx >= 0; idx-- {
		if mo.shouldOffloadMessage(messages[idx], messages, mc) {
			return true
		}
	}
	return false
}

// shouldOffloadMessage 判断消息是否符合卸载条件。
//
// 5 条规则（全部通过才卸载）：
//  1. 角色在 OffloadMessageTypes 中
//  2. content 是字符串
//  3. content 长度 > LargeMessageThreshold
//  4. 不是已卸载消息（OffloadMixin）
//  5. 不是受保护工具的结果
//
// 对应 Python: MessageOffloader._should_offload_message()
func (mo *MessageOffloader) shouldOffloadMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	cfg := mo.config

	// 规则 1：角色检查
	roleMatch := false
	role := message.GetRole().String()
	for _, rt := range cfg.OffloadMessageTypes {
		if rt == role {
			roleMatch = true
			break
		}
	}
	if !roleMatch {
		return false
	}

	// 规则 2：content 必须是字符串
	content := message.GetContent().Text()
	if content == "" {
		// content 不是纯文本（可能是多模态或空内容），不卸载
		return false
	}

	// 规则 3：长度检查
	if len(content) <= cfg.LargeMessageThreshold {
		return false
	}

	// 规则 4：已卸载消息不重复卸载
	if schema.IsOffloaded(message) {
		return false
	}

	// 规则 5：受保护工具消息不卸载
	if mo.isProtectedToolMessage(message, contextMessages) {
		return false
	}

	return true
}

// isProtectedToolMessage 检查消息是否为受保护工具的结果。
//
// 支持 "tool_name" 和 "tool_name:pattern" 两种格式。
// 后者使用 filepath.Match 对工具参数值做通配符匹配。
//
// 对应 Python: MessageOffloader._is_protected_tool_message()
func (mo *MessageOffloader) isProtectedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	// 只检查 ToolMessage
	if message.GetRole() != llm_schema.RoleTypeTool {
		return false
	}

	// 回溯查找对应的 ToolCall
	toolCall := processor.ResolveToolCallFromMessage(message, contextMessages)
	if toolCall == nil {
		return false
	}

	toolName := processor.ExtractToolName(toolCall)
	toolArgs := extractToolArgs(toolCall)

	for _, protected := range mo.config.ProtectedToolNames {
		if colonIdx := strings.Index(protected, ":"); colonIdx != -1 {
			protectedTool := protected[:colonIdx]
			protectedPattern := protected[colonIdx+1:]
			if toolName == protectedTool && matchPattern(toolArgs, protectedPattern) {
				return true
			}
		} else {
			if toolName == protected {
				return true
			}
		}
	}
	return false
}

// extractToolArgs 从 ToolCall 提取参数字典。
//
// 支持多种格式：JSON string、map 结构。
//
// 对应 Python: MessageOffloader._extract_tool_args()
func extractToolArgs(toolCall *llm_schema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	// ToolCall.Arguments 是 JSON string
	if toolCall.Arguments != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolCall.Arguments), &parsed); err == nil {
			return parsed
		}
	}
	return map[string]any{}
}

// matchPattern 检查参数值是否匹配通配符模式。
//
// 使用 fnmatch 库实现与 Python fnmatch 一致的通配符匹配，
// 支持 *、?、[...] 等模式，且 * 匹配任意字符包括 /。
//
// 对应 Python: MessageOffloader._match_pattern()
func matchPattern(args map[string]any, pattern string) bool {
	for _, value := range args {
		if strVal, ok := value.(string); ok {
			if fnmatch.Match(pattern, strVal, 0) {
				return true
			}
		}
	}
	return false
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("MessageOffloader",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*MessageOffloaderConfig)
			if !ok {
				return nil, fmt.Errorf("MessageOffloader: 配置类型不匹配，期望 *MessageOffloaderConfig，实际 %T", config)
			}
			return NewMessageOffloader(cfg)
		},
	)
}

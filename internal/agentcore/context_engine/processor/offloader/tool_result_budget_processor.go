package offloader

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolResultBudgetProcessorConfig 工具结果预算处理器配置。
//
// 按轮次控制工具结果 Token 预算。此处理器不使用 MessageOffloader
// 的消息数/Token 数触发逻辑，messages_threshold 和 messages_to_keep
// 仅作为兼容字段保留，供通用配置序列化使用。
//
// 对应 Python: ToolResultBudgetProcessorConfig (pydantic.BaseModel)
type ToolResultBudgetProcessorConfig struct {
	// ── 实际使用字段 ──

	// TokensThreshold 每轮工具结果 Token 预算（默认 50000）
	TokensThreshold int
	// LargeMessageThreshold 单条工具消息最小卸载大小（默认 10000）
	LargeMessageThreshold int
	// TrimSize 卸载后占位符保留的前 N 个字符（默认 3000）
	TrimSize int
	// ToolNameAllowlist 白名单工具名称列表（在名单内的工具结果永不卸载，默认 nil）
	ToolNameAllowlist []string
	// OffloadFilePrefix 卸载文件名前缀（默认 "ToolResultBudgetProcessor"）
	OffloadFilePrefix string

	// ── 兼容字段（不使用，保留用于序列化兼容）──

	// OffloadMessageTypes 兼容字段；仅支持 ["tool"]
	OffloadMessageTypes []string
	// MessagesThreshold 兼容字段；此处理器不使用消息数触发（0=未设置）
	MessagesThreshold int
	// MessagesToKeep 兼容字段；此处理器不按计数保留尾部（0=未设置）
	MessagesToKeep int
}

// ToolResultBudgetProcessor 工具结果预算处理器。
//
// 按对话轮次控制工具结果的 Token 预算。每轮内所有 ToolMessage 的
// Token 总数超过 TokensThreshold 时，从最大的工具结果开始逐个卸载，
// 直到该轮预算内。卸载后的消息用 <persisted-output> 标签占位，
// 保留前 TrimSize 字符的预览。
//
// 对应 Python: openjiuwen/core/context_engine/processor/offloader/tool_result_budget_processor.py
type ToolResultBudgetProcessor struct {
	*processor.BaseProcessor
	// config 具体配置
	config *ToolResultBudgetProcessorConfig
	// sysOperation 系统操作接口，通过 WithSysOption 选项注入
	sysOperation sysop.SysOperation
}

// offloadCandidate 卸载候选消息
type offloadCandidate struct {
	idx  int
	size int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ToolResultBudgetProcessorOption 构造选项函数
type ToolResultBudgetProcessorOption func(*ToolResultBudgetProcessor)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// PersistedOutputTag 卸载后占位符开始标签
	PersistedOutputTag = "<persisted-output>"
	// PersistedOutputClosingTag 卸载后占位符结束标签
	PersistedOutputClosingTag = "</persisted-output>"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolResultBudgetProcessor 创建工具结果预算处理器实例。
//
// 对应 Python: ToolResultBudgetProcessor.__init__(config)
func NewToolResultBudgetProcessor(config *ToolResultBudgetProcessorConfig, opts ...ToolResultBudgetProcessorOption) (*ToolResultBudgetProcessor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)
	p := &ToolResultBudgetProcessor{
		BaseProcessor: bp,
		config:        config,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// ProcessorType 返回处理器类型标识。
func (p *ToolResultBudgetProcessor) ProcessorType() string {
	return "ToolResultBudgetProcessor"
}

// WithSysOption 注入系统操作接口选项。
func WithSysOption(op sysop.SysOperation) ToolResultBudgetProcessorOption {
	return func(p *ToolResultBudgetProcessor) {
		p.sysOperation = op
	}
}

// Validate 校验工具结果预算处理器配置。
//
// 校验流程：先应用默认值填充零值字段，再校验核心字段有效性。
// 与 Python Pydantic Field(default=..., gt=0) 行为对齐：零值自动填充默认值。
//
// 对应 Python: ToolResultBudgetProcessorConfig._validate_config()
func (c *ToolResultBudgetProcessorConfig) Validate() error {
	c.applyDefaults()
	if c.TokensThreshold <= 0 {
		return fmt.Errorf("ToolResultBudgetProcessorConfig.TokensThreshold(%d) 必须大于 0", c.TokensThreshold)
	}
	if c.LargeMessageThreshold <= 0 {
		return fmt.Errorf("ToolResultBudgetProcessorConfig.LargeMessageThreshold(%d) 必须大于 0", c.LargeMessageThreshold)
	}
	if c.TrimSize <= 0 {
		return fmt.Errorf("ToolResultBudgetProcessorConfig.TrimSize(%d) 必须大于 0", c.TrimSize)
	}
	return nil
}

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：存在轮次内工具结果 Token 总数超过预算。
//
// 对应 Python: ToolResultBudgetProcessor.trigger_add_messages()
func (p *ToolResultBudgetProcessor) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	allMessages := append(allMsgs, messagesToAdd...)
	exceededRounds := p.roundBudgetExceeded(allMessages, mc)
	if len(exceededRounds) > 0 {
		logger.Info(logger.ComponentAgentCore).
			Str("processor_type", p.ProcessorType()).
			Msg("存在轮次工具结果超过预算，触发卸载")
		return true, nil
	}
	return false, nil
}

// OnAddMessages 执行工具结果预算卸载。
//
// 遍历每个对话轮次，若该轮工具结果 Token 总数超过预算，
// 从最大的工具结果开始逐个卸载，直到该轮预算内。
//
// 对应 Python: ToolResultBudgetProcessor.on_add_messages()
func (p *ToolResultBudgetProcessor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	// 从 opts 中提取 sysOperation
	po := iface.NewProcessorOption(opts...)
	p.sysOperation = po.SysOperation

	contextMessages, _ := mc.GetMessages(0, true)
	allMessages := append(contextMessages, messagesToAdd...)
	contextSize := len(contextMessages)
	updatedMessages := make([]llm_schema.BaseMessage, len(allMessages))
	copy(updatedMessages, allMessages)

	var modifiedIndices []int
	for _, roundRange := range p.iterRoundRanges(updatedMessages) {
		changed, newIndices := p.shrinkRoundToBudget(ctx, updatedMessages, roundRange, mc)
		if changed {
			modifiedIndices = append(modifiedIndices, newIndices...)
		}
	}

	if len(modifiedIndices) == 0 {
		return nil, messagesToAdd, nil
	}

	mc.SetMessages(updatedMessages[:contextSize], true)

	// 对齐 Python: sorted(set(modified_indices))，去重排序
	uniqueIndices := make(map[int]struct{})
	for _, idx := range modifiedIndices {
		uniqueIndices[idx] = struct{}{}
	}
	sortedIndices := make([]int, 0, len(uniqueIndices))
	for idx := range uniqueIndices {
		sortedIndices = append(sortedIndices, idx)
	}
	sort.Ints(sortedIndices)

	event := &iface.ContextEvent{
		EventType:        p.ProcessorType(),
		MessagesToModify: sortedIndices,
	}
	return event, updatedMessages[contextSize:], nil
}

// OnGetContextWindow 透传上下文窗口。
func (p *ToolResultBudgetProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, cw, nil
}

// TriggerGetContextWindow 不触发上下文窗口处理。
func (p *ToolResultBudgetProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (p *ToolResultBudgetProcessor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (p *ToolResultBudgetProcessor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyDefaults 设置默认值。
//
// 对应 Python: ToolResultBudgetProcessorConfig.__init__() 默认值
func (c *ToolResultBudgetProcessorConfig) applyDefaults() {
	if c.TokensThreshold == 0 {
		c.TokensThreshold = 50000
	}
	if c.LargeMessageThreshold == 0 {
		c.LargeMessageThreshold = 10000
	}
	if c.TrimSize == 0 {
		c.TrimSize = 3000
	}
	if c.OffloadFilePrefix == "" {
		c.OffloadFilePrefix = "ToolResultBudgetProcessor"
	}
	if len(c.OffloadMessageTypes) == 0 {
		c.OffloadMessageTypes = []string{"tool"}
	}
}

// iterRoundRanges 调用 FindAllDialogueRound，将 DialogueRound 转为 [2]int 范围。
//
// assistantIdx 为 nil 时用 len(messages)-1 作为轮次结束索引。
func (p *ToolResultBudgetProcessor) iterRoundRanges(messages []llm_schema.BaseMessage) [][2]int {
	rounds := processor.FindAllDialogueRound(messages)
	result := make([][2]int, 0, len(rounds))
	for _, r := range rounds {
		if r[0] == nil {
			continue
		}
		startIdx := *r[0]
		endIdx := len(messages) - 1
		if r[1] != nil {
			endIdx = *r[1]
		}
		if endIdx < startIdx {
			continue
		}
		result = append(result, [2]int{startIdx, endIdx})
	}
	return result
}

// roundBudgetExceeded 返回超预算的轮次范围列表。
func (p *ToolResultBudgetProcessor) roundBudgetExceeded(messages []llm_schema.BaseMessage, mc iface.ModelContext) [][2]int {
	var exceeded [][2]int
	for _, roundRange := range p.iterRoundRanges(messages) {
		totalSize := p.roundToolResultSize(messages, roundRange[0], roundRange[1], mc)
		if totalSize > p.config.TokensThreshold {
			// 检查是否有可卸载的候选
			candidates := p.collectRoundCandidates(messages, roundRange[0], roundRange[1], mc)
			if len(candidates) > 0 {
				exceeded = append(exceeded, roundRange)
			}
		}
	}
	return exceeded
}

// roundToolResultSize 计算单轮内所有 ToolMessage 的 messageSize 总和。
func (p *ToolResultBudgetProcessor) roundToolResultSize(messages []llm_schema.BaseMessage, startIdx, endIdx int, mc iface.ModelContext) int {
	total := 0
	for i := startIdx; i <= endIdx && i < len(messages); i++ {
		msg := messages[i]
		if msg.GetRole() == llm_schema.RoleTypeTool {
			total += p.messageSize(msg, mc)
		}
	}
	return total
}

// collectRoundCandidates 收集单轮内可卸载的候选消息（idx, size）。
//
// 只收集 shouldOffloadMessage 返回 true 的消息。
func (p *ToolResultBudgetProcessor) collectRoundCandidates(messages []llm_schema.BaseMessage, startIdx, endIdx int, mc iface.ModelContext) []offloadCandidate {
	var candidates []offloadCandidate
	for i := startIdx; i <= endIdx && i < len(messages); i++ {
		msg := messages[i]
		if p.shouldOffloadMessage(msg, messages, mc) {
			candidates = append(candidates, offloadCandidate{
				idx:  i,
				size: p.messageSize(msg, mc),
			})
		}
	}
	return candidates
}

// shrinkRoundToBudget 将轮次内工具结果缩减到预算内。
//
// 循环：当总 Token > threshold 时，收集候选 → 按大小降序 → 卸载最大的 → 替换原消息。
// 返回 (是否有变更, 被修改的消息索引列表)。
func (p *ToolResultBudgetProcessor) shrinkRoundToBudget(ctx context.Context, messages []llm_schema.BaseMessage, roundRange [2]int, mc iface.ModelContext) (bool, []int) {
	startIdx, endIdx := roundRange[0], roundRange[1]
	var modifiedIndices []int
	changed := false

	for p.roundToolResultSize(messages, startIdx, endIdx, mc) > p.config.TokensThreshold {
		candidates := p.collectRoundCandidates(messages, startIdx, endIdx, mc)
		if len(candidates) == 0 {
			break
		}
		// 按大小降序排列
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].size > candidates[j].size
		})
		targetIdx := candidates[0].idx
		offloaded, err := p.offloadToolMessage(ctx, messages[targetIdx], mc)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", p.ProcessorType()).
				Int("message_idx", targetIdx).
				Err(err).
				Msg("卸载工具消息失败，跳过当前候选继续尝试")
			// 对齐 Python：卸载失败时 continue 而非 break，继续尝试下一个候选
			continue
		}
		if offloaded != nil {
			messages[targetIdx] = offloaded
			modifiedIndices = append(modifiedIndices, targetIdx)
			changed = true
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", p.ProcessorType()).
				Int("message_idx", targetIdx).
				Msg("卸载返回空消息，跳过当前候选继续尝试")
			continue
		}
	}
	return changed, modifiedIndices
}

// shouldOffloadMessage 判断消息是否符合卸载条件。
//
// 5 条规则（全部通过才卸载）：
//  1. 角色在 OffloadMessageTypes 中
//  2. 不是已卸载消息
//  3. IsText() 纯文本内容
//  4. 非白名单工具消息
//  5. size > LargeMessageThreshold
//
// 对应 Python: ToolResultBudgetProcessor._should_offload_message()
func (p *ToolResultBudgetProcessor) shouldOffloadMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	// 规则 1：严格类型检查，对齐 Python isinstance(message, ToolMessage)
	if message.GetRole() != llm_schema.RoleTypeTool {
		return false
	}

	// 规则 2：已卸载消息不重复卸载
	if p.isAlreadyOffloaded(message) {
		return false
	}

	// 规则 3：内容必须是纯文本且非空
	content := message.GetContent()
	if !content.IsText() || content.Text() == "" {
		return false
	}

	// 规则 4：白名单工具消息不卸载
	if p.isAllowlistedToolMessage(message, contextMessages) {
		return false
	}

	// 规则 5：大小检查
	size := p.messageSize(message, mc)
	return size > p.config.LargeMessageThreshold
}

// isAllowlistedToolMessage 检查消息是否为白名单工具的结果。
//
// 用 ResolveToolNameFromMessage 回溯工具名，检查是否在 ToolNameAllowlist 中。
func (p *ToolResultBudgetProcessor) isAllowlistedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	if message.GetRole() != llm_schema.RoleTypeTool {
		return false
	}
	if len(p.config.ToolNameAllowlist) == 0 {
		return false
	}
	toolName := processor.ResolveToolNameFromMessage(message, contextMessages)
	if toolName == "" {
		return false
	}
	for _, allowed := range p.config.ToolNameAllowlist {
		if toolName == allowed {
			return true
		}
	}
	return false
}

// isAlreadyOffloaded 检查消息是否已被卸载。
func (p *ToolResultBudgetProcessor) isAlreadyOffloaded(msg llm_schema.BaseMessage) bool {
	return schema.IsOffloaded(msg)
}

// messageSize 计算消息的 Token 数。
//
// Token 计数优先（通过 mc.TokenCounter()），降级到 EstimateMessageTokens 字符估算。
func (p *ToolResultBudgetProcessor) messageSize(msg llm_schema.BaseMessage, mc iface.ModelContext) int {
	if mc != nil {
		tokenCounter := mc.TokenCounter()
		if tokenCounter != nil {
			count, err := tokenCounter.CountMessages([]llm_schema.BaseMessage{msg}, "")
			if err == nil && count > 0 {
				return count
			}
		}
	}
	return processor.EstimateMessageTokens(msg)
}

// offloadToolMessage 卸载单条工具消息。
//
// 两阶段构建：先用 "pending" 构建占位内容 → 调用 OffloadMessages →
// 提取实际 handle/type → 重建最终内容。
//
// 调用 OffloadMessages 时传递 tool_call_id/name/metadata/sys_operation，
// 与 Python offload_messages(tool_call_id=..., name=..., metadata=..., sys_operation=...) 对齐。
//
// 对应 Python: ToolResultBudgetProcessor._offload_tool_message()
func (p *ToolResultBudgetProcessor) offloadToolMessage(ctx context.Context, message llm_schema.BaseMessage, mc iface.ModelContext) (llm_schema.BaseMessage, error) {
	content := message.GetContent().Text()
	if content == "" {
		return message, nil
	}

	offloadHandle, offloadPath := p.newOffloadHandleAndPath(mc)

	preview := content
	hasMore := len(content) > p.config.TrimSize
	if hasMore {
		preview = content[:p.config.TrimSize]
	}

	// 阶段1：用 "pending" 构建
	persistedContent := buildPersistedOutputMessage(len(content), "pending", preview, hasMore)

	// 从原始 ToolMessage 提取 tool_call_id/name/metadata
	toolCallID := processor.GetToolCallID(message)
	msgName := message.GetName()
	msgMetadata := message.GetMetadata()

	offloadOpts := []iface.Option{
		iface.WithOffloadHandle(offloadHandle),
		iface.WithOffloadPath(offloadPath),
		iface.WithToolCallID(toolCallID),
		iface.WithSysOperation(p.sysOperation),
	}
	if msgName != "" {
		offloadOpts = append(offloadOpts, iface.WithName(msgName))
	}
	if msgMetadata != nil {
		offloadOpts = append(offloadOpts, iface.WithMetadata(msgMetadata))
	}

	offloadMsg, err := p.OffloadMessages(
		ctx, mc,
		"tool",
		persistedContent,
		[]llm_schema.BaseMessage{message},
		offloadOpts...,
	)
	if err != nil {
		return nil, err
	}
	if offloadMsg == nil {
		return message, nil
	}

	// 阶段2：提取实际 handle/type，重建内容
	actualHandle := offloadHandle
	actualType := "filesystem"
	if om, ok := offloadMsg.(schema.Offloadable); ok {
		info := om.GetOffloadInfo()
		actualHandle = info.OffloadHandle
		actualType = info.OffloadType
	}

	finalContent := buildPersistedOutputMessage(len(content),
		fmt.Sprintf("[[OFFLOAD: handle=%s, type=%s, path=%s]]", actualHandle, actualType, offloadPath),
		preview, hasMore)
	offloadMsg.SetContent(llm_schema.NewTextContent(finalContent))

	logger.Info(logger.ComponentAgentCore).
		Str("processor_type", p.ProcessorType()).
		Str("offload_handle", actualHandle).
		Str("offload_type", actualType).
		Str("tool_call_id", toolCallID).
		Int("original_size", len(content)).
		Int("trim_size", p.config.TrimSize).
		Msg("工具结果已卸载")

	return offloadMsg, nil
}

// buildPersistedOutputMessage 构建 <persisted-output> 格式的占位内容。
//
// 对应 Python: ToolResultBudgetProcessor._build_persisted_output_message()
func buildPersistedOutputMessage(originalSize int, offloadHandle string, preview string, hasMore bool) string {
	suffix := "\n...\n"
	if !hasMore {
		suffix = "\n"
	}
	return fmt.Sprintf("%s\nOutput too large (%d bytes).\n%s\nPreview (first %d chars):\n%s%s%s",
		PersistedOutputTag,
		originalSize,
		offloadHandle,
		len(preview),
		preview,
		suffix,
		PersistedOutputClosingTag,
	)
}

// newOffloadHandleAndPath 生成卸载句柄和文件路径。
//
// 对应 Python: ToolResultBudgetProcessor._new_offload_handle_and_path()
//
// ⤵️ 5.31 回填：mc.WorkspaceDir() 方法
func (p *ToolResultBudgetProcessor) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
	offloadHandle := uuid.New().String()
	sessionID := mc.SessionID()

	// ⤵️ 5.31 回填：使用 mc.WorkspaceDir() 获取工作目录
	// 当前 ModelContext 接口没有 WorkspaceDir() 方法，使用空字符串
	workspaceDir := mc.WorkspaceDir()

	filePrefix := p.config.OffloadFilePrefix
	if filePrefix == "" {
		filePrefix = p.ProcessorType()
	}
	fileName := fmt.Sprintf("%s_%s.json", filePrefix, offloadHandle)
	if workspaceDir != "" {
		return offloadHandle, filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return offloadHandle, ""
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("ToolResultBudgetProcessor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*ToolResultBudgetProcessorConfig)
			if !ok {
				return nil, fmt.Errorf("ToolResultBudgetProcessor: 配置类型不匹配，期望 *ToolResultBudgetProcessorConfig，实际 %T", config)
			}
			return NewToolResultBudgetProcessor(cfg)
		},
	)
}

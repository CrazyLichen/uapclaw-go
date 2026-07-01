package compressor

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MicroCompactProcessorConfig 微压缩处理器配置。
//
// 清除旧工具结果内容以减少 Token 消耗，保留每个工具最近的若干条结果。
//
// 对应 Python: MicroCompactProcessorConfig (pydantic.BaseModel)
type MicroCompactProcessorConfig struct {
	// TriggerThreshold 触发阈值，可清除结果数超出保留尾部的数量阈值
	TriggerThreshold int
	// CompactableToolNames 可压缩的工具名称列表
	CompactableToolNames []string
	// KeepRecentPerTool 每个工具保留最近的 ToolMessage 数量
	KeepRecentPerTool int
	// ClearedMarker 清除旧内容时的替换文本
	ClearedMarker string
}

// MicroCompactProcessor 微压缩处理器，清除旧工具结果内容以减少 Token 消耗。
//
// 当某个可压缩工具的 ToolMessage 数量超过 triggerThreshold + keepRecentPerTool 时，
// 将超出保留窗口的旧 ToolMessage 的 content 替换为 clearedMarker，
// 保留每个工具最近的 keepRecentPerTool 条结果。
//
// 不需要调用 LLM，是处理器链中最轻量的压缩手段。
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/micro_compact_processor.py (MicroCompactProcessor)
type MicroCompactProcessor struct {
	*processor.BaseProcessor
	// mcpConfig 微压缩处理器具体配置
	mcpConfig *MicroCompactProcessorConfig
}

// ──────────────────────────── 常量 ────────────────────────────

// MicroCompactClearedMarker 旧工具结果内容清除标记
const MicroCompactClearedMarker = "[Old tool result Content cleared]"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMicroCompactProcessorConfig 创建微压缩处理器配置，使用默认值。
func NewMicroCompactProcessorConfig() *MicroCompactProcessorConfig {
	return &MicroCompactProcessorConfig{
		TriggerThreshold: 5,
		CompactableToolNames: []string{
			"grep", "glob", "read_file", "web_search", "web_fetch",
		},
		KeepRecentPerTool: 15,
		ClearedMarker:     MicroCompactClearedMarker,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 校验微压缩处理器配置。
func (c *MicroCompactProcessorConfig) Validate() error {
	if c.TriggerThreshold <= 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.TriggerThreshold 必须大于 0，当前值: %d", c.TriggerThreshold)
	}
	if c.KeepRecentPerTool < 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.KeepRecentPerTool 不能为负数，当前值: %d", c.KeepRecentPerTool)
	}
	if c.ClearedMarker == "" {
		return fmt.Errorf("MicroCompactProcessorConfig.ClearedMarker 不能为空")
	}
	if len(c.CompactableToolNames) == 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.CompactableToolNames 不能为空")
	}
	return nil
}

// NewMicroCompactProcessor 创建微压缩处理器实例。
func NewMicroCompactProcessor(config *MicroCompactProcessorConfig) (*MicroCompactProcessor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)
	return &MicroCompactProcessor{
		BaseProcessor: bp,
		mcpConfig:     config,
	}, nil
}

// ProcessorType 返回处理器类型标识。
func (mcp *MicroCompactProcessor) ProcessorType() string {
	return "MicroCompactProcessor"
}

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：
//  1. 消息列表构成一个完整的 API 轮次
//  2. 某个可压缩工具的 ToolMessage 数量超过 triggerThreshold + keepRecentPerTool
//
// 对应 Python: MicroCompactProcessor.trigger_add_messages()
func (mcp *MicroCompactProcessor) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	allMessages := append(mc.GetMessages(0, true), messagesToAdd...)
	if !mcp.IsAPIRound(allMessages) {
		return false, nil
	}
	return mcp.hasAnyToolExceedThreshold(allMessages), nil
}

// OnAddMessages 执行微压缩，将旧 ToolMessage 内容替换为清除标记。
//
// 对应 Python: MicroCompactProcessor.on_add_messages()
func (mcp *MicroCompactProcessor) OnAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	allMessages := append(mc.GetMessages(0, true), messagesToAdd...)

	// 从 opts 提取 force 标志
	po := iface.NewProcessorOption(opts...)
	force := false
	if po.Extra != nil {
		if v, ok := po.Extra["force"].(bool); ok && v {
			force = true
		}
	}

	indicesToClear := mcp.collectFlatIndicesForCompact(allMessages, force)
	if len(indicesToClear) == 0 {
		return nil, messagesToAdd, nil
	}

	marker := mcp.mcpConfig.ClearedMarker
	var modifiedIndices []int
	for _, index := range indicesToClear {
		tm, ok := allMessages[index].(*llm_schema.ToolMessage)
		if !ok {
			continue
		}
		if tm.GetContent().Text() == marker {
			continue
		}
		allMessages[index].SetContent(llm_schema.NewTextContent(marker))
		modifiedIndices = append(modifiedIndices, index)
	}

	if len(modifiedIndices) == 0 {
		return nil, messagesToAdd, nil
	}

	// 收集被清除消息对应的工具名集合
	clearedTools := make(map[string]bool)
	for _, index := range modifiedIndices {
		toolName := processor.ResolveToolNameFromMessage(allMessages[index], allMessages)
		if toolName != "" {
			clearedTools[toolName] = true
		}
	}
	var toolNames []string
	for name := range clearedTools {
		toolNames = append(toolNames, name)
	}

	mc.SetMessages(allMessages, true)

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "MicroCompactProcessor_cleared").
		Int("cleared_count", len(modifiedIndices)).
		Strs("tools", toolNames).
		Bool("force", force).
		Msg("微压缩处理器清除了旧工具结果内容")

	return &iface.ContextEvent{
		EventType:        mcp.ProcessorType(),
		MessagesToModify: modifiedIndices,
	}, []llm_schema.BaseMessage{}, nil
}

// SaveState 导出处理器内部状态（无状态，返回空 map）。
func (mcp *MicroCompactProcessor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（无状态，空操作）。
func (mcp *MicroCompactProcessor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectCompactableIndicesByTool 遍历消息，按工具名分组收集可压缩 ToolMessage 索引。
//
// 对应 Python: MicroCompactProcessor._collect_compactable_indices_by_tool()
func (mcp *MicroCompactProcessor) collectCompactableIndicesByTool(messages []llm_schema.BaseMessage) map[string][]int {
	allowedNames := make(map[string]bool, len(mcp.mcpConfig.CompactableToolNames))
	for _, name := range mcp.mcpConfig.CompactableToolNames {
		allowedNames[name] = true
	}

	result := make(map[string][]int)
	for index, message := range messages {
		tm, ok := message.(*llm_schema.ToolMessage)
		if !ok {
			continue
		}
		if tm.GetContent().Text() == mcp.mcpConfig.ClearedMarker {
			continue
		}
		toolName := processor.ResolveToolNameFromMessage(message, messages)
		if toolName == "" {
			continue
		}
		if allowedNames[toolName] {
			result[toolName] = append(result[toolName], index)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasAnyToolExceedThreshold 判断任一工具的 ToolMessage 数量是否超过触发阈值。
//
// 对应 Python: MicroCompactProcessor._has_any_tool_exceed_threshold()
func (mcp *MicroCompactProcessor) hasAnyToolExceedThreshold(messages []llm_schema.BaseMessage) bool {
	groupedIndices := mcp.collectCompactableIndicesByTool(messages)
	threshold := mcp.mcpConfig.TriggerThreshold + mcp.mcpConfig.KeepRecentPerTool
	for _, indices := range groupedIndices {
		if len(indices) > threshold {
			return true
		}
	}
	return false
}

// collectFlatIndicesForCompact 收集需要清除的索引列表。
//
// force=true 时阈值降为 KeepRecentPerTool；超过阈值的工具，保留尾部 KeepRecentPerTool 条，
// 其余加入清除列表。
//
// 对应 Python: MicroCompactProcessor._collect_flat_indices_for_compact()
func (mcp *MicroCompactProcessor) collectFlatIndicesForCompact(messages []llm_schema.BaseMessage, force bool) []int {
	grouped := mcp.collectCompactableIndicesByTool(messages)
	var result []int
	for _, indices := range grouped {
		threshold := mcp.mcpConfig.KeepRecentPerTool
		if !force {
			threshold += mcp.mcpConfig.TriggerThreshold
		}
		if len(indices) > threshold {
			keepCount := mcp.mcpConfig.KeepRecentPerTool
			if keepCount > 0 {
				result = append(result, indices[:len(indices)-keepCount]...)
			} else {
				result = append(result, indices...)
			}
		}
	}
	return result
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("MicroCompactProcessor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*MicroCompactProcessorConfig)
			if !ok {
				return nil, fmt.Errorf("MicroCompactProcessor: 配置类型不匹配，期望 *MicroCompactProcessorConfig，实际 %T", config)
			}
			mcp, err := NewMicroCompactProcessor(cfg)
			if err != nil {
				return nil, err
			}
			return mcp, nil
		},
	)
}

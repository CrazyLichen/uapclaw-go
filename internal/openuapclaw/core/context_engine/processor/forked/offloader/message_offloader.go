// Package offloader 提供 forked 消息卸载器（确定性规则压缩优先 + head/tail 预览兜底）。
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/offloader/message_offloader.py（714 行）
//
// 文件目录：
//
//	offloader/
//	├── doc.go                  # 包文档
//	└── message_offloader.go   # MessageSummaryOffloader（forked 版）
package offloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor"
	forked "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	rulecompression "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/offloader/rule_compression"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// omitString 省略标记
	// Python: message_offloader.py:24 (OMIT_STRING)
	omitString = "..."

	// MessageOffloaderDebugLogDirEnv 调试目录环境变量（OPENUAPCLAW_* 前缀，D-24.15-b2 同族）
	// Python: message_offloader.py:25 (MESSAGE_OFFLOADER_DEBUG_LOG_DIR_ENV)
	MessageOffloaderDebugLogDirEnv = "OPENUAPCLAW_MESSAGE_OFFLOADER_DEBUG_LOG_DIR"

	// messageOffloaderDebugLogFile 调试日志文件名
	// Python: message_offloader.py:26
	messageOffloaderDebugLogFile = "message_offloader_debug.jsonl"

	// offloadPreviewHeadTailChars 预览头尾保留字符数
	// Python: message_offloader.py:30 (_OFFLOAD_PREVIEW_HEAD_TAIL_CHARS)
	offloadPreviewHeadTailChars = 2000
)

// OffloadStrategy 卸载策略字面量集合。
// Python: message_offloader.py:27 (OffloadStrategy Literal)
type OffloadStrategy = string

const (
	// OffloadStrategyRuleThenTruncate 默认策略：先规则压缩再截断预览
	// Python: message_offloader.py:29 (_OFFLOAD_STRATEGY)
	OffloadStrategyRuleThenTruncate OffloadStrategy = "rule_then_truncate"
	// OffloadStrategyRuleOnly 仅规则压缩
	OffloadStrategyRuleOnly OffloadStrategy = "rule_only"
	// OffloadStrategyTruncateOnly 仅截断
	OffloadStrategyTruncateOnly OffloadStrategy = "truncate_only"
)

// offloadStrategy 包级策略（Go 无模块级可变字面量环境，固定默认）。
var offloadStrategy OffloadStrategy = OffloadStrategyRuleThenTruncate

// nowFunc 时间源（测试可覆盖）。
var nowFunc = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// ──────────────────────────── 结构体 ────────────────────────────

// MessageSummaryOffloaderConfig 上下文相对工具结果卸载的最小配置。
// Python: message_offloader.py:33 (MessageSummaryOffloaderConfig)
type MessageSummaryOffloaderConfig struct {
	// AddMessageThresholdRatio 新增工具消息处理的上下文容量比例阈值（>0）
	// Python: Field(default=0.1, gt=0)
	AddMessageThresholdRatio float64 `json:"add_message_threshold_ratio"`
	// TtlSeconds TTL 处理就绪前的空闲秒数（≥0）
	// Python: Field(default=300, ge=0)
	TTLSeconds int `json:"ttl_seconds"`
	// TTLContextOccupancyRatio TTL 处理就绪的上下文占用比例（>0）
	// Python: Field(default=0.5, gt=0)
	TTLContextOccupancyRatio float64 `json:"ttl_context_occupancy_ratio"`
	// TTLMessageThresholdRatio 单条 TTL 工具消息处理的容量比例（>0）
	// Python: Field(default=0.05, gt=0)
	TTLMessageThresholdRatio float64 `json:"ttl_message_threshold_ratio"`
	// ProtectedToolNames 必须保持内联的工具名或 tool:argument-pattern 项
	// Python: Field(default_factory=lambda: ["read_file"])
	ProtectedToolNames []string `json:"protected_tool_names"`
	// EnableDebugDump 开启后持久化调试记录
	EnableDebugDump bool `json:"enable_debug_dump"`
	// DebugDumpDir 调试记录目录（可选）
	DebugDumpDir string `json:"debug_dump_dir"`
}

// NewMessageSummaryOffloaderConfig 创建配置，字段使用 Python 默认值。
func NewMessageSummaryOffloaderConfig() *MessageSummaryOffloaderConfig {
	return &MessageSummaryOffloaderConfig{
		AddMessageThresholdRatio: 0.1,
		TTLSeconds:               300,
		TTLContextOccupancyRatio: 0.5,
		TTLMessageThresholdRatio: 0.05,
		ProtectedToolNames:       []string{"read_file"},
	}
}

// Validate 校验配置（pydantic Field 约束等价）。
func (c *MessageSummaryOffloaderConfig) Validate() error {
	if c.AddMessageThresholdRatio <= 0 {
		return fmt.Errorf("add_message_threshold_ratio 必须 > 0，当前值: %v", c.AddMessageThresholdRatio)
	}
	if c.TTLSeconds < 0 {
		return fmt.Errorf("ttl_seconds 不能为负数，当前值: %d", c.TTLSeconds)
	}
	if c.TTLContextOccupancyRatio <= 0 || c.TTLMessageThresholdRatio <= 0 {
		return fmt.Errorf("ttl 比例阈值必须 > 0")
	}
	return nil
}

// SetModelDefaults iface.ProcessorConfig 实现（本配置无模型字段）。
func (c *MessageSummaryOffloaderConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {
}

// GetModel 本配置无模型字段，返回 nil。
func (c *MessageSummaryOffloaderConfig) GetModel() *llm_schema.ModelRequestConfig { return nil }

// MessageSummaryOffloader forked 消息卸载器。
// Python: message_offloader.py:66 (MessageSummaryOffloader)
type MessageSummaryOffloader struct {
	*forked.ContextProcessor
	*processor.BaseProcessor
	// rulePipeline 规则压缩管线
	rulePipeline *rulecompression.RuleCompressionPipeline
}

// NewMessageSummaryOffloader 创建卸载器。
// Python: message_offloader.py:67 (__init__)
func NewMessageSummaryOffloader(config *MessageSummaryOffloaderConfig) *MessageSummaryOffloader {
	mo := &MessageSummaryOffloader{
		ContextProcessor: forked.NewContextProcessor(config),
		BaseProcessor:    &processor.BaseProcessor{},
		rulePipeline: rulecompression.NewRuleCompressionPipeline(
			nil, nowFunc,
			config.EnableDebugDump, config.DebugDumpDir,
		),
	}
	return mo
}

// ProcessorType 处理器类型标识。
func (mo *MessageSummaryOffloader) ProcessorType() string { return "MessageSummaryOffloader" }

// Config 强类型配置。
func (mo *MessageSummaryOffloader) Config() *MessageSummaryOffloaderConfig {
	if cfg, ok := mo.ContextProcessor.Config().(*MessageSummaryOffloaderConfig); ok {
		return cfg
	}
	return NewMessageSummaryOffloaderConfig()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TriggerAddMessages 新增消息阈值触发判定。
// Python: message_offloader.py:74 (trigger_add_messages)
func (mo *MessageSummaryOffloader) TriggerAddMessages(
	_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option,
) (bool, error) {
	thresholdTokens := mo.addMessageTokenThreshold(mc)
	triggered := false
	for index, message := range messagesToAdd {
		toolMsg, ok := message.(*llm_schema.ToolMessage)
		if !ok || !toolMsg.Content.IsText() {
			continue
		}
		contentTokens := mo.countMessagesTokens(mc, []llm_schema.BaseMessage{message}, false)
		if contentTokens > thresholdTokens {
			triggered = true
			mo.writeDebugLog(mc, "add_message_triggered_offload", map[string]any{
				"pass_name": "add", "message_index": index,
				"threshold_tokens": thresholdTokens, "content_tokens": contentTokens,
				"content_chars": len(toolMsg.Content.Text()), "original_content": toolMsg.Content.Text(),
			}, message)
		} else {
			mo.writeDebugLog(mc, "threshold_check", map[string]any{
				"pass_name": "add", "message_index": index,
				"threshold_tokens": thresholdTokens, "content_tokens": contentTokens,
				"hit": false, "reason": "message_below_threshold",
			}, message)
		}
	}
	return triggered, nil
}

// OnAddMessages 新增消息处理（逐条超阈压缩/卸载）。
// Python: message_offloader.py:109 (on_add_messages)
func (mo *MessageSummaryOffloader) OnAddMessages(
	ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option,
) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	existing, _ := mc.GetMessages(0, true)
	allMessages := append(append([]llm_schema.BaseMessage(nil), existing...), messagesToAdd...)
	thresholdTokens := mo.addMessageTokenThreshold(mc)
	maxChars := mo.addMessageMaxChars(mc)
	processed := append([]llm_schema.BaseMessage(nil), messagesToAdd...)
	event := &iface.ContextEvent{EventType: mo.ProcessorType()}
	contextSize := mc.Len()

	for index, message := range messagesToAdd {
		replacement := mo.processAddedMessage(ctx, message, mc, allMessages, thresholdTokens, maxChars)
		if sameMessage(replacement, message) {
			continue
		}
		processed[index] = replacement
		event.MessagesToModify = append(event.MessagesToModify, contextSize+index)
	}

	if len(event.MessagesToModify) > 0 {
		return event, processed, nil
	}
	return nil, processed, nil
}

// TriggerGetContextWindow TTL 空闲 + 占用判定。
// Python: message_offloader.py:237 (trigger_get_context_window)
func (mo *MessageSummaryOffloader) TriggerGetContextWindow(
	_ context.Context, mc iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option,
) (bool, error) {
	now := mo.rulePipeline.CurrentTime()
	var previousAccess float64
	var hasPrevious bool
	if accessor, ok := mc.(WindowAccessAtProvider); ok {
		previousAccess, hasPrevious = accessor.LastWindowAccessAt()
		accessor.SetWindowAccessAt(now)
	}
	cfg := mo.Config()
	if cfg.TTLSeconds <= 0 {
		return false, nil
	}
	if !hasPrevious {
		return false, nil
	}
	if now-previousAccess < float64(cfg.TTLSeconds) {
		return false, nil
	}
	return mo.contextOccupancyTokens(mc) >= mo.ttlOccupancyTokenThreshold(mc), nil
}

// OnGetContextWindow TTL 处理（超阈 ToolMessage 压缩/卸载并回写窗口）。
// Python: message_offloader.py:257 (on_get_context_window)
func (mo *MessageSummaryOffloader) OnGetContextWindow(
	ctx context.Context, mc iface.ModelContext, cw iface.ContextWindow, opts ...iface.Option,
) (*iface.ContextEvent, iface.ContextWindow, error) {
	messages, _ := mc.GetMessages(0, true)
	processed := append([]llm_schema.BaseMessage(nil), messages...)
	event := &iface.ContextEvent{EventType: mo.ProcessorType()}
	ttlMessageThresholdTokens := mo.ttlMessageTokenThreshold(mc)
	ttlMaxChars := mo.ttlMessageMaxChars(mc)

	for index, message := range processed {
		if !mo.isProcessableToolMessage(message, processed) {
			continue
		}
		if mo.countMessagesTokens(mc, []llm_schema.BaseMessage{message}, false) <= ttlMessageThresholdTokens {
			continue
		}
		replacement := mo.processTTLMessage(ctx, message, mc, ttlMessageThresholdTokens, ttlMaxChars)
		if sameMessage(replacement, message) {
			continue
		}
		processed[index] = replacement
		event.MessagesToModify = append(event.MessagesToModify, index)
	}

	if len(event.MessagesToModify) > 0 {
		mc.SetMessages(processed, true)
		replaceWindowMessages(&cw, processed)
		return event, cw, nil
	}
	return nil, cw, nil
}

// OnAddTrigger 恒 false（基座缺省由 TriggerAddMessages 控制语义一致）。

// LoadState 无状态。
// Python: message_offloader.py:710 (load_state)
func (mo *MessageSummaryOffloader) LoadState(map[string]any) {}

// SaveState 无状态。
// Python: message_offloader.py:713 (save_state)
func (mo *MessageSummaryOffloader) SaveState() map[string]any { return map[string]any{} }

// ──────────────────────────── 非导出函数 ────────────────────────────

// processAddedMessage 单条新增消息处理。
// Python: message_offloader.py:155 (_process_added_message)
func (mo *MessageSummaryOffloader) processAddedMessage(
	ctx context.Context,
	message llm_schema.BaseMessage,
	mc iface.ModelContext,
	contextMessages []llm_schema.BaseMessage,
	thresholdTokens int,
	maxChars int,
) llm_schema.BaseMessage {
	if !mo.isProcessableToolMessage(message, contextMessages) {
		return message
	}
	if mo.countMessagesTokens(mc, []llm_schema.BaseMessage{message}, false) <= thresholdTokens {
		return message
	}

	if mo.shouldTryRuleCompression() {
		mo.writeDebugLog(mc, "rule_compression_triggered", map[string]any{
			"pass_name": "add", "threshold_tokens": thresholdTokens,
			"max_chars": maxChars, "original_content": messageContent(message),
		}, message)
		compressed := mo.rulePipeline.Compress(message, mc, "add", maxChars, false, contextMessages)
		if isRuleCompressedMessage(compressed) {
			mo.writeDebugLog(mc, "rule_compression_completed", map[string]any{
				"pass_name": "add", "threshold_tokens": thresholdTokens, "max_chars": maxChars,
				"original_content": messageContent(message), "compressed_content": messageContent(compressed),
			}, compressed)
			return mo.finalizeRuleCompressedMessage(ctx, compressed, mc, message, maxChars, true, mo.shouldTruncateRulePreview())
		}
	}

	if mo.shouldFallbackToPlainOffload() {
		return mo.offloadMessage(ctx, message, mc, message)
	}
	return message
}

// finalizeRuleCompressedMessage 规则压缩收尾（按需卸载原文 + 截断预览）。
// Python: message_offloader.py:203 (_finalize_rule_compressed_message)
func (mo *MessageSummaryOffloader) finalizeRuleCompressedMessage(
	ctx context.Context,
	message llm_schema.BaseMessage,
	mc iface.ModelContext,
	originalMessage llm_schema.BaseMessage,
	maxChars int,
	offloadOriginal bool,
	truncateOffloadedPreview bool,
) llm_schema.BaseMessage {
	if !offloadOriginal && len(messageContent(message)) <= maxChars {
		return message
	}

	offloaded := mo.offloadMessage(ctx, message, mc, originalMessage)
	if !truncateOffloadedPreview {
		return offloaded
	}
	if mo.shouldKeepFullRuleCompressedPreview(message) {
		return offloaded
	}
	if len(messageContent(offloaded)) <= maxChars {
		return offloaded
	}
	replaced := cloneToolMessageWithContent(offloaded,
		truncatePreservingOffloadMarker(messageContent(offloaded), maxChars))
	return replaced
}

// processTTLMessage TTL 单条处理。
// Python: message_offloader.py:292 (_process_ttl_message)
func (mo *MessageSummaryOffloader) processTTLMessage(
	ctx context.Context,
	message llm_schema.BaseMessage,
	mc iface.ModelContext,
	ttlMessageThresholdTokens int,
	maxChars int,
) llm_schema.BaseMessage {
	mo.writeDebugLog(mc, "ttl_message_triggered_offload", map[string]any{
		"pass_name": "ttl", "threshold_tokens": ttlMessageThresholdTokens, "max_chars": maxChars,
		"content_tokens": mo.countMessagesTokens(mc, []llm_schema.BaseMessage{message}, false),
		"content_chars":  len(messageContent(message)), "original_content": messageContent(message),
	}, message)
	if !mo.shouldTryRuleCompression() {
		if !mo.shouldFallbackToPlainOffload() {
			return message
		}
		return mo.offloadMessage(ctx, message, mc, message)
	}
	if isRuleCompressedMessage(message) {
		return message
	}
	mo.writeDebugLog(mc, "rule_compression_triggered", map[string]any{
		"pass_name": "ttl", "threshold_tokens": ttlMessageThresholdTokens,
		"max_chars": maxChars, "original_content": messageContent(message),
	}, message)
	processed := mo.rulePipeline.Compress(message, mc, "ttl", maxChars, true, contextMessagesOf(mc))
	if isRuleCompressedMessage(processed) {
		mo.writeDebugLog(mc, "rule_compression_completed", map[string]any{
			"pass_name": "ttl", "threshold_tokens": ttlMessageThresholdTokens, "max_chars": maxChars,
			"original_content": messageContent(message), "compressed_content": messageContent(processed),
		}, processed)
		return mo.finalizeRuleCompressedMessage(ctx, processed, mc, message, maxChars, true, mo.shouldTruncateRulePreview())
	}
	if len(messageContent(processed)) <= maxChars {
		return processed
	}
	if !mo.shouldFallbackToPlainOffload() {
		return message
	}
	return mo.offloadMessage(ctx, processed, mc, message)
}

// offloadMessage 卸载单条消息（预览裁剪 + BaseProcessor.OffloadMessages）。
// Python: message_offloader.py:358 (_offload_message)
func (mo *MessageSummaryOffloader) offloadMessage(
	ctx context.Context,
	message llm_schema.BaseMessage,
	mc iface.ModelContext,
	originalMessage llm_schema.BaseMessage,
) llm_schema.BaseMessage {
	content := messageContent(message)
	if !isRuleCompressedMessage(message) {
		content = mo.headTailPreview(content, "Content truncated and offloaded.")
	} else if ruleCompressionRequestsOriginalOffload(message) {
		content = content + "\n[Original content offloaded.]"
	}
	offloadHandle, offloadPath := mo.newOffloadHandleAndPath(mc)
	offloaded, err := mo.BaseProcessor.OffloadMessages(
		ctx, mc, messageRole(message), content,
		[]llm_schema.BaseMessage{originalMessage},
		iface.WithOffloadHandle(offloadHandle),
		iface.WithOffloadPath(offloadPath),
		iface.WithOffloadType("filesystem"),
		iface.WithToolCallID(toolCallIDOf(message)),
		iface.WithMetadata(messageMetadata(message)),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[MessageSummaryOffloader] offload failed")
		return originalMessage
	}
	if offloaded != nil {
		mo.writeDebugLog(mc, "message_offloaded", map[string]any{
			"offload_handle": offloadHandle, "offload_path": offloadPath,
			"offloaded_content": messageContent(offloaded),
		}, offloaded)
		return offloaded
	}
	return originalMessage
}

// isProcessableToolMessage 可处理判定（ToolMessage + 文本 + 非卸载消息 + 非保护工具）。
// Python: message_offloader.py:417 (_is_processable_tool_message)
func (mo *MessageSummaryOffloader) isProcessableToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	toolMsg, ok := message.(*llm_schema.ToolMessage)
	if !ok || !toolMsg.Content.IsText() {
		return false
	}
	if isOffloadMessage(message) {
		return false
	}
	return !mo.isProtectedToolMessage(message, contextMessages)
}

// isRuleCompressedMessage 规则压缩标记判定。
// Python: message_offloader.py:430 (_is_rule_compressed_message)
func isRuleCompressedMessage(message llm_schema.BaseMessage) bool {
	metadata := messageMetadata(message)
	_, ok := metadata["rule_compressed_at"]
	return ok && metadata["rule_compressed_at"] != nil
}

// isOffloadMessage OffloadMixin 等价判定（Go 侧卸载消息以 [[OFFLOAD: 标记承载，
// metadata offload_handle 判型）。
func isOffloadMessage(message llm_schema.BaseMessage) bool {
	metadata := messageMetadata(message)
	_, ok := metadata["offload_handle"]
	return ok && metadata["offload_handle"] != nil
}

// truncatePreservingOffloadMarker 截断保尾标记。
// Python: message_offloader.py:434 (_truncate_preserving_offload_marker)
func truncatePreservingOffloadMarker(content string, maxChars int) string {
	markerStart := strings.LastIndex(content, "[[OFFLOAD:")
	if markerStart < 0 {
		return content[:min(maxChars, len(content))]
	}
	marker := strings.TrimSpace(content[markerStart:])
	body := strings.TrimRight(content[:markerStart], " \t\n")
	markerSeparator := "\n...\n"
	bodyBudget := maxChars - len(marker) - len(markerSeparator)
	if bodyBudget <= 0 {
		return marker
	}
	if len(body) <= bodyBudget {
		return body + markerSeparator + marker
	}
	bodySeparator := "\n...\n"
	tailBudget := bodyBudget / 2
	headBudget := bodyBudget - len(bodySeparator) - tailBudget
	if headBudget <= 0 {
		return strings.TrimLeft(body[len(body)-bodyBudget:], " \t\n") + markerSeparator + marker
	}
	return strings.TrimRight(body[:headBudget], " \t\n") + bodySeparator +
		strings.TrimLeft(body[len(body)-tailBudget:], " \t\n") + markerSeparator + marker
}

// ruleCompressionRequestsOriginalOffload GIT_DIFF 压缩要求原文卸载判定。
// Python: message_offloader.py:456 (_rule_compression_requests_original_offload)
func ruleCompressionRequestsOriginalOffload(message llm_schema.BaseMessage) bool {
	metadata := messageMetadata(message)
	details, _ := metadata["rule_compression_details"].(map[string]any)
	shouldOffload, _ := details["should_offload_original"].(bool)
	typeName, _ := metadata["rule_compression_type"].(string)
	return typeName == "GIT_DIFF" && shouldOffload && !isOffloadMessage(message)
}

// shouldKeepFullRuleCompressedPreview 保留完整预览判定。
// Python: message_offloader.py:465 (_should_keep_full_rule_compressed_preview)
func (mo *MessageSummaryOffloader) shouldKeepFullRuleCompressedPreview(message llm_schema.BaseMessage) bool {
	return ruleCompressionRequestsOriginalOffload(message)
}

// contextOccupancyTokens 上下文占用 token（usage 优先）。
// Python: message_offloader.py:468
func (mo *MessageSummaryOffloader) contextOccupancyTokens(mc iface.ModelContext) int {
	messages, _ := mc.GetMessages(0, true)
	return mo.countMessagesTokens(mc, messages, true)
}

func (mo *MessageSummaryOffloader) ttlOccupancyTokenThreshold(mc iface.ModelContext) int {
	return support.ResolveRatioTokenThreshold(mo.contextMax(mc), mo.Config().TTLContextOccupancyRatio)
}

func (mo *MessageSummaryOffloader) addMessageTokenThreshold(mc iface.ModelContext) int {
	return support.ResolveRatioTokenThreshold(mo.contextMax(mc), mo.Config().AddMessageThresholdRatio)
}

func (mo *MessageSummaryOffloader) ttlMessageTokenThreshold(mc iface.ModelContext) int {
	return support.ResolveRatioTokenThreshold(mo.contextMax(mc), mo.Config().TTLMessageThresholdRatio)
}

func (mo *MessageSummaryOffloader) addMessageMaxChars(mc iface.ModelContext) int {
	return mo.characterBudget(mc, mo.Config().AddMessageThresholdRatio)
}

func (mo *MessageSummaryOffloader) ttlMessageMaxChars(mc iface.ModelContext) int {
	return mo.characterBudget(mc, mo.Config().TTLMessageThresholdRatio)
}

// characterBudget token 阈值反推字符阈值（×3 与 len//3 兜底互逆）。
// Python: message_offloader.py:486 (_character_budget)
func (mo *MessageSummaryOffloader) characterBudget(mc iface.ModelContext, ratio float64) int {
	return max(int(float64(mo.contextMax(mc))*3*ratio), 1)
}

func (mo *MessageSummaryOffloader) contextMax(mc iface.ModelContext) int {
	return support.ResolveContextMax(mc, "")
}

func (mo *MessageSummaryOffloader) countMessagesTokens(mc iface.ModelContext, messages []llm_schema.BaseMessage, usageAware bool) int {
	return support.CountMessagesTokens(messages, mc.TokenCounter(), "", mo.ProcessorType(), usageAware)
}

// headTailPreview 头尾预览。
// Python: message_offloader.py:508 (_head_tail_preview)
func (mo *MessageSummaryOffloader) headTailPreview(content string, description string) string {
	keepChars := offloadPreviewHeadTailChars
	if keepChars <= 0 {
		return "[" + description + "]"
	}
	if len(content) <= keepChars*2 {
		return content
	}
	return content[:keepChars] + "\n" + omitString + " [" + description + "] " + omitString + "\n" + content[len(content)-keepChars:]
}

func (mo *MessageSummaryOffloader) shouldTryRuleCompression() bool {
	return offloadStrategy != OffloadStrategyTruncateOnly
}

func (mo *MessageSummaryOffloader) shouldFallbackToPlainOffload() bool {
	return offloadStrategy != OffloadStrategyRuleOnly
}

func (mo *MessageSummaryOffloader) shouldTruncateRulePreview() bool {
	return offloadStrategy == OffloadStrategyRuleThenTruncate
}

// writeDebugLog 调试记录落盘（jsonl 追加）。
// Python: message_offloader.py:540 (_write_debug_log)
func (mo *MessageSummaryOffloader) writeDebugLog(mc iface.ModelContext, event string, payload map[string]any, message llm_schema.BaseMessage) {
	if !mo.Config().EnableDebugDump {
		return
	}
	logPath := mo.debugLogPath(mc)
	record := map[string]any{
		"event":      event,
		"session_id": safeContextValue(mc, "session_id"),
		"context_id": safeContextValue(mc, "context_id"),
	}
	if message != nil {
		record["role"] = messageRole(message)
		record["tool_call_id"] = toolCallIDOf(message)
		record["message_metadata"] = messageMetadata(message)
	}
	for key, value := range payload {
		record[key] = value
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			defer file.Close()
			data, err := json.Marshal(record)
			if err == nil {
				_, _ = file.Write(data)
				_, _ = file.WriteString("\n")
				return
			}
		}
	}
	logger.Warn(logComponent).Msg("[MessageSummaryOffloader] failed to write debug log")
}

func (mo *MessageSummaryOffloader) debugLogPath(mc iface.ModelContext) string {
	return filepath.Join(mo.debugLogDir(mc), messageOffloaderDebugLogFile)
}

// debugLogDir 调试目录解析（config > env > workspace 约定）。
// Python: message_offloader.py:576 (_debug_log_dir)
func (mo *MessageSummaryOffloader) debugLogDir(mc iface.ModelContext) string {
	if dumpDir := mo.Config().DebugDumpDir; dumpDir != "" {
		return mustAbs(mo.expandDebugDirTemplate(dumpDir, mc))
	}
	if envDir := os.Getenv(MessageOffloaderDebugLogDirEnv); envDir != "" {
		return mustAbs(mo.expandDebugDirTemplate(envDir, mc))
	}
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir != "" {
		return filepath.Join(workspaceDir, "context",
			safeContextValue(mc, "session_id")+"_context", "message_offloader_debug_logs")
	}
	return mustAbs(filepath.Join("context", "message_offloader_debug_logs"))
}

func (mo *MessageSummaryOffloader) expandDebugDirTemplate(path string, mc iface.ModelContext) string {
	if !strings.Contains(path, "{session_id}") && !strings.Contains(path, "{context_id}") {
		return path
	}
	path = strings.ReplaceAll(path, "{session_id}", safeFilenamePart(safeContextValue(mc, "session_id")))
	return strings.ReplaceAll(path, "{context_id}", safeFilenamePart(safeContextValue(mc, "context_id")))
}

// safeContextValue 上下文值兜底。
func safeContextValue(mc iface.ModelContext, methodName string) string {
	switch methodName {
	case "session_id":
		if v := mc.SessionID(); v != "" {
			return v
		}
	case "context_id":
		if v := mc.ContextID(); v != "" {
			return v
		}
	}
	return "unknown"
}

// safeFilenamePart 文件名安全化。
// Python: message_offloader.py:612 (_safe_filename_part)
func safeFilenamePart(value string) string {
	safe := regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(value, "-")
	safe = strings.Trim(safe, "-")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	if safe == "" {
		return "unknown"
	}
	return safe
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// replaceWindowMessages 窗口内按 context_message_id 替换。
// Python: message_offloader.py:628 (_replace_window_messages)
func replaceWindowMessages(cw *iface.ContextWindow, processedMessages []llm_schema.BaseMessage) {
	replacements := map[string]llm_schema.BaseMessage{}
	for _, message := range processedMessages {
		if id := contextMessageID(message); id != "" {
			replacements[id] = message
		}
	}
	for i, message := range cw.ContextMessages {
		if id := contextMessageID(message); id != "" {
			if replacement, ok := replacements[id]; ok {
				cw.ContextMessages[i] = replacement
			}
		}
	}
}

// contextMessageID 元数据消息 id。
// Python: message_offloader.py:643 (_message_id)
func contextMessageID(message llm_schema.BaseMessage) string {
	id, _ := messageMetadata(message)["context_message_id"].(string)
	return id
}

// newOffloadHandleAndPath 生成卸载句柄与路径。
// Python: message_offloader.py:646 (_new_offload_handle_and_path)
func (mo *MessageSummaryOffloader) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
	offloadHandle := strings.ReplaceAll(uuid.New().String(), "-", "")
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir == "" {
		return offloadHandle, ""
	}
	fileName := mo.ProcessorType() + "_" + offloadHandle + ".json"
	return offloadHandle, filepath.Join(
		workspaceDir, "context", mc.SessionID()+"_context", "offload", fileName,
	)
}

// isProtectedToolMessage 保护工具判定（name 或 name:pattern 形态）。
// Python: message_offloader.py:663 (_is_protected_tool_message)
func (mo *MessageSummaryOffloader) isProtectedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	toolMsg, ok := message.(*llm_schema.ToolMessage)
	if !ok {
		return false
	}
	toolName, toolArgs := resolveToolCallInfo(toolMsg, contextMessages)
	if toolName == "" {
		return false
	}
	for _, protected := range mo.Config().ProtectedToolNames {
		if !strings.Contains(protected, ":") {
			if toolName == protected {
				return true
			}
			continue
		}
		parts := strings.SplitN(protected, ":", 2)
		if toolName == parts[0] && matchPattern(toolArgs, parts[1]) {
			return true
		}
	}
	return false
}

// resolveToolCallInfo 反查 ToolMessage 对应 tool_call 的名称与参数。
func resolveToolCallInfo(toolMsg *llm_schema.ToolMessage, contextMessages []llm_schema.BaseMessage) (string, map[string]any) {
	for i := len(contextMessages) - 1; i >= 0; i-- {
		assistantMsg, ok := contextMessages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, toolCall := range assistantMsg.ToolCalls {
			if toolCall.ID == toolMsg.ToolCallID {
				return toolCall.Name, extractToolArgs(toolCall)
			}
		}
	}
	return "", nil
}

// extractToolArgs 参数 JSON 解析为 map。
// Python: message_offloader.py:686 (_extract_tool_args)
func extractToolArgs(toolCall *llm_schema.ToolCall) map[string]any {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(toolCall.Arguments), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// matchPattern 模式匹配任一字符串值（fnmatch 语义）。
// Python: message_offloader.py:707 (_match_pattern)
func matchPattern(args map[string]any, pattern string) bool {
	for _, value := range args {
		if text, ok := value.(string); ok && globMatch(pattern, text) {
			return true
		}
	}
	return false
}

// globMatch 简化 fnmatch（* 任意串、? 单字符）。
func globMatch(pattern, value string) bool {
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\*`, ".*")
	escaped = strings.ReplaceAll(escaped, `\?`, ".")
	matched, err := regexp.Match("^"+escaped+"$", []byte(value))
	return err == nil && matched
}

// ──────────────────────────── 消息助手 ────────────────────────────

// messageContent 取消息文本（MessageContent 解包，铁律）。
func messageContent(message llm_schema.BaseMessage) string {
	content := message.GetContent()
	if content.IsText() {
		return content.Text()
	}
	return ""
}

// messageRole 消息角色。
func messageRole(message llm_schema.BaseMessage) string { return message.GetRole().String() }

// toolCallIDOf 消息 tool_call_id。
func toolCallIDOf(message llm_schema.BaseMessage) string {
	if toolMsg, ok := message.(*llm_schema.ToolMessage); ok {
		return toolMsg.ToolCallID
	}
	return ""
}

// messageMetadata 消息元数据（nil 安全）。
func messageMetadata(message llm_schema.BaseMessage) map[string]any {
	if accessor, ok := message.(interface{ GetMetadata() map[string]any }); ok {
		return accessor.GetMetadata()
	}
	return map[string]any{}
}

// sameMessage 指针等价判定（Python `replacement is message`）。
func sameMessage(a, b llm_schema.BaseMessage) bool {
	type comparable interface {
		Comparable(llm_schema.BaseMessage) bool
	}
	return a == b
}

// cloneToolMessageWithContent 替换内容的 ToolMessage 复制。
// Python: model_copy(update={"content": ...})
func cloneToolMessageWithContent(message llm_schema.BaseMessage, content string) llm_schema.BaseMessage {
	if toolMsg, ok := message.(*llm_schema.ToolMessage); ok {
		return llm_schema.NewToolMessage(toolMsg.ToolCallID, content)
	}
	return message
}

// contextMessagesOf 上下文消息快照。
func contextMessagesOf(mc iface.ModelContext) []llm_schema.BaseMessage {
	messages, _ := mc.GetMessages(0, true)
	return messages
}

// ──────────────────────────── 能力面 ────────────────────────────

// WindowAccessAtProvider 窗口访问时间能力面（TTL 判定消费）。
// Python: context.last_context_window_access_at()/set_last_context_window_access_at()。
// 偏差 D-24.15-a6：访问时间落库由 SessionModelContext 实现随批补齐；
// 未实现该能力面的上下文 TTL 永不触发（等价 PY previous_access None 分支）。
type WindowAccessAtProvider interface {
	// LastWindowAccessAt 返回上次窗口访问时间戳与是否已记录
	LastWindowAccessAt() (float64, bool)
	// SetWindowAccessAt 写入本次访问时间戳
	SetWindowAccessAt(float64)
}

// 编译期兜底
var (
	_ iface.ContextProcessor = (*MessageSummaryOffloader)(nil)
	_                        = cecontext.CompressionRecallConfigProvider(nil)
	_                        = sessioninterfaces.SessionFacade(nil)
)

// newPipelineWithNow 以指定时间源重建管线（测试辅助）。
func newPipelineWithNow(now func() float64, config *MessageSummaryOffloaderConfig) *rulecompression.RuleCompressionPipeline {
	return rulecompression.NewRuleCompressionPipeline(nil, now, config.EnableDebugDump, config.DebugDumpDir)
}

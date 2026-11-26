// 本文件移植 forked 压缩器家族共享基座。
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/base.py（912 行）。
// 方法/步骤一比一；偏差登记见 spec「已知偏差」节。
package compressor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forked "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked"
	recall "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/recall"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	forkedsupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/support"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrefixCompactSpan 压缩跨度：窗口消息三分段。
// Python: base.py:61 (PrefixCompactSpan, @dataclass(frozen=True))
type PrefixCompactSpan struct {
	// PreservedPrefix 保留前缀
	PreservedPrefix []llm_schema.BaseMessage
	// MessagesToCompress 待压缩消息
	MessagesToCompress []llm_schema.BaseMessage
	// ProtectedTail 保护尾巴
	ProtectedTail []llm_schema.BaseMessage
}

// HasTarget 是否存在可压缩目标。
// Python: base.py:67 (PrefixCompactSpan.has_target)
func (s PrefixCompactSpan) HasTarget() bool {
	return len(s.MessagesToCompress) > 0
}

// PrefixCompactProcessorConfig 前缀压缩处理器配置。
// 实现 forked.ModelBindingConfig（编译锚见文件尾部），供模型热切换回填。
// Python: base.py:71 (PrefixCompactProcessorConfig)
type PrefixCompactProcessorConfig struct {
	// TriggerContextRatio 触发阈值（占上下文窗口比例）
	// Python: Field(default=0.4, gt=0.0, lt=1.0)
	TriggerContextRatio float64 `json:"trigger_context_ratio"`
	// MinTargetContextRatio 最小压缩目标比例
	// Python: Field(default=0.1, ge=0.0, lt=1.0)
	MinTargetContextRatio float64 `json:"min_target_context_ratio"`
	// Model 模型请求配置（可选）
	// Python: model: ModelRequestConfig | None
	Model *llm_schema.ModelRequestConfig `json:"model"`
	// ModelClient 模型客户端配置（可选）
	// Python: model_client: ModelClientConfig | None
	ModelClient *llm_schema.ModelClientConfig `json:"model_client"`
	// EnableCompressionDump 逐次压缩调用落盘开关（默认关，零开销）
	// Python: enable_compression_dump: bool = Field(default=False)
	EnableCompressionDump bool `json:"enable_compression_dump"`
	// CompressionDumpDir 落盘目录（空走环境变量/workspace 回退）
	// Python: compression_dump_dir: str | None
	CompressionDumpDir string `json:"compression_dump_dir"`
	// EnableKVCacheAffinity KV cache 亲和开关（由 rail 从 ReActAgent 配置派生）
	// Python: enable_kv_cache_affinity: bool = Field(default=False)
	EnableKVCacheAffinity bool `json:"enable_kv_cache_affinity"`
}

// NewPrefixCompactProcessorConfig 创建配置，字段使用 Python 默认值。
func NewPrefixCompactProcessorConfig() *PrefixCompactProcessorConfig {
	return &PrefixCompactProcessorConfig{
		TriggerContextRatio:   0.4,
		MinTargetContextRatio: 0.1,
	}
}

// Validate 校验配置（Python pydantic Field 约束等价）。
func (c *PrefixCompactProcessorConfig) Validate() error {
	if c.TriggerContextRatio <= 0 || c.TriggerContextRatio >= 1 {
		return fmt.Errorf("trigger_context_ratio 必须在 (0,1) 区间，当前值: %v", c.TriggerContextRatio)
	}
	if c.MinTargetContextRatio < 0 || c.MinTargetContextRatio >= 1 {
		return fmt.Errorf("min_target_context_ratio 必须在 [0,1) 区间，当前值: %v", c.MinTargetContextRatio)
	}
	return nil
}

// GetModel 返回配置携带的模型请求配置。
// Python: PrefixCompactProcessorConfig.model 属性
func (c *PrefixCompactProcessorConfig) GetModel() *llm_schema.ModelRequestConfig { return c.Model }

// GetModelClient 返回配置携带的模型客户端配置。
// Python: PrefixCompactProcessorConfig.model_client 属性
func (c *PrefixCompactProcessorConfig) GetModelClient() *llm_schema.ModelClientConfig {
	return c.ModelClient
}

// SetModel 写回模型请求配置。
// Python: model_validator 经 Pydantic 赋值；Go 显式写回（D-24.2-7 窄接口）。
func (c *PrefixCompactProcessorConfig) SetModel(m *llm_schema.ModelRequestConfig) { c.Model = m }

// SetModelClient 写回模型客户端配置。
func (c *PrefixCompactProcessorConfig) SetModelClient(mc *llm_schema.ModelClientConfig) {
	c.ModelClient = mc
}

// PrefixCompactProcessor 前缀压缩处理器基座（forked 压缩器家族共享）。
// 类级属性（memory_block_open 等）以字段承载，子类构造器可覆盖。
// Python: base.py:143 (PrefixCompactProcessor)
type PrefixCompactProcessor struct {
	*forked.ContextProcessor
	// MemoryBlockOpen 记忆块开启标记
	// Python: memory_block_open: str = "<memory_block>"
	MemoryBlockOpen string
	// MemoryBlockClose 记忆块关闭标记
	// Python: memory_block_close: str = "</memory_block>"
	MemoryBlockClose string
	// DefaultPrompt 默认压缩提示词（子类覆盖）
	// Python: default_prompt: str = ""
	DefaultPrompt string
	// ProcessorLabel 处理器标签（= processor_type）
	// Python: processor_label: str = "PrefixCompactProcessor"
	ProcessorLabel string
	// MemoryBlockMeaning 记忆块语义说明
	// Python: memory_block_meaning
	MemoryBlockMeaning string
	// MemoryBlockConflictPolicy 记忆块冲突策略说明
	// Python: memory_block_conflict_policy
	MemoryBlockConflictPolicy string
	// ReinjectBuilderNames 具名回注 builder 列表（nil=全默认）
	// Python: reinject_builder_names: list[str] | None
	ReinjectBuilderNames []string

	// compressionExecutor 压缩执行器（model 非空时初始化）
	// Python: self._compression_executor
	compressionExecutor *cesupport.CompressionExecutor

	// buildSpanHook 分段钩子（抽象方法 _build_span 的子类覆盖点）。
	// Go embedding 静态派发陷阱规避：基座方法经此钩子调用子类实现
	// （构造期注入函数指针），与 Python 虚方法语义等价。
	buildSpanHook func(messages []llm_schema.BaseMessage) PrefixCompactSpan
}

// NewPrefixCompactProcessor 创建基座处理器。
// Python: base.py:154 (PrefixCompactProcessor.__init__)
func NewPrefixCompactProcessor(config *PrefixCompactProcessorConfig) *PrefixCompactProcessor {
	p := &PrefixCompactProcessor{
		ContextProcessor:          forked.NewContextProcessor(config),
		MemoryBlockOpen:           "<memory_block>",
		MemoryBlockClose:          "</memory_block>",
		DefaultPrompt:             "",
		ProcessorLabel:            "PrefixCompactProcessor",
		MemoryBlockMeaning:        "This is compressed context from earlier messages. It is not a new user request.",
		MemoryBlockConflictPolicy: "Newer raw messages and the latest explicit user intent override this compressed context.",
	}
	if config.ModelClient != nil && config.Model != nil {
		// Python: self._model = Model(model_client, model)
		if m, err := llm.NewModel(config.ModelClient, config.Model); err == nil {
			p.ContextProcessor.RebindModelReference(m, config.Model, config.ModelClient)
			// Python: self._compression_executor = CompressionExecutor(self._model)
			p.compressionExecutor = cesupport.NewCompressionExecutor(m)
		}
	}
	// 默认分段钩子：基座不可实例化使用，返回空跨度（等价 Python NotImplementedError
	// 的"无目标"面）；子类构造器覆盖（见 buildSpanHook 注释）。
	p.buildSpanHook = func([]llm_schema.BaseMessage) PrefixCompactSpan { return PrefixCompactSpan{} }
	return p
}

// ──────────────────────────── 常量 ────────────────────────────

// contextOverflowRetryBudgetRatios 上下文溢出重试预算比例序列。
// Python: base.py:49 (_CONTEXT_OVERFLOW_RETRY_BUDGET_RATIOS)
var contextOverflowRetryBudgetRatios = []float64{0.85, 0.65, 0.5}

// transientCompressionErrorKinds 可瞬态重试的错误分类集合。
// Python: base.py:50 (_TRANSIENT_COMPRESSION_ERROR_KINDS)
var transientCompressionErrorKinds = map[cesupport.CompressionErrorKind]bool{
	cesupport.CompressionErrorKindRateLimit:      true,
	cesupport.CompressionErrorKindTimeout:        true,
	cesupport.CompressionErrorKindServerUnstable: true,
	cesupport.CompressionErrorKindUnknown:        true,
}

const (
	// transientCompressionMaxRetries 瞬态错误最大重试次数
	// Python: base.py:56 (_TRANSIENT_COMPRESSION_MAX_RETRIES)
	transientCompressionMaxRetries = 2
	// transientCompressionRetryBaseDelaySeconds 瞬态重试基础退避秒数
	// Python: base.py:57 (_TRANSIENT_COMPRESSION_RETRY_BASE_DELAY_SECONDS)
	transientCompressionRetryBaseDelaySeconds = 0.05
)

// stateSnapshotPattern 摘要快照抽取正则
// Python: base.py:785 re.search(r"<state_snapshot>\s*(.*?)\s*</state_snapshot>", ..., DOTALL|IGNORECASE)
var stateSnapshotPattern = regexp.MustCompile(`(?is)<state_snapshot>\s*(.*?)\s*</state_snapshot>`)

// ──────────────────────────── 全局变量 ────────────────────────────

// PrefixCompactProcessorConfig 实现 ModelBindingConfig 的编译期锚（D-24.2-7）。
var _ forked.ModelBindingConfig = (*PrefixCompactProcessorConfig)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// RejectLegacyRecallConfigFields 拒绝旧版 recall 配置字段（已迁 ContextEngineConfig.compression_recall_config）。
// Python: base.py:87 (PrefixCompactProcessorConfig._reject_legacy_recall_config, model_validator)
func RejectLegacyRecallConfigFields(value map[string]any) error {
	if value == nil {
		return nil
	}
	legacyFields := []string{
		"enable_recall",
		"recall_chunk_size_tokens",
		"recall_chunk_overlap_tokens",
	}
	var configured []string
	for _, field := range legacyFields {
		if _, ok := value[field]; ok {
			configured = append(configured, field)
		}
	}
	if len(configured) > 0 {
		// Python: sorted(configured_fields)；legacyFields 本身按字典序排列
		return fmt.Errorf("%s moved to ContextEngineConfig.compression_recall_config", strings.Join(configured, ", "))
	}
	return nil
}

// AdjustKeepRecentForToolBoundaries 把 keep_recent 起点外扩到工具配对边界（不拆散 tool_call/ToolMessage）。
// Python: base.py:101 (adjust_keep_recent_for_tool_boundaries)
func AdjustKeepRecentForToolBoundaries(messages []llm_schema.BaseMessage, keepRecent int) int {
	if keepRecent <= 0 || len(messages) == 0 {
		return max(keepRecent, 0)
	}

	start := max(len(messages)-keepRecent, 0)
	for {
		protectedToolResultIDs := toolResultIDs(messages[start:])
		if len(protectedToolResultIDs) == 0 {
			break
		}

		adjustedStart := start
		for index, message := range messages[:start] {
			am, ok := message.(*llm_schema.AssistantMessage)
			if !ok {
				continue
			}
			toolIDs := messageToolCallIDs(am)
			if intersects(toolIDs, protectedToolResultIDs) {
				adjustedStart = min(adjustedStart, index)
			}
		}

		if adjustedStart == start {
			break
		}
		start = adjustedStart
	}

	return len(messages) - start
}

// FindLastRealUserMessageIndex 返回最后一条真实用户消息的下标（无则 -1）。
// Python: base.py:126 (find_last_real_user_message_index)
func FindLastRealUserMessageIndex(messages []llm_schema.BaseMessage) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if isRealUserMessage(messages[index]) {
			return index
		}
	}
	return -1
}

// ProcessorType 处理器类型标识。
// Python: processor_label 类属性（元类注入 __processor_type，D-24.2-1）。
func (p *PrefixCompactProcessor) ProcessorType() string { return p.ProcessorLabel }

// RebindModel 把缓存的压缩执行器迁到当前 agent 模型。
// Python: base.py:164 (PrefixCompactProcessor.rebind_model)
func (p *PrefixCompactProcessor) RebindModel(
	model *llm.Model,
	modelConfig *llm_schema.ModelRequestConfig,
	modelClientConfig *llm_schema.ModelClientConfig,
) bool {
	changed := p.ContextProcessor.RebindModelReference(model, modelConfig, modelClientConfig)
	if p.ContextProcessor.Model() != nil && (changed || p.compressionExecutor == nil) {
		p.compressionExecutor = cesupport.NewCompressionExecutor(p.ContextProcessor.Model())
	}
	return changed
}

// TriggerGetContextWindow 窗口物化触发判定（被动触发，超阈值才压缩）。
// Python: base.py:181 (trigger_get_context_window)
func (p *PrefixCompactProcessor) TriggerGetContextWindow(
	ctx context.Context,
	mc iface.ModelContext,
	cw iface.ContextWindow,
	opts ...iface.Option,
) (bool, error) {
	_ = ctx
	opt := p.newProcessorOption(opts)
	if p.compressionExecutor == nil {
		return false, nil
	}

	totalTokens := p.countContextWindowTokens(cw, mc)
	contextMax := p.resolveContextMax(mc, opt.ModelName)
	absoluteThreshold := p.resolveTriggerTokenLimit(contextMax)
	if totalTokens < absoluteThreshold {
		p.writeContextDebug(mc, "threshold_check", map[string]any{
			"phase": "get_context_window", "total_tokens": totalTokens,
			"context_max": contextMax, "threshold": absoluteThreshold,
			"hit": false, "reason": "below_threshold",
		})
		return false, nil
	}

	span := p.buildSpanHook(cw.ContextMessages)
	if !span.HasTarget() {
		p.writeContextDebug(mc, "threshold_check", map[string]any{
			"phase": "get_context_window", "total_tokens": totalTokens,
			"context_max": contextMax, "threshold": absoluteThreshold,
			"hit": false, "reason": "no_compressible_span",
		})
		return false, nil
	}
	targetTokens := p.countMessagesTokens(span.MessagesToCompress, mc, false)
	minTargetTokens := int(float64(contextMax) * p.minTargetContextRatio())
	if targetTokens < minTargetTokens {
		logger.Info(logComponent).
			Str("processor", p.ProcessorType()).
			Int("target_tokens", targetTokens).
			Int("min_target", minTargetTokens).
			Msgf("[%s skipped] compression target tokens below min target", p.ProcessorType())
		p.writeContextDebug(mc, "threshold_check", map[string]any{
			"phase": "get_context_window", "total_tokens": totalTokens,
			"context_max": contextMax, "threshold": absoluteThreshold,
			"hit": false, "reason": "target_below_min",
			"target_tokens": targetTokens, "min_target_tokens": minTargetTokens,
		})
		return false, nil
	}

	logger.Info(logComponent).
		Str("processor", p.ProcessorType()).
		Int("total_tokens", totalTokens).
		Int("threshold", absoluteThreshold).
		Int("context_max", contextMax).
		Msgf("[%s triggered] context tokens reached threshold", p.ProcessorType())
	p.writeContextDebug(mc, "threshold_check", map[string]any{
		"phase": "get_context_window", "total_tokens": totalTokens,
		"context_max": contextMax, "threshold": absoluteThreshold,
		"hit": true, "reason": "reached_threshold",
		"target_tokens": targetTokens,
	})
	return true, nil
}

// OnGetContextWindow 执行压缩并回写窗口/上下文。
// Python: base.py:255 (on_get_context_window)
func (p *PrefixCompactProcessor) OnGetContextWindow(
	ctx context.Context,
	mc iface.ModelContext,
	cw iface.ContextWindow,
	opts ...iface.Option,
) (*iface.ContextEvent, iface.ContextWindow, error) {
	opt := p.newProcessorOption(opts)
	if p.compressionExecutor == nil {
		return nil, cw, nil
	}

	p.ResetCompressionUsage()
	originalMessages := append([]llm_schema.BaseMessage(nil), cw.ContextMessages...)
	span := p.buildSpanHook(originalMessages)
	if !span.HasTarget() {
		return nil, cw, nil
	}
	p.writeContextDebug(mc, "span_built", map[string]any{
		"phase":                "get_context_window",
		"preserved_prefix":     spanSummary(span.PreservedPrefix),
		"messages_to_compress": spanSummary(span.MessagesToCompress),
		"protected_tail":       spanSummary(span.ProtectedTail),
	})

	prompt := p.buildPrompt(span, optString(opt.Extra, "preserve_instruction"))
	invokeResult := p.invokeCompressionWithRetries(ctx, mc, &cw, span, prompt)
	if invokeResult == nil {
		return nil, cw, nil
	}
	response, span, request := invokeResult.response, invokeResult.span, invokeResult.request
	p.RecordCompressionUsage(response.Response)

	summary := extractStateSnapshotOrRaw(response.Content())
	if summary == "" {
		return nil, cw, nil
	}

	archive := p.archiveCompressedMessages(mc, originalMessages, span)
	memoryID := ""
	if archive != nil {
		memoryID = archive.MemoryID
	}
	memoryMessage := llm_schema.NewUserMessage(p.wrapMemoryBlock(summary, memoryID))
	newMessages := p.buildCompactedMessages(mc, originalMessages, span, memoryMessage)
	if !p.hasCompressionBenefit(mc, originalMessages, newMessages) {
		if archive != nil {
			p.deleteCompressionArchive(archive)
		}
		return nil, cw, nil
	}

	cw.ContextMessages = newMessages
	mc.SetMessages(newMessages, true)

	p.dumpCompressionArtifact(mc, &cw, originalMessages, span, prompt, request, response.Content(), summary, newMessages)

	return &iface.ContextEvent{
		EventType:        p.ProcessorType(),
		MessagesToModify: indexRange(len(originalMessages)),
		CompactSummary:   memoryMessage.GetContent().Text(),
		CompressionUsage: p.CurrentCompressionUsage(),
	}, cw, nil
}

// compressionInvokeResult invokeCompressionWithRetries 的成功产物三元组。
// Python: base.py:353 tuple[CompressionResult, PrefixCompactSpan, CompressionRequest]
type compressionInvokeResult struct {
	response *cesupport.CompressionResult
	span     PrefixCompactSpan
	request  *cesupport.CompressionRequest
}

// invokeCompressionWithRetries 带溢出预算收缩与瞬态退避的压缩调用循环。
// Python: base.py:358 (_invoke_compression_with_retries)
func (p *PrefixCompactProcessor) invokeCompressionWithRetries(
	ctx context.Context,
	mc iface.ModelContext,
	cw *iface.ContextWindow,
	span PrefixCompactSpan,
	prompt string,
) *compressionInvokeResult {
	if p.compressionExecutor == nil {
		return nil
	}

	overflowRetryIndex := 0
	transientRetryCount := 0
	for {
		request := cesupport.NewCompressionRequestFromContextWindow(
			prompt, *cw, len(span.ProtectedTail), nil,
		)
		response, err := p.invokeCompression(ctx, mc, request)
		if err == nil {
			return &compressionInvokeResult{response: response, span: span, request: request}
		}
		exc, isCompressionErr := err.(*cesupport.CompressionError)
		if !isCompressionErr {
			// Python: except Exception 分支（base.py:487）
			logger.Warn(logComponent).
				Str("processor", p.ProcessorType()).
				Err(err).
				Msg("compression failed")
			return nil
		}

		if exc.IsContextOverflow() {
			// Python: base.py:375-444 context-overflow 分支
			if overflowRetryIndex >= len(contextOverflowRetryBudgetRatios) {
				logger.Warn(logComponent).
					Str("processor", p.ProcessorType()).
					Err(err).
					Msg("compression failed after context-overflow retries")
				p.writeContextDebug(mc, "compression_retry", map[string]any{
					"attempt": overflowRetryIndex + 1, "error_type": "context_overflow",
					"error_message": exc.Message, "retried": false, "reason": "retry_budget_exhausted",
				})
				return nil
			}
			budgetRatio := contextOverflowRetryBudgetRatios[overflowRetryIndex]
			nextSpan := p.buildContextOverflowRetrySpan(mc, cw, span, prompt, budgetRatio)
			overflowRetryIndex++
			if nextSpan == nil || !nextSpan.HasTarget() {
				logger.Warn(logComponent).
					Str("processor", p.ProcessorType()).
					Err(err).
					Msg("compression context-overflow retry stopped: no smaller compressible span")
				p.writeContextDebug(mc, "compression_retry", map[string]any{
					"attempt": overflowRetryIndex, "error_type": "context_overflow",
					"error_message": exc.Message, "retried": false, "reason": "no_smaller_span",
				})
				return nil
			}
			logger.Warn(logComponent).
				Str("processor", p.ProcessorType()).
				Int("attempt", overflowRetryIndex).
				Float64("budget_ratio", budgetRatio).
				Int("exclude_recent_before", len(span.ProtectedTail)).
				Int("exclude_recent_after", len(nextSpan.ProtectedTail)).
				Msg("compression context_overflow retry")
			p.writeContextDebug(mc, "compression_retry", map[string]any{
				"attempt": overflowRetryIndex, "error_type": "context_overflow",
				"error_message": exc.Message, "retried": true, "reason": "budget_ratio_shrink",
				"budget_ratio": budgetRatio,
			})
			span = *nextSpan
			continue
		}

		if transientCompressionErrorKinds[exc.Kind] && transientRetryCount < transientCompressionMaxRetries {
			// Python: base.py:446-470 transient 分支（0.05*2^n 退避）
			transientRetryCount++
			delay := transientCompressionRetryBaseDelaySeconds * float64(int64(1)<<(transientRetryCount-1))
			logger.Warn(logComponent).
				Str("processor", p.ProcessorType()).
				Int("attempt", transientRetryCount).
				Str("kind", string(exc.Kind)).
				Float64("delay_seconds", delay).
				Msg("compression transient retry")
			p.writeContextDebug(mc, "compression_retry", map[string]any{
				"attempt": transientRetryCount, "error_type": "transient:" + string(exc.Kind),
				"error_message": exc.Message, "retried": true, "reason": "transient_backoff",
				"delay_seconds": delay,
			})
			if delay > 0 {
				time.Sleep(time.Duration(delay * float64(time.Second)))
			}
			continue
		}

		logger.Warn(logComponent).
			Str("processor", p.ProcessorType()).
			Str("kind", string(exc.Kind)).
			Err(err).
			Msg("compression failed")
		return nil
	}
}

// invokeCompression 单次压缩调用（affinity kwargs + 清理收尾）。
// Python: base.py:477 (_invoke_compression)
func (p *PrefixCompactProcessor) invokeCompression(
	ctx context.Context,
	mc iface.ModelContext,
	request *cesupport.CompressionRequest,
) (*cesupport.CompressionResult, error) {
	if p.compressionExecutor == nil {
		// Python: raise RuntimeError；Go 侧等价为分类错误上抛由重试循环吞并
		return nil, &cesupport.CompressionError{Kind: cesupport.CompressionErrorKindUnknown, Message: "compression executor is not initialized"}
	}

	invokeKwargs, identity := p.buildCompressorAffinity(mc)
	result, err := p.compressionExecutor.Invoke(ctx, request, invokeKwargs)
	if identity != nil {
		p.cleanupCompressorCache(identity)
	}
	return result, err
}

// buildCompressorAffinity 构造压缩器专用 KV cache 亲和 kwargs 与身份。
// Python: base.py:494 (_build_compressor_affinity)
func (p *PrefixCompactProcessor) buildCompressorAffinity(mc iface.ModelContext) (map[string]any, *kv_cache.KVCacheIdentity) {
	if !p.config().EnableKVCacheAffinity {
		return nil, nil
	}
	model := p.ContextProcessor.Model()
	if model == nil {
		return nil, nil
	}

	ownerCacheID, _ := kv_cache.ResolveSessionLineage(mc.GetSessionRef())
	ownerCacheID = strings.TrimSpace(firstNonEmpty(ownerCacheID, mc.SessionID()))
	identity, err := kv_cache.ContextCompressorCacheIdentity(ownerCacheID, p.ProcessorType())
	if err != nil {
		return nil, nil
	}
	invokeKwargs := model.BuildKVCacheAffinityInvokeKwargs(mc.GetSessionRef(), identity.CacheID, identity.ParentCacheID, 0)
	if len(invokeKwargs) == 0 {
		return nil, nil
	}
	return invokeKwargs, &identity
}

// cleanupCompressorCache 逐出压缩器占用的 KV cache（best-effort）。
// Python: base.py:516 (_cleanup_compressor_cache) → model.evict_kvc
func (p *PrefixCompactProcessor) cleanupCompressorCache(identity *kv_cache.KVCacheIdentity) {
	model := p.ContextProcessor.Model()
	if model == nil {
		return
	}
	ok := model.EvictKvc(context.Background(), &model_clients.KVCacheActionParams{
		SessionID:       identity.CacheID,
		ParentSessionID: identity.ParentCacheID,
		Target:          "session",
	})
	if !ok {
		logger.Warn(logComponent).
			Str("processor", p.ProcessorType()).
			Str("cache_id", identity.CacheID).
			Msg("failed to evict compressor KV cache")
	}
}

// buildContextOverflowRetrySpan 按预算比例收缩压缩跨度（挤出保护尾巴）。
// Python: base.py:545 (_build_context_overflow_retry_span)
func (p *PrefixCompactProcessor) buildContextOverflowRetrySpan(
	mc iface.ModelContext,
	cw *iface.ContextWindow,
	span PrefixCompactSpan,
	prompt string,
	budgetRatio float64,
) *PrefixCompactSpan {
	if len(span.MessagesToCompress) == 0 {
		return nil
	}

	contextMax := p.resolveContextMax(mc, "")
	budgetTokens := max(int(float64(contextMax)*budgetRatio), 1)
	fixedMessages := append(append([]llm_schema.BaseMessage(nil), cw.SystemMessages...), span.PreservedPrefix...)
	fixedMessages = append(fixedMessages, llm_schema.NewUserMessage(prompt))
	fixedTokens := p.countMessagesTokens(fixedMessages, mc, false)
	fixedTokens += p.countToolsTokens(cw.Tools, mc)
	targetTokens := budgetTokens - fixedTokens
	if targetTokens <= 0 {
		return nil
	}

	fitCount := 0
	runningTokens := 0
	for _, message := range span.MessagesToCompress {
		messageTokens := p.countMessagesTokens([]llm_schema.BaseMessage{message}, mc, false)
		if fitCount > 0 && runningTokens+messageTokens > targetTokens {
			break
		}
		if runningTokens+messageTokens > targetTokens {
			break
		}
		runningTokens += messageTokens
		fitCount++
	}

	if fitCount >= len(span.MessagesToCompress) {
		fitCount = max(len(span.MessagesToCompress)-1, 0)
	}
	if fitCount <= 0 && len(span.MessagesToCompress) > 1 {
		fitCount = 1
	}
	if fitCount <= 0 {
		return nil
	}

	movedToTail := span.MessagesToCompress[fitCount:]
	if len(movedToTail) == 0 {
		return nil
	}
	protectedTail := append(append([]llm_schema.BaseMessage(nil), movedToTail...), span.ProtectedTail...)
	return &PrefixCompactSpan{
		PreservedPrefix:    append([]llm_schema.BaseMessage(nil), span.PreservedPrefix...),
		MessagesToCompress: append([]llm_schema.BaseMessage(nil), span.MessagesToCompress[:fitCount]...),
		ProtectedTail:      protectedTail,
	}
}

// dumpCompressionArtifact 落盘压缩调用产物（追踪不得打断主链路）。
// Python: base.py:595 (_dump_compression_artifact)
func (p *PrefixCompactProcessor) dumpCompressionArtifact(
	mc iface.ModelContext,
	cw *iface.ContextWindow,
	originalMessages []llm_schema.BaseMessage,
	span PrefixCompactSpan,
	prompt string,
	request *cesupport.CompressionRequest,
	responseContent string,
	summary string,
	newMessages []llm_schema.BaseMessage,
) {
	cfg := p.config()
	if !cfg.EnableCompressionDump {
		return
	}
	cesupport.DumpCompressionArtifact(&cesupport.CompressionDumpInput{
		Processor:        p,
		Context:          mc,
		Config:           cfg,
		ProcessorType:    p.ProcessorType(),
		OriginalMessages: originalMessages,
		Span: cesupport.CompressionSpanShape{
			PreservedPrefix:    span.PreservedPrefix,
			MessagesToCompress: span.MessagesToCompress,
			ProtectedTail:      span.ProtectedTail,
		},
		Prompt:          prompt,
		Request:         request,
		ResponseContent: responseContent,
		Summary:         summary,
		NewMessages:     newMessages,
		Usage:           p.CurrentCompressionUsage(),
	})
}

// buildSpan 分段抽象方法（子类覆盖点：经 buildSpanHook 派发）。
// Python: base.py:638 (_build_span, raise NotImplementedError)
func (p *PrefixCompactProcessor) buildSpan(messages []llm_schema.BaseMessage) PrefixCompactSpan {
	return p.buildSpanHook(messages)
}

// buildTraceContext 构建调试/落盘的追踪上下文。
// Python: base.py:641 (_build_trace_context)
func (p *PrefixCompactProcessor) buildTraceContext(mc iface.ModelContext) map[string]any {
	traceContext := map[string]any{
		"call_site":             "compressor",
		"compression_processor": p.ProcessorType(),
	}
	if sid := mc.SessionID(); sid != "" {
		traceContext["session_id"] = sid
	}
	if cid := mc.ContextID(); cid != "" {
		traceContext["context_id"] = cid
	}
	if sess := mc.GetSessionRef(); sess != nil {
		if _, ok := traceContext["session_id"]; !ok {
			traceContext["session_id"] = sess.GetSessionID()
		}
		if dumper, ok := sess.(interface{ DumpState() map[string]any }); ok {
			traceContext["session_state"] = dumper.DumpState()
			traceContext["session_state_source"] = "session.dump_state"
		}
		// Python: hasattr(session,'get_state') → session_global_state；
		// Go SessionFacade.GetState 需类型化 StateKey 且 Python 侧是无参全局取态，
		// 语义不可一一对应——偏差 D-24.15-a4：该字段不填充。
	}
	return traceContext
}

// resolveTriggerTokenLimit 由比例阈值解析绝对 token 阈值。
// Python: base.py:679 (_resolve_trigger_token_limit)
func (p *PrefixCompactProcessor) resolveTriggerTokenLimit(contextMax int) int {
	return cesupport.ResolveRatioTokenThreshold(contextMax, p.config().TriggerContextRatio)
}

// buildPrompt 组装压缩提示词（可追加用户保留指令）。
// Python: base.py:682 (_build_prompt)
func (p *PrefixCompactProcessor) buildPrompt(span PrefixCompactSpan, preserveInstruction string) string {
	_ = span
	instruction := strings.TrimSpace(preserveInstruction)
	if instruction == "" {
		return p.DefaultPrompt
	}
	return strings.TrimRight(p.DefaultPrompt, " \t\n") + "\n\n" +
		"User preservation instruction:\n" +
		"The user specifically asked this compaction to preserve the following information when relevant:\n" +
		instruction + "\n\n" +
		"Treat this as a preservation priority for the summary, not as a new task to execute.\n" +
		"Do not invent details. Preserve only information supported by the conversation."
}

// buildCompactedMessages 组装压缩后消息（含状态回注）。
// Python: base.py:696 (_build_compacted_messages)
func (p *PrefixCompactProcessor) buildCompactedMessages(
	mc iface.ModelContext,
	originalMessages []llm_schema.BaseMessage,
	span PrefixCompactSpan,
	memoryMessage *llm_schema.UserMessage,
) []llm_schema.BaseMessage {
	memoryMsg := llm_schema.BaseMessage(memoryMessage)
	newMessages := append(append([]llm_schema.BaseMessage(nil), span.PreservedPrefix...), memoryMsg)
	newMessages = append(newMessages, span.ProtectedTail...)
	reinjectedMessage := cesupport.BuildCompressorReinjectedStateMessage(
		originalMessages, newMessages, mc, p.config(), p.ReinjectBuilderNames, nil,
	)
	if reinjectedMessage != nil {
		result := append(append([]llm_schema.BaseMessage(nil), span.PreservedPrefix...), memoryMsg, reinjectedMessage)
		return append(result, span.ProtectedTail...)
	}
	return newMessages
}

// archiveCompressedMessages 归档被压缩消息（recall 配置开启时）。
// Python: base.py:714 (_archive_compressed_messages)；asyncio.to_thread → Go 同步调用（偏差 D-24.15-a3）。
func (p *PrefixCompactProcessor) archiveCompressedMessages(
	mc iface.ModelContext,
	originalMessages []llm_schema.BaseMessage,
	span PrefixCompactSpan,
) *recall.CompressionArchive {
	var recallConfig *ceschema.CompressionRecallConfig
	if provider, ok := mc.(cecontext.CompressionRecallConfigProvider); ok {
		recallConfig = provider.CompressionRecallConfig()
	}
	if recallConfig == nil || !recallConfig.Enabled {
		return nil
	}

	archive, err := recall.ArchiveCompressionMessages(
		mc, p.ProcessorType(),
		originalMessages,
		append([]llm_schema.BaseMessage(nil), span.MessagesToCompress...),
		append([]llm_schema.BaseMessage(nil), span.PreservedPrefix...),
		recallConfig.ChunkSizeTokens,
		recallConfig.ChunkOverlapTokens,
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("processor", p.ProcessorType()).
			Err(err).
			Msg("compression recall archive failed; continuing without a recall marker")
		return nil
	}
	return archive
}

// deleteCompressionArchive 回滚归档（压缩无收益时）。
// Python: base.py:751 (_delete_compression_archive)
func (p *PrefixCompactProcessor) deleteCompressionArchive(archive *recall.CompressionArchive) {
	if err := recall.DeleteCompressionArchive(archive); err != nil {
		logger.Warn(logComponent).
			Str("processor", p.ProcessorType()).
			Err(err).
			Msg("compression recall archive rollback failed")
	}
}

// wrapMemoryBlock 用记忆块包裹摘要（含可选召回标记）。
// Python: base.py:766 (_wrap_memory_block)
func (p *PrefixCompactProcessor) wrapMemoryBlock(summary string, memoryID string) string {
	recallMarker := ""
	if memoryID != "" {
		recallMarker = fmt.Sprintf("\n[[COMPRESSION_RECALL: id=%s]]", memoryID)
	}
	return p.MemoryBlockOpen + "\n" +
		"<meaning>\n" + p.MemoryBlockMeaning + "\n" +
		"</meaning>\n" +
		"<conflict_policy>\n" + p.MemoryBlockConflictPolicy + "\n" +
		"</conflict_policy>\n" +
		"<summary>\n" + summary + "\n" +
		"</summary>" + recallMarker + "\n" +
		p.MemoryBlockClose
}

// hasCompressionBenefit 压缩后 token 是否下降。
// Python: base.py:793 (_has_compression_benefit)
func (p *PrefixCompactProcessor) hasCompressionBenefit(
	mc iface.ModelContext,
	originalMessages []llm_schema.BaseMessage,
	newMessages []llm_schema.BaseMessage,
) bool {
	originalTokens := p.countMessagesTokens(originalMessages, mc, false)
	newTokens := p.countMessagesTokens(newMessages, mc, false)
	return originalTokens <= 0 || newTokens < originalTokens
}

// countContextWindowTokens 计数窗口总 token（usage 优先）。
// Python: base.py:803 (_count_context_window_tokens)
func (p *PrefixCompactProcessor) countContextWindowTokens(cw iface.ContextWindow, mc iface.ModelContext) int {
	messages := append(append([]llm_schema.BaseMessage(nil), cw.SystemMessages...), cw.ContextMessages...)
	if usageTokens, ok := cesupport.CountUsageTokensWithTail(messages); ok {
		// 模型上报的 usage 已包含该基线时的 system prompt 与 tools
		return usageTokens
	}
	return p.countMessagesTokens(messages, mc, false) + p.countToolsTokens(cw.Tools, mc)
}

// countToolsTokens 计数工具定义 token。
// Python: base.py:815 (_count_tools_tokens)
func (p *PrefixCompactProcessor) countToolsTokens(tools []schema.ToolInfoInterface, mc iface.ModelContext) int {
	counter := mc.TokenCounter()
	if counter != nil {
		if n, err := counter.CountTools(tools, ""); err == nil {
			return n
		}
		logger.Warn(logComponent).
			Str("processor", p.ProcessorType()).
			Msg("tool token counter failed")
	}
	total := 0
	for _, t := range tools {
		total += estimateTextTokens(serializeTool(t))
	}
	return total
}

// countMessagesTokens 计数消息 token。
// Python: base.py:824 (_count_messages_tokens)
func (p *PrefixCompactProcessor) countMessagesTokens(
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	usageAware bool,
) int {
	return cesupport.CountMessagesTokens(messages, mc.TokenCounter(), "", p.ProcessorType(), usageAware)
}

// CountMessagesTokens 诊断/扩展点用的消息 token 计数。
// Python: base.py:838 (count_messages_tokens)
func (p *PrefixCompactProcessor) CountMessagesTokens(messages []llm_schema.BaseMessage, mc iface.ModelContext) int {
	return p.countMessagesTokens(messages, mc, false)
}

// resolveContextMax 解析上下文容量（实例方法）。
// Python: base.py:843 (_resolve_context_max, @staticmethod)
func (p *PrefixCompactProcessor) resolveContextMax(mc iface.ModelContext, modelName string) int {
	return cesupport.ResolveContextMax(mc, modelName)
}

// ResolveContextMax 诊断/扩展点用的容量解析（静态等价）。
// Python: base.py:847 (resolve_context_max)
func (p *PrefixCompactProcessor) ResolveContextMax(mc iface.ModelContext, modelName string) int {
	return cesupport.ResolveContextMax(mc, modelName)
}

// LoadState 恢复状态（基座无状态）。
// Python: base.py:883 (load_state)
func (p *PrefixCompactProcessor) LoadState(map[string]any) {}

// SaveState 保存状态（基座无状态）。
// Python: base.py:886 (save_state)
func (p *PrefixCompactProcessor) SaveState() map[string]any { return map[string]any{} }

// ResolveContextMaxForDump dump 窄接口：等价 processor.resolve_context_max(context, {})。
// Python: compression_dump.py 经 processor.resolve_context_max 访问（D-24.15-b1 窄接口缝）。
func (p *PrefixCompactProcessor) ResolveContextMaxForDump(mc cesupport.DumpContextIdentifiers, modelName string) int {
	return cesupport.ResolveContextMax(mc, modelName)
}

// CountMessagesTokensForDump dump 窄接口：等价 processor.count_messages_tokens(messages, context)。
func (p *PrefixCompactProcessor) CountMessagesTokensForDump(mc cesupport.DumpContextIdentifiers, messages []llm_schema.BaseMessage) int {
	var counter token.TokenCounter
	if tc, ok := mc.(interface{ TokenCounter() token.TokenCounter }); ok {
		counter = tc.TokenCounter()
	}
	return cesupport.CountMessagesTokens(messages, counter, "", p.ProcessorType(), false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// config 取强类型配置。
func (p *PrefixCompactProcessor) config() *PrefixCompactProcessorConfig {
	if c, ok := p.ContextProcessor.Config().(*PrefixCompactProcessorConfig); ok {
		return c
	}
	return &PrefixCompactProcessorConfig{}
}

// minTargetContextRatio 读取最小目标比例（缺省 0.1）。
// Python: getattr(self.config, "min_target_context_ratio", 0.0)
func (p *PrefixCompactProcessor) minTargetContextRatio() float64 {
	return p.config().MinTargetContextRatio
}

// newProcessorOption 应用选项。
func (p *PrefixCompactProcessor) newProcessorOption(opts []iface.Option) *iface.ProcessorOption {
	o := &iface.ProcessorOption{}
	for _, apply := range opts {
		apply(o)
	}
	return o
}

// extractStateSnapshotOrRaw 从响应文本抽取 <state_snapshot>，无则原文。
// Python: base.py:783 (_extract_state_snapshot_or_raw, @staticmethod)
func extractStateSnapshotOrRaw(content string) string {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return ""
	}
	match := stateSnapshotPattern.FindStringSubmatch(raw)
	if match == nil {
		return raw
	}
	snapshot := strings.TrimSpace(match[1])
	if snapshot == "" {
		return raw
	}
	return snapshot
}

// estimateTextTokens 文本 token 估算（len//3，下限 1）。
// Python: base.py:852 (_estimate_text_tokens, @staticmethod)
func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return max(len(text)/3, 1)
}

// contextDebugEnabled 调试落盘开关。
// Python: base.py:855 (_context_debug_enabled)
func (p *PrefixCompactProcessor) contextDebugEnabled() bool {
	return p.config().EnableCompressionDump
}

// contextDebugDir 调试目录。
// Python: base.py:858 (_context_debug_dir)
func (p *PrefixCompactProcessor) contextDebugDir() string {
	return p.config().CompressionDumpDir
}

// writeContextDebug 写调试记录（开关关时零开销）。
// Python: base.py:861 (_write_context_debug)
func (p *PrefixCompactProcessor) writeContextDebug(mc iface.ModelContext, event string, payload map[string]any) {
	forkedsupport.WriteDebugRecord(
		context.Background(), mc, p.ProcessorType(), event,
		p.contextDebugEnabled(), p.contextDebugDir(), payload,
	)
}

// spanSummary 跨度消息摘要（idx/role/tokens/preview）。
// Python: base.py:872 (_span_summary, @staticmethod)
func spanSummary(messages []llm_schema.BaseMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for idx, message := range messages {
		content := message.GetContent().String()
		result = append(result, map[string]any{
			"idx":     idx,
			"role":    message.GetRole(),
			"tokens":  max(len(content)/3, 1),
			"preview": truncateString(content, 200),
		})
	}
	return result
}

// messageToolCallIds 收集 AssistantMessage 的 tool_call id 集合。
// Python: base.py:890 (_message_tool_call_ids)
func messageToolCallIDs(message *llm_schema.AssistantMessage) map[string]bool {
	result := map[string]bool{}
	for _, toolCall := range message.ToolCalls {
		if toolCall.ID != "" {
			result[toolCall.ID] = true
		}
	}
	return result
}

// toolResultIds 收集 ToolMessage 的 tool_call_id 集合。
// Python: base.py:901 (_tool_result_ids)
func toolResultIDs(messages []llm_schema.BaseMessage) map[string]bool {
	result := map[string]bool{}
	for _, message := range messages {
		if tm, ok := message.(*llm_schema.ToolMessage); ok && tm.ToolCallID != "" {
			result[tm.ToolCallID] = true
		}
	}
	return result
}

// serializeTool 工具定义序列化（排序 JSON）。
// Python: base.py:909 (_serialize_tool)
func serializeTool(t schema.ToolInfoInterface) string {
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Sprint(t)
	}
	return string(data)
}

// isRealUserMessage 是否真实用户消息（排除内部前缀）。
// Python: base.py:133 (_is_real_user_message)
func isRealUserMessage(message llm_schema.BaseMessage) bool {
	if _, ok := message.(*llm_schema.UserMessage); !ok {
		return false
	}
	content := message.GetContent()
	if !content.IsText() {
		return true
	}
	stripped := strings.TrimLeft(content.Text(), " \t\n")
	for _, prefix := range cesupport.InternalUserPrefixes {
		if strings.HasPrefix(stripped, prefix) {
			return false
		}
	}
	return true
}

// indexRange 0..n-1 下标序列。
func indexRange(n int) []int {
	result := make([]int, n)
	for i := range result {
		result[i] = i
	}
	return result
}

// intersects 集合是否相交。
func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// optString 从 Extra 读取字符串选项。
func optString(extra map[string]any, key string) string {
	if v, ok := extra[key].(string); ok {
		return v
	}
	return ""
}

// truncateString rune 安全截断。
func truncateString(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

// firstNonEmpty 首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

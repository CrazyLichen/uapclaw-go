package compressor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/recall"
	fsupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	fdsupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/support"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// SessionMemoryBlockOpen 会话记忆块的开启标记
	// Python: session_memory_compressor.py:27 (SESSION_MEMORY_BLOCK_OPEN)
	SessionMemoryBlockOpen = "<memory_block_session>"
	// SessionMemoryBlockClose 会话记忆块的关闭标记
	// Python: session_memory_compressor.py:28 (SESSION_MEMORY_BLOCK_CLOSE)
	SessionMemoryBlockClose = "</memory_block_session>"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMemoryCompressorConfig 会话记忆压缩器配置。
// Python: session_memory_compressor.py:31 (SessionMemoryCompressorConfig)
type SessionMemoryCompressorConfig struct {
	// Enabled 是否启用（默认关，rail preset 中由 session_memory 开关驱动）
	Enabled bool `json:"enabled"`
	// TriggerContextRatio 触发阈值（占上下文窗口的比例）
	TriggerContextRatio float64 `json:"trigger_context_ratio"`
	// SessionMemoryPath 显式笔记路径（nil 时走 runtime/workspace 回退）
	SessionMemoryPath *string `json:"session_memory_path"`
	// MaxNotesChars 笔记长度上限（超长不物化）
	MaxNotesChars int `json:"max_notes_chars"`
	// Memory 会话记忆更新配置（与 SessionMemoryManager 共用）
	Memory *cecontext.SessionMemoryConfig `json:"memory"`
	// EnableContextDebug 调试落盘开关
	EnableContextDebug bool `json:"enable_context_debug"`
	// ContextDebugDir 调试目录（空走环境变量/workspace 回退）
	ContextDebugDir string `json:"context_debug_dir"`
}

// NewSessionMemoryCompressorConfig 创建配置，字段使用 Python 默认值。
func NewSessionMemoryCompressorConfig() *SessionMemoryCompressorConfig {
	memoryCfg := cecontext.NewSessionMemoryConfig()
	return &SessionMemoryCompressorConfig{
		Enabled:             false,
		TriggerContextRatio: 0.8,
		MaxNotesChars:       120_000,
		Memory:              &memoryCfg,
	}
}

// Validate 校验配置（Python pydantic Field 约束等价）。
func (c *SessionMemoryCompressorConfig) Validate() error {
	if c.TriggerContextRatio <= 0 || c.TriggerContextRatio >= 1 {
		return fmt.Errorf("trigger_context_ratio 必须在 (0,1) 区间，当前值: %v", c.TriggerContextRatio)
	}
	if c.MaxNotesChars <= 0 {
		return fmt.Errorf("max_notes_chars 必须 > 0，当前值: %d", c.MaxNotesChars)
	}
	return nil
}

// SetModelDefaults 设置模型配置默认值（iface.ProcessorConfig 实现）。
// Python: SessionMemoryCompressorConfig 无 model 字段——内存配置挂 Memory 子对象
func (c *SessionMemoryCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Memory != nil {
		c.Memory.SetModelDefaults(model, modelClient)
	}
}

// GetModel 返回 Model 字段（本配置无直接 Model 字段，返回 nil）。
func (c *SessionMemoryCompressorConfig) GetModel() *llm_schema.ModelRequestConfig {
	return nil
}

// SessionMemoryCompressor 把已提交的会话记忆物化为紧凑上下文块。
// Python: session_memory_compressor.py:41 (SessionMemoryCompressor)
type SessionMemoryCompressor struct {
	*forked.ContextProcessor
	// config 处理器配置
	config *SessionMemoryCompressorConfig
}

// NewSessionMemoryCompressor 创建压缩器实例。
// Python: SessionMemoryCompressor.__init__ (session_memory_compressor.py:47)
func NewSessionMemoryCompressor(config *SessionMemoryCompressorConfig) *SessionMemoryCompressor {
	return &SessionMemoryCompressor{
		ContextProcessor: forked.NewContextProcessor(config),
		config:           config,
	}
}

// ProcessorType 处理器类型标识。
// 偏差 D-24.2-1：Python MetaContextProcessor 元类注入类名；Go 显式返回。
func (c *SessionMemoryCompressor) ProcessorType() string {
	return "SessionMemoryCompressor"
}

// Config 返回配置对象。
func (c *SessionMemoryCompressor) Config() *SessionMemoryCompressorConfig {
	return c.config
}

// ──────────────────────────── 导出函数（iface.ContextProcessor 实现） ────────────────────────────

// TriggerGetContextWindow 启用且达阈值且候选可构建时介入。
// Python: session_memory_compressor.py:50 (trigger_get_context_window)
func (c *SessionMemoryCompressor) TriggerGetContextWindow(ctx context.Context, mc iface.ModelContext, cw iface.ContextWindow, opts ...iface.Option) (bool, error) {
	if !c.config.Enabled {
		return false, nil
	}
	if !c.contextReachesThreshold(ctx, mc, &cw) {
		return false, nil
	}
	return c.buildCandidate(mc, &cw, false) != nil, nil
}

// OnGetContextWindow 物化记忆块并替换锚点之前的消息。
// Python: session_memory_compressor.py:63 (on_get_context_window)
func (c *SessionMemoryCompressor) OnGetContextWindow(ctx context.Context, mc iface.ModelContext, cw iface.ContextWindow, opts ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	candidate := c.buildCandidate(mc, &cw, true)
	if candidate == nil {
		return nil, cw, nil
	}

	preservedMessages := candidate.preservedMessages
	originalMessages := cw.ContextMessages
	memoryContent := candidate.memoryContent

	archive := c.archiveReplacedMessages(ctx, mc, originalMessages,
		originalMessages[:len(originalMessages)-len(preservedMessages)])
	if archive != nil {
		memoryContent = withRecallMarker(memoryContent, archive.MemoryID)
	}

	memoryMessage := llm_schema.NewUserMessage(memoryContent)
	newMessages := append([]llm_schema.BaseMessage{memoryMessage}, preservedMessages...)
	if len(newMessages) > len(originalMessages) {
		return nil, cw, nil
	}

	cw.ContextMessages = newMessages
	mc.SetMessages(newMessages, true)
	indices := make([]int, len(originalMessages))
	for i := range indices {
		indices[i] = i
	}
	return &iface.ContextEvent{
		EventType:        c.ProcessorType(),
		MessagesToModify: indices,
		CompactSummary:   memoryContent,
	}, cw, nil
}

// OnAddMessages 默认直通（Python forked 基类默认实现）。
func (c *SessionMemoryCompressor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messages []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, messages, nil
}

// TriggerAddMessages 默认不介入（Python forked 基类默认实现）。
func (c *SessionMemoryCompressor) TriggerAddMessages(ctx context.Context, mc iface.ModelContext, messages []llm_schema.BaseMessage, opts ...iface.Option) (bool, error) {
	return false, nil
}

// LoadState 空实现（无状态）。
// Python: session_memory_compressor.py:246 (load_state)
func (c *SessionMemoryCompressor) LoadState(state map[string]any) {}

// SaveState 返回空状态。
// Python: session_memory_compressor.py:249 (save_state)
func (c *SessionMemoryCompressor) SaveState() map[string]any {
	return map[string]any{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// archiveCandidate 构建候选的内部载体
type archiveCandidate struct {
	memoryContent     string
	preservedMessages []llm_schema.BaseMessage
}

// archiveReplacedMessages 把被记忆块替换的消息归档（best-effort，失败不阻断压缩）。
// gate = 引擎级 CompressionRecallConfig.enabled。
// Python: session_memory_compressor.py:99 (_archive_replaced_messages)
func (c *SessionMemoryCompressor) archiveReplacedMessages(
	ctx context.Context,
	mc iface.ModelContext,
	originalMessages []llm_schema.BaseMessage,
	messagesToCompress []llm_schema.BaseMessage,
) *recall.CompressionArchive {
	var recallConfig *ceschema.CompressionRecallConfig
	if provider, ok := mc.(cecontext.CompressionRecallConfigProvider); ok {
		recallConfig = provider.CompressionRecallConfig()
	}
	if recallConfig == nil || !recallConfig.Enabled {
		return nil
	}
	if len(messagesToCompress) == 0 {
		return nil
	}
	// Python: asyncio.to_thread + except → warning + None（归档失败绝不阻断压缩）
	archive, err := recall.ArchiveCompressionMessages(
		mc,
		c.ProcessorType(),
		originalMessages,
		messagesToCompress,
		nil,
		recallConfig.ChunkSizeTokens,
		recallConfig.ChunkOverlapTokens,
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_MEMORY_ARCHIVE_FAILED").
			Str("processor", c.ProcessorType()).
			Err(err).
			Msg("compression recall archive failed; continuing without a recall marker")
		return nil
	}
	return archive
}

// withRecallMarker 在记忆块尾部追加 COMPRESSION_RECALL 标记（close 前优先）。
// Python: session_memory_compressor.py:142 (_with_recall_marker, @staticmethod)
func withRecallMarker(content string, memoryID string) string {
	marker := fmt.Sprintf("[[COMPRESSION_RECALL: id=%s]]", memoryID)
	if strings.HasSuffix(content, SessionMemoryBlockClose) {
		head := content[:len(content)-len(SessionMemoryBlockClose)]
		return head + marker + "\n" + SessionMemoryBlockClose
	}
	return content + "\n" + marker
}

// buildCandidate 构建候选（记忆消息 + 保留消息）。
// Python: session_memory_compressor.py:150 (_build_candidate)
func (c *SessionMemoryCompressor) buildCandidate(mc iface.ModelContext, cw *iface.ContextWindow, checkThreshold bool) *archiveCandidate {
	if !c.config.Enabled {
		return nil
	}
	if checkThreshold && !c.contextReachesThreshold(context.Background(), mc, cw) {
		return nil
	}
	notes := c.loadNotes(mc)
	if notes == "" {
		return nil
	}
	if len([]rune(notes)) > c.config.MaxNotesChars {
		return nil
	}

	runtime := c.getRuntime(mc)
	anchor, _ := runtime["notes_upto_message_id"].(string)
	messages := cw.ContextMessages
	anchorIndex := cecontext.FindMessageIndexByContextMessageID(messages, anchor)
	if anchorIndex < 0 {
		return nil
	}

	preservedMessages := messages[anchorIndex+1:]
	memoryContent := SessionMemoryBlockOpen + "\n" +
		"<meaning>Durable asynchronously maintained session memory; not a new user request.</meaning>\n" +
		"<conflict_policy>New raw messages and the latest explicit user intent " +
		"override this memory.</conflict_policy>\n" +
		fmt.Sprintf("<coverage>notes_upto_message_id=%s; coverage_type=completed_api_rounds</coverage>\n", anchor) +
		"<summary>\n" +
		strings.TrimSpace(notes) + "\n" +
		"</summary>\n" +
		SessionMemoryBlockClose
	return &archiveCandidate{
		memoryContent:     memoryContent,
		preservedMessages: preservedMessages,
	}
}

// contextReachesThreshold 阈值判定（token ≥ context_max × ratio）。
// Python: session_memory_compressor.py:188 (_context_reaches_threshold)
func (c *SessionMemoryCompressor) contextReachesThreshold(ctx context.Context, mc iface.ModelContext, cw *iface.ContextWindow) bool {
	messages := append(append([]llm_schema.BaseMessage{}, cw.SystemMessages...), cw.ContextMessages...)
	var counter token.TokenCounter
	if mc != nil {
		counter = mc.TokenCounter()
	}
	modelName := ""
	if provider, ok := mc.(fsupport.ContextWindowProvider); ok {
		modelName = provider.ModelName()
	}
	currentTokens := fsupport.CountMessagesTokens(messages, counter, modelName, c.ProcessorType(), true)
	contextMax := fsupport.ResolveContextMax(mc, modelName)
	threshold := fsupport.ResolveRatioTokenThreshold(contextMax, c.config.TriggerContextRatio)
	reached := currentTokens >= threshold

	// Python: write_debug_record（threshold_check 事件）
	fdsupport.WriteDebugRecord(ctx, mc, c.ProcessorType(), "threshold_check", c.config.EnableContextDebug,
		c.config.ContextDebugDir, map[string]any{
			"phase":        "get_context_window",
			"total_tokens": currentTokens,
			"context_max":  contextMax,
			"threshold":    threshold,
			"hit":          reached,
			"reason":       map[bool]string{true: "reached_threshold", false: "below_threshold"}[reached],
		})
	if !reached {
		return false
	}
	return true
}

// loadNotes 读取笔记文件内容（不存在/读失败 → 空）。
// Python: session_memory_compressor.py:218 (_load_notes)
func (c *SessionMemoryCompressor) loadNotes(mc iface.ModelContext) string {
	path := c.resolveNotesPath(mc)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// resolveNotesPath 笔记路径三级回退：显式配置 → runtime memory_path → workspace 约定路径。
// Python: session_memory_compressor.py:227 (_resolve_notes_path)
func (c *SessionMemoryCompressor) resolveNotesPath(mc iface.ModelContext) string {
	if c.config.SessionMemoryPath != nil && *c.config.SessionMemoryPath != "" {
		abs, err := filepath.Abs(*c.config.SessionMemoryPath)
		if err != nil {
			return *c.config.SessionMemoryPath
		}
		return abs
	}
	runtime := c.getRuntime(mc)
	if memoryPath, _ := runtime["memory_path"].(string); memoryPath != "" {
		abs, err := filepath.Abs(memoryPath)
		if err != nil {
			return memoryPath
		}
		return abs
	}
	workspaceDir := ""
	sessionID := ""
	if mc != nil {
		workspaceDir = mc.WorkspaceDir()
		sessionID = mc.SessionID()
	}
	if workspaceDir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(workspaceDir, "context", sessionID+"_context", "session_memory", "session_context.md")
}

// getRuntime 会话记忆 runtime 状态（notes_upto_message_id / memory_path）。
// Python: session_memory_compressor.py:241 (_get_runtime, @staticmethod)
func (c *SessionMemoryCompressor) getRuntime(mc iface.ModelContext) map[string]any {
	var sess sessioninterfaces.SessionFacade
	if mc != nil {
		sess = mc.GetSessionRef()
	}
	return cecontext.GetSessionMemoryRuntime(sess)
}

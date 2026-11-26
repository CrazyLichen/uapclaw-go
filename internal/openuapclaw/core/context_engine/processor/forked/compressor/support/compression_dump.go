package support

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// CompressionDumpDirEnv 压缩产物落盘目录环境变量。
// Python: compression_dump.py:24 COMPRESSION_DUMP_DIR_ENV（OPENJIUWEN_*，
// 本仓环境变量统一改前缀 OPENUAPCLAW_*，同偏差 D-24.2-5 口径）。
const CompressionDumpDirEnv = "OPENUAPCLAW_COMPRESSION_DUMP_DIR"

// ──────────────────────────── 结构体 ────────────────────────────

// DumpProcessorStats dump 对压缩处理器的窄能力面。
// Python: compression_dump.py 直接引用 PrefixCompactProcessor 类型
// （processor.resolve_context_max / processor.count_messages_tokens）；
// Go 侧 compressor → support 单向导入，dump 反向引用会成环（Python 靠
// 函数内懒导入规避），故以窄接口承载所需两个只读方法。
// 偏差 D-24.15-b1：窄接口替代直接类型引用；回收条件=无（Go 包图限制）。
// DumpContextIdentifiers dump 所需的上下文标识窄面。
// Python: compression_dump.py 对 context 走 getattr duck typing
// （session_id / context_id / workspace_dir）；Go 侧以窄接口承载同一鸭子面，
// SessionModelContext 天然满足。偏差 D-24.15-b1 同批登记。
type DumpContextIdentifiers interface {
	// SessionID 会话 ID
	SessionID() string
	// ContextID 上下文 ID
	ContextID() string
	// WorkspaceDir 工作目录
	WorkspaceDir() string
}

type DumpProcessorStats interface {
	// ResolveContextMaxForDump 等价 processor.resolve_context_max(context, {})
	ResolveContextMaxForDump(mc DumpContextIdentifiers, modelName string) int
	// CountMessagesTokensForDump 等价 processor.count_messages_tokens(messages, context)
	CountMessagesTokensForDump(mc DumpContextIdentifiers, messages []llm_schema.BaseMessage) int
}

// CompressionSpanShape dump 视角的 span 只读形状（计数用）。
// Python: compression_dump.py 引用 PrefixCompactSpan 三字段；
// Go 侧 span 类型在 compressor 包，此处以切片载体解耦包环（同 D-24.15-b1）。
type CompressionSpanShape struct {
	// PreservedPrefix 保留前缀
	PreservedPrefix []llm_schema.BaseMessage
	// MessagesToCompresess 待压缩消息
	MessagesToCompress []llm_schema.BaseMessage
	// ProtectedTail 保护尾巴
	ProtectedTail []llm_schema.BaseMessage
}

// CompressionDumpInput 单次压缩调用的落盘输入
// Python: compression_dump.py:26 (CompressionDumpInput)
type CompressionDumpInput struct {
	// Processor 压缩处理器（窄接口，见 DumpProcessorStats）
	Processor DumpProcessorStats
	// Context 模型上下文（标识窄面，见 DumpContextIdentifiers）
	Context DumpContextIdentifiers
	// Config 处理器配置（读 enable_compression_dump / compression_dump_dir）
	Config any
	// ProcessorType 处理器类型标识
	ProcessorType string
	// OriginalMessages 压缩前窗口消息
	OriginalMessages []llm_schema.BaseMessage
	// Span 压缩跨度
	Span CompressionSpanShape
	// Prompt 压缩提示词
	Prompt string
	// Request 实际发出的压缩请求
	Request *CompressionRequest
	// ResponseContent 模型原始响应文本
	ResponseContent string
	// Summary 抽取后的摘要
	Summary string
	// NewMessages 压缩后的新消息列表
	NewMessages []llm_schema.BaseMessage
	// Usage 压缩用量
	Usage map[string]any
}

// configGetter 从 Config（map[string]any 或 struct+tag）读取键值。
// Python: getattr(config, field, None)。map 形态直接取；其余形态由调用方
// 传入的键不存在时返回零值（保持 best-effort 语义）。
func configGetter(config any) func(key string) any {
	if m, ok := config.(map[string]any); ok {
		return func(key string) any { return m[key] }
	}
	return func(string) any { return nil }
}

// DumpCompressionArtifact 落盘单次压缩调用产物，供离线效果分析。
// 目录解析顺序：config.compression_dump_dir > 环境变量 > workspace 默认目录。
// 返回写入的文件路径；禁用或失败返回空串。文件系统错误一律吞掉，
// 追踪不得打断压缩主链路。
// Python: compression_dump.py:37 (dump_compression_artifact)
func DumpCompressionArtifact(compression *CompressionDumpInput) string {
	get := configGetter(compression.Config)
	if enabled, ok := get("enable_compression_dump").(bool); !ok || !enabled {
		return ""
	}

	dumpDir := resolveDumpDir(compression.Context, compression.Config)
	if dumpDir == "" {
		logDumpSkip(compression.ProcessorType)
		return ""
	}

	contextMax := compression.Processor.ResolveContextMaxForDump(compression.Context, "")
	tokensBefore := compression.Processor.CountMessagesTokensForDump(compression.Context, compression.OriginalMessages)
	tokensAfter := compression.Processor.CountMessagesTokensForDump(compression.Context, compression.NewMessages)
	targetTokens := compression.Processor.CountMessagesTokensForDump(compression.Context, compression.Span.MessagesToCompress)
	messagesSentToModel := BuildMessages(compression.Request)

	payload := map[string]any{
		"processor_type": compression.ProcessorType,
		"session_id":     safeContextDumpValue(compression.Context, "session_id", "unknown_session"),
		"context_id":     safeContextDumpValue(compression.Context, "context_id", "unknown_context"),
		"trigger": map[string]any{
			"context_max":         contextMax,
			"total_tokens_before": tokensBefore,
			"target_tokens":       targetTokens,
		},
		"span": map[string]any{
			"preserved_prefix_count":       len(compression.Span.PreservedPrefix),
			"messages_to_compress_count":   len(compression.Span.MessagesToCompress),
			"protected_tail_count":         len(compression.Span.ProtectedTail),
		},
		"compression_request": map[string]any{
			"prompt":                  compression.Prompt,
			"exclude_recent_messages": compression.Request.ExcludeRecentMessages,
			"system_messages_count":   len(compression.Request.SystemMessages),
			"tools_count":             len(compression.Request.Tools),
			"tools":                   toDumpJSONable(compression.Request.Tools),
			"messages_sent_to_model":  toDumpJSONable(messagesSentToModel),
		},
		"compression_response": map[string]any{
			"raw_content":       compression.ResponseContent,
			"extracted_summary": compression.Summary,
			"usage":             compression.Usage,
		},
		"context_after": map[string]any{
			"messages":           toDumpJSONable(compression.NewMessages),
			"total_tokens_after": tokensAfter,
		},
		"benefit": buildBenefit(tokensBefore, tokensAfter),
	}

	fileName := buildDumpFileName(
		payload["session_id"].(string),
		compression.ProcessorType,
	)
	dumpPath := filepath.Join(dumpDir, fileName)
	payload["timestamp"] = float64(time.Now().UnixNano()) / 1e9
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		logDumpFail(compression.ProcessorType, err)
		return ""
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		logDumpFail(compression.ProcessorType, err)
		return ""
	}
	if err := os.WriteFile(dumpPath, data, 0o644); err != nil {
		logDumpFail(compression.ProcessorType, err)
		return ""
	}
	logDumpOK(compression.ProcessorType, dumpPath)
	return dumpPath
}

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveDumpDir 三级目录解析
// Python: compression_dump.py:105 (_resolve_dump_dir)
func resolveDumpDir(mc DumpContextIdentifiers, config any) string {
	get := configGetter(config)
	if configured, ok := get("compression_dump_dir").(string); ok && configured != "" {
		return expandDumpDirTemplate(configured, mc)
	}
	if envDir := os.Getenv(CompressionDumpDirEnv); envDir != "" {
		return expandDumpDirTemplate(envDir, mc)
	}
	workspaceDir := dumpWorkspaceDir(mc)
	if workspaceDir == "" {
		return ""
	}
	sessionID := safeContextDumpValue(mc, "session_id", "unknown_session")
	return filepath.Join(workspaceDir, "context", sessionID+"_context", "compression_logs")
}

// dumpWorkspaceDir 取工作目录（best-effort）
// Python: compression_dump.py:124 (_workspace_dir)
func dumpWorkspaceDir(mc DumpContextIdentifiers) string {
	if mc == nil {
		return ""
	}
	return strings.TrimSpace(mc.WorkspaceDir())
}

// expandDumpDirTemplate 展开 {session_id}/{context_id} 模板
// Python: compression_dump.py:133 (_expand_dump_dir_template)
func expandDumpDirTemplate(path string, mc DumpContextIdentifiers) string {
	if !strings.Contains(path, "{session_id}") && !strings.Contains(path, "{context_id}") {
		return path
	}
	path = strings.ReplaceAll(path, "{session_id}",
		safeFilenamePart(safeContextDumpValue(mc, "session_id", "unknown_session")))
	path = strings.ReplaceAll(path, "{context_id}",
		safeFilenamePart(safeContextDumpValue(mc, "context_id", "unknown_context")))
	return path
}

// safeContextDumpValue best-effort 读取上下文标识
// Python: compression_dump.py:141 (_safe_context_value)
func safeContextDumpValue(mc DumpContextIdentifiers, method string, fallback string) string {
	if mc == nil {
		return fallback
	}
	var value string
	switch method {
	case "session_id":
		value = mc.SessionID()
	case "context_id":
		value = mc.ContextID()
	default:
		return fallback
	}
	if value == "" {
		return fallback
	}
	return value
}

// buildBenefit 压缩收益统计
// Python: compression_dump.py:156 (_build_benefit)
func buildBenefit(tokensBefore int, tokensAfter int) map[string]any {
	reduction := tokensBefore - tokensAfter
	if reduction < 0 {
		reduction = 0
	}
	ratio := 0.0
	if tokensBefore > 0 {
		ratio = float64(reduction) / float64(tokensBefore)
	}
	return map[string]any{
		"tokens_before":   tokensBefore,
		"tokens_after":    tokensAfter,
		"reduction":       reduction,
		"reduction_ratio": ratio,
	}
}

// buildDumpFileName 毫秒时间戳前缀文件名（与 RuleCompressionPipeline 命名一致）
// Python: compression_dump.py:171 (_build_file_name)
func buildDumpFileName(sessionID string, processorType string) string {
	parts := []string{
		fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond)),
		safeFilenamePart(processorType),
		safeFilenamePart(sessionID),
	}
	return strings.Join(parts, "_") + ".json"
}

// dumpSafeFilenamePartRe 文件名净化正则（[^A-Za-z0-9_.-]+ → "-"）
var dumpSafeFilenamePartRe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// safeFilenamePart 文件名净化，超长截断到 80
// Python: compression_dump.py:183 (_safe_filename_part)
func safeFilenamePart(value string) string {
	safe := strings.Trim(dumpSafeFilenamePartRe.ReplaceAllString(value, "-"), "-")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	if safe == "" {
		return "unknown"
	}
	return safe
}

// toDumpJSONable 把压缩产物收敛为 JSON 兼容值
// Python: compression_dump.py:190 (_to_jsonable)
func toDumpJSONable(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = toDumpJSONable(item)
		}
		return out
	case []llm_schema.BaseMessage:
		out := make([]any, 0, len(v))
		for _, message := range v {
			out = append(out, toDumpJSONable(message))
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, toDumpJSONable(item))
		}
		return out
	case string, int, int64, float64, bool, nil:
		return v
	case interface{ MarshalJSON() ([]byte, error) }:
		if data, err := v.MarshalJSON(); err == nil {
			var decoded any
			if err := json.Unmarshal(data, &decoded); err == nil {
				return toDumpJSONable(decoded)
			}
		}
		return fmt.Sprint(v)
	default:
		return fmt.Sprint(value)
	}
}

// 日志封装（组件级 logger；support 为基础设施层 → ComponentCommon）
// Python: compression_dump.py logger.info("[..] compression dump skipped..") 等

// logDumpSkip 记录跳过（无可用目录）
// Python: compression_dump.py:51 logger.info("[%s] compression dump skipped: no dump dir resolved", ...)
func logDumpSkip(processorType string) {
	logger.Info(logComponent).
		Str("processor_type", processorType).
		Msg("compression dump skipped: no dump dir resolved")
}

// logDumpOK 记录成功
// Python: compression_dump.py:120 logger.info("[%s] dumped compression artifact path=%s", ...)
func logDumpOK(processorType string, path string) {
	logger.Info(logComponent).
		Str("processor_type", processorType).
		Str("path", path).
		Msg("dumped compression artifact")
}

// logDumpFail 记录失败（防御路径，不得打断压缩主链路）
// Python: compression_dump.py:116 logger.warning("[%s] failed to dump compression artifact: %s", ... exc_info=True)
func logDumpFail(processorType string, err error) {
	logger.Warn(logComponent).
		Str("processor_type", processorType).
		Err(err).
		Msg("failed to dump compression artifact")
}

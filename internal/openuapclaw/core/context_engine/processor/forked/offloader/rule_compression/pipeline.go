// 规则压缩管线（固定上下文相对字符预算）。
// 对应 Python 代码：offloader/rule_compression/pipeline.py（267 行）
package rulecompression

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

// ──────────────────────────── 常量 ────────────────────────────

// charactersPerToken 字符预算与 len//3 兜底口径对齐。
// Python: pipeline.py:23 (CHARACTERS_PER_TOKEN)
const charactersPerToken = 3

// RuleCompressionDumpDirEnv 落盘目录环境变量（OPENUAPCLAW_* 前缀约定，偏差 D-24.15-b2 同族）。
// Python: pipeline.py:24 (RULE_COMPRESSION_DUMP_DIR_ENV="OPENJIUWEN_RULE_COMPRESSION_DUMP_DIR")
const RuleCompressionDumpDirEnv = "OPENUAPCLAW_RULE_COMPRESSION_DUMP_DIR"

// pipelineLogComponent 日志组件
const pipelineLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// PipelineContext 管线消费的上下文窄面（PY 鸭子式访问 workspace_dir/session_id/
// context_id/get_messages 的等价收窄；偏差 D-24.15-b8）。
type PipelineContext interface {
	GetMessages(size int, withHistory bool) ([]llm_schema.BaseMessage, error)
	WorkspaceDir() string
	SessionID() string
	ContextID() string
}

// RuleCompressionPipeline 确定性规则压缩管线。
// Python: pipeline.py:27 (RuleCompressionPipeline)
type RuleCompressionPipeline struct {
	router          *RuleContentRouter
	timeFunc        func() float64
	enableDump      bool
	dumpDirOverride string
}

// NewRuleCompressionPipeline 创建管线。
// Python: pipeline.py:33 (__init__)
func NewRuleCompressionPipeline(router *RuleContentRouter, timeFunc func() float64, enableDump bool, dumpDir string) *RuleCompressionPipeline {
	if router == nil {
		router = NewRuleContentRouter()
	}
	if timeFunc == nil {
		timeFunc = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	return &RuleCompressionPipeline{router: router, timeFunc: timeFunc, enableDump: enableDump, dumpDirOverride: dumpDir}
}

// CurrentTime 当前时间（可注入）。
// Python: pipeline.py:42 (current_time)
func (p *RuleCompressionPipeline) CurrentTime() float64 { return p.timeFunc() }

// ContextCharacterCapacity 上下文字符容量。
// Python: pipeline.py:46 (context_character_capacity)
func (p *RuleCompressionPipeline) ContextCharacterCapacity(mc PipelineContext) int {
	contextTokens := 0
	type windowProvider interface{ ContextWindowTokens() int }
	if provider, ok := mc.(windowProvider); ok {
		contextTokens = provider.ContextWindowTokens()
	}
	return max(int(float64(contextTokens))*charactersPerToken, 1)
}

// Compress 单消息规则压缩（ToolMessage 且超预算才处理）。
// Python: pipeline.py:51 (compress)
func (p *RuleCompressionPipeline) Compress(
	message llm_schema.BaseMessage,
	mc PipelineContext,
	passName string,
	maxChars int,
	force bool,
	contextMessages []llm_schema.BaseMessage,
) llm_schema.BaseMessage {
	toolMsg, ok := message.(*llm_schema.ToolMessage)
	if !ok {
		return message
	}
	if !toolMsg.Content.IsText() {
		return message
	}
	toolContent := toolMsg.Content.Text()
	if !force && len(toolContent) <= maxChars {
		return message
	}

	original := toolContent
	if contextMessages == nil {
		contextMessages, _ = mc.GetMessages(0, true)
	}
	toolName, toolArguments := p.toolInfoForMessage(toolMsg, contextMessages)
	queryTerms := p.queryTermsForMessage(contextMessages, toolName, toolArguments)
	ruleCtx := NewRuleContext(max(maxChars/charactersPerToken, 1))
	ruleCtx.HeadTokens = max(maxChars/(charactersPerToken*2), 1)
	ruleCtx.TailTokens = max(maxChars/(charactersPerToken*2), 1)
	ruleCtx.CountTokens = func(text string) int { return max(len(text)/charactersPerToken, 1) }
	ruleCtx.QueryTerms = queryTerms
	ruleCtx.ToolName = toolName
	ruleCtx.FilePath = ExtractFilePathArgument(toolArguments)
	result := p.router.Compress(original, ruleCtx)
	content := result.Content
	if !result.Modified {
		return message
	}

	metadata := map[string]any{}
	metadata["rule_compressed_at"] = p.CurrentTime()
	metadata["rule_compression_type"] = string(result.ContentType)
	metadata["rule_compression_pass"] = passName
	if result.Details != nil {
		metadata["rule_compression_details"] = result.Details
	}
	dumpPath := p.dumpResult(mc, dumpInput{
		original: original, compressed: content, passName: passName,
		contentType: string(result.ContentType), toolCallID: toolMsg.ToolCallID,
		details: result.Details,
	})
	if dumpPath != "" {
		metadata["rule_compression_dump_path"] = dumpPath
	}
	logger.Info(pipelineLogComponent).
		Str("tool_call_id", toolMsg.ToolCallID).
		Str("pass", passName).
		Str("type", string(result.ContentType)).
		Int("chars_before", len(original)).
		Int("chars_after", len(content)).
		Msg("[RuleCompression] applied")

	// 返回替换 content 后的 ToolMessage（metadata 入 extra 字段形态）
	replaced := llm_schema.NewToolMessage(toolMsg.ToolCallID, content)
	return replaced
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dumpInput 落盘入参。
type dumpInput struct {
	original    string
	compressed  string
	passName    string
	contentType string
	toolCallID  string
	details     map[string]any
}

// dumpResult 落盘压缩产物。
// Python: pipeline.py:100 (_dump_result)
func (p *RuleCompressionPipeline) dumpResult(mc PipelineContext, in dumpInput) string {
	if !p.enableDump {
		return ""
	}
	dumpDir := p.dumpDir(mc)
	if dumpDir == "" {
		return ""
	}
	timestamp := p.CurrentTime()
	sessionID := safeContextValue(mc, "session_id", "unknown_session")
	contextID := safeContextValue(mc, "context_id", "unknown_context")
	safeParts := []string{
		fmt.Sprintf("%d", int(timestamp*1000)), in.passName, in.contentType, in.toolCallID,
	}
	parts := make([]string, len(safeParts))
	for i, part := range safeParts {
		parts[i] = safeFilenamePart(part)
	}
	dumpPath := filepath.Join(dumpDir, strings.Join(parts, "_")+".json")
	payload := map[string]any{
		"timestamp":             timestamp,
		"session_id":            sessionID,
		"context_id":            contextID,
		"tool_call_id":          in.toolCallID,
		"rule_compression_type": in.contentType,
		"rule_compression_pass": in.passName,
		"original_chars":        len(in.original),
		"compressed_chars":      len(in.compressed),
		"details":               orEmptyMap(in.details),
		"original_content":      in.original,
		"compressed_content":    in.compressed,
	}
	if err := os.MkdirAll(dumpDir, 0o755); err == nil {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err == nil {
			if err := os.WriteFile(dumpPath, data, 0o644); err == nil {
				logger.Info(pipelineLogComponent).Str("path", dumpPath).Msg("[RuleCompression] dumped compression payload")
				return dumpPath
			}
		}
	}
	logger.Warn(pipelineLogComponent).Msg("[RuleCompression] failed to dump compression payload")
	return ""
}

// dumpDir 落盘目录解析（override > env > workspace 约定路径）。
// Python: pipeline.py:137 (_dump_dir)
func (p *RuleCompressionPipeline) dumpDir(mc PipelineContext) string {
	if p.dumpDirOverride != "" {
		return p.expandDumpDirTemplate(p.dumpDirOverride, mc)
	}
	if envDir := os.Getenv(RuleCompressionDumpDirEnv); envDir != "" {
		return p.expandDumpDirTemplate(envDir, mc)
	}
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir == "" {
		return ""
	}
	sessionID := safeContextValue(mc, "session_id", "unknown_session")
	return filepath.Join(workspaceDir, "context", sessionID+"_context", "rule_compression_logs")
}

// expandDumpDirTemplate 模板展开。
// Python: pipeline.py:157 (_expand_dump_dir_template)
func (p *RuleCompressionPipeline) expandDumpDirTemplate(path string, mc PipelineContext) string {
	if !strings.Contains(path, "{session_id}") && !strings.Contains(path, "{context_id}") {
		return path
	}
	sessionID := safeFilenamePart(safeContextValue(mc, "session_id", "unknown_session"))
	contextID := safeFilenamePart(safeContextValue(mc, "context_id", "unknown_context"))
	path = strings.ReplaceAll(path, "{session_id}", sessionID)
	return strings.ReplaceAll(path, "{context_id}", contextID)
}

// safeContextValue 上下文值兜底读取。
// Python: pipeline.py:176 (_safe_context_value)
func safeContextValue(mc PipelineContext, methodName string, fallback string) string {
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
	return fallback
}

// safeFilenamePart 文件名安全化。
// Python: pipeline.py:187 (_safe_filename_part)
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

// queryTermsForMessage 最近真实用户消息 + 工具信息的词项。
// Python: pipeline.py:196 (_query_terms_for_message)
func (p *RuleCompressionPipeline) queryTermsForMessage(
	contextMessages []llm_schema.BaseMessage,
	toolName string,
	toolArguments any,
) map[string]bool {
	latestUserContent := ""
	for i := len(contextMessages) - 1; i >= 0; i-- {
		if userMsg, ok := contextMessages[i].(*llm_schema.UserMessage); ok && userMsg.Content.IsText() {
			latestUserContent = userMsg.Content.Text()
			break
		}
	}
	return ExtractQueryTerms(latestUserContent, toolName, toolArguments)
}

// toolInfoForMessage 反查 ToolMessage 对应的 tool_call。
// Python: pipeline.py:215 (_tool_info_for_message)
func (p *RuleCompressionPipeline) toolInfoForMessage(
	message *llm_schema.ToolMessage,
	contextMessages []llm_schema.BaseMessage,
) (string, any) {
	for i := len(contextMessages) - 1; i >= 0; i-- {
		assistantMsg, ok := contextMessages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, toolCall := range assistantMsg.ToolCalls {
			if toolCall.ID == message.ToolCallID {
				return toolCall.Name, toolCall.Arguments
			}
		}
	}
	return "", nil
}

// orEmptyMap nil 转 nil 语义保持（Python details or {}）。
func orEmptyMap(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	return details
}

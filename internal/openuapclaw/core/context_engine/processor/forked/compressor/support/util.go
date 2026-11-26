package support

import (
	"encoding/json"
	"regexp"
	"strings"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"

	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	ceprocessor "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/reinjection"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/support"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// ──────────────────────────── 常量 ────────────────────────────

// StateReinjectionMarker 状态回注标记
// Python: util.py STATE_REINJECTION_MARKER
const StateReinjectionMarker = "[STATE_REINJECTION]"

// InternalUserPrefixes 标记"用户角色但携带内部状态"的消息前缀表；
// 此类消息不得视为真实用户轮次。
// Python: util.py:38 (INTERNAL_USER_PREFIXES)
// 注：<memory_block_session> 为 session_memory_compressor.py 的
// SESSION_MEMORY_BLOCK_OPEN 内联（Python 为避免循环依赖同样内联于此）。
var InternalUserPrefixes = []string{
	"<system-reminder>",
	"<memory_block_current>",
	"<memory_block_dialogue>",
	"<memory_block_round>",
	"<memory_block_session>",
	"<recovered_context>",
	"[STATE_REINJECTION]",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ContextWindowProvider 暴露模型名与窗口 token 原料的可选能力面。
// Python 通过 getattr(context, "_model_name"/"_context_window_tokens"/
// "_model_context_window_tokens") 读取私有属性；Go 侧 ModelContext 无对应
// 公开访问器，SessionModelContext 按需实现本接口（偏差 D-24.2-2 平台差异）。
type ContextWindowProvider interface {
	// ModelName 当前模型名
	ModelName() string
	// ContextWindowTokens 全局上下文窗口 token 数（0 表示未设置）
	ContextWindowTokens() int
	// ModelContextWindowTokens 模型名到窗口 token 的映射表
	ModelContextWindowTokens() map[string]int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateContentTokens 无 token 计数器时的近似计数（len//3）。
// Python: util.py estimate_content_tokens
func EstimateContentTokens(content any) int {
	text := cesupport.SanitizeContentForText(content)
	n := len([]rune(text)) / 3
	if n < 1 {
		return 1
	}
	return n
}

// CountMessagesTokens 按 tokenizer 优先、字符兜底的策略计数。
// usageAware=true 时优先取最后一条携带 usage_metadata 的 AssistantMessage
// 上报的累计 total_tokens 作基线，仅对其后的尾部消息做 len//3 估算；
// 单消息/局部列表调用方必须保持 usageAware=false（累计口径不可按消息切分）。
// Python: util.py:271 (count_messages_tokens)
func CountMessagesTokens(
	messages []llm_schema.BaseMessage,
	counter token.TokenCounter,
	modelName string,
	processorType string,
	usageAware bool,
) int {
	if len(messages) == 0 {
		return 0
	}
	if usageAware {
		if usageTokens, ok := countUsageTokensWithTail(messages); ok {
			return usageTokens
		}
	}
	if counter != nil {
		if n, err := counter.CountMessages(messages, modelName); err == nil {
			return n
		}
		// Python: except Exception → logger.warning + 字符兜底（防御路径）
	}
	total := 0
	for _, message := range messages {
		total += EstimateContentTokens(message.GetContent().String())
	}
	return total
}

// CountUsageTokensWithTail 导出包装：usage 优先的累计 token 计数（base 基座消费）。
// Python: util.py count_usage_tokens_with_tail。
func CountUsageTokensWithTail(messages []llm_schema.BaseMessage) (int, bool) {
	return countUsageTokensWithTail(messages)
}

// ResolveContextMax 解析上下文处理器比例阈值使用的 token 容量。
// Python: util.py:332 (resolve_context_max) → ContextUtils.resolve_context_max
func ResolveContextMax(ctx any, modelName string) int {
	name := modelName
	fallback := 0
	mapping := map[string]int{}
	if p, ok := ctx.(ContextWindowProvider); ok {
		if name == "" {
			name = p.ModelName()
		}
		fallback = p.ContextWindowTokens()
		mapping = p.ModelContextWindowTokens()
	}
	// Python: ContextUtils.resolve_context_max
	return cecontext.ResolveContextMax(name, fallback, mapping)
}

// ResolveRatioTokenThreshold 把容量比例换算为正数 token 阈值。
// Python: util.py resolve_ratio_token_threshold
func ResolveRatioTokenThreshold(contextMax int, ratio float64) int {
	t := int(float64(contextMax) * ratio)
	if t < 1 {
		return 1
	}
	return t
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countUsageTokensWithTail 返回模型上报的累计基线 + 尾部消息估算之和。
// Python: util.py count_usage_tokens_with_tail
func countUsageTokensWithTail(messages []llm_schema.BaseMessage) (int, bool) {
	base, tail, ok := lastUsageBase(messages)
	if !ok {
		return 0, false
	}
	total := base
	for _, message := range tail {
		total += len3(message)
	}
	return total, true
}

// lastUsageBase 从尾部向前找最后一条携带有效 usage_metadata 的 AssistantMessage，
// 返回 (total_tokens, 其后的尾部消息)。
// Python: util.py _last_usage_base
func lastUsageBase(messages []llm_schema.BaseMessage) (int, []llm_schema.BaseMessage, bool) {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		message := messages[idx]
		if !iface.HasValidUsageMetadata(message) {
			continue
		}
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok || assistant.UsageMetadata == nil || assistant.UsageMetadata.TotalTokens <= 0 {
			continue
		}
		return assistant.UsageMetadata.TotalTokens, messages[idx+1:], true
	}
	return 0, nil, false
}

// len3 单消息 len//3 估算（与本模块兜底系数一致）。
// Python: util.py _len3
func len3(message llm_schema.BaseMessage) int {
	return len([]rune(cesupport.SanitizeContentForText(message.GetContent().String()))) / 3
}

// appendUtilHelpers 以下为 24.15 Task 2 补齐的 util.py 缺失符号（附录 A.2 清单）。
// Python: util.py:73-258 / :410-453

// IsSkillFilePath 判断路径是否为 skill.md 文件
// Python: util.py:73 (is_skill_file_path)
func IsSkillFilePath(filePath string) bool {
	if filePath == "" {
		return false
	}
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	normalized = strings.ToLower(normalized)
	return strings.HasSuffix(normalized, "/skill.md") || strings.HasSuffix(normalized, "skill.md")
}

// ExtractSkillNameFromPath 从 skill.md 路径抽取技能名
// Python: util.py:80 (extract_skill_name_from_path)
func ExtractSkillNameFromPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	normalized := strings.TrimSuffix(strings.ReplaceAll(filePath, "\\", "/"), "/")
	parts := strings.Split(normalized, "/")
	if len(parts) >= 2 && strings.ToLower(parts[len(parts)-1]) == "skill.md" {
		return parts[len(parts)-2]
	}
	return ""
}

// RoundContainsSkillRead 判断消息列表中是否包含读取 skill.md 的 read_file 调用
// Python: util.py:90 (round_contains_skill_read)
func RoundContainsSkillRead(messages []llm_schema.BaseMessage) bool {
	for _, message := range messages {
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, toolCall := range assistant.ToolCalls {
			if toolCall.Name != "read_file" {
				continue
			}
			argumentsText := toolCall.Arguments
			parsed := ParseToolArguments(argumentsText)
			filePath := ExtractArgumentValue(parsed, argumentsText, []string{"file_path"})
			if IsSkillFilePath(filePath) {
				return true
			}
		}
	}
	return false
}

// GroupCompletedAPIRounds 按已完成 API 轮次分组消息
// Python: util.py:106 (group_completed_api_rounds) → session_memory_manager 同名函数
func GroupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][]llm_schema.BaseMessage {
	ranges := ceprocessor.GroupCompletedAPIRounds(messages)
	out := make([][]llm_schema.BaseMessage, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, append([]llm_schema.BaseMessage(nil), messages[r[0]:r[1]]...))
	}
	return out
}

// MessageSignature 消息签名（role + 文本 + tool_call ids）
// Python: util.py:110 (message_signature)
func MessageSignature(message llm_schema.BaseMessage) string {
	toolCallIDs := []string{}
	if assistant, ok := message.(*llm_schema.AssistantMessage); ok {
		for _, toolCall := range assistant.ToolCalls {
			toolCallIDs = append(toolCallIDs, toolCall.ID)
		}
	}
	raw := messageRole(message) + "|" + MessageToText(message) + "|" + strings.Join(toolCallIDs, "|")
	return raw
}

// messageRole 取消息角色
func messageRole(message llm_schema.BaseMessage) string {
	switch message.(type) {
	case *llm_schema.UserMessage:
		return "user"
	case *llm_schema.AssistantMessage:
		return "assistant"
	case *llm_schema.SystemMessage:
		return "system"
	case *llm_schema.ToolMessage:
		return "tool"
	default:
		return ""
	}
}

// ParseToolArguments 解析工具参数 JSON 文本；失败返回空表
// Python: util.py:167 (parse_tool_arguments)
func ParseToolArguments(argumentsText string) map[string]any {
	if argumentsText == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsText), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// ExtractArgumentValue 依次按候选键取字符串参数值；解析失败时正则兜底
// Python: util.py:177 (extract_argument_value)
func ExtractArgumentValue(parsedArguments map[string]any, argumentsText string, keys []string) string {
	for _, key := range keys {
		if value, ok := parsedArguments[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, key := range keys {
		re, err := regexp.Compile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]+)"`)
		if err != nil {
			continue
		}
		if m := re.FindStringSubmatch(argumentsText); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// DescribeToolCall 生成工具调用的可读描述
// Python: util.py:145 (describe_tool_call)
func DescribeToolCall(toolName string, argumentsText string) string {
	parsedArguments := ParseToolArguments(argumentsText)
	switch toolName {
	case "read_file", "write_file", "edit_file":
		filePath := ExtractArgumentValue(parsedArguments, argumentsText, []string{"file_path"})
		if filePath == "" {
			filePath = "[unknown]"
		}
		return toolName + " path=" + filePath
	case "glob":
		pattern := ExtractArgumentValue(parsedArguments, argumentsText, []string{"pattern"})
		path := ExtractArgumentValue(parsedArguments, argumentsText, []string{"path"})
		if pattern == "" {
			pattern = "[unknown]"
		}
		if path == "" {
			path = "."
		}
		return "glob pattern=" + pattern + " path=" + path
	case "grep":
		pattern := ExtractArgumentValue(parsedArguments, argumentsText, []string{"pattern"})
		path := ExtractArgumentValue(parsedArguments, argumentsText, []string{"path", "file_path"})
		if pattern == "" {
			pattern = "[unknown]"
		}
		if path == "" {
			path = "[unknown]"
		}
		return "grep pattern=" + pattern + " path=" + path
	default:
		return toolName + " args=" + argumentsText
	}
}

// FindToolResultText 按 tool_call_id 逆序查找工具结果文本
// Python: util.py:193 (find_tool_result_text)
func FindToolResultText(messages []llm_schema.BaseMessage, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if toolMessage, ok := messages[i].(*llm_schema.ToolMessage); ok && toolMessage.ToolCallID == toolCallID {
			return MessageToText(toolMessage)
		}
	}
	return ""
}

// ExtractToolResultHint 按工具名抽取结果摘要提示（白名单内才处理）
// Python: util.py:205 (extract_tool_result_hint)
func ExtractToolResultHint(toolName string, resultText string, allowedToolNames []string) string {
	if resultText == "" {
		return ""
	}
	allowed := false
	for _, name := range allowedToolNames {
		if name == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	switch toolName {
	case "read_file":
		var parts []string
		if m := extractNumberedOrQuoted(resultText, "file_path", true); m != "" {
			parts = append(parts, "result_path="+m)
		}
		if m := extractNumberedOrQuoted(resultText, "line_count", false); m != "" {
			parts = append(parts, "lines="+m)
		}
		return strings.Join(parts, " ")
	case "glob":
		if m := extractNumberedOrQuoted(resultText, "count", false); m != "" {
			return "matches=" + m
		}
	case "grep":
		if m := extractNumberedOrQuoted(resultText, "count", false); m != "" {
			return "hits=" + m
		}
	case "edit_file":
		if m := extractNumberedOrQuoted(resultText, "replacements", false); m != "" {
			return "replacements=" + m
		}
	case "write_file":
		if m := extractNumberedOrQuoted(resultText, "bytes_written", false); m != "" {
			return "bytes_written=" + m
		}
	}
	return ""
}

// extractNumberedOrQuoted 在结果文本中抽取 `"key":"value"`（字符串值）或 `"key":123`（数值）。
// Python: util.py:215-238 内联的正则查找
func extractNumberedOrQuoted(resultText string, key string, quoted bool) string {
	if quoted {
		re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]+)"`)
		if m := re.FindStringSubmatch(resultText); len(m) > 1 {
			return m[1]
		}
		return ""
	}
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*(\d+)`)
	if m := re.FindStringSubmatch(resultText); len(m) > 1 {
		return m[1]
	}
	return ""
}

// MessageToText 消息内容序列化为文本（清洗图片载荷）
// Python: util.py:242 (message_to_text)
func MessageToText(message llm_schema.BaseMessage) string {
	if message == nil {
		return ""
	}
	content := message.GetContent()
	if content.IsText() {
		// PY：str content 原样返回；Go MessageContent 包装形态需先取 Text
		return cesupport.SanitizeContentForText(content.Text())
	}
	return cesupport.SanitizeContentForText(content)
}

// IsSummaryMessage 判断消息是否为携带指定标记的压缩记忆块
// Python: util.py:247 (is_summary_message)
func IsSummaryMessage(message llm_schema.BaseMessage, summaryMarker string) bool {
	userMessage, ok := message.(*llm_schema.UserMessage)
	if !ok {
		return false
	}
	content := userMessage.GetContent()
	if !content.IsText() {
		// PY 要求 content 为 str（纯文本）；Go 多模态分片形态不视为摘要
		return false
	}
	return strings.HasPrefix(content.Text(), summaryMarker)
}

// CollectSummaryIndices 收集所有携带指定标记的摘要消息索引
// Python: util.py:256 (collect_summary_indices)
func CollectSummaryIndices(messages []llm_schema.BaseMessage, summaryMarker string) []int {
	out := []int{}
	for i, message := range messages {
		if IsSummaryMessage(message, summaryMarker) {
			out = append(out, i)
		}
	}
	return out
}

// FindLastCompletedAPIRoundEndIdx 返回选定范围内最后一个完整 API 轮次的绝对结束索引
// Python: util.py:410 (find_last_completed_api_round_end_idx)
func FindLastCompletedAPIRoundEndIdx(messages []llm_schema.BaseMessage, startIdx int, endIdx int) int {
	if endIdx < startIdx {
		return endIdx
	}
	end := endIdx + 1
	if end > len(messages) {
		end = len(messages)
	}
	candidateMessages := messages[startIdx:end]
	completedRounds := ceprocessor.GroupCompletedAPIRounds(candidateMessages)
	if len(completedRounds) == 0 {
		return startIdx - 1
	}
	completedEnd := completedRounds[len(completedRounds)-1][1]
	return startIdx + completedEnd - 1
}

// IterSummaryMergeRanges 返回可参与二段合并的连续摘要消息区间
// Python: util.py:427 (iter_summary_merge_ranges)
func IterSummaryMergeRanges(messages []llm_schema.BaseMessage, summaryMarker string, minBlocks int) [][2]int {
	var ranges [][2]int
	startIdx := -1
	previousIdx := -1

	for idx, message := range messages {
		if IsSummaryMessage(message, summaryMarker) {
			if startIdx == -1 {
				startIdx = idx
			}
			previousIdx = idx
			continue
		}
		if startIdx != -1 && previousIdx != -1 {
			if previousIdx-startIdx+1 >= minBlocks {
				ranges = append(ranges, [2]int{startIdx, previousIdx})
			}
			startIdx = -1
			previousIdx = -1
		}
	}

	if startIdx != -1 && previousIdx != -1 {
		if previousIdx-startIdx+1 >= minBlocks {
			ranges = append(ranges, [2]int{startIdx, previousIdx})
		}
	}

	return ranges
}

// ───────── 24.15 Task 6：util.py 回注装配符号（附录 A.2 reinjection 半区） ─────────

// ReinjectedStateBuilderSpec 具名回注构建器描述（support 侧载体）
// Python: util.py:66 (ReinjectedStateBuilderSpec)
type ReinjectedStateBuilderSpec struct {
	// Name 构建器名
	Name string
	// Label 展示标签
	Label string
	// Builder 构建函数
	Builder reinjection.ReinjectBuilderFunc
}

// BuildCompressorReinjectedStateMessage 为压缩器装配单条回注状态消息
// Python: util.py:346 (build_compressor_reinjected_state_message)
func BuildCompressorReinjectedStateMessage(
	sourceMessages []llm_schema.BaseMessage,
	messagesToKeep []llm_schema.BaseMessage,
	context any,
	config any,
	builderNames []string,
	sessionState map[string]any,
) *llm_schema.UserMessage {
	if sessionState == nil {
		sessionState = GetSessionStateForReinjection(context)
	}
	maxChars := 12000
	if m, ok := config.(map[string]any); ok {
		if v, ok := m["reinject_max_chars_per_section"].(int); ok && v > 0 {
			maxChars = v
		}
	}
	ctx := &reinjection.ReinjectContext{
		SessionState:   sessionState,
		SourceMessages: sourceMessages,
		MessagesToKeep: messagesToKeep,
		WorkspaceRoot:  GetWorkspaceRootForReinjection(context),
		Config:         config,
		StateMarker:    StateReinjectionMarker,
		Truncate: func(text string) string {
			return TruncateStateText(text, maxChars)
		},
		Context: context,
	}
	return reinjection.BuildSingleReinjectedStateMessage(ctx, builderSpecsToReinject(builderNames), nil)
}

// builderSpecsToReinject 把具名描述转为回注器 spec 列表
// Python: util.py:369 build_single_reinjected_state_message(ctx, builder_names=...)
// （Python 侧由调用方传入注册表 specs；Go 侧经 StateReinjector 注册表解析，
//
//	24.15-a2 偏差：specs 来源从显式入参改为包级注册表，见 base.go 装配段）
func builderSpecsToReinject(builderNames []string) []reinjection.ReinjectBuilderSpec {
	reinjector := reinjection.DefaultStateReinjector()
	specs := reinjector.IterBuilders()
	if builderNames == nil {
		return specs
	}
	active := map[string]bool{}
	for _, name := range builderNames {
		active[name] = true
	}
	out := make([]reinjection.ReinjectBuilderSpec, 0, len(specs))
	for _, spec := range specs {
		if active[spec.Name] {
			out = append(out, spec)
		}
	}
	return out
}

// GetSessionStateForReinjection best-effort 读取会话状态字典
// Python: util.py:372 (get_session_state_for_reinjection)
func GetSessionStateForReinjection(context any) map[string]any {
	type sessionProvider interface {
		GetSessionRef() sessioninterfaces.SessionFacade
	}
	provider, ok := context.(sessionProvider)
	if !ok {
		return map[string]any{}
	}
	session := provider.GetSessionRef()
	if session == nil {
		return map[string]any{}
	}
	state := session.DumpState()
	if state == nil {
		return map[string]any{}
	}
	return state
}

// GetWorkspaceRootForReinjection best-effort 解析回注工作区根
// Python: util.py:389 (get_workspace_root_for_reinjection)
func GetWorkspaceRootForReinjection(context any) string {
	provider, ok := context.(interface{ WorkspaceDir() string })
	if !ok {
		return ""
	}
	return provider.WorkspaceDir()
}

// TruncateStateText 头尾截断长状态文本并带截断标记
// Python: util.py:400 (truncate_state_text)
func TruncateStateText(text string, maxChars int) string {
	textLen := len([]rune(text))
	if maxChars <= 0 || textLen <= maxChars {
		return text
	}
	runes := []rune(text)
	headLen := maxChars / 2
	head := string(runes[:headLen])
	tailStart := textLen - (maxChars - headLen)
	if tailStart < 0 {
		tailStart = 0
	}
	tail := string(runes[tailStart:])
	return head + "\n...[STATE_REINJECTION_TRUNCATED]...\n" + tail
}

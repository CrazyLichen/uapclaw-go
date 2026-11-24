package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConversationSignalDetector 从 Trajectory 或消息列表中提取演化信号。
// 统一在线/离线信号检测接口，支持执行失败、脚本产物、协作信号和
// LLM 辅助用户反馈检测。
// Python: openjiuwen/agent_evolving/signal/from_conv.py ConversationSignalDetector
type ConversationSignalDetector struct {
	// existingSkills 已有技能名称集合，用于 skill_name 解析
	existingSkills map[string]bool
	// llm 可选 LLM 实例，用于被动用户消息检测
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
}

// skillReadEntry 技能读取历史条目。
type skillReadEntry struct {
	msgIdx    int
	skillName string
}

// ConvDetectorOption ConversationSignalDetector 构造选项函数。
type ConvDetectorOption func(*ConversationSignalDetector)

// ──────────────────────────── 枚举 ────────────────────────────

// SignalDetector 向后兼容别名。
// Python: SignalDetector = ConversationSignalDetector
type SignalDetector = ConversationSignalDetector

// ──────────────────────────── 常量 ────────────────────────────

// logComponent from_conv 包日志组件常量
const logComponent = logger.ComponentAgentCore

// userFeedbackPromptCN 中文用户反馈检测提示词。
// Python: _USER_FEEDBACK_PROMPT_CN（原文复刻，不翻译）
const userFeedbackPromptCN = "判断以下用户消息是否包含对当前 skill 的被动纠正或可沉淀的改进反馈。\n" +
	"只有当用户消息明确指出 agent 的理解、步骤、顺序或工具使用需要调整时，" +
	"才认为值得转成演进信号。\n\n" +
	"当前 skill：{skill_name}\n" +
	"最近用户消息：{user_messages}\n\n" +
	`输出 JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// userFeedbackPromptEN 英文用户反馈检测提示词。
// Python: _USER_FEEDBACK_PROMPT_EN（原文复刻，不翻译）
const userFeedbackPromptEN = "Determine whether the following user messages contain passive corrective feedback " +
	"or reusable improvement guidance for the current skill.\n" +
	"Only treat the messages as an evolution signal when the user is clearly correcting " +
	"the agent's understanding, ordering, steps, or tool usage.\n\n" +
	"Current skill: {skill_name}\n" +
	"Recent user messages: {user_messages}\n\n" +
	`Output JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// userFeedbackSkilllessPromptCN 无技能归属的中文用户反馈检测提示词。
// Python: _USER_FEEDBACK_SKILLESS_PROMPT_CN（原文复刻，不翻译）
const userFeedbackSkilllessPromptCN = "判断以下用户消息是否包含对 agent 行为的被动纠正或可沉淀的改进反馈。\n" +
	"只有当用户消息明确指出 agent 的理解、步骤、顺序或工具使用需要调整时，" +
	"才认为值得转成演进信号。\n\n" +
	"最近用户消息：{user_messages}\n\n" +
	`输出 JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// userFeedbackSkilllessPromptEN 无技能归属的英文用户反馈检测提示词。
// Python: _USER_FEEDBACK_SKILLESS_PROMPT_EN（原文复刻，不翻译）
const userFeedbackSkilllessPromptEN = "Determine whether the following user messages contain passive corrective feedback " +
	"or reusable improvement guidance about the agent's behavior.\n" +
	"Only treat the messages as an evolution signal when the user is clearly correcting " +
	"the agent's understanding, ordering, steps, or tool usage.\n\n" +
	"Recent user messages: {user_messages}\n\n" +
	`Output JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// ──────────────────────────── 全局变量 ────────────────────────────

// failureKeywords 匹配执行失败关键词（中英文）。
// Python: _FAILURE_KEYWORDS
// 注意：Python 使用 (?!...) 负向前瞻排除 "error = None"，
// Go 正则不支持该语法，改为匹配 error 后在 matchFailureKeyword 中过滤。
var failureKeywords = regexp.MustCompile(
	`(?i)error|exception|traceback|failed|failure|timeout|timed out` +
		`|errno|connectionerror|oserror|valueerror|typeerror` +
		`|错误|异常|失败|超时` +
		`|no such file|permission denied|access denied` +
		`|command not found|not recognized` +
		`|module not found` +
		`|econnrefused|econnreset|enoent|enotfound` +
		`|npm err!`,
)

var errorEqualsNonePattern = regexp.MustCompile(`(?i)error\s*=\s*None`)

// correctionPatterns 用户纠正模式列表（中英文）。
// Python: _CORRECTION_PATTERNS
var correctionPatterns = []string{
	`不对[，,。!]?`,
	`不是[这那]`,
	`错[了啦]`,
	`应该(是|用|改|换)`,
	`你搞错[了啦]`,
	`这不对`,
	`重新(来|做|执行|尝试)`,
	`你理解错[了啦]`,
	`纠正一下`,
	`我的意思是`,
	`that('s| is) (wrong|incorrect|not right)`,
	`you'?re wrong`,
	`should (be|use|have)`,
	`actually[,，]`,
	`no[,，] (wait|actually)`,
	`correct(ion)?:`,
	`fix(ed)?:`,
}

// correctionPattern 合并后的用户纠正正则。
// Python: _CORRECTION_PATTERN
var correctionPattern = regexp.MustCompile(
	strings.Join(correctionPatterns, "|"),
)

// skillMDPattern 匹配 SKILL.md 路径。
// Python: _SKILL_MD_PATTERN
var skillMDPattern = regexp.MustCompile(`[/\\]+([^/\\]+)[/\\]+SKILL\.md`)

// toolSchemaPattern 匹配工具 schema 输出。
// Python: _TOOL_SCHEMA_PATTERN
var toolSchemaPattern = regexp.MustCompile(`\{'content': '---\\nname: [^\n]+\\ndescription:`)

// dataFetchTools 数据获取工具集合。
// Python: _DATA_FETCH_TOOLS
var dataFetchTools = map[string]bool{
	"mcp_fetch_webpage": true, "fetch_webpage": true, "web_fetch": true,
	"search": true, "web_search": true, "google_search": true, "bing_search": true,
	"view_file": true, "read_file": true, "cat_file": true,
	"list_directory": true, "ls": true, "get_url": true, "curl": true, "wget": true,
}

// codeExecTools 代码执行工具集合。
// Python: _CODE_EXEC_TOOLS
var codeExecTools = map[string]bool{
	"code": true, "bash": true, "execute_python_code": true, "run_python": true,
	"exec_code": true, "execute_code": true, "python_exec": true, "run_code": true,
}

// execContentKeys 可执行内容参数键。
// Python: _EXEC_CONTENT_KEYS
var execContentKeys = []string{
	"code", "code_block", "script", "source", "python_code",
	"command", "cmd", "shell_command",
}

// errorEqualsNoneOnlyStart 匹配以 "\s*=\s*None" 开头的字符串，
// 用于验证 "error" 后面是否跟着 "= None"。
var errorEqualsNoneOnlyStart = regexp.MustCompile(`(?i)^\s*=\s*None`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewConversationSignalDetector 创建 ConversationSignalDetector 实例。
// Python: ConversationSignalDetector(existing_skills)
func NewConversationSignalDetector(opts ...ConvDetectorOption) *ConversationSignalDetector {
	d := &ConversationSignalDetector{
		existingSkills: map[string]bool{},
		language:       "cn",
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// WithExistingSkills 设置已有技能集合。
func WithExistingSkills(skills map[string]bool) ConvDetectorOption {
	return func(d *ConversationSignalDetector) {
		if skills != nil {
			d.existingSkills = skills
		}
	}
}

// BindLLM 绑定可选 LLM 上下文，用于被动用户消息检测。
// 返回自身以支持链式调用。
// Python: ConversationSignalDetector.bind_llm(llm, model, language)
func (d *ConversationSignalDetector) BindLLM(llm *llm.Model, model, language string) *ConversationSignalDetector {
	d.llm = llm
	d.model = model
	if language != "" {
		d.language = language
	}
	return d
}

// Detect 从消息列表中检测演化信号，返回去重后的 EvolutionSignal 列表。
// Python: ConversationSignalDetector.detect(trajectory_or_messages)
func (d *ConversationSignalDetector) Detect(msgs []map[string]any) []*EvolutionSignal {
	return d.detectMessageSignals(msgs, nil)
}

// detectMessageSignals 消息信号检测统一中间层：异常吞→空列表，
// 按 enabled_signal_types 过滤（缺省 {"execution_failure","script_artifact"}）后去重。
// Python: _detect_message_signals（from_conv.py:259-284）。
func (d *ConversationSignalDetector) detectMessageSignals(input any, signalTypes map[string]bool) (result []*EvolutionSignal) {
	defer func() {
		// Python: except Exception → logger.warning + return []
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).
				Str("method", "detectMessageSignals").
				Any("panic", rec).
				Msg("[ConversationSignalDetector] message signal detection failed")
			result = []*EvolutionSignal{}
		}
	}()
	var messages []map[string]any
	switch v := input.(type) {
	case []map[string]any:
		messages = v
	case *trajectory.Trajectory:
		if v == nil {
			return []*EvolutionSignal{}
		}
		// Python: trajectory_to_messages(input_data)（messages.py 投影）
		converted, err := trajectory.TrajectoryToMessages(v, nil, false)
		if err != nil {
			logger.Warn(logComponent).
				Str("method", "detectMessageSignals").
				Err(err).
				Msg("[ConversationSignalDetector] message signal detection failed")
			return []*EvolutionSignal{}
		}
		messages = converted
	case nil:
		return []*EvolutionSignal{}
	default:
		return []*EvolutionSignal{}
	}
	signals := d.detectFromMessages(messages)
	// Python: enabled_signal_types = signal_types or {"execution_failure", "script_artifact"}
	enabled := signalTypes
	if enabled == nil {
		enabled = map[string]bool{"execution_failure": true, "script_artifact": true}
	}
	filtered := make([]*EvolutionSignal, 0, len(signals))
	for _, signal := range signals {
		if enabled[signal.SignalType] {
			filtered = append(filtered, signal)
		}
	}
	return d.deduplicate(filtered)
}

// ParseMessages 将 BaseMessage 列表归一化为 map 列表（含 tool_calls）。
// 对齐 Python: SkillEvolutionRail._parse_messages(messages)
// （agent_evolving 基础设施层等价实现——openjiuwen 的 slash 层
// _collect_messages_for_evolve 收集会话消息后经此转换供 detect 使用）。
// dict 消息直接透传；对象消息按 role/content/tool_calls 归一化。
func ParseMessages(messages []llmschema.BaseMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, llmschema.BaseMessageToMap(msg))
	}
	return result
}

// DetectTrajectorySignals 使用常规对话规则检测被动轨迹信号。
// Python: ConversationSignalDetector.detect_trajectory_signals(trajectory, messages)
func (d *ConversationSignalDetector) DetectTrajectorySignals(
	traj *trajectory.Trajectory,
	messages []map[string]any,
	signalTypes ...map[string]bool,
) []*EvolutionSignal {
	// Python: input_data = messages if messages is not None else trajectory；均空返回 []
	var input any
	if messages != nil {
		input = messages
	} else if traj != nil {
		input = traj
	} else {
		return nil
	}
	var types map[string]bool
	if len(signalTypes) > 0 {
		types = signalTypes[0]
	}
	return d.detectMessageSignals(input, types)
}

// DetectUserMessageFeedback 使用 LLM 判断用户消息是否为被动纠正，返回 user_correction 信号。
// Python: ConversationSignalDetector.detect_user_message_feedback(trajectory_or_messages)
func (d *ConversationSignalDetector) DetectUserMessageFeedback(
	ctx context.Context,
	msgs []map[string]any,
) []*EvolutionSignal {
	signals, _ := d.DetectUserIntent(ctx, msgs)
	var result []*EvolutionSignal
	for _, sig := range signals {
		result = append(result, MakeEvolutionSignal(
			"user_correction",
			"Examples",
			sig.Excerpt,
			WithSkillName(stringPtrValue(sig.SkillName)),
			WithContext(sig.Context),
		))
	}
	return result
}

// DetectUserIntent 使用 LLM 判断被动用户消息，转换为标准信号。
// Python: ConversationSignalDetector.detect_user_intent(trajectory_or_messages)
func (d *ConversationSignalDetector) DetectUserIntent(
	ctx context.Context,
	msgs []map[string]any,
	allowSkillless ...bool,
) ([]*EvolutionSignal, error) {
	// 提取最近 5 条用户消息
	var userMessages []string
	for _, msg := range msgs {
		role := getField[string](msg, "role", "")
		content := strings.TrimSpace(getField[string](msg, "content", ""))
		if role == "user" && content != "" {
			userMessages = append(userMessages, content)
		}
	}
	if len(userMessages) > 5 {
		userMessages = userMessages[len(userMessages)-5:]
	}
	if len(userMessages) == 0 {
		return nil, nil
	}

	// Python: allow_skillless=False 缺省（技能演化口径）；TTSE 传 True 走
	// 无归属检测路径。
	skillName := d.inferSkillFromMessages(msgs)
	if skillName == "" {
		if len(allowSkillless) == 0 || !allowSkillless[0] {
			return nil, nil
		}
		return d.detectSkilllessUserFeedback(ctx, userMessages)
	}

	// 无 LLM 时走 fallback 正则
	if d.llm == nil || d.model == "" {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	promptTemplate := userFeedbackPromptCN
	if d.language != "cn" {
		promptTemplate = userFeedbackPromptEN
	}
	userText := strings.Join(userMessages, "\n")
	if len(userText) > 2000 {
		userText = userText[:2000]
	}
	prompt := strings.ReplaceAll(promptTemplate, "{skill_name}", skillName)
	prompt = strings.ReplaceAll(prompt, "{user_messages}", userText)

	// 直接调用 Model.Invoke，对齐 Python
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	llmMessages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
	resp, err := d.llm.Invoke(timeoutCtx, llmMessages, model_clients.WithInvokeModel(d.model))
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectUserIntent").
			Str("error", err.Error()).
			Msg("[ConversationSignalDetector] user feedback detection failed")
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	raw := responseToText(resp)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}
	if _, ok := parsed["is_feedback"]; !ok {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	isFeedback := false
	switch v := parsed["is_feedback"].(type) {
	case bool:
		isFeedback = v
	default:
		isFeedback = fmt.Sprintf("%v", v) == "true"
	}

	if !isFeedback {
		return nil, nil
	}

	excerpt := strings.TrimSpace(fmt.Sprintf("%v", parsed["excerpt"]))
	if excerpt == "" || excerpt == "<nil>" {
		excerpt = userMessages[len(userMessages)-1]
	}
	return []*EvolutionSignal{d.makeUserFeedbackSignal(excerpt, skillName)}, nil
}

// detectSkilllessUserFeedback 无技能归属的纠正反馈检测（TTSE / 技能无关口径）。
// Python: _detect_skillless_user_feedback（from_conv.py:390-428）——LLM 判定
// 失败/不可解析时回退 _skillless_pattern_fallback。
func (d *ConversationSignalDetector) detectSkilllessUserFeedback(ctx context.Context, userMessages []string) ([]*EvolutionSignal, error) {
	text := ""
	if len(userMessages) > 0 {
		text = strings.TrimSpace(userMessages[len(userMessages)-1])
	}
	if text == "" {
		return nil, nil
	}
	// Python: if self._llm is None or not self._model: return self._skillless_pattern_fallback(text)
	if d.llm == nil || d.model == "" {
		return d.skilllessPatternFallback(text), nil
	}

	promptTemplate := userFeedbackSkilllessPromptCN
	if d.language != "cn" {
		promptTemplate = userFeedbackSkilllessPromptEN
	}
	userText := strings.Join(userMessages, "\n")
	if len(userText) > 2000 {
		userText = userText[:2000]
	}
	prompt := strings.ReplaceAll(promptTemplate, "{user_messages}", userText)

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	llmMessages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
	resp, err := d.llm.Invoke(timeoutCtx, llmMessages, model_clients.WithInvokeModel(d.model))
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "detectSkilllessUserFeedback").
			Str("error", err.Error()).
			Msg("[ConversationSignalDetector] skillless user feedback detection failed")
		return d.skilllessPatternFallback(text), nil
	}

	raw := responseToText(resp)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return d.skilllessPatternFallback(text), nil
	}
	if parsed == nil {
		return d.skilllessPatternFallback(text), nil
	}
	if !boolOfAny(parsed["is_feedback"]) {
		return nil, nil
	}
	excerpt := strings.TrimSpace(fmt.Sprintf("%v", parsed["excerpt"]))
	if excerpt == "" || excerpt == "<nil>" {
		excerpt = text
	}
	return []*EvolutionSignal{d.makeUserFeedbackSignal(excerpt, "")}, nil
}

// skilllessPatternFallback 仅在纠正线索命中时发出无归属反馈信号。
// Python: _skillless_pattern_fallback（from_conv.py:430-434）。
func (d *ConversationSignalDetector) skilllessPatternFallback(text string) []*EvolutionSignal {
	if text == "" || correctionPattern.MatchString(text) == false {
		return nil
	}
	return []*EvolutionSignal{d.makeUserFeedbackSignal(text, "")}
}

// boolOfAny 归一 is_feedback 布尔（bool 原生 / 字符串 "true"）。
func boolOfAny(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	default:
		return fmt.Sprintf("%v", value) == "true"
	}
}

// ConvertTrajectoryToMessages 把轨迹投影为消息列表格式（TTSE/调用方兼容包装）。
// Python: ConversationSignalDetector.convert_trajectory_to_messages(trajectory)
// —— 上游实现即 trajectory_to_messages(trajectory) 一行委托（messages.py 投影）。
func (d *ConversationSignalDetector) ConvertTrajectoryToMessages(traj *trajectory.Trajectory) []map[string]any {
	messages, err := trajectory.TrajectoryToMessages(traj, nil, false)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[ConversationSignalDetector] trajectory message projection failed")
		return nil
	}
	return messages
}

// MatchFailureKeywords 检查 content 是否包含失败关键词。
// 导出此函数供 sharing 包的 ShareStager 使用，
// 因为 Go 无法跨包访问非导出变量 failureKeywords。
// Python: openjiuwen/agent_evolving/signal/from_conv._FAILURE_KEYWORDS.search(content)
func MatchFailureKeywords(content string) bool {
	return failureKeywords.MatchString(content)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// findFailureKeywordIndex 返回内容中失败关键词的位置。
// Python: _FAILURE_KEYWORDS 中 error(?!\s*=\s*None) 的负向前瞻语义：
// 如果唯一匹配是 "error = None" 中的 "error"，返回 nil。
func findFailureKeywordIndex(content string) []int {
	// 快速路径：如果完全不包含失败关键词
	if !failureKeywords.MatchString(content) {
		return nil
	}
	// 如果包含 "error = None"，需要逐个匹配验证
	if errorEqualsNonePattern.MatchString(content) {
		// 找到所有失败关键词匹配，排除 "error = None" 中的 "error"
		for _, loc := range failureKeywords.FindAllStringIndex(content, -1) {
			matched := content[loc[0]:loc[1]]
			if strings.EqualFold(matched, "error") {
				// 检查后面是否跟着 " = None"
				after := content[loc[1]:]
				if errorEqualsNoneOnlyStart.MatchString(after) {
					continue // 跳过 "error = None"
				}
			}
			return loc
		}
		return nil
	}
	return failureKeywords.FindStringIndex(content)
}

// getField 泛型字典字段读取，从 map[string]any 中读取指定键的值。
// 如果键不存在或值为 nil，返回 defaultVal。
// Python: _get_field(obj, key, default)
func getField[T any](m map[string]any, key string, defaultVal T) T {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok || v == nil {
		return defaultVal
	}
	if typed, ok := v.(T); ok {
		return typed
	}
	return defaultVal
}

// getToolCalls 从消息中提取 tool_calls 列表，统一返回 []map[string]any。
// 消息中的 tool_calls 可能是 []any（JSON 反序列化）或 []map[string]any（手动构造）。
func getToolCalls(m map[string]any) []map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m["tool_calls"]
	if !ok || v == nil {
		return nil
	}

	// 直接是 []map[string]any
	if tcSlice, ok := v.([]map[string]any); ok {
		return tcSlice
	}

	// JSON 反序列化的 []any，每个元素是 map[string]any
	if anySlice, ok := v.([]any); ok {
		result := make([]map[string]any, 0, len(anySlice))
		for _, item := range anySlice {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}

	return nil
}

// argsToJSON 将 map[string]any 转换为 JSON 字符串，用于 extractToMember/extractTaskID。
func argsToJSON(args map[string]any) string {
	if args == nil {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return string(data)
}

// responseToText 将 LLM 响应转换为纯文本。
// Python: _response_to_text(response)
func responseToText(resp *llmschema.AssistantMessage) string {
	if resp == nil {
		return ""
	}
	return resp.GetContent().String()
}

// extractAroundMatch 返回匹配位置前后的摘录。
// Python: _extract_around_match(content, match, before, after)
func extractAroundMatch(content string, matchStart, matchEnd, before, after int) string {
	start := matchStart - before
	if start < 0 {
		start = 0
	}
	end := matchEnd + after
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}

// detectFromMessages 扫描消息列表，返回检测到的信号。
// Python: ConversationSignalDetector._detect_from_messages(messages)
func (d *ConversationSignalDetector) detectFromMessages(messages []map[string]any) []*EvolutionSignal {
	var signals []*EvolutionSignal
	var skillReadHistory []skillReadEntry
	pendingScripts := map[string]string{}
	toolCallIDToName := map[string]string{}

	for msgIdx, msg := range messages {
		role := getField[string](msg, "role", "")
		content := getField[string](msg, "content", "")

		if role == "assistant" {
			if tcSlice := getToolCalls(msg); len(tcSlice) > 0 {
				if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
					skillReadHistory = append(skillReadHistory, skillReadEntry{msgIdx, detected})
				}
				for _, tc := range tcSlice {
					tcID := getField[string](tc, "id", "")
					tcName := getField[string](tc, "name", "")
					if tcID != "" && tcName != "" {
						toolCallIDToName[tcID] = tcName
					}
					if codeExecTools[strings.ToLower(tcName)] {
						code := d.extractCodeFromArgs(tc)
						if code != "" && tcID != "" {
							pendingScripts[tcID] = code
						}
					}
				}
			}
		}

		if role == "tool" || role == "function" {
			toolName := ""
			if v, ok := msg["name"]; ok {
				toolName = fmt.Sprintf("%v", v)
			}
			if toolName == "" {
				if v, ok := msg["tool_name"]; ok {
					toolName = fmt.Sprintf("%v", v)
				}
			}
			toolCallID := ""
			if v, ok := msg["tool_call_id"]; ok {
				toolCallID = fmt.Sprintf("%v", v)
			}
			if toolName == "" && toolCallID != "" {
				toolName = toolCallIDToName[toolCallID]
			}

			activeSkill := d.resolveActiveSkill(msgIdx, skillReadHistory)

			// 脚本产物检测
			if toolCallID != "" {
				if script, exists := pendingScripts[toolCallID]; exists {
					hasFailure := content != "" && findFailureKeywordIndex(content) != nil
					if !hasFailure {
						signals = append(signals, MakeEvolutionSignal(
							"script_artifact", "Scripts", truncateString(script, 600),
							WithToolName(toolName),
							WithSkillName(activeSkill),
							WithSource("passive_conversation"),
						))
					}
					delete(pendingScripts, toolCallID)
				}
			}

			// 跳过数据获取工具
			if dataFetchTools[strings.ToLower(toolName)] {
				continue
			}

			// 执行失败检测
			if content != "" {
				match := findFailureKeywordIndex(content)
				if match != nil {
					// 跳过工具 schema 输出
					if toolSchemaPattern.MatchString(content) {
						continue
					}
					excerpt := extractAroundMatch(content, match[0], match[1], 300, 300)
					opts := []SignalOption{
						WithSkillName(activeSkill),
						WithSource("passive_conversation"),
					}
					if toolName != "" {
						opts = append(opts, WithToolName(toolName))
					}
					signals = append(signals, MakeEvolutionSignal(
						"execution_failure", "Troubleshooting", excerpt, opts...,
					))
				}
			}
		}
	}
	return signals
}

// resolveActiveSkill 返回 msgIdx 处或之前最近读取的技能名称。
// Python: ConversationSignalDetector._resolve_active_skill(msg_idx, skill_read_history)
func (d *ConversationSignalDetector) resolveActiveSkill(msgIdx int, history []skillReadEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].msgIdx <= msgIdx {
			return history[i].skillName
		}
	}
	return ""
}

// detectSkillFromToolCalls 从工具调用中检测 SKILL.md 读取的技能名称。
// Python: ConversationSignalDetector._detect_skill_from_tool_calls(tool_calls)
func (d *ConversationSignalDetector) detectSkillFromToolCalls(toolCalls []map[string]any) string {
	for _, tc := range toolCalls {
		// 12-04：name/arguments 兼容 OpenAI 嵌套 function 形态（对齐 upstream
		// tool_call_name(tc)/tool_call_arguments(tc) —— trajectory/messages.py 投影
		// 输出 normalize 后即嵌套形态）。
		name := strings.ToLower(toolCallNameOf(tc))
		arguments := toolCallArgumentsOf(tc)

		var skillName string

		matched := skillMDPattern.FindStringSubmatch(arguments)
		if len(matched) >= 2 && d.isSkillMDReadTool(name) {
			skillName = matched[1]
		} else if name == "skill_tool" {
			// 尝试解析 arguments（12-04：经 toolCallArgumentsOf 兼容嵌套形态）
			rawArgs := any(arguments)
			if rawArgs != nil {
				var argsDict map[string]any
				if argsStr, ok := rawArgs.(string); ok {
					if err := json.Unmarshal([]byte(argsStr), &argsDict); err != nil {
						logger.Debug(logComponent).Err(err).Str("args", argsStr).
							Msg("[ConversationSignalDetector] failed to parse skill_tool arguments")
					}
				} else if m, ok := rawArgs.(map[string]any); ok {
					argsDict = m
				}
				if argsDict != nil {
					if v, ok := argsDict["skill_name"]; ok && v != nil {
						skillName = fmt.Sprintf("%v", v)
					}
				}
			}
		}

		if skillName != "" && d.isExistingSkill(skillName) {
			return skillName
		}
	}
	return ""
}

// isExistingSkill 检查技能名称是否在已有技能集合中。
// Python: ConversationSignalDetector._is_existing_skill(skill_name)
func (d *ConversationSignalDetector) isExistingSkill(skillName string) bool {
	if len(d.existingSkills) == 0 {
		return true
	}
	return d.existingSkills[skillName]
}

// isSkillMDReadTool 判断工具是否为文件读取类工具。
// Python: ConversationSignalDetector._is_skill_md_read_tool(name)
func (d *ConversationSignalDetector) isSkillMDReadTool(name string) bool {
	if name == "" {
		return true
	}
	return strings.Contains(name, "file") || strings.Contains(name, "read")
}

// inferSkillFromMessages 从消息列表推断当前活跃技能。
// Python: ConversationSignalDetector._infer_skill_from_messages(messages)
func (d *ConversationSignalDetector) inferSkillFromMessages(messages []map[string]any) string {
	var skillReadHistory []skillReadEntry
	for msgIdx, msg := range messages {
		role := getField[string](msg, "role", "")
		if role == "assistant" {
			if tcSlice := getToolCalls(msg); len(tcSlice) > 0 {
				if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
					skillReadHistory = append(skillReadHistory, skillReadEntry{msgIdx, detected})
				}
			}
		}
	}
	return d.resolveActiveSkill(len(messages), skillReadHistory)
}

// extractCodeFromArgs 从代码执行工具调用中提取内联代码或命令内容。
// Python: ConversationSignalDetector._extract_code_from_args(tool_call)
func (d *ConversationSignalDetector) extractCodeFromArgs(toolCall map[string]any) string {
	rawArgs := toolCall["arguments"]
	if rawArgs == nil {
		return ""
	}

	var argsDict map[string]any
	switch v := rawArgs.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &argsDict); err != nil {
			return ""
		}
	case map[string]any:
		argsDict = v
	default:
		return ""
	}

	if argsDict == nil {
		return ""
	}
	for _, key := range execContentKeys {
		if val, exists := argsDict[key]; exists {
			if s, ok := val.(string); ok && len(strings.TrimSpace(s)) > 20 {
				return s
			}
		}
	}
	return ""
}

// fallbackUserFeedbackSignals 正则 fallback 检测用户纠正。
// Python: ConversationSignalDetector._fallback_user_feedback_signals(user_messages, skill_name)
func (d *ConversationSignalDetector) fallbackUserFeedbackSignals(userMessages []string, skillName string) []*EvolutionSignal {
	for i := len(userMessages) - 1; i >= 0; i-- {
		if correctionPattern.MatchString(userMessages[i]) {
			return []*EvolutionSignal{d.makeUserFeedbackSignal(userMessages[i], skillName)}
		}
	}
	return nil
}

// makeUserFeedbackSignal 构建用户反馈信号。
// Python: ConversationSignalDetector._make_user_feedback_signal(excerpt, skill_name)
func (d *ConversationSignalDetector) makeUserFeedbackSignal(excerpt, skillName string) *EvolutionSignal {
	opts := []SignalOption{WithSource("passive_conversation")}
	if skillName != "" {
		// Python: skill_name=None（无归属路径）时不携带技能名
		opts = append(opts, WithSkillName(skillName))
	}
	return MakeEvolutionSignal(
		schema.UserIntentSignal,
		"Instructions",
		truncateString(excerpt, 600),
		opts...,
	)
}

// 【12-04 迁移注记】Python: ConversationSignalDetector._detect_collaboration_signals /
// _is_team_member_context / _resolve_active_skill_for_step / _extract_to_member /
// _extract_task_id 已在上游 HEAD（5a35c39f，bd05f826 同期重构）删除——上游当前
// 信号面只有消息驱动检测（detect/detect_trajectory_signals/detect_user_intent），
// 协作信号无任何生产消费方。Go 侧同批删除（规则 8；grep collaboration_send/
// passive_collaboration 归零）。

// deduplicate 基于 fingerprint 去重信号列表。
// Python: ConversationSignalDetector._deduplicate(signals)
func (d *ConversationSignalDetector) deduplicate(signals []*EvolutionSignal) []*EvolutionSignal {
	seen := map[[4]string]bool{}
	var deduped []*EvolutionSignal
	for _, sig := range signals {
		key := MakeSignalFingerprint(sig)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, sig)
	}
	return deduped
}

// toolCallNameOf 兼容扁平/OpenAI 嵌套形态读取工具名。
// Python: trajectory/messages.py tool_call_name。
func toolCallNameOf(tc map[string]any) string {
	if v, ok := tc["name"].(string); ok && v != "" {
		return v
	}
	if fn, ok := tc["function"].(map[string]any); ok {
		if v, ok := fn["name"].(string); ok {
			return v
		}
	}
	return ""
}

// toolCallArgumentsOf 兼容扁平/OpenAI 嵌套形态读取工具参数。
// Python: trajectory/messages.py tool_call_arguments。
func toolCallArgumentsOf(tc map[string]any) string {
	if v, ok := tc["arguments"].(string); ok && v != "" {
		return v
	}
	if fn, ok := tc["function"].(map[string]any); ok {
		if v, ok := fn["arguments"].(string); ok {
			return v
		}
	}
	if v := tc["arguments"]; v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// GetToolCallsOf 导出消息 tool_calls 读取（rails 调试/消费面）。
func GetToolCallsOf(m map[string]any) []map[string]any { return getToolCalls(m) }

package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConversationSignalDetector 从 Trajectory 或消息列表中提取演化信号。
//
// 统一在线/离线信号检测接口，支持执行失败、脚本产物、协作信号和
// LLM 辅助用户反馈检测。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_conv.py ConversationSignalDetector
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

// ConvDetectorOption ConversationSignalDetector 构造选项函数。
type ConvDetectorOption func(*ConversationSignalDetector)

// skillReadEntry 技能读取历史条目。
type skillReadEntry struct {
	msgIdx    int
	skillName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// SignalDetector 向后兼容别名。
//
// 对应 Python: SignalDetector = ConversationSignalDetector
type SignalDetector = ConversationSignalDetector

// ──────────────────────────── 常量 ────────────────────────────

// logComponent from_conv 包日志组件常量
const logComponent = logger.ComponentAgentCore

// userFeedbackPromptCN 中文用户反馈检测提示词。
//
// 对应 Python: _USER_FEEDBACK_PROMPT_CN（原文复刻，不翻译）
const userFeedbackPromptCN = "判断以下用户消息是否包含对当前 skill 的被动纠正或可沉淀的改进反馈。\n" +
	"只有当用户消息明确指出 agent 的理解、步骤、顺序或工具使用需要调整时，" +
	"才认为值得转成演进信号。\n\n" +
	"当前 skill：{skill_name}\n" +
	"最近用户消息：{user_messages}\n\n" +
	`输出 JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// userFeedbackPromptEN 英文用户反馈检测提示词。
//
// 对应 Python: _USER_FEEDBACK_PROMPT_EN（原文复刻，不翻译）
const userFeedbackPromptEN = "Determine whether the following user messages contain passive corrective feedback " +
	"or reusable improvement guidance for the current skill.\n" +
	"Only treat the messages as an evolution signal when the user is clearly correcting " +
	"the agent's understanding, ordering, steps, or tool usage.\n\n" +
	"Current skill: {skill_name}\n" +
	"Recent user messages: {user_messages}\n\n" +
	`Output JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n"

// ──────────────────────────── 全局变量 ────────────────────────────

// failureKeywords 匹配执行失败关键词（中英文）。
//
// 对应 Python: _FAILURE_KEYWORDS
// 注意：Python 使用 (?!...) 负向前瞻排除 "error = None"，
// Go regexp 不支持该语法，改为匹配 error 后在 matchFailureKeyword 中过滤。
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

// errorEqualsNonePattern 匹配 "error = None"（Python 负向前瞻的替代方案）。
var errorEqualsNonePattern = regexp.MustCompile(`(?i)error\s*=\s*None`)

// correctionPatterns 用户纠正模式列表（中英文）。
//
// 对应 Python: _CORRECTION_PATTERNS
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
//
// 对应 Python: _CORRECTION_PATTERN
var correctionPattern = regexp.MustCompile(
	strings.Join(correctionPatterns, "|"),
)

// skillMDPattern 匹配 SKILL.md 路径。
//
// 对应 Python: _SKILL_MD_PATTERN
var skillMDPattern = regexp.MustCompile(`[/\\]+([^/\\]+)[/\\]+SKILL\.md`)

// toolSchemaPattern 匹配工具 schema 输出。
//
// 对应 Python: _TOOL_SCHEMA_PATTERN
var toolSchemaPattern = regexp.MustCompile(`\{'content': '---\\nname: [^\n]+\\ndescription:`)

// dataFetchTools 数据获取工具集合。
//
// 对应 Python: _DATA_FETCH_TOOLS
var dataFetchTools = map[string]bool{
	"mcp_fetch_webpage": true, "fetch_webpage": true, "web_fetch": true,
	"search": true, "web_search": true, "google_search": true, "bing_search": true,
	"view_file": true, "read_file": true, "cat_file": true,
	"list_directory": true, "ls": true, "get_url": true, "curl": true, "wget": true,
}

// codeExecTools 代码执行工具集合。
//
// 对应 Python: _CODE_EXEC_TOOLS
var codeExecTools = map[string]bool{
	"code": true, "bash": true, "execute_python_code": true, "run_python": true,
	"exec_code": true, "execute_code": true, "python_exec": true, "run_code": true,
}

// execContentKeys 可执行内容参数键。
//
// 对应 Python: _EXEC_CONTENT_KEYS
var execContentKeys = []string{
	"code", "code_block", "script", "source", "python_code",
	"command", "cmd", "shell_command",
}

// collaborationSignalTypes 协作信号类型集合。
//
// 对应 Python: _COLLABORATION_SIGNAL_TYPES
var collaborationSignalTypes = map[string]bool{
	"collaboration_send": true, "collaboration_claim": true,
	"collaboration_view": true, "collaboration_receive": true,
	"collaboration_failure": true,
}

// collaborationFailurePattern 协作失败匹配正则。
//
// 对应 Python: _COLLABORATION_FAILURE_PATTERN
var collaborationFailurePattern = regexp.MustCompile(
	`member.*failed|member.*error|member.*timeout` +
		`|invoke.*exception|spawn.*failed` +
		`|task.*error|task.*timeout` +
		`|collaboration.*failed` +
		`|协作.*失败|成员.*异常|任务.*超时`,
)

// errorEqualsNoneOnlyStart 匹配以 "\s*=\s*None" 开头的字符串，
// 用于验证 "error" 后面是否跟着 "= None"。
var errorEqualsNoneOnlyStart = regexp.MustCompile(`(?i)^\s*=\s*None`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewConversationSignalDetector 创建 ConversationSignalDetector 实例。
//
// 对应 Python: ConversationSignalDetector(existing_skills)
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
//
// 对应 Python: ConversationSignalDetector.bind_llm(llm, model, language)
func (d *ConversationSignalDetector) BindLLM(llm *llm.Model, model, language string) *ConversationSignalDetector {
	d.llm = llm
	d.model = model
	if language != "" {
		d.language = language
	}
	return d
}

// Detect 从消息列表中检测演化信号，返回去重后的 EvolutionSignal 列表。
//
// 对应 Python: ConversationSignalDetector.detect(trajectory_or_messages)
func (d *ConversationSignalDetector) Detect(msgs []map[string]any) []*EvolutionSignal {
	signals := d.detectFromMessages(msgs)
	return d.deduplicate(signals)
}

// DetectTrajectorySignals 使用常规对话规则检测被动轨迹信号。
//
// 对应 Python: ConversationSignalDetector.detect_trajectory_signals(trajectory, messages)
func (d *ConversationSignalDetector) DetectTrajectorySignals(
	traj *trajectory.Trajectory,
	messages []map[string]any,
) []*EvolutionSignal {
	if messages != nil {
		signals := d.detectFromMessages(messages)
		if traj != nil {
			signals = append(signals, d.detectCollaborationSignals(traj)...)
		}
		return d.deduplicate(signals)
	}
	if traj == nil {
		return nil
	}
	// 无消息时，从轨迹转换
	converted := d.ConvertTrajectoryToMessages(traj)
	signals := d.detectFromMessages(converted)
	signals = append(signals, d.detectCollaborationSignals(traj)...)
	return d.deduplicate(signals)
}

// DetectUserMessageFeedback 使用 LLM 判断用户消息是否为被动纠正，返回 user_correction 信号。
//
// 对应 Python: ConversationSignalDetector.detect_user_message_feedback(trajectory_or_messages)
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
//
// 对应 Python: ConversationSignalDetector.detect_user_intent(trajectory_or_messages)
func (d *ConversationSignalDetector) DetectUserIntent(
	ctx context.Context,
	msgs []map[string]any,
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

	skillName := d.inferSkillFromMessages(msgs)
	if skillName == "" {
		return nil, nil
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

// ConvertTrajectoryToMessages 将 Trajectory.steps 转换为消息列表格式。
//
// 消息格式与 Detect() 期望的格式匹配：
//   - LLM 步骤：从 LLMCallDetail 提取 messages（含 tool_calls）
//   - Tool 步骤：从 ToolCallDetail.call_result 提取工具结果
//
// 对应 Python: ConversationSignalDetector.convert_trajectory_to_messages(trajectory)
func (d *ConversationSignalDetector) ConvertTrajectoryToMessages(traj *trajectory.Trajectory) []map[string]any {
	var messages []map[string]any
	toolCallIDToName := map[string]string{}

	for _, step := range traj.Steps {
		if step.Kind == trajectory.StepKindLLM {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			for _, msg := range llmDetail.Messages {
				messages = append(messages, msg)
				if tcSlice := getToolCalls(msg); len(tcSlice) > 0 {
					for _, tc := range tcSlice {
						tcID := getField[string](tc, "id", "")
						tcName := getField[string](tc, "name", "")
						if tcID != "" && tcName != "" {
							toolCallIDToName[tcID] = tcName
						}
					}
				}
			}
		} else if step.Kind == trajectory.StepKindTool {
			toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
			if !ok {
				continue
			}
			toolName := toolDetail.ToolName
			toolCallID := toolDetail.ToolCallID
			if toolCallID == "" && step.Meta != nil {
				if v, ok := step.Meta["tool_call_id"]; ok {
					toolCallID = fmt.Sprintf("%v", v)
				}
			}

			if toolName == "" && toolCallID != "" {
				toolName = toolCallIDToName[toolCallID]
			}

			resultContent := ""
			if toolDetail.CallResult != nil {
				resultContent = fmt.Sprintf("%v", toolDetail.CallResult)
			}

			toolMsg := map[string]any{
				"role":    "tool",
				"content": resultContent,
			}
			if toolCallID != "" {
				toolMsg["tool_call_id"] = toolCallID
			}
			if toolName != "" {
				toolMsg["name"] = toolName
			}

			messages = append(messages, toolMsg)
		}
	}

	return messages
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchFailureKeyword 判断内容是否包含失败关键词。
// 对齐 Python _FAILURE_KEYWORDS 中 error(?!\s*=\s*None) 的负向前瞻语义：
// 匹配 "error" 但排除 "error = None" 的情况。
func matchFailureKeyword(content string) bool {
	return failureKeywords.MatchString(content) && !errorEqualsNonePattern.MatchString(content)
}

// findFailureKeywordIndex 返回内容中失败关键词的位置。
// 对齐 Python _FAILURE_KEYWORDS 中 error(?!\s*=\s*None) 的负向前瞻语义：
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
//
// 对应 Python: _get_field(obj, key, default)
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
//
// 对应 Python: _response_to_text(response)
func responseToText(resp *llmschema.AssistantMessage) string {
	if resp == nil {
		return ""
	}
	return resp.GetContent().String()
}

// extractAroundMatch 返回匹配位置前后的摘录。
//
// 对应 Python: _extract_around_match(content, match, before, after)
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
//
// 对应 Python: ConversationSignalDetector._detect_from_messages(messages)
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
					hasFailure := content != "" && matchFailureKeyword(content)
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
//
// 对应 Python: ConversationSignalDetector._resolve_active_skill(msg_idx, skill_read_history)
func (d *ConversationSignalDetector) resolveActiveSkill(msgIdx int, history []skillReadEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].msgIdx <= msgIdx {
			return history[i].skillName
		}
	}
	return ""
}

// detectSkillFromToolCalls 从工具调用中检测 SKILL.md 读取的技能名称。
//
// 对应 Python: ConversationSignalDetector._detect_skill_from_tool_calls(tool_calls)
func (d *ConversationSignalDetector) detectSkillFromToolCalls(toolCalls []map[string]any) string {
	for _, tc := range toolCalls {
		name := strings.ToLower(getField[string](tc, "name", ""))
		arguments := getField[string](tc, "arguments", "")

		var skillName string

		matched := skillMDPattern.FindStringSubmatch(arguments)
		if len(matched) >= 2 && d.isSkillMDReadTool(name) {
			skillName = matched[1]
		} else if name == "skill_tool" {
			// 尝试解析 arguments
			rawArgs := tc["arguments"]
			if rawArgs != nil {
				var argsDict map[string]any
				if argsStr, ok := rawArgs.(string); ok {
					_ = json.Unmarshal([]byte(argsStr), &argsDict)
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
//
// 对应 Python: ConversationSignalDetector._is_existing_skill(skill_name)
func (d *ConversationSignalDetector) isExistingSkill(skillName string) bool {
	if len(d.existingSkills) == 0 {
		return true
	}
	return d.existingSkills[skillName]
}

// isSkillMDReadTool 判断工具是否为文件读取类工具。
//
// 对应 Python: ConversationSignalDetector._is_skill_md_read_tool(name)
func (d *ConversationSignalDetector) isSkillMDReadTool(name string) bool {
	if name == "" {
		return true
	}
	return strings.Contains(name, "file") || strings.Contains(name, "read")
}

// inferSkillFromMessages 从消息列表推断当前活跃技能。
//
// 对应 Python: ConversationSignalDetector._infer_skill_from_messages(messages)
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
//
// 对应 Python: ConversationSignalDetector._extract_code_from_args(tool_call)
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
//
// 对应 Python: ConversationSignalDetector._fallback_user_feedback_signals(user_messages, skill_name)
func (d *ConversationSignalDetector) fallbackUserFeedbackSignals(userMessages []string, skillName string) []*EvolutionSignal {
	for i := len(userMessages) - 1; i >= 0; i-- {
		if correctionPattern.MatchString(userMessages[i]) {
			return []*EvolutionSignal{d.makeUserFeedbackSignal(userMessages[i], skillName)}
		}
	}
	return nil
}

// makeUserFeedbackSignal 构建用户反馈信号。
//
// 对应 Python: ConversationSignalDetector._make_user_feedback_signal(excerpt, skill_name)
func (d *ConversationSignalDetector) makeUserFeedbackSignal(excerpt, skillName string) *EvolutionSignal {
	return MakeEvolutionSignal(
		schema.UserIntentSignal,
		"Instructions",
		truncateString(excerpt, 600),
		WithSkillName(skillName),
		WithSource("passive_conversation"),
	)
}

// detectCollaborationSignals 检测团队协作信号。
// 仅在 TeamSkill 成员执行上下文中触发。
//
// 对应 Python: ConversationSignalDetector._detect_collaboration_signals(trajectory)
func (d *ConversationSignalDetector) detectCollaborationSignals(traj *trajectory.Trajectory) []*EvolutionSignal {
	if !d.isTeamMemberContext(traj) {
		return nil
	}

	var signals []*EvolutionSignal
	meta := traj.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	memberID := fmt.Sprintf("%v", meta["member_id"])
	if memberID == "" || memberID == "<nil>" {
		memberID = "unknown"
	}

	// 从轨迹步骤构建技能读取历史
	var skillReadHistory []skillReadEntry
	for idx, step := range traj.Steps {
		if step.Kind == trajectory.StepKindLLM {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			for _, msg := range llmDetail.Messages {
				if tcSlice := getToolCalls(msg); len(tcSlice) > 0 {
					if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
						skillReadHistory = append(skillReadHistory, skillReadEntry{idx, detected})
					}
				}
			}
		}
	}

	for _, step := range traj.Steps {
		if step.Kind != trajectory.StepKindTool {
			continue
		}
		toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
		if !ok {
			continue
		}

		toolName := strings.ToLower(toolDetail.ToolName)
		stepMeta := step.Meta
		if stepMeta == nil {
			stepMeta = map[string]any{}
		}

		activeSkill := d.resolveActiveSkillForStep(step, traj.Steps, skillReadHistory)

		// 1. send_message（发送消息）
		if toolName == "send_message" {
			callArgs := argsToJSON(toolDetail.CallArgs)
			toMember := d.extractToMember(callArgs)
			if toMember != "" && toMember != memberID {
				signals = append(signals, MakeEvolutionSignal(
					"collaboration_send", "Collaboration",
					fmt.Sprintf("发送消息给成员 %s", toMember),
					WithToolName(toolName),
					WithSkillName(activeSkill),
					WithSource("passive_collaboration"),
					WithContext(map[string]any{"from_member": memberID, "to_member": toMember}),
				))
			}
		}

		// 2. claim_task（认领任务）
		if toolName == "claim_task" {
			callArgs := argsToJSON(toolDetail.CallArgs)
			taskID := d.extractTaskID(callArgs)
			if taskID != "" {
				signals = append(signals, MakeEvolutionSignal(
					"collaboration_claim", "Collaboration",
					fmt.Sprintf("认领任务 %s", taskID),
					WithToolName(toolName),
					WithSkillName(activeSkill),
					WithSource("passive_collaboration"),
					WithContext(map[string]any{"member_id": memberID, "task_id": taskID}),
				))
			}
		}

		// 3. view_task（查看任务）
		if toolName == "view_task" {
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_view", "Collaboration",
				"查看团队任务状态",
				WithToolName(toolName),
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID}),
			))
		}

		// 4. parent_invoke_id（接收上下文）
		if _, exists := stepMeta["parent_invoke_id"]; exists {
			parentID := fmt.Sprintf("%v", stepMeta["parent_invoke_id"])
			opts := []SignalOption{
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID, "parent_invoke_id": parentID}),
			}
			if toolName != "" {
				opts = append(opts, WithToolName(toolName))
			}
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_receive", "Collaboration",
				fmt.Sprintf("接收来自 %s 的上下文/结果", parentID),
				opts...,
			))
		}

		// 5. 协作失败
		content := fmt.Sprintf("%v", toolDetail.CallResult)
		if match := collaborationFailurePattern.FindStringIndex(content); match != nil {
			excerpt := extractAroundMatch(content, match[0], match[1], 300, 300)
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_failure", "Collaboration", excerpt,
				WithToolName(toolName),
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID}),
			))
		}
	}

	return signals
}

// isTeamMemberContext 判断轨迹是否处于团队协作成员上下文。
//
// 对应 Python: ConversationSignalDetector._is_team_member_context(trajectory)
func (d *ConversationSignalDetector) isTeamMemberContext(traj *trajectory.Trajectory) bool {
	meta := traj.Meta
	if meta == nil {
		return false
	}
	if _, exists := meta["member_id"]; exists {
		if source, ok := meta["source"]; !ok || fmt.Sprintf("%v", source) != "standalone" {
			return true
		}
	}
	for key := range trajectory.CrossMemberMetaKeys {
		if _, exists := meta[key]; exists {
			return true
		}
	}
	return false
}

// resolveActiveSkillForStep 为轨迹步骤解析活跃技能。
//
// 对应 Python: ConversationSignalDetector._resolve_active_skill_for_step(step, all_steps, skill_read_history)
func (d *ConversationSignalDetector) resolveActiveSkillForStep(
	step *trajectory.TrajectoryStep,
	allSteps []*trajectory.TrajectoryStep,
	skillReadHistory []skillReadEntry,
) string {
	stepIdx := -1
	for i, s := range allSteps {
		if s == step {
			stepIdx = i
			break
		}
	}
	if stepIdx < 0 {
		return ""
	}
	for i := len(skillReadHistory) - 1; i >= 0; i-- {
		if skillReadHistory[i].msgIdx <= stepIdx {
			return skillReadHistory[i].skillName
		}
	}
	return ""
}

// extractToMember 从 send_message 参数中提取目标成员。
//
// 对应 Python: ConversationSignalDetector._extract_to_member(call_args)
func (d *ConversationSignalDetector) extractToMember(callArgs string) string {
	var argsDict map[string]any
	if err := json.Unmarshal([]byte(callArgs), &argsDict); err == nil {
		if v := argsDict["to_member_name"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
		if v := argsDict["to"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
	}
	// 正则回退
	patterns := []string{
		`to_member_name["']?\s*[:=]\s*["']([^"']+)["']`,
		`to["']?\s*[:=]\s*["']([^"']+)["']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(callArgs); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// extractTaskID 从 claim_task 参数中提取任务 ID。
//
// 对应 Python: ConversationSignalDetector._extract_task_id(call_args)
func (d *ConversationSignalDetector) extractTaskID(callArgs string) string {
	var argsDict map[string]any
	if err := json.Unmarshal([]byte(callArgs), &argsDict); err == nil {
		if v := argsDict["task_id"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
	}
	re := regexp.MustCompile(`task_id["']?\s*[:=]\s*["']([^"']+)["']`)
	if m := re.FindStringSubmatch(callArgs); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// deduplicate 基于 fingerprint 去重信号列表。
//
// 对应 Python: ConversationSignalDetector._deduplicate(signals)
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

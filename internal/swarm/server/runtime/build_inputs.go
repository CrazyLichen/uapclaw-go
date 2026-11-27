package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	uinterrupt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/interrupt"
	codeprompt "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/prompt"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInputs 构建 adapter 所需的 inputs 字典。
// Python: JiuWenClaw._build_inputs(request) -> (inputs, memoryMode, rawQuery)
//
// 返回: inputs 字典、memoryMode 字符串、原始 query。
func (uc *UapClawSwarm) BuildInputs(request *schema.AgentRequest) (map[string]any, string, string) {
	// 1. 获取配置
	// Python: config_base = get_config()（含 _normalize_config——2026-10-28 审查接线）
	var configBase map[string]any
	if cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig)); err == nil {
		if raw, err2 := cfg.Load(); err2 == nil {
			configBase = raw
		}
	}

	memoryMode := ""
	if configBase != nil {
		if mm, ok := configBase["memory_mode"]; ok {
			if mmStr, ok := mm.(string); ok {
				memoryMode = mmStr
			}
		}
	}

	// 2. 解析 params
	params := parseRequestParams(request)

	// 3. 提取基础字段
	query, _ := params["query"].(string)
	channel := extractChannelFromSessionID(request)
	// Python: language = resolve_language(config_base.get("preferred_language", "zh"))
	// 使用 prompts.ResolveLanguage 标准化，对齐 DeepAdapter.resolveRuntimeLanguage
	rawLang := "zh"
	if configBase != nil {
		if lang, ok := configBase["preferred_language"]; ok {
			if langStr, ok := lang.(string); ok && langStr != "" {
				rawLang = langStr
			}
		}
	}
	language := prompts.ResolveLanguage(rawLang)

	// 4. 提取 trusted_dirs
	var trustedDirs []string
	if rawDirs, ok := params["trusted_dirs"]; ok {
		if dirsSlice, ok := rawDirs.([]any); ok {
			for _, d := range dirsSlice {
				if dirStr, ok := d.(string); ok && strings.TrimSpace(dirStr) != "" {
					trustedDirs = append(trustedDirs, strings.TrimSpace(dirStr))
				}
			}
		}
	}

	// 5. 提取 project_dir / cwd
	metadata := request.Metadata
	projectDir := extractStringWithFallback(params, "project_dir", metadata, "project_dir")
	cwd := extractStringWithFallback(params, "cwd", metadata, "cwd")

	// 6. 构建 finalQuery
	var finalQuery any

	// PATH A: InteractiveInput 类型守卫。
	// 从 JSON 反序列化后 params["query"] 只能是 string，此分支在 WebSocket 路径中不可达。
	// 但保留防御性守卫，与 Python isinstance(query, InteractiveInput) 对齐。
	// 若未来有内部代码直接构造 inputs["query"] 为 *InteractiveInput，此守卫会生效。
	// 当前始终走 PATH B/C。
	if ii, ok := params["query"].(*interaction.InteractiveInput); ok {
		finalQuery = ii
	} else {
		// PATH B: answers 分支 → 构建 InteractiveInput
		answers, _ := params["answers"].([]any)
		if len(answers) > 0 {
			requestID, _ := params["request_id"].(string)
			source, _ := params["source"].(string)
			interruptID, _ := params["interrupt_id"].(string)
			interactiveInput := buildInteractiveInputFromAnswers(requestID, answers, source, interruptID)
			if interactiveInput != nil {
				finalQuery = interactiveInput
			} else {
				// answers 无法构建 InteractiveInput，fallback 到 BuildUserPrompt
				files, _ := params["files"].(map[string]any)
				finalQuery = BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)
			}
		} else {
			// PATH C: 普通对话 → BuildUserPrompt 包装
			files, _ := params["files"].(map[string]any)
			finalQuery = BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)
		}
	}

	// Python: interaction_context 存在时记录 debug 日志
	if metadata != nil {
		if ctx, ok := metadata["interaction_context"]; ok {
			if ctxStr, ok := ctx.(string); ok && strings.TrimSpace(ctxStr) != "" {
				truncated := query
				if len(truncated) > 2000 {
					truncated = truncated[:2000]
				}
				logger.Info(logComponent).
					Str("event_type", "build_inputs_debug").
					Str("query", truncated).
					Msg("[_build_inputs][DEBUG] interaction_context present")
			}
		}
	}

	// 7. 组装 inputs 字典
	sessionIDStr := ""
	if request.SessionID != nil {
		sessionIDStr = *request.SessionID
	}
	inputs := map[string]any{
		"conversation_id": sessionIDStr,
		"query":           finalQuery,
		"channel":         channel,
		"language":        language,
	}

	// 是否启用记忆
	enableMemory := true
	if metadata != nil {
		if em, ok := metadata["enable_memory"]; ok {
			if emBool, ok := em.(bool); ok {
				enableMemory = emBool
			}
		}
	}
	inputs["enable_memory"] = enableMemory

	// 可选字段
	if len(trustedDirs) > 0 {
		inputs["trusted_dirs"] = trustedDirs
	}
	if projectDir != "" {
		inputs["project_dir"] = projectDir
	}
	if cwd != "" {
		inputs["cwd"] = cwd
	}

	// run 字段
	if run, ok := params["run"]; ok {
		inputs["run"] = run
	}

	// cron 字段转换
	if cron, ok := params["cron"]; ok {
		inputs["run"] = map[string]any{
			"kind":    "cron",
			"context": map[string]any{"extra": map[string]any{"cron": cron}},
		}
	}

	return inputs, memoryMode, query
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildInteractiveInputFromAnswers 从用户答案构建 InteractiveInput。
// Python: JiuWenSwarm._build_interactive_input_from_answers()（interface.py:2251-2486）
//
// requestID：工具调用 ID；interruptID：所答中断的 id（chat.ask_user_question
// 等事件携带）。有 interrupt_id 时按中断 id 回答，否则按工具调用 id 回答：
// 升级前挂起的中断、agent-core 分配 id 之前流出的 ask_user 问题都没有 id，
// 单独发布的旧版 IDE 插件也不会带。
//
// 已知偏差（登记）：Python 在演化审批与 always_allow 两个分支含企业版
// 降级（is_enterprise() 时 allow_always→allow_once / persist_allow=False）；
// Go 暂不移植企业版逻辑（用户裁决 2026-12-27），个人版行为与 Python 一致。
func buildInteractiveInputFromAnswers(requestID string, answers []any, source string, interruptID string) *interaction.InteractiveInput {
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		return nil
	}

	// Python: address(value)（interface.py:2278-2282）
	address := func(value any) {
		if interruptID != "" {
			_ = ii.Answer(interruptID, value)
		} else {
			_ = ii.Update(requestID, value)
		}
	}

	// 客户端工具结果：一条结果对一个暂停的 client tool call，按它的 interrupt_id
	// 回答；没答到的调用保持暂停，继续等它的客户端。
	// Python: if source == CLIENT_TOOL_INTERRUPT_SOURCE: ...（interface.py:2284-2288）
	if source == schema.ClientToolInterruptSource {
		for _, answer := range clientToolAnswers(answers) {
			_ = ii.Answer(answer.interruptID, answer.result)
		}
		return ii
	}

	// AskUserRail 路径
	// Python: if source == "ask_user_interrupt": ...（interface.py:2290-2348）
	if source == "ask_user_interrupt" {
		answersDict := make(map[string]any)
		freeTextAnswer := ""
		hasFreeText := false
		for _, raw := range answers {
			answer, isMap := raw.(map[string]any)
			if !isMap {
				continue
			}
			questionText := strings.TrimSpace(anyToStr(answer["question"]))
			selectedOptions, _ := answer["selected_options"].([]any)
			customInput := strings.TrimSpace(anyToStr(answer["custom_input"]))

			var answerValue any
			if len(selectedOptions) > 0 {
				// 归一化每个选项为去空白字符串，丢空项
				cleanedOptions := make([]string, 0, len(selectedOptions))
				for _, rawOption := range selectedOptions {
					option := strings.TrimSpace(anyToStr(rawOption))
					if option != "" {
						cleanedOptions = append(cleanedOptions, option)
					}
				}
				if customInput != "" {
					// "Other" 只是 UI 占位符。保留正常多选并追加用户文本。
					normalOptions := make([]string, 0, len(cleanedOptions))
					for _, option := range cleanedOptions {
						if option != "Other" {
							normalOptions = append(normalOptions, option)
						}
					}
					if len(normalOptions) > 0 {
						answerValue = append(normalOptions, customInput)
					} else {
						answerValue = customInput
					}
				} else if len(cleanedOptions) == 1 {
					// 裸 "Other" 没有自定义文本，视为不完整（#2330）
					if cleanedOptions[0] == "Other" {
						answerValue = ""
					} else {
						answerValue = cleanedOptions[0]
					}
				} else if len(cleanedOptions) > 0 {
					// 多选：保留真实选择，丢弃纯占位 Other
					normalOptions := make([]string, 0, len(cleanedOptions))
					for _, option := range cleanedOptions {
						if option != "Other" {
							normalOptions = append(normalOptions, option)
						}
					}
					if len(normalOptions) > 0 {
						answerValue = normalOptions
					} else {
						answerValue = ""
					}
				} else {
					answerValue = ""
				}
			} else if customInput != "" {
				answerValue = customInput
			} else {
				answerValue = ""
			}

			if truthyValue(answerValue) {
				if questionText != "" {
					answersDict[questionText] = answerValue
				} else if text, isStr := answerValue.(string); isStr {
					freeTextAnswer = text
					hasFreeText = true
				} else if list, isList := answerValue.([]string); isList {
					freeTextAnswer = strings.Join(list, ", ")
					hasFreeText = true
				}
			}
		}
		if len(answersDict) == 0 && hasFreeText {
			answersDict["__free_text__"] = freeTextAnswer
		}
		address(map[string]any{"answers": answersDict})
		logger.Info(logComponent).
			Str("request_id", requestID).
			Int("answer_count", len(answersDict)).
			Msg("[JiuWenSwarm] AskUserRail InteractiveInput.update")
		return ii
	}

	// 技能演化审批路径（新写法 evolution_interrupt 与旧写法 skill_evolution_approval）
	// Python: if source in EVOLUTION_INTERRUPT_METADATA_SOURCES: ...（interface.py:2350-2386）
	if source == schema.EvolutionInterruptSource || source == schema.LegacySkillEvolutionApprovalSource {
		var answer map[string]any
		if len(answers) > 0 {
			answer, _ = answers[0].(map[string]any)
		}
		var selectedOptions []any
		var customInput string
		if answer != nil {
			selectedOptions, _ = answer["selected_options"].([]any)
			customInput = anyToStr(answer["custom_input"])
		}
		value := ""
		if len(selectedOptions) > 0 {
			value = strings.TrimSpace(anyToStr(selectedOptions[0]))
		}
		actionByValue := map[string]string{
			"accept":       "allow_once",
			"接收":           "allow_once",
			"接受":           "allow_once",
			"allow_once":   "allow_once",
			"本次允许":         "allow_once",
			"Allow Once":   "allow_once",
			"allow_always": "allow_always",
			"总是允许":         "allow_always",
			"Always Allow": "allow_always",
			"reject":       "reject",
			"拒绝":           "reject",
			"Reject":       "reject",
		}
		action, known := actionByValue[value]
		if !known {
			warnUnrecognisedApprovalOption("SkillEvolutionApproval", source, requestID, value, "rejected_as_unknown_option")
			action = "reject"
		}
		// Python: elif action == "allow_always" and is_enterprise(): 降级 allow_once
		// ——企业版降级 Go 暂不移植（见函数头偏差登记）。
		payload := map[string]any{"action": action}
		if customInput != "" {
			payload["feedback"] = customInput
		}
		address(payload)
		logger.Info(logComponent).
			Str("request_id", requestID).
			Str("payload", fmt.Sprint(payload)).
			Msg("[JiuWenSwarm] SkillEvolutionApproval InteractiveInput.update")
		return ii
	}

	// 未知 source（非 permission/confirm）返回 nil
	// Python: if source and source not in {"permission_interrupt", "confirm_interrupt"}: return None
	if source != "" && source != "permission_interrupt" && source != "confirm_interrupt" {
		return nil
	}

	// PermissionRail / ConfirmRail 路径
	// Python: interface.py:2394-2486
	var answer map[string]any
	if len(answers) > 0 {
		answer, _ = answers[0].(map[string]any)
	}
	var selectedOptions []any
	var customInput string
	if answer != nil {
		selectedOptions, _ = answer["selected_options"].([]any)
		customInput = anyToStr(answer["custom_input"])
	}

	var value string
	if len(selectedOptions) > 0 {
		value = anyToStr(selectedOptions[0])
	}

	// 选项字符串来自各渲染端：web / CLI 回传 value，TUI 回传 label。
	// 统一走 permission_options 的词表解析，不在维护一份字面量元组。
	// Python: action = resolve_permission_action(value)
	action := uinterrupt.ResolvePermissionAction(value)

	var confirmPayload map[string]any
	switch {
	case action == uinterrupt.AllowOnce:
		confirmPayload = map[string]any{"approved": true, "auto_confirm": false, "feedback": ""}
	case action == uinterrupt.SessionAllow:
		confirmPayload = map[string]any{"approved": true, "auto_confirm": true, "persist_allow": false, "feedback": ""}
	case action == uinterrupt.AlwaysAllow:
		// Python: persist_allow = not is_enterprise()（企业版权限卡不提供"永久记住"）
		// ——企业版降级 Go 暂不移植（见函数头偏差登记），恒为 true。
		confirmPayload = map[string]any{"approved": true, "auto_confirm": true, "persist_allow": true, "feedback": ""}
	case codeprompt.IsPlanExecuteOptionValue(value):
		// Web 的"执行"：批准并退出 plan，但本轮不再调模型。真正的执行由前端
		// 紧接着补发的普通消息开启新一轮完成。plan_execute 是额外键，
		// ConfirmPayload 会忽略它，rail 在校验前从原始 dict 读取。
		confirmPayload = map[string]any{"approved": true, "auto_confirm": false, "feedback": "", "plan_execute": true}
	case codeprompt.IsPlanSkipOptionValue(value):
		// Web 的"跳过"：不退出 plan，也不继续修改，直接结束本轮。
		confirmPayload = map[string]any{
			"approved":     false,
			"auto_confirm": false,
			"feedback":     firstNonEmpty(customInput, codeprompt.PlanSkipFeedback(preferredLanguage())),
			"plan_skip":    true,
		}
	case codeprompt.IsPlanReviseOptionValue(value):
		// Web 的"下一步"：不退出 plan，按修改意见续跑。
		confirmPayload = map[string]any{
			"approved":     false,
			"auto_confirm": false,
			"feedback":     firstNonEmpty(customInput, "用户希望继续规划"),
			"plan_revise":  true,
		}
	case action == uinterrupt.Reject:
		feedback := customInput
		if feedback == "" {
			if uinterrupt.IsKeepPlanningValue(value) {
				feedback = "用户希望继续规划"
			} else {
				feedback = "用户拒绝"
			}
		}
		confirmPayload = map[string]any{"approved": false, "auto_confirm": false, "feedback": feedback}
	case customInput != "":
		// 选项没认出来，但用户填了自由文本，就用文本当反馈继续拒绝。
		warnUnrecognisedApprovalOption("PermissionRail", source, requestID, value, "rejected_with_custom_input")
		confirmPayload = map[string]any{"approved": false, "auto_confirm": false, "feedback": customInput}
	default:
		warnUnrecognisedApprovalOption("PermissionRail", source, requestID, value, "rejected_as_unknown_option")
		confirmPayload = map[string]any{"approved": false, "auto_confirm": false, "feedback": "未知选项: " + value}
	}

	// Smart queue 卡片用它自己的 card id 作答；队列会重新映射键。
	// Python: card_ids = _permission_card_ids_from_answers(answers) if source == "permission_interrupt" else []
	cardIDs := permissionCardIDsFromAnswers(answers, source)
	if len(cardIDs) == 0 {
		address(confirmPayload)
		return ii
	}
	_ = ii.Update(cardIDs[0], confirmPayload)
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("card_id", cardIDs[0]).
		Msg("[JiuWenSwarm] PermissionRail card InteractiveInput.update")
	return ii
}

// clientToolAnswer 一条客户端工具结果，指向它所回答的中断。
// Python: runtime/client_tools.py ClientToolAnswer
type clientToolAnswer struct {
	requestID   string
	interruptID string
	result      *tool.ClientToolResult
}

// clientToolAnswers 提取 client_tool_interrupt 恢复携带的结果。
// 每条答案必须自带 request_id、interrupt_id 与 result；其余条目不回答任何调用。
// Python: runtime/client_tools.py:69-84 client_tool_answers
//
// result 在 build 层就地 ClientToolResult.coerce（对齐 Python 的同点 coerce），
// 类型从 any 收窄为 *tool.ClientToolResult。Python 的
// except ValidationError: continue 实际不可达——coerce 对信封校验失败会落到
// 整体即结果的兜底分支，恒成功返回；Go 的 CoerceClientToolResult 同样恒成功，
// 故无丢弃分支。
func clientToolAnswers(answers []any) []clientToolAnswer {
	parsed := make([]clientToolAnswer, 0)
	for _, raw := range answers {
		answer, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		if _, hasResult := answer["result"]; !hasResult {
			continue
		}
		requestID, _ := answer["request_id"].(string)
		interruptID, _ := answer["interrupt_id"].(string)
		if requestID == "" || interruptID == "" {
			continue
		}
		parsed = append(parsed, clientToolAnswer{
			requestID:   requestID,
			interruptID: interruptID,
			result:      tool.CoerceClientToolResult(answer["result"]),
		})
	}
	return parsed
}

// permissionCardIDsFromAnswers 归一化唯一的权限卡片定位符。
// Python: _permission_card_ids_from_answers（interface.py:256-271）
// 仅 permission_interrupt 来源读取；非该来源恒空（与 Python 三元一致）。
func permissionCardIDsFromAnswers(answers []any, source string) []string {
	if source != "permission_interrupt" {
		return nil
	}
	if len(answers) != 1 {
		return nil
	}
	answer, isMap := answers[0].(map[string]any)
	if !isMap {
		return nil
	}
	cardID, isStr := answer["card_id"].(string)
	if !isStr {
		return nil
	}
	cardID = strings.TrimSpace(cardID)
	if cardID == "" || len(cardID) > 128 {
		return nil
	}
	return []string{cardID}
}

// warnUnrecognisedApprovalOption 审批选项没认出来时留告警，然后才按拒绝兜底。
// 取值为空是渲染端的"取消"（TUI 按 Esc 回传空串），属正常路径，不告警。
// Python: _warn_unrecognised_approval_option（interface.py:401-425）
func warnUnrecognisedApprovalOption(branch, source, requestID, value, outcome string) {
	if uinterrupt.NormalizeOptionValue(value) == "" {
		return
	}
	logger.Warn(logComponent).
		Str("branch", branch).
		Str("source", source).
		Str("request_id", requestID).
		Str("value", value).
		Str("outcome", outcome).
		Msg("[JiuWenSwarm] unrecognised approval option: the answer is refused because it could not be decoded, not because the user declined; check that the option vocabulary in permission_options matches what the channel sends")
}

// preferredLanguage 读取 config 的 preferred_language（缺省 zh）。
// Python: get_config().get("preferred_language")
func preferredLanguage() string {
	if cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig)); err == nil {
		if raw, err2 := cfg.Load(); err2 == nil {
			if lang, ok := raw["preferred_language"].(string); ok && lang != "" {
				return lang
			}
		}
	}
	return "zh"
}

// anyToStr 值转字符串（nil / 非字符串 → 空串）。
func anyToStr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// truthyValue 对齐 Python 的 truthy 判断（本函数涉及的取值只有 string / []string / ""）。
func truthyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []string:
		return len(typed) > 0
	default:
		return true
	}
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// parseRequestParams 解析 AgentRequest.Params（json.RawMessage）为 map。
func parseRequestParams(request *schema.AgentRequest) map[string]any {
	if len(request.Params) == 0 {
		return make(map[string]any)
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return make(map[string]any)
	}
	return params
}

// extractChannelFromSessionID 从 sessionID 提取 channel（第一个 _ 前部分）。
func extractChannelFromSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil && *request.SessionID != "" {
		parts := strings.SplitN(*request.SessionID, "_", 2)
		if parts[0] != "" {
			return parts[0]
		}
	}
	return "web"
}

// extractStringWithFallback 从 params 和 metadata 提取字符串，params 优先。
func extractStringWithFallback(params map[string]any, paramKey string, metadata map[string]any, metaKey string) string {
	// params 优先
	if val, ok := params[paramKey]; ok {
		if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
			return strings.TrimSpace(str)
		}
	}
	// metadata 兜底
	if metadata != nil {
		if val, ok := metadata[metaKey]; ok {
			if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

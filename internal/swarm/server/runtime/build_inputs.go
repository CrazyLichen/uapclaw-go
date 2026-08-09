package runtime

import (
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInputs 构建 adapter 所需的 inputs 字典。
//
// 对齐 Python: JiuWenClaw._build_inputs(request) -> (inputs, memoryMode, rawQuery)
//
// 返回: inputs 字典、memoryMode 字符串、原始 query。
func (uc *UapClaw) BuildInputs(request *schema.AgentRequest) (map[string]any, string, string) {
	// 1. 获取配置
	var configBase map[string]any
	if cfg, err := config.New(""); err == nil {
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
	// 对齐 Python: language = resolve_language(config_base.get("preferred_language", "zh"))
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
			interactiveInput := buildInteractiveInputFromAnswers(requestID, answers, source)
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

	// 对齐 Python：interaction_context 存在时记录 debug 日志
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
					Msg("[_build_inputs][DEBUG] interaction_context 存在")
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
// 对齐 Python: JiuWenClaw._build_interactive_input_from_answers()
func buildInteractiveInputFromAnswers(requestID string, answers []any, source string) *interaction.InteractiveInput {
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		return nil
	}

	// AskUserRail 路径：source == "ask_user_interrupt"
	if source == "ask_user_interrupt" {
		answersDict := make(map[string]any)
		for _, a := range answers {
			if answer, ok := a.(map[string]any); ok {
				questionText, _ := answer["question"].(string)
				selectedOptions, _ := answer["selected_options"].([]any)
				var answerValue string
				if len(selectedOptions) > 0 {
					answerValue, _ = selectedOptions[0].(string)
				}
				if questionText != "" && answerValue != "" {
					answersDict[questionText] = answerValue
				}
			}
		}
		_ = ii.Update(requestID, map[string]any{"answers": answersDict})
		return ii
	}

	// 未知 source（非 permission_interrupt）返回 nil
	if source != "" && source != "permission_interrupt" {
		return nil
	}

	// PermissionRail 路径：approve/reject/always_allow
	var answer map[string]any
	if len(answers) > 0 {
		answer, _ = answers[0].(map[string]any)
	}
	if answer == nil {
		answer = make(map[string]any)
	}

	var selectedOptions []any
	if so, ok := answer["selected_options"].([]any); ok {
		selectedOptions = so
	}
	var value string
	if len(selectedOptions) > 0 {
		value, _ = selectedOptions[0].(string)
	}
	customInput, _ := answer["custom_input"].(string)

	var confirmPayload map[string]any
	switch value {
	case "approve", "本次允许", "Approve":
		confirmPayload = map[string]any{"approved": true, "auto_confirm": false, "feedback": ""}
	case "always_allow", "总是允许", "Always Allow":
		confirmPayload = map[string]any{"approved": true, "auto_confirm": true, "persist_allow": true, "feedback": ""}
	case "reject", "拒绝", "Reject":
		feedback := customInput
		if feedback == "" {
			feedback = "用户拒绝"
		}
		confirmPayload = map[string]any{"approved": false, "auto_confirm": false, "feedback": feedback}
	default:
		confirmPayload = map[string]any{"approved": false, "auto_confirm": false, "feedback": "未知选项: " + value}
	}

	_ = ii.Update(requestID, confirmPayload)
	return ii
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

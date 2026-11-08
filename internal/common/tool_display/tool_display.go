package tool_display

// call_goal schema 注入与参数剥离。
//
// 对应 Python 代码：jiuwenswarm/common/tool_display.py（87 行）
// 包级说明见 doc.go。

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// callGoalMaxLen call_goal 最大长度（超出截断，避免撑爆展示/事件负载）。
	//
	// Python: _CALL_GOAL_MAX_LEN
	callGoalMaxLen = 200

	// ellipsis 截断后追加的省略号。
	//
	// Python: "…"
	ellipsis = "…"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// callGoalKeys call_goal 的可接受键名，按优先级排列。
	//
	// Python: _CALL_GOAL_KEYS = ("call_goal", "callGoal")
	callGoalKeys = []string{"call_goal", "callGoal"}

	// callGoalSchema call_goal 的 JSON Schema 定义。
	//
	// Python: _CALL_GOAL_SCHEMA
	callGoalSchema = map[string]any{
		"type": "string",
		"description": "一句简短说明，讲清这次工具调用要达成的目标（仅界面展示）。" +
			"必须使用当前对话所用的语言，不要固定用中文。" +
			"Write it in the same language as the conversation with the user; " +
			"do not default to Chinese. " +
			"中文对话写「调研 openJiuwen 官网信息」「创建三子棋对战团队」「通知 player-x 落子」，" +
			"英文对话写 \"Research the openJiuwen website\" \"Create a tic-tac-toe team\"。" +
			"不要只写工具名或裸 URL；不影响工具实际执行。" +
			"与工具自带的 display_name（成员/团队展示名）以及 send_message.summary 都不是同一个字段，" +
			"调用 spawn_member / spawn_teammate / send_message / build_team 时也必须填写。",
	}

	// uiDisplayNameMarkers 判定 display_name 是否为曾误注入的 UI 文案。
	//
	// Python: 内联在 inject_call_goal_schema 的 if 条件里
	// （"界面展示" in desc or "UI only" in desc or "不影响工具实际执行" in desc）
	uiDisplayNameMarkers = []string{"界面展示", "UI only", "不影响工具实际执行"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectCallGoalSchema 给工具 JSON Schema 注入可选 call_goal，
// 供主模型随 tool_call 一并产出。
//
// 若工具已有 call_goal 定义则不覆盖；永远不要碰业务 display_name
// （仅当它明显是本模块曾误注入的 UI 文案时才移除）。
//
// Python: inject_call_goal_schema(parameters)
func InjectCallGoalSchema(parameters any) {
	params, ok := parameters.(map[string]any)
	if !ok {
		return
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		params["properties"] = props
	}

	// 若工具已有 call_goal 定义则不覆盖
	if _, exists := props["call_goal"]; !exists {
		props["call_goal"] = copyMap(callGoalSchema)
	}

	// 清理此前误注入的 UI 用 display_name（仅当描述像我们的 UI 文案时）
	if existing, ok := props["display_name"].(map[string]any); ok {
		desc := asString(existing["description"])
		for _, marker := range uiDisplayNameMarkers {
			if strings.Contains(desc, marker) {
				delete(props, "display_name")
				break
			}
		}
	}
}

// ExtractCallGoal 从 arguments 取出 call_goal，并返回剥掉该字段后的
// arguments（保持原类型风格）。
//
// 不会触碰 display_name（team 成员名等业务字段）。call_goal 截断到
// callGoalMaxLen，避免异常超长撑爆展示/事件负载。
//
// Python: extract_call_goal(arguments) -> tuple[str, Any]
func ExtractCallGoal(arguments any) (string, any) {
	switch v := arguments.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(v))
		for k, val := range v {
			cleaned[k] = val
		}
		name := ""
		for _, key := range callGoalKeys {
			raw := cleaned[key]
			delete(cleaned, key)
			s, ok := raw.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" && name == "" {
				name = truncate(s, callGoalMaxLen)
			}
		}
		return name, cleaned

	case string:
		if strings.TrimSpace(v) == "" {
			return "", arguments
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return "", arguments
		}
		name, cleanedMap := ExtractCallGoal(parsed)
		encoded, err := json.Marshal(cleanedMap)
		if err != nil {
			return "", arguments
		}
		return name, string(encoded)

	default:
		return "", arguments
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncate 把文本压成单行后按字符数截断，超长追加省略号。
//
// Python: _truncate(value, max_len)
// 注意 Python 用 len(one_line) 计字符数（中文按 1 计），Go 需转 rune 切片。
func truncate(value string, maxLen int) string {
	oneLine := strings.Join(strings.Fields(value), " ")
	runes := []rune(oneLine)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + ellipsis
	}
	return oneLine
}

// copyMap 浅拷贝 map，避免注入的 schema 被调用方修改后污染全局定义。
//
// Python: dict(_CALL_GOAL_SCHEMA)
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// asString 把任意值转字符串，nil 转空串。
//
// Python: str(existing.get("description") or "")
func asString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

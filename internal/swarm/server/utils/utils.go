package utils

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// metadataChatIDKeys ChatID 回退查找的 metadata 键列表。
// 对齐 Python: get_chat_id 中的 feishu/wecom/dingtalk/xiaoyi 优先级
var metadataChatIDKeys = []string{
	"feishu_chat_id",
	"wecom_chat_id",
	"dingtalk_chat_id",
	"xiaoyi_session_id",
}

// teamModeValues IsTeamParams 匹配的 mode 值集合。
// 对齐 Python: is_team_params 中的 {"team", "team.plan", "code.team"}
var teamModeValues = map[string]bool{
	"team":      true,
	"team.plan": true,
	"code.team": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetChatID 获取请求的 Chat ID（平台聊天标识）。
// 优先使用顶层 ChatID 字段，回退到 Metadata 中的平台特定字段。
// 对齐 Python: get_chat_id(request) (utils.py L5-24)
func GetChatID(req *schema.AgentRequest) string {
	// 优先使用顶层字段
	if req.ChatID != nil && *req.ChatID != "" {
		return *req.ChatID
	}
	// 回退到 metadata 中的平台字段
	if req.Metadata != nil {
		for _, key := range metadataChatIDKeys {
			if v, ok := req.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// IsTeamParams 判断参数是否为团队模式。
// 检查 params["team"] truthy 或 params["mode"] 为已知团队模式字符串。
// 对齐 Python: is_team_params(params) (utils.py L26-42)
func IsTeamParams(params map[string]any) bool {
	if params == nil {
		return false
	}
	// 检查 team 键 truthy
	if team, ok := params["team"]; ok {
		if isTruthy(team) {
			return true
		}
	}
	// 检查 mode 键为已知团队模式
	mode, ok := params["mode"]
	if !ok {
		return false
	}
	modeStr, ok := mode.(string)
	if !ok {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(modeStr))
	return teamModeValues[normalized]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTruthy 判断值是否为 truthy（非 nil/非 false/非 ""/非 0）。
// 对齐 Python: truthy 判断逻辑
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != ""
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}

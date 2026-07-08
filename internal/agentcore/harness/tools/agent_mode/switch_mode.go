package agent_mode

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SwitchModeInput switch_mode 工具输入参数。
type SwitchModeInput struct {
	Mode string `json:"mode"`
}

// ──────────────────────────── 常量 ────────────────────────────

// 对齐 Python L165-184: 中英文消息
var (
	switchModeInvalidMsg = map[string]string{
		"cn": "无效模式 '{mode}'。支持模式：normal、plan。",
		"en": "Invalid mode '{mode}'. Supported modes: plan, normal.",
	}
	switchModeToNormalMsg = map[string]string{
		"cn": "已切换为 normal 模式。",
		"en": "Switched mode to normal.",
	}
	switchModeToPlanMsg = map[string]string{
		"cn": "已切换为 plan 模式。\n下一步：调用 enter_plan_mode 继续 Plan 工作流。",
		"en": "Switched mode to plan.\nNext step: call enter_plan_mode to continue the plan workflow.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSwitchModeTool 创建 switch_mode 工具实例。
//
// 对齐 Python: SwitchModeTool.__init__() L205-221 + invoke() L224-256
func NewSwitchModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("switch_mode", "switch_mode", lang, nil, agentID)

	fn := func(ctx context.Context, input SwitchModeInput, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		rawMode := strings.TrimSpace(strings.ToLower(input.Mode))

		// 对齐 Python L230: 校验模式值
		if rawMode != hschema.AgentModePlan.String() && rawMode != hschema.AgentModeNormal.String() {
			msg := strings.ReplaceAll(switchModeInvalidMsg[lang], "{mode}", rawMode)
			return map[string]any{"error": msg}, nil
		}

		// 提取 session
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "switch_mode 需要 session"}, nil
		}

		// 对齐 Python L239-249: 调用 agent.SwitchMode
		agent.SwitchMode(sess, rawMode)

		var message string
		var currentMode string
		if rawMode == hschema.AgentModePlan.String() {
			message = switchModeToPlanMsg[lang]
			currentMode = hschema.AgentModePlan.String()
		} else {
			message = switchModeToNormalMsg[lang]
			currentMode = hschema.AgentModeNormal.String()
		}

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "switch_mode").
			Str("mode", currentMode).
			Msg("AgentModeRail 已切换模式")

		return map[string]any{
			"current_mode": currentMode,
			"message":      message,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

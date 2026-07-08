package agent_mode

import (
	"context"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 对齐 Python L140-162: exit_plan_mode 中英文消息
var (
	exitPlanEmptyMsg = map[string]string{
		"cn": "规划模式已结束。你现在可以结束本轮。\n计划文件：{plan_path}",
		"en": "Plan mode ended. You can now exit the turn.\nPlan file: {plan_path}",
	}
	exitPlanWithContentPrefix = map[string]string{
		"cn": "规划模式已结束。\n计划文件：{plan_path}\n\n## 计划：\n",
		"en": "Plan mode ended. \nPlan file: {plan_path}\n\n## Plan:\n",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExitPlanModeTool 创建 exit_plan_mode 工具实例。
//
// 对齐 Python: ExitPlanModeTool.__init__() L326-348 + invoke() L350-378
func NewExitPlanModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("exit_plan_mode", "exit_plan_mode", lang, nil, agentID)

	fn := func(ctx context.Context, _ map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "exit_plan_mode 需要 session"}, nil
		}

		// 对齐 Python L363-365: 读取 plan 文件内容
		planPath := agent.GetPlanFilePath(sess)
		planText := ""
		if planPath != "" {
			data, err := os.ReadFile(planPath)
			if err == nil {
				planText = string(data)
			}
		}

		planPathStr := formatPlanPath(planPath)

		// 对齐 Python L370-371: 空计划
		if strings.TrimSpace(planText) == "" {
			msg := strings.ReplaceAll(exitPlanEmptyMsg[lang], "{plan_path}", planPathStr)
			return map[string]any{"plan_path": planPath, "message": msg}, nil
		}

		// 对齐 Python L373-375: 有内容 → 恢复模式 + 返回前缀 + 计划全文
		agent.RestoreModeAfterPlanExit(sess)
		prefix := strings.ReplaceAll(exitPlanWithContentPrefix[lang], "{plan_path}", planPathStr)

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "exit_plan_mode").
			Str("plan_path", planPath).
			Int("plan_length", len(planText)).
			Msg("AgentModeRail 已退出 plan 模式")

		return map[string]any{
			"plan_path": planPath,
			"message":   prefix + planText,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

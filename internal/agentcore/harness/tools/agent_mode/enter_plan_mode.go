package agent_mode

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 对齐 Python L114-138: enter_plan_mode 中英文消息
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	enterPlanExistsMsg = map[string]string{
		"cn": "计划文件已存在，路径：{plan_path}\n你可以阅读计划文件然后做增量修改。请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n",
		"en": "Plan file already exists at: {plan_path}\nYou can read it and make incremental edits. Continue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end.",
	}
	enterPlanCreatedMsg = map[string]string{
		"cn": "计划文件已创建于：{plan_path}\n请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n除计划文件外，请勿编辑任何其他文件。\n",
		"en": "Plan file created at: {plan_path}\nContinue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end. DO NOT edit any files except the plan file.\n",
	}
)

// NewEnterPlanModeTool 创建 enter_plan_mode 工具实例。
//
// 对齐 Python: EnterPlanModeTool.__init__() L262-286 + invoke() L288-320
// ──────────────────────────── 导出函数 ────────────────────────────

func NewEnterPlanModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("enter_plan_mode", "enter_plan_mode", lang, nil, agentID)

	fn := func(ctx context.Context, _ map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "enter_plan_mode 需要 session"}, nil
		}

		// 对齐 Python L299-309: 若已有 plan_slug 且文件存在 → 返回已存在消息
		state := agent.LoadState(sess)
		if state.PlanMode.PlanSlug != "" {
			workspaceRoot := getWorkspaceRoot(agent)
			existingPath := ResolvePlanFilePath(workspaceRoot, state.PlanMode.PlanSlug)
			if _, err := os.Stat(existingPath); err == nil {
				msg := strings.ReplaceAll(enterPlanExistsMsg[lang], "{plan_path}", formatPlanPath(existingPath))
				return map[string]any{"plan_path": existingPath, "message": msg}, nil
			}
		}

		// 对齐 Python L311-319: 生成 slug → 解析路径 → 保存 slug 到 state
		workspaceRoot := getWorkspaceRoot(agent)
		slug := GetOrCreatePlanSlug(workspaceRoot)
		planPath := ResolvePlanFilePath(workspaceRoot, slug)
		_ = os.MkdirAll(filepath.Dir(planPath), 0o755)

		state.PlanMode.PlanSlug = slug
		agent.SaveState(sess, state)

		msg := strings.ReplaceAll(enterPlanCreatedMsg[lang], "{plan_path}", formatPlanPath(planPath))

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "enter_plan_mode").
			Str("plan_slug", slug).
			Str("plan_path", planPath).
			Msg("AgentModeRail 已创建 plan 文件")

		return map[string]any{"plan_path": planPath, "message": msg}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

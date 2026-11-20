package scheduling

// reviewer_runner.go 临时 reviewer 运行体的生产实现（D-25.2b-13① 的默认缝）。
//
// Python: _spawn_temp_reviewer (scheduler.py L768-988)——reviewer 基础规格派生、
// verify/view 工具装配、按类型的提示词模板、评审请求渲染（含上一票与
// upstream）、最多 3 次重试（181001 模型调用失败检测）、用后即弃。
//
// 偏差 D-25.2b-13②：运行中 nudge steer 随 DeepAgent steer 面落地后接线；
// execution subject（轨迹归因分组）随 otel 执行主体面落地后补。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/harness"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TempReviewerDeps 临时 reviewer 运行体的依赖集（装配点从 kernel inf/bp 取）。
type TempReviewerDeps struct {
	// DB 团队数据库（reviewer-scoped TaskManager 与 task_links 读取）。
	DB database.TeamDatabase
	// Blueprint 蓝图（ReviewerAgent/Agents 派生基础规格）。
	Blueprint *schema.TeamAgentSpec
	// BuildCtx leader 的构建上下文（Derive 出 reviewer ctx）。
	BuildCtx hschema.BuildContextInterface
	// TeamName 团队名。
	TeamName string
	// Language 渲染语言。
	Language string
	// TemplateLoader per-team A 类加载器（nil 用框架只读）。
	TemplateLoader TemplateLoaderFunc
	// LeaderMemberName leader 成员名。
	LeaderMemberName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTempReviewerRunner 构建生产运行体（装配点调用一次注入 SetReviewerRunner）。
// Python: _spawn_temp_reviewer 的闭包化。
func BuildTempReviewerRunner(deps TempReviewerDeps) ReviewerRunnerFunc {
	return func(ctx context.Context, reviewer string, task *database.TeamTaskBase) bool {
		return runTempReviewer(ctx, deps, reviewer, task)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runTempReviewer 构建一次性 reviewer harness 并运行 verify_task。
// Python: _spawn_temp_reviewer 主体（scheduler.py L796-980）。
// 返回值仅作提示——投票真相由 followUpUnvotedReviewer 经 GetReviewTally 复核。
func runTempReviewer(ctx context.Context, deps TempReviewerDeps, reviewer string, task *database.TeamTaskBase) bool {
	// 1. 基础规格：spec.reviewer_agent 优先，否则 teammate/leader 规格收窄。
	// Python: L800-806。
	var baseAgentSpec *schema.DeepAgentSpec
	if deps.Blueprint.ReviewerAgent != nil {
		baseAgentSpec = deps.Blueprint.ReviewerAgent
	} else {
		memberSpec, ok := deps.Blueprint.Agents["teammate"]
		if !ok {
			memberSpec, ok = deps.Blueprint.Agents["leader"]
		}
		if !ok {
			logger.Error(logComponent).Str("reviewer", reviewer).
				Msg("[scheduler] no base agent spec for temp reviewer")
			return false
		}
		baseAgentSpec = BuildReviewerAgentSpec(&memberSpec, nil, nil)
	}

	// 2. reviewer-scoped TeamTaskManager：verify_task 的身份守卫以任务行上的
	// reviewer 名通过；view_task 经同一 manager 读，in_review 列出该 reviewer 队列。
	// Python: L810-820。
	reviewerTM := tools.NewTeamTaskManager(deps.DB, deps.TeamName, reviewer, nil, nil, "", "", deps.LeaderMemberName)

	// 3. 按类型的提示词模板与专属指令。
	// Python: L831-846。
	reviewerType := "verifier"
	instruction := ""
	for _, detail := range task.ReviewerDetails() {
		if detail["reviewer_id"] == reviewer {
			reviewerType = detail["type"]
			if reviewerType == "" {
				reviewerType = "verifier"
			}
			instruction = detail["instruction"]
			break
		}
	}
	templateName := reviewerTemplateMap(reviewerType)
	if reviewerType == "inspector" && instruction == "" {
		if dims, dimsErr := ateamprompts.LoadTemplate("reviewer_dims_for_inspector", deps.Language); dimsErr == nil {
			instruction = dims.Content()
		}
	}
	tpl, tplErr := ateamprompts.LoadTemplate(templateName, deps.Language)
	if tplErr != nil {
		logger.Error(logComponent).Err(tplErr).Str("reviewer", reviewer).
			Msg("[scheduler] reviewer template load failed")
		return false
	}
	systemPrompt := strings.ReplaceAll(strings.ReplaceAll(tpl.Content(),
		"{{reviewer}}", reviewer), "{{instruction}}", instruction)

	// 4. 评审请求提示词：派发时按当前任务行渲染；上一轮自己的票是唯一记忆。
	// Python: L858-868。
	previousVote, _ := reviewerTM.GetPreviousRoundVote(ctx, task, reviewer)
	var upstream []*database.TeamTaskBase
	if dao := deps.DB.Task(); dao != nil {
		upstream, _, _ = ateamprompts.TaskLinks(ctx, dao, task.TaskID)
	}
	reviewPrompt := renderReviewRequestWithLoader(task, deps.Language, reviewer, previousVote, upstream, deps.TemplateLoader)

	// 5. 组装 reviewer 规格：系统提示词 + verify/view 工具 + 基础规格 rails。
	// Python: L847-856。
	reviewerSpec := *baseAgentSpec
	reviewerSpec.SystemPrompt = systemPrompt
	verifyCard := tools.NewVerifyTaskToolInstance(reviewerTM, deps.Language, "verify_task_scheduled").Card()
	viewCard := tools.NewViewTaskToolInstance(reviewerTM, deps.Language, "view_task_reviewer").Card()

	// 6. 最多 3 次重试：181001 模型调用失败检测；每次新 harness 新 card id，
	// run_once 在每次调用时 teardown 工具。
	// Python: L872-952。
	var result map[string]any
	for attempt := 0; attempt < 3; attempt++ {
		attemptSpec := reviewerSpec
		attemptSpec.Card = &agentschema.AgentCard{
			BaseCard: commonschema.BaseCard{
				ID:          fmt.Sprintf("%s_%s_review_%s", deps.TeamName, reviewer, uuid.NewString()[:12]),
				Name:        reviewer,
				Description: "scheduled-mode temp reviewer",
			},
		}
		attemptSpec.ToolCards = append(attemptSpec.ToolCards, verifyCard, viewCard)
		reviewerCtx := deps.BuildCtx
		if reviewerCtx != nil {
			lang := deps.Language
			role := "teammate"
			reviewerCtx = reviewerCtx.Derive(hschema.BuildContextOverrides{
				Language: &lang, MemberName: &reviewer, Role: &role,
			})
		}
		reviewerHarness := harness.TeamHarnessBuild(attemptSpec, "teammate", reviewer, false, reviewerCtx, nil)
		if attempt == 0 {
			logger.Info(logComponent).Str("reviewer", reviewer).Str("task_id", task.TaskID).
				Msg("[reviewer_built] temp reviewer harness built")
		}
		output, runErr := reviewerHarness.RunOnce(ctx, reviewPrompt, nil)
		_ = reviewerHarness.Dispose(context.Background())
		if runErr == nil {
			result = output
		}
		outputStr := fmt.Sprint(result)
		if !strings.Contains(outputStr, "181001") {
			break
		}
		logger.Warn(logComponent).Str("reviewer", reviewer).
			Str("task_id", task.TaskID).Int("attempt", attempt+1).
			Str("output", truncateText(outputStr, 200)).
			Msg("[reviewer_retry]")
		time.Sleep(time.Duration(2*attempt) * time.Second)
	}
	logger.Info(logComponent).Str("reviewer", reviewer).Str("task_id", task.TaskID).
		Str("output", truncateText(fmt.Sprint(result), 2000)).
		Msg("[reviewer_finish]")

	// 投票判定交由 followUpUnvotedReviewer 经计票复核；此处不做判定。
	return false
}

// reviewerTemplateMap 类型 → 提示词模板名。
// Python: _REVIEWER_TEMPLATE_MAP (scheduler.py L976 附近)。
func reviewerTemplateMap(reviewerType string) string {
	switch reviewerType {
	case "challenger":
		return "reviewer_challenger"
	case "inspector":
		return "reviewer_inspector"
	default:
		return "reviewer_verifier"
	}
}

// truncateText 文本截断（日志字段）。
func truncateText(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

// renderReviewRequestWithLoader 带加载器的评审请求渲染（A 类加载器或框架只读）。
// Python: render_review_request_for_harness 的 loader 参数语义。
func renderReviewRequestWithLoader(task *database.TeamTaskBase, language, reviewer string, previousVote *database.TeamTaskReviewVoteBase, upstream []*database.TeamTaskBase, loader TemplateLoaderFunc) string {
	if loader != nil {
		return renderReviewRequestViaLoader(task, language, reviewer, previousVote, upstream, loader)
	}
	return renderReviewRequestForHarness(task, language, reviewer, previousVote, upstream)
}

// renderReviewRequestViaLoader 经注入加载器渲染（模板读演进 workspace 值）。
// 与 renderReviewRequestForHarness 同构，仅加载面不同。
func renderReviewRequestViaLoader(task *database.TeamTaskBase, language, reviewer string, previousVote *database.TeamTaskReviewVoteBase, upstream []*database.TeamTaskBase, loader TemplateLoaderFunc) string {
	load := func(name string) string {
		tpl, err := loader(name, language)
		if err != nil || tpl == nil {
			return ""
		}
		return tpl.Content()
	}
	return assembleReviewRequestBody(task, language, reviewer, previousVote, upstream, load)
}

// 本文件提供调度器消息装配（F_62，模板化经 F_63）。
//
// 两个受众、两种机制：
//   - 成员收 leader 身份信箱消息。措辞不在这里组装——本模块只构造投递载荷
//     （meta：模板键 + 行引用 + 瞬态参数），信箱行存空 content。正文在
//     prompts/<lang>/scheduler_*.md，投递时对照*当前*任务行渲染，离线成员
//     信箱里滞留的 handoff 永不携带过时任务简报。见 message_template_expand.go。
//   - leader 收直接输入注入（digest / escalation）。不经信箱，无 meta 通道、
//     无投递时展开——保持单行 i18n 字符串。
//
// Python: agent/scheduling/render.py（25.2a 半区：meta_task_start/revised/
// subtask_review/is_task_start_meta + leader 渲染 task_done/all_done/
// escalation_retired_owner；评审半区渲染随 25.2b）
package scheduling

import (
	"strconv"
	"strings"

	ateamprompts "github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 常量 ────────────────────────────

// 模板键，即 prompts/<lang>/<key>.md 的 basename。
// Python: render.py L38-48（25.2a 消费子集）
const (
	templateTaskStart      = "scheduler_task_start"
	templateTaskStartPlan  = "scheduler_task_start_plan"
	templateTaskResume     = "scheduler_task_resume"
	templateTaskRevised    = "scheduler_task_revised"
	templateSubtaskReview  = "scheduler_subtask_review"
	templateReviewRequest  = "scheduler_review_request"      // 25.2b
	templateReviewPrevFB   = "scheduler_review_previous_feedback" // 25.2b
	templateReviewSubNote  = "scheduler_review_submission_note"   // 25.2b
	templateReviewUpstream = "scheduler_review_upstream"          // 25.2b
	templateRework         = "scheduler_rework"                   // 25.2b
	templateReworkSummary  = "scheduler_rework_summary"           // 25.2b
)

// taskStartTemplates 每一个 start 模板都算 owner 的 start（计划闸 start 或
// 子任务后的 resume 也是 start）。
// Python: _TASK_START_TEMPLATES（render.py L51）
var taskStartTemplates = map[string]bool{
	templateTaskStart:     true,
	templateTaskStartPlan: true,
	templateTaskResume:    true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MetaTaskStart 构造 assignee 启动指令的投递载荷（plan 闸感知）。
// resume 标记父任务在子任务完成后重新开始（F_128）：owner 是继续而非重新开始。
// Python: meta_task_start（render.py L54-66）
func MetaTaskStart(task *database.TeamTaskBase, resume bool) map[string]any {
	template := templateTaskStart
	if resume {
		template = templateTaskResume
	} else if task.Status == string(schema.TaskStatusPlanning) {
		template = templateTaskStartPlan
	}
	return buildMetaRefsTask(template, task.TaskID)
}

// MetaTaskRevised 推送被编辑任务最新正文给执行者的投递载荷（F_129）。
// Python: meta_task_revised（render.py L69-71）
func MetaTaskRevised(taskID string) map[string]any {
	return buildMetaRefsTask(templateTaskRevised, taskID)
}

// MetaSubtaskReview 请挂起父任务的 owner 审视其子任务的投递载荷（F_129）。
// Python: meta_subtask_review（render.py L74-76）
func MetaSubtaskReview(taskID string) map[string]any {
	return buildMetaRefsTask(templateSubtaskReview, taskID)
}

// IsTaskStartMeta 判断解析后的信箱 meta 是否为绑定 task_id 的启动 handoff。
// handoff 兜底（B_06）向信箱问「这个 owner 是否收到过该任务的启动指令」；
// 模板键私有于本模块，所以问题在这里回答而不是让 scheduler 做字符串匹配。
// 每个 start 模板都算——计划闸 start 或子任务后的 resume 也是 owner 的 start。
// Python: is_task_start_meta（render.py L79-93）
func IsTaskStartMeta(meta map[string]any, taskID string) bool {
	if meta == nil {
		return false
	}
	tpl, _ := meta["template"].(string)
	if !taskStartTemplates[tpl] {
		return false
	}
	refs, _ := meta["refs"].(map[string]any)
	if refs == nil {
		return false
	}
	v, _ := refs["task"].(string)
	return v == taskID
}

// RenderLeaderTaskDone 注入 leader 的终态 digest，携带完成摘要。
// digest 取代成员自己的完成汇报（F_119）：点名任务、有界的完成摘要，以及
// 还有更多可读时（完成备注，或被截断的摘要）去哪读。
// 不带板面计数：digest 作为 follow-up 排队，可能在更新任务创建之后才到达，
// 这里快照的计数会被读成过时进度；leader 查实时看板。
// Python: render_leader_task_done（render.py L130-149）
func RenderLeaderTaskDone(task *database.TeamTaskBase, verified bool) string {
	howKey := "scheduler.leader_task_done_how_direct"
	if verified {
		howKey = "scheduler.leader_task_done_how_verified"
	}
	how, _ := schema.T(howKey)
	title := task.Title
	lines := []string{tKw("scheduler.leader_task_done", map[string]any{
		"task_id": task.TaskID, "title": title, "how": how,
	})}
	if summary := ateamprompts.CompletionSummary(task); summary != "" {
		lines = append(lines, tKw("scheduler.leader_task_done_summary", map[string]any{"summary": summary}))
	}
	note := ""
	if task.Note != nil {
		note = *task.Note
	}
	if strings.TrimSpace(note) != "" || ateamprompts.CompletionSummaryTruncated(task) {
		lines = append(lines, tKw("scheduler.leader_task_done_note_hint", map[string]any{"task_id": task.TaskID}))
	}
	if hint, err := schema.T("scheduler.leader_task_done_board_hint"); err == nil {
		lines = append(lines, hint)
	}
	return strings.Join(lines, "\n")
}

// RenderLeaderEscalationRetiredOwner 开放任务的承担者已关闭时注入 leader 的升级。
// Python: render_leader_escalation_retired_owner（render.py L188-196）
func RenderLeaderEscalationRetiredOwner(task *database.TeamTaskBase, owner string) string {
	return tKw("scheduler.leader_escalation_retired_owner", map[string]any{
		"task_id": task.TaskID, "title": task.Title,
		"owner": owner, "status": task.Status,
	})
}

// RenderLeaderAllDone 看板清空时注入 leader 的最终 digest。
// Python: render_leader_all_done（render.py L199-201）
func RenderLeaderAllDone(count int64) string {
	return tKw("scheduler.leader_all_done", map[string]any{"count": count})
}

// RenderReviewerNudge 提醒仍在运行未投票的临时 reviewer 收尾投票（25.2b 消费，
// i18n 键先落齐保持装配面完整）。Python: render_reviewer_nudge（render.py L204-206）
func RenderReviewerNudge(elapsedMinutes int64) string {
	return tKw("scheduler.reviewer_nudge", map[string]any{"elapsed": elapsedMinutes})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMetaRefsTask 组装 {template, refs:{task}} 形态的投递载荷。
func buildMetaRefsTask(template, taskID string) map[string]any {
	return ateamprompts.BuildMeta(template, map[string]string{"task": taskID}, nil)
}

// tKw 带占位符的 i18n 取值；键缺失返回错误串（键表静态，缺失属装配 bug）。
func tKw(key string, kwargs map[string]any) string {
	v, err := schema.T(key, kwargs)
	if err != nil {
		return "[" + key + "] " + strconv.Quote(err.Error())
	}
	return v
}

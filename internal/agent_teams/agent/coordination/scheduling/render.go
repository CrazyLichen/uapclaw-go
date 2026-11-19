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
	"fmt"
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
	templateReviewRequest  = "scheduler_review_request"           // 25.2b
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

// ──────────────────────────── 25.2b b 半区 ────────────────────────────

// MetaReviewRequest 新开一轮时某位验收人的投递载荷。
// Python: meta_review_request（render.py L96-98）。
func MetaReviewRequest(task *database.TeamTaskBase) map[string]any {
	return buildMetaRefsTask(templateReviewRequest, task.TaskID)
}

// MetaRework 该轮结算失败后作者侧的投递载荷。
// max_rounds 与 feedback 是 params 而非 refs：上限是**已解析**值（任务列可为
// NULL 回落 spec 默认值），feedback 汇总的是刚关闭那一轮的票行——任务行两者
// 在投递时都答不了。Python: meta_rework（render.py L101-114）。
func MetaRework(task *database.TeamTaskBase, maxRounds int64, feedback string) map[string]any {
	none, _ := schema.T("scheduler.none")
	if feedback == "" {
		feedback = none
	}
	return ateamprompts.BuildMeta(templateRework, map[string]string{"task": task.TaskID}, map[string]string{
		"max_rounds": fmt.Sprintf("%d", maxRounds),
		"feedback":   feedback,
	})
}

// MetaReworkSummary 要求承担者向 leader 发送返工总结的投递载荷。
// 与升级信一起发送（轮上限耗尽时），leader 在同一决策窗口内同时收到验收人
// 反馈与承担者的自我评估。Python: meta_rework_summary（render.py L116-127）。
func MetaReworkSummary(task *database.TeamTaskBase, maxRounds int64) map[string]any {
	return ateamprompts.BuildMeta(templateReworkSummary, map[string]string{"task": task.TaskID}, map[string]string{
		"max_rounds": fmt.Sprintf("%d", maxRounds),
	})
}

// RenderLeaderEscalationRounds 轮上限耗尽时注入 leader 的升级。
// Python: render_leader_escalation_rounds（render.py L152-161）。
func RenderLeaderEscalationRounds(task *database.TeamTaskBase, feedback string) string {
	none, _ := schema.T("scheduler.none")
	if feedback == "" {
		feedback = none
	}
	return tKw("scheduler.leader_escalation_rounds", map[string]any{
		"task_id": task.TaskID, "title": task.Title,
		"rounds": task.ReviewRound, "feedback": feedback,
	})
}

// RenderLeaderEscalationStall 评审轮无票停摆时注入 leader 的升级。
// Python: render_leader_escalation_stall（render.py L163-174）。
func RenderLeaderEscalationStall(task *database.TeamTaskBase, minutes int64, voted, pending []string) string {
	none, _ := schema.T("scheduler.none")
	votedText := strings.Join(voted, ", ")
	if len(voted) == 0 {
		votedText = none
	}
	pendingText := strings.Join(pending, ", ")
	if len(pending) == 0 {
		pendingText = none
	}
	return tKw("scheduler.leader_escalation_stall", map[string]any{
		"task_id": task.TaskID, "title": task.Title,
		"round": task.ReviewRound, "minutes": minutes,
		"voted": votedText, "pending": pendingText,
	})
}

// RenderLeaderEscalationNoVote reviewer 连续结束运行仍无票时注入 leader 的升级。
// Python: render_leader_escalation_no_vote（render.py L176-186）。
func RenderLeaderEscalationNoVote(task *database.TeamTaskBase, reviewer string, runs int64) string {
	return tKw("scheduler.leader_escalation_no_vote", map[string]any{
		"task_id": task.TaskID, "title": task.Title,
		"round": task.ReviewRound, "reviewer": reviewer, "runs": runs,
	})
}

// FormatFailFeedback 把各验收人的 fail 反馈拼成带署名的块。
// Python: format_fail_feedback（render.py L209-219）。
func FormatFailFeedback(failFeedback map[string]string) string {
	lines := make([]string, 0, len(failFeedback))
	for reviewer, feedback := range failFeedback {
		lines = append(lines, "- "+reviewer+": "+feedback)
	}
	return strings.Join(lines, "\n")
}

// submissionLabels 提交正文的两段标签。
// Python: _SUBMISSION_LABELS（render.py L222-225）。
var submissionLabels = map[string][2]string{
	"cn": {"摘要", "详情"},
	"en": {"Summary", "Detail"},
}

// SubmissionBody 验收人要看的承担者 summary 与完整 note，逐字（不截断）。
// reviewer 必须看到完整提交内容，所以这里一律不截断。每段带标签，缺失则省略。
// Python: _submission_body（render.py L227-235）。
func SubmissionBody(task *database.TeamTaskBase, language string) string {
	labels, exists := submissionLabels[language]
	if !exists {
		labels = submissionLabels["en"]
	}
	summaryLabel, noteLabel := labels[0], labels[1]
	var parts []string
	summary := ""
	if task.Summary != nil {
		summary = strings.TrimSpace(*task.Summary)
	}
	if summary != "" {
		parts = append(parts, summaryLabel+": "+summary)
	}
	note := ""
	if task.Note != nil {
		note = strings.TrimSpace(*task.Note)
	}
	if note != "" {
		parts = append(parts, noteLabel+":\n"+note)
	}
	return strings.Join(parts, "\n\n")
}

// renderReviewRequestForHarness 把评审请求渲染为临时 reviewer harness 的
// 纯用户提示词。临时 reviewer 每轮全新上下文，因此把上一轮自己的票
// （previousVote）渲染进请求：reviewer 可据此核对它要求的返工是否完成。
// 无上一票（首轮或上轮未投）则省略该段。承担者的完成 summary 与完整 note
// 逐字渲染，reviewer 无需 view_task 往返即可定位交付物；无则省略该段。
// upstream 任务行带完成摘要列出——评审依赖其他任务产物的 reviewer 知道
// 输入从哪来；无 upstream 任务省略该段。可选段省略后留下的空分隔行会被
// 折叠（\\n{3,} → \\n\\n）。
//
// Python: render_review_request_for_harness（render.py L238-308）。
func renderReviewRequestForHarness(
	task *database.TeamTaskBase,
	language, reviewer string,
	previousVote *database.TeamTaskReviewVoteBase,
	upstream []*database.TeamTaskBase,
) string {
	template, loadErr := ateamprompts.LoadTemplate(templateReviewRequest, language)
	if loadErr != nil {
		return ""
	}
	body := template.Content()
	body = strings.ReplaceAll(body, "{{task.task_id}}", task.TaskID)
	body = strings.ReplaceAll(body, "{{task.title}}", task.Title)
	body = strings.ReplaceAll(body, "{{task.content}}", task.Content)
	body = strings.ReplaceAll(body, "{{task.status}}", task.Status)
	assignee := ""
	if task.Assignee != nil {
		assignee = *task.Assignee
	}
	body = strings.ReplaceAll(body, "{{task.assignee}}", assignee)
	body = strings.ReplaceAll(body, "{{task.review_round}}", fmt.Sprintf("%d", task.ReviewRound))
	maxRounds := ""
	if task.MaxReviewRounds != nil {
		maxRounds = fmt.Sprintf("%d", *task.MaxReviewRounds)
	}
	body = strings.ReplaceAll(body, "{{task.max_review_rounds}}", maxRounds)
	reviewerNames := task.Reviewers()
	body = strings.ReplaceAll(body, "{{task.reviewer}}", strings.Join(reviewerNames, ", "))
	// 从结构化验收人列表注入该 reviewer 的专属指令。
	instruction := ""
	if reviewer != "" {
		for _, detail := range task.ReviewerDetails() {
			if detail["reviewer_id"] == reviewer {
				instruction = detail["instruction"]
				break
			}
		}
	}
	body = strings.ReplaceAll(body, "{{task.reviewer_instruction}}", instruction)
	previous := ""
	if previousVote != nil {
		prevTpl, prevErr := ateamprompts.LoadTemplate(templateReviewPrevFB, language)
		if prevErr == nil {
			previous = strings.TrimSpace(prevTpl.Content())
			none, _ := schema.T("scheduler.none")
			feedback := none
			if previousVote.Feedback != nil {
				feedback = *previousVote.Feedback
			}
			previous = strings.ReplaceAll(previous, "{{vote.review_round}}", fmt.Sprintf("%d", previousVote.ReviewRound))
			previous = strings.ReplaceAll(previous, "{{vote.decision}}", previousVote.Decision)
			previous = strings.ReplaceAll(previous, "{{vote.feedback}}", feedback)
		}
	}
	body = strings.ReplaceAll(body, "{{task.previous_feedback}}", previous)
	submissionNote := ""
	submissionBody := SubmissionBody(task, language)
	if submissionBody != "" {
		if noteTpl, noteErr := ateamprompts.LoadTemplate(templateReviewSubNote, language); noteErr == nil {
			submissionNote = strings.TrimSpace(noteTpl.Content())
			submissionNote = strings.ReplaceAll(submissionNote, "{{task.submission_body}}", submissionBody)
		}
	}
	body = strings.ReplaceAll(body, "{{task.submission_note}}", submissionNote)
	upstreamSection := ""
	if len(upstream) > 0 {
		if upTpl, upErr := ateamprompts.LoadTemplate(templateReviewUpstream, language); upErr == nil {
			upstreamSection = strings.TrimSpace(upTpl.Content())
			upstreamSection = strings.ReplaceAll(upstreamSection, "{{task.upstream_brief}}", ateamprompts.UpstreamBrief(upstream, language))
		}
	}
	body = strings.ReplaceAll(body, "{{task.upstream_section}}", upstreamSection)
	// 上方省略的可选段会留下空分隔行——折叠连续 3 个以上换行。
	return collapseBlankLines(body)
}

// collapseBlankLines 折叠连续空行（\\n{3,} → \\n\\n）。
// Python: re.sub(r"\\n{3,}", "\\n\\n", body)。
func collapseBlankLines(body string) string {
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	return body
}

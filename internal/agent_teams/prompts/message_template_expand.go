// 本文件是 message_template.py（560 行）两阶段渲染引擎的 25.2a 收回部分：
// parse_meta / build_meta / expand_message / _resolve_namespaces / _substitute /
// task_links / revision_getters，以及 completion_summary / upstream_brief /
// subtask_brief 等任务简报渲染器。25.0c 时曾登记 ⤵️25.6，因 25.2 调度 handoff
// 链（信箱行 content 为空、投递时按当前任务行渲染，F_63）成为硬前置而提前。
//
// 占位符契约（message_template.py 模块 docstring）：
//   - 单一语法 {{ns.field}}，单遍 re.sub 解析——替换值**永不重扫**，LLM 撰写的
//     任务正文里含 {{...}} 无法注入占位符；
//   - 命名空间声明显式字段白名单（无 getattr 透传），新 DB 列不会静默泄入提示词；
//   - 未知命名空间 / 字段 / 缺行渲染为 <missing:ns.field> 而非报错——lazy-safe。
//
// 模板是 prompts/<lang>/<key>.md 代码资产，永不入库：改模板立即对已排队消息
// 生效，半渲染的 {{...}} 字符串不可能到达读者。
//
// Python: openjiuwen/agent_teams/message_template.py（L32-431 / L252-300 / L442-545）
package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentTeam

const (
	// SummaryRenderLimit 完成摘要的渲染长度上限（F_119）。存储值永不截断，
	// 只有轻量渲染位（digest、简报）截断。
	// Python: SUMMARY_RENDER_LIMIT = 200（message_template.py L124）
	SummaryRenderLimit = 200

	// RevisionDiffMinChars 正文短于该值时不渲染 revision diff（差异太小，
	// 直接看全文更清楚）。Python: REVISION_DIFF_MIN_CHARS = 2000（schema/task.py L149）
	RevisionDiffMinChars = 2000

	// diffContextLines2 revision diff 每个变更区域保留的上下文行数（D9）。
	// Python: _DIFF_CONTEXT_LINES = 2
	diffContextLines2 = 2

	// refTask / refMember / nsParam meta.refs 的引用键与 param 命名空间。
	// Python: _REF_TASK/_REF_MEMBER/_NS_PARAM（message_template.py L56-58）
	refTask   = "task"
	refMember = "member"
	nsParam   = "param"
)

// placeholderRe {{ns.field}} 占位符（允许花括号内空白）。
// Python: _PLACEHOLDER_RE（message_template.py L55）
var placeholderRe = regexp.MustCompile(`\{\{\s*([a-z_]+)\.([a-z_]+)\s*\}\}`)

// blankRunRe 三连以上换行折叠为双换行（可选段省略后留下的空隔）。
// Python: _BLANK_RUN_RE（message_template.py L56）
var blankRunRe = regexp.MustCompile(`\n{3,}`)

// nonePhrase 各语言「无」。
// Python: _NONE_PHRASE（message_template.py L76-77）
var nonePhrase = map[string]string{"cn": "无", "en": "none"}

// changeNotesHeading / diffHeading F_129 两个 section 的标题。
// Python: _CHANGE_NOTES_HEADING / _DIFF_HEADING（message_template.py L81-86）
var changeNotesHeading = map[string]string{
	"cn": "## 修改说明",
	"en": "## What changed",
}
var diffHeading = map[string]string{
	"cn": "## 与你上次看到的版本相比（仅作定位，以完整内容为准）",
	"en": "## Compared with the version you last saw (a locator only — the full content is authoritative)",
}

// staleMark 子任务陈旧标记。Python: _STALE_MARK（message_template.py L87-88）
var staleMark = map[string]string{"cn": "需复核", "en": "needs review"}

// upstreamNoteHint 上游简报的收尾提示。
// Python: _UPSTREAM_NOTE_HINT（message_template.py L90-93）
var upstreamNoteHint = map[string]string{
	"cn": "需要某个上游任务的详细完成说明时，用 `view_task(action='note', task_id=...)` 查看。",
	"en": "For an upstream task's full completion note, call `view_task(action='note', task_id=...)`.",
}

// taskLinkFields 计算型任务字段（F_118 / D7）。
// Python: _TASK_LINK_FIELDS（message_template.py L71-72）
var taskLinkFields = []string{"upstream_brief", "downstream_brief"}

// revisionFields F_129 计算型 section 字段。
// Python: _REVISION_FIELDS（message_template.py L79-80）
var revisionFields = []string{"change_notes", "revision_diff", "subtask_brief"}

// ──────────────────────────── 结构体 ────────────────────────────

// ExpandedMessage 一条消息行渲染出的投递文本。
// Python: ExpandedMessage（message_template.py L224-248）
type ExpandedMessage struct {
	// Body 渲染后的文档（模板消息）或发送者原始 content（普通消息）。
	Body string
	// IsTemplate 该行是否携带框架模板。投递位用它丢弃回复提示——框架指令
	// 用工具调用应答，不是回复。
	IsTemplate bool
	// ShownTaskID / ShownRevision 文档展示了该任务的 content 时的 (task_id,
	// revision)（F_129）：投递位据此把它记为 assignee 已见。HasShownRevision
	// 区分「展示了 revision 0」与「未展示」。
	ShownTaskID      string
	ShownRevision    int64
	HasShownRevision bool
}

// ExpandGetters 展开所需的行查询。窄函数而非 DB 句柄：两个投递位（进程内
// 信箱 drain、外部 inbox）经不同对象取行，测试可一行伪造。
// Python: TaskGetter / MemberGetter / TaskLinksGetter / RevisionSnapshotGetter /
// SubtasksGetter 类型别名（message_template.py L37-52）
type ExpandGetters struct {
	// TaskGetter 按 task_id 取任务行（不存在返回 nil, nil）。
	TaskGetter func(ctx context.Context, taskID string) (*database.TeamTaskBase, error)
	// MemberGetter 按 member_name 取成员行（不存在返回 nil, nil）。
	MemberGetter func(ctx context.Context, memberName string) (*database.TeamMember, error)
	// TaskLinksGetter 取 (upstream, downstream) 任务行（可选；nil 时简报渲染为「无」，F_118/D7）。
	TaskLinksGetter func(ctx context.Context, taskID string) ([]*database.TeamTaskBase, []*database.TeamTaskBase, error)
	// RevisionSnapshotGetter 取 (task_id, revision) 时刻的正文快照（F_129/D9；可选；nil 时不渲染 diff）。
	RevisionSnapshotGetter func(ctx context.Context, taskID string, revision int64) (*string, error)
	// SubtasksGetter 取子任务行（F_129/D5；可选；nil 时子任务简报渲染为「无」）。
	SubtasksGetter func(ctx context.Context, taskID string) ([]*database.TeamTaskBase, error)
	// Loader 模板加载器。nil 用框架只读 LoadTemplate；团队 handler 可绑定
	// workspace cache（scheduler_* 模板读演进值）。
	Loader func(name, language string) (*PromptTemplate, error)
	// Language 渲染语言（"cn"/"en"）。
	Language string
}

// refUnresolvedError meta.refs 指名的行取不到。与「命名空间答不上的占位符」
// 不同：缺*字段*是模板 bug，行内渲染 <missing:ns.field>；缺*行*（任务被清、
// 成员被移除）意味着文档没有主语——投递降级为 fallback 行，而不是投出一篇
// 千疮百孔的简报。
// Python: RefUnresolved（message_template.py L209-221）
type refUnresolvedError struct{ msg string }

func (e *refUnresolvedError) Error() string { return e.msg }

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseMeta 解析 meta 列为模板描述符；无 meta / 坏 JSON / 缺 template 键均返回
// nil——都意味着「普通消息，content 原样渲染」。
// Python: parse_meta（message_template.py L252-269）
func ParseMeta(raw *string) map[string]any {
	if raw == nil || *raw == "" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(*raw), &meta); err != nil {
		logger.Warn(logComponent).Msg("[message_template] malformed meta, treating as a plain message")
		return nil
	}
	if _, ok := meta["template"]; !ok {
		return nil
	}
	return meta
}

// BuildMeta 组装一条模板消息的投递载荷。refs 是*投递时*解析的行绑定
// （{"task": task_id} / {"member": member_name}）——表能回答的都放这里，读者
// 看到的是当下真相而非快照；params 是表答不了的瞬态标量（如聚合的 reviewer
// 反馈块），发送时冻结。
// Python: build_meta（message_template.py L271-289）
func BuildMeta(template string, refs map[string]string, params map[string]string) map[string]any {
	meta := map[string]any{"template": template}
	if len(refs) > 0 {
		meta["refs"] = refs
	}
	if len(params) > 0 {
		paramsStr := make(map[string]string, len(params))
		for k, v := range params {
			paramsStr[k] = v
		}
		meta["params"] = paramsStr
	}
	return meta
}

// FallbackLine 展开失败时合成一行替身。刻意不存副本：meta 里的 template key
// 和 task_id 已足够读者恢复（view_task 补全其余），健康投递为几乎不触发的
// fallback 付出零成本。
// Python: fallback_line（message_template.py L291-303）
func FallbackLine(meta map[string]any) string {
	template := "?"
	if v, ok := meta["template"].(string); ok && v != "" {
		template = v
	}
	refs, _ := meta["refs"].(map[string]any)
	taskID := ""
	if refs != nil {
		if v, ok := refs[refTask].(string); ok {
			taskID = v
		}
	}
	if taskID != "" {
		return fmt.Sprintf("[%s] task_id=%s — details unavailable, call view_task for the task.", template, taskID)
	}
	return fmt.Sprintf("[%s] — details unavailable.", template)
}

// CompletionSummary 把任务完成摘要渲染为一行有界文本。
// 作者没写 summary 时回落到完成 note 的第一个非空行；换行折叠为空格，超过
// SummaryRenderLimit 截断加尾随「…」。
// Python: completion_summary（message_template.py L128-149）
func CompletionSummary(task *database.TeamTaskBase) string {
	summary := ""
	if task.Summary != nil {
		summary = strings.Join(strings.Fields(*task.Summary), " ")
	}
	if summary != "" {
		return truncateWithEllipsis(summary, SummaryRenderLimit)
	}
	if task.Note == nil {
		return ""
	}
	for _, line := range strings.Split(*task.Note, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return truncateWithEllipsis(strings.Join(strings.Fields(line), " "), SummaryRenderLimit)
	}
	return ""
}

// CompletionSummaryTruncated CompletionSummary 是否丢过正文：渲染行被截断，
// 或回落到了 note 而 note 不止一行。
// Python: completion_summary_truncated（message_template.py L151-163）
func CompletionSummaryTruncated(task *database.TeamTaskBase) bool {
	if task.Summary != nil {
		summary := strings.Join(strings.Fields(*task.Summary), " ")
		if summary != "" {
			return len([]rune(summary)) > SummaryRenderLimit
		}
	}
	if task.Note == nil {
		return false
	}
	var noteLines []string
	for _, line := range strings.Split(*task.Note, "\n") {
		if strings.TrimSpace(line) != "" {
			noteLines = append(noteLines, line)
		}
	}
	if len(noteLines) == 0 {
		return false
	}
	first := strings.Join(strings.Fields(noteLines[0]), " ")
	return len(noteLines) > 1 || len([]rune(first)) > SummaryRenderLimit
}

// UpstreamLines 每个上游任务一条 bullet：
// `- task_id · title · assignee · <完成摘要>`。
// Python: upstream_lines（message_template.py L172-181）
func UpstreamLines(upstream []*database.TeamTaskBase) []string {
	lines := make([]string, 0, len(upstream))
	for _, task := range upstream {
		lines = append(lines, "- "+TaskLine(task, true))
	}
	return lines
}

// UpstreamBrief 每个上游任务一行（带完成摘要），收尾一行提示
// view_task(action='note') 可看完整说明。无上游任务返回 ""。
// Python: upstream_brief（message_template.py L183-201）
func UpstreamBrief(upstream []*database.TeamTaskBase, language string) string {
	lines := UpstreamLines(upstream)
	if len(lines) == 0 {
		return ""
	}
	hint, ok := upstreamNoteHint[language]
	if !ok {
		hint = upstreamNoteHint["en"]
	}
	lines = append(lines, hint)
	return strings.Join(lines, "\n")
}

// TaskLine 任务一行简报：`task_id · title · assignee · <summary?>`。
// Python: _task_line（message_template.py L165-170）
func TaskLine(task *database.TeamTaskBase, withSummary bool) string {
	parts := []string{task.TaskID + " · " + task.Title}
	if task.Assignee != nil && *task.Assignee != "" {
		parts = append(parts, "· "+*task.Assignee)
	}
	if withSummary {
		if summary := CompletionSummary(task); summary != "" {
			parts = append(parts, "· "+summary)
		}
	}
	return strings.Join(parts, " ")
}

// SubtaskBrief 每个子任务一条 bullet：`- id · title · assignee · status`，
// 落后父 revision 的加陈旧标记（F_129 / D5）。
// Python: subtask_brief（message_template.py L478-492）
func SubtaskBrief(subtasks []*database.TeamTaskBase, parentRevision int64, language string) string {
	if len(subtasks) == 0 {
		return phraseNone(language)
	}
	mark := phraseMark(language)
	lines := make([]string, 0, len(subtasks))
	for _, row := range subtasks {
		line := "- " + TaskLine(row, false) + " · " + row.Status
		parent := parentRevision
		if stale := StaleAgainst(row, &parent); stale != nil {
			line += fmt.Sprintf(" · [%s: r%d → r%d]", mark, stale[1], stale[0])
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// StaleAgainst 开放子任务落后其父任务时返回 (parent_revision, based_on)。
// 顶层任务 / 已终态 / 基于父当前 revision 时返回 nil。
// Python: stale_against（tools/database/graph.py L32-52）
func StaleAgainst(task *database.TeamTaskBase, parentRevision *int64) []int64 {
	if task.ParentTaskID == nil || parentRevision == nil {
		return nil
	}
	if task.Status == fsm.TaskStatusCompleted || task.Status == fsm.TaskStatusCancelled {
		return nil
	}
	basedOn := task.ParentRevision
	if basedOn >= *parentRevision {
		return nil
	}
	return []int64{*parentRevision, basedOn}
}

// ExpandMessage 把一条消息行渲染为投递文本。
//
// 普通消息原样通过。模板消息按收件人语言加载 markdown，对照 meta.refs 指名的
// *当前* 行解析全部 {{ns.field}}，返回整篇文档；任何失败（模板没了、任务行
// 被删、meta 不可读）降级为 fallback 行——投递永不因渲染问题而死。
// Python: expand_message（message_template.py L304-365）
func ExpandMessage(ctx context.Context, msg *database.TeamMessageBase, getters ExpandGetters) (ExpandedMessage, error) {
	meta := ParseMeta(msg.Meta)
	if meta == nil {
		return ExpandedMessage{Body: msg.Content, IsTemplate: false}, nil
	}
	loader := getters.Loader
	if loader == nil {
		loader = LoadTemplate
	}
	templateKey, _ := meta["template"].(string)
	template, err := loader(templateKey, getters.Language)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("template", templateKey).
			Msg("[message_template] expansion of template failed, delivering the fallback line")
		return ExpandedMessage{Body: FallbackLine(meta), IsTemplate: true}, nil
	}
	body := template.Content()

	// 链接简报要额外查询；只有模板真的渲染其一才付代价（今天：task-start handoff）。
	linksWanted := false
	for _, field := range taskLinkFields {
		if strings.Contains(body, refTask+"."+field) {
			linksWanted = true
			break
		}
	}
	revisionWanted := map[string]bool{}
	for _, field := range revisionFields {
		if strings.Contains(body, refTask+"."+field) {
			revisionWanted[field] = true
		}
	}

	values, task, rerr := resolveNamespaces(ctx, meta, getters, linksWanted)
	if rerr != nil {
		logger.Warn(logComponent).Err(rerr).Str("template", templateKey).
			Msg("[message_template] expansion of template failed, delivering the fallback line")
		return ExpandedMessage{Body: FallbackLine(meta), IsTemplate: true}, nil
	}
	if task != nil && len(revisionWanted) > 0 {
		for field, value := range revisionFieldsSections(ctx, task, revisionWanted, getters) {
			values[refTask][field] = value
		}
	}
	body = substitute(body, values)
	if len(revisionWanted) > 0 {
		// 空 revision section 留下的空行折叠掉。
		body = strings.TrimSpace(blankRunRe.ReplaceAllString(body, "\n\n"))
	}
	out := ExpandedMessage{Body: body, IsTemplate: true}
	if task != nil && strings.Contains(template.Content(), refTask+".content") {
		out.ShownTaskID = task.TaskID
		out.ShownRevision = task.Revision
		out.HasShownRevision = true
	}
	return out, nil
}

// TaskLinks 从 TaskDao 取 (upstream, downstream) 行。两个投递位（进程内
// drain、外部 inbox）绑定的唯一 DB 形状助手，两者对"简报显示哪些行"永不漂移。
// 被清掉的任务行直接缺席。
// Python: task_links（message_template.py L494-512）
func TaskLinks(ctx context.Context, dao database.TaskDao, taskID string) ([]*database.TeamTaskBase, []*database.TeamTaskBase, error) {
	deps, err := dao.GetTaskDependencies(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	upstreamIDs := make([]string, 0, len(deps))
	for _, dep := range deps {
		upstreamIDs = append(upstreamIDs, dep.DependsOnID)
	}
	downstreamIDs, err := dao.GetDependentTaskIDs(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(upstreamIDs)+len(downstreamIDs))
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, upstreamIDs...), downstreamIDs...) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	rows, err := dao.GetTasksByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	rowMap := make(map[string]*database.TeamTaskBase, len(rows))
	for _, row := range rows {
		rowMap[row.TaskID] = row
	}
	upstream := make([]*database.TeamTaskBase, 0, len(upstreamIDs))
	for _, id := range upstreamIDs {
		if row, ok := rowMap[id]; ok {
			upstream = append(upstream, row)
		}
	}
	downstream := make([]*database.TeamTaskBase, 0, len(downstreamIDs))
	for _, id := range downstreamIDs {
		if row, ok := rowMap[id]; ok {
			downstream = append(downstream, row)
		}
	}
	return upstream, downstream, nil
}

// RevisionGetters 两个投递位绑定的 F_129 getters（基于 TaskDao）。
// Python: revision_getters（message_template.py L514-528）
func RevisionGetters(dao database.TaskDao, teamName string) (func(context.Context, string, int64) (*string, error), func(context.Context, string) ([]*database.TeamTaskBase, error)) {
	snapshot := func(ctx context.Context, taskID string, revision int64) (*string, error) {
		return dao.GetRevisionSnapshot(ctx, teamName, taskID, revision)
	}
	subtasks := func(ctx context.Context, taskID string) ([]*database.TeamTaskBase, error) {
		return dao.GetSubtasks(ctx, teamName, taskID)
	}
	return snapshot, subtasks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateWithEllipsis 按 rune 截断并加尾随「…」。
func truncateWithEllipsis(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

// phraseNone / phraseMark 各语言短语，缺语言回落 en（对齐 .get(language, [\"en\"])）。
func phraseNone(language string) string {
	if v, ok := nonePhrase[language]; ok {
		return v
	}
	return nonePhrase["en"]
}

func phraseMark(language string) string {
	if v, ok := staleMark[language]; ok {
		return v
	}
	return staleMark["en"]
}

// taskFieldValue task.* 白名单字段读取器——永不 raw getattr，加 DB 列不会
// 意外扩宽提示词面。
// Python: _TASK_FIELDS（message_template.py L60-69）
func taskFieldValue(task *database.TeamTaskBase, field string) (string, bool) {
	switch field {
	case "task_id":
		return task.TaskID, true
	case "title":
		return task.Title, true
	case "content":
		return task.Content, true
	case "status":
		return task.Status, true
	case "assignee":
		if task.Assignee != nil {
			return *task.Assignee, true
		}
		return "", true
	case "reviewer":
		return strings.Join(TaskReviewers(task), ", "), true
	case "review_round":
		return strconv.FormatInt(task.ReviewRound, 10), true
	case "max_review_rounds":
		if task.MaxReviewRounds == nil {
			return "", true
		}
		return strconv.FormatInt(*task.MaxReviewRounds, 10), true
	case "revision":
		return strconv.FormatInt(task.Revision, 10), true
	default:
		return "", false
	}
}

// memberFieldValue member.* 白名单字段读取器。
// Python: _MEMBER_FIELDS（message_template.py L203-207）
func memberFieldValue(member *database.TeamMember, field string) (string, bool) {
	switch field {
	case "member_name":
		return member.MemberName, true
	case "display_name":
		return member.DisplayName, true
	case "desc":
		return member.Desc, true
	default:
		return "", false
	}
}

// resolveNamespaces 取 refs 指名的行并投影到白名单。返回 values 与被引用的
// 任务行（无则 nil）。refs 指名的行不存在时返回 refUnresolvedError——调用方
// 降级为 fallback 行。
// Python: _resolve_namespaces（message_template.py L367-405）
func resolveNamespaces(ctx context.Context, meta map[string]any, getters ExpandGetters, linksWanted bool) (map[string]map[string]string, *database.TeamTaskBase, error) {
	values := map[string]map[string]string{nsParam: {}}
	params, _ := meta["params"].(map[string]any)
	for key, value := range params {
		values[nsParam][key] = toStringValue(value)
	}
	refs, _ := meta["refs"].(map[string]any)

	var task *database.TeamTaskBase
	taskID := ""
	if refs != nil {
		taskID = toStringValue(refs[refTask])
	}
	if taskID != "" && getters.TaskGetter != nil {
		row, err := getters.TaskGetter(ctx, taskID)
		if err != nil {
			return nil, nil, err
		}
		if row == nil {
			return nil, nil, &refUnresolvedError{msg: "task " + taskID + " not found"}
		}
		fields := map[string]string{}
		for name := range taskFieldNames {
			if v, ok := taskFieldValue(row, name); ok {
				fields[name] = v
			}
		}
		if linksWanted && getters.TaskLinksGetter != nil {
			upstream, downstream, err := getters.TaskLinksGetter(ctx, taskID)
			if err != nil {
				return nil, nil, err
			}
			briefs := taskBriefs(upstream, downstream, getters.Language)
			for k, v := range briefs {
				fields[k] = v
			}
		}
		values[refTask] = fields
		task = row
	}

	memberName := ""
	if refs != nil {
		memberName = toStringValue(refs[refMember])
	}
	if memberName != "" && getters.MemberGetter != nil {
		member, err := getters.MemberGetter(ctx, memberName)
		if err != nil {
			return nil, nil, err
		}
		if member == nil {
			return nil, nil, &refUnresolvedError{msg: "member " + memberName + " not found"}
		}
		fields := map[string]string{}
		for name := range memberFieldNames {
			if v, ok := memberFieldValue(member, name); ok {
				fields[name] = v
			}
		}
		values[refMember] = fields
	}

	return values, task, nil
}

// taskFieldNames / memberFieldNames 白名单键集合（驱动投影循环）。
var taskFieldNames = map[string]bool{
	"task_id": true, "title": true, "content": true, "status": true,
	"assignee": true, "reviewer": true, "review_round": true,
	"max_review_rounds": true, "revision": true,
}
var memberFieldNames = map[string]bool{
	"member_name": true, "display_name": true, "desc": true,
}

// taskBriefs 链接简报（F_118 / D7）：上游行带作者完成摘要（F_119），
// 完整细节一次 view_task(action='note') 之遥。
// Python: _task_briefs（message_template.py L203-208 上方的 _task_briefs）
func taskBriefs(upstream, downstream []*database.TeamTaskBase, language string) map[string]string {
	none := phraseNone(language)
	up := UpstreamBrief(upstream, language)
	if up == "" {
		up = none
	}
	if len(downstream) == 0 {
		return map[string]string{"upstream_brief": up, "downstream_brief": none}
	}
	lines := make([]string, 0, len(downstream))
	for _, task := range downstream {
		lines = append(lines, "- "+TaskLine(task, false))
	}
	return map[string]string{"upstream_brief": up, "downstream_brief": strings.Join(lines, "\n")}
}

// revisionFieldsSections 渲染请求的 F_129 section；空的为 ""。
// Python: _revision_fields（message_template.py L407-421）
func revisionFieldsSections(ctx context.Context, task *database.TeamTaskBase, wanted map[string]bool, getters ExpandGetters) map[string]string {
	out := map[string]string{}
	if wanted["change_notes"] {
		lines := ChangeNotesLines(task)
		heading := changeNotesHeading[getters.Language]
		if heading == "" {
			heading = changeNotesHeading["en"]
		}
		if len(lines) > 0 {
			parts := append([]string{heading, ""}, lines...)
			out["change_notes"] = strings.Join(parts, "\n")
		} else {
			out["change_notes"] = ""
		}
	}
	if wanted["revision_diff"] {
		out["revision_diff"] = revisionDiffSection(ctx, task, getters)
	}
	if wanted["subtask_brief"] {
		var rows []*database.TeamTaskBase
		if getters.SubtasksGetter != nil {
			rows, _ = getters.SubtasksGetter(ctx, task.TaskID)
		}
		out["subtask_brief"] = SubtaskBrief(rows, task.Revision, getters.Language)
	}
	return out
}

// revisionDiffSection 渲染 F_129 diff section；无可渲染时 ""。
// Python: _revision_diff_section（message_template.py L423-441）
func revisionDiffSection(ctx context.Context, task *database.TeamTaskBase, getters ExpandGetters) string {
	observed := task.ObservedRevision
	current := task.Content
	if getters.RevisionSnapshotGetter == nil || observed >= task.Revision {
		return ""
	}
	previous, err := getters.RevisionSnapshotGetter(ctx, task.TaskID, observed)
	if err != nil {
		return ""
	}
	if previous == nil {
		return ""
	}
	prevLen := len([]rune(*previous))
	curLen := len([]rune(current))
	if prevLen < RevisionDiffMinChars && curLen < RevisionDiffMinChars {
		return ""
	}
	diff := RevisionDiff(*previous, current)
	if diff == "" {
		return ""
	}
	heading := diffHeading[getters.Language]
	if heading == "" {
		heading = diffHeading["en"]
	}
	return heading + "\n\n```diff\n" + diff + "\n```"
}

// substitute 单遍替换全部 {{ns.field}}（替换值永不重扫）。
// Python: _substitute（message_template.py L531-545）
func substitute(template string, values map[string]map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(template, func(match string) string {
		sub := placeholderRe.FindStringSubmatch(match)
		namespace, field := sub[1], sub[2]
		resolved, ok := values[namespace][field]
		if !ok {
			return "<missing:" + namespace + "." + field + ">"
		}
		return resolved
	})
}

// toStringValue meta JSON 里的标量转字符串（params/refs 值域）。
func toStringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// TaskReviewers 解析 reviewer JSON 列为 reviewer_id 列表。旧格式（字符串列表）
// 读取时自动升级为新对象格式。未设置或损坏返回空列表——无 reviewer 集即
// 「无 verify 门」，完成路径直接 COMPLETED。
// Python: TeamTaskBase.reviewers()（tools/models.py L195-232）
func TaskReviewers(task *database.TeamTaskBase) []string {
	if task.Reviewer == nil || *task.Reviewer == "" {
		return []string{}
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(*task.Reviewer), &parsed); err != nil {
		// 旧格式：["name1","name2"]（字符串数组）
		var legacy []string
		if err2 := json.Unmarshal([]byte(*task.Reviewer), &legacy); err2 == nil {
			return legacy
		}
		return []string{}
	}
	names := make([]string, 0, len(parsed))
	for _, item := range parsed {
		if id, ok := item["reviewer_id"].(string); ok && id != "" {
			names = append(names, id)
		}
	}
	return names
}

package prompts

import (
	"strings"

	"github.com/wk8/go-ordered-map/v2"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// 本文件提供投递到成员对话历史的团队状态消息正文。
//
// 与 sections.go 相对：那个文件为 system prompt 组装 PromptSection，本文件渲染
// 成员**被随时间告知**的团队状态——自己的身份、团队元数据、成员名册。
//
// 这些正文不属于 system prompt（它们要么成员间不同，要么团队存在后才出现），
// 也不再走每轮提示词附件：附件在每次模型调用时重新编码、永远无法命中 KV 缓存。
// 在数据首次出现的时刻一次性写进对话历史，同样的 token 只编码一次、之后一直被缓存。
//
// 投递、探针与持久化基线在 25.0c 的 TeamContextTracker；本文件只做纯渲染 + 名册 diff。
//
// Python: openjiuwen/agent_teams/prompts/messages.py (500 行)

// ──────────────────────────── 结构体 ────────────────────────────

// RosterMember 名册行，本文件所有渲染函数的输入。
//
// Python: _list_roster() 产出的扁平 dict (team_context.py L598-624)
type RosterMember struct {
	// MemberName 语义成员标识，diff 的 key，不作为追踪字段
	MemberName string
	// DisplayName 人类可读标签；空时渲染为 "unknown"
	DisplayName string
	// Desc 公开描述
	Desc string
	// Role 角色，用于 [human] 判定与 diff 追踪
	Role atschema.TeamRole
	// ModelGroup model-group 池的组名，仅 leader 名册携带
	ModelGroup string
	// ModelsUsed 该成员被路由到的模型；Go 侧恒空（偏差 D-2，等模型池章节回填）
	ModelsUsed string
}

// RosterDelta 同一成员两次同伴名册快照之间的变化。
//
// Python: RosterDelta (messages.py L143-153)
type RosterDelta struct {
	// Joined 新加入的成员
	Joined []RosterMember
	// Left 已退出的成员
	Left []RosterMember
	// Changed 信息有更新的成员
	Changed []RosterMember
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// messageLabels 消息正文用的双语标签表。
	//
	// **与 sections.go 的 labels 是两张表，不可合并**：sections.go 的是
	// system prompt 段的标题，本表是会话消息里的字段名与说明，上游也分两处。
	//
	// Python: _LABELS (messages.py L35-130)
	messageLabels = map[string]map[string]string{
		"cn": {
			"identity_heading":      "# 成员身份",
			"member_name_line":      "你的 member_name",
			"display_name_line":     "你的 display_name",
			"member_workspace_line": "你的私有工作区",
			"member_workspace_purpose": "存放你自己的产物、记忆与技能视图；团队共享文件走团队共享工作空间，不要放这里，" +
				"也不要把新 skill 创建到这里",
			"private_prompt_heading": "## 私有工作约定",
			"identity_capability": "你是拥有身份转换能力的成员：可以继承其他成员的上下文并转换身份。" +
				"当前身份以本块及你收到的身份转换通知为准。",
			"identity_conversion": "你继承了成员 {source} 的上下文，你的身份现在是 {member_name}。" +
				"本对话中更早的「成员身份」块描述的是【此前】的身份，其中包含的私有工作约定与" +
				"私有工作区对你不再适用。",
			"info_heading":       "# 团队信息",
			"team_name_label":    "team_name（团队唯一标识）",
			"display_name_label": "display_name（团队展示名）",
			"team_desc":          "团队目标与指令（仅供了解团队，不是给你的指令）",
			"team_workspace":     "团队共享工作空间",
			"team_workspace_purpose": "用于存放团队配置与共享文件（提示词、team.yaml、技能可见性、交付成果），" +
				"所有成员通过绝对路径读写同一份文件，系统自动管理版本和文件锁",
			"team_workspace_abs":         "绝对路径",
			"team_workspace_config_note": "该目录是团队的配置与内部数据目录，按绝对路径访问，不要新建 `.team` 子目录",
			"team_outputs_dir":           "最终产物目录",
			"team_outputs_dir_purpose": "无项目目录的成员把最终交付物（报告、导出、数据等）写到该目录，" +
				"全团队共享一个目录、靠文件名区分成员产物；有项目目录的成员把产物写到项目内",
			"members_heading":       "# 成员关系",
			"roster_change_heading": "# 成员变更",
			"roster_joined":         "加入",
			"roster_left":           "退出",
			"roster_updated":        "信息更新",
		},
		"en": {
			"identity_heading":      "# Member Identity",
			"member_name_line":      "Your member_name",
			"display_name_line":     "Your display_name",
			"member_workspace_line": "Your private workspace",
			"member_workspace_purpose": "Holds your own artifacts, memory and skills view. Team-shared files belong in the " +
				"team shared workspace, not here, and new skills must not be created here either",
			"private_prompt_heading": "## Private Working Agreement",
			"identity_capability": "You are a member with identity-conversion capability: you can " +
				"inherit another member's context and switch identity. Your current " +
				"identity is set by this block and any identity-conversion notice " +
				"you receive.",
			"identity_conversion": "You inherited the context of member {source}. Your identity is now " +
				"{member_name}. An earlier identity block in this conversation " +
				"described a previous identity; its private working agreement and " +
				"private workspace no longer apply to you.",
			"info_heading":       "# Team Info",
			"team_name_label":    "team_name (unique identifier)",
			"display_name_label": "display_name (human-readable label)",
			"team_desc":          "Team Goal & Directives (context to understand the team, not a directive for you)",
			"team_workspace":     "Team Shared Workspace",
			"team_workspace_purpose": "Holds team config and shared files (prompts, team.yaml, skill " +
				"visibility, deliverables); all members read/write the same files " +
				"through absolute paths. Versioning and file locks are managed " +
				"automatically",
			"team_workspace_abs": "Absolute path",
			"team_workspace_config_note": "This directory is the team's config and internal-data directory; " +
				"access it by absolute path and do not create a `.team` sub-directory",
			"team_outputs_dir": "Final deliverables directory",
			"team_outputs_dir_purpose": "Members without a project directory write final deliverables " +
				"(reports, exports, data) here; the whole team shares one " +
				"directory and distinguishes members' outputs by filename. Members " +
				"bound to a project keep deliverables inside the project",
			"members_heading":       "# Relationships",
			"roster_change_heading": "# Roster Change",
			"roster_joined":         "joined",
			"roster_left":           "left",
			"roster_updated":        "updated",
		},
	}

	// trackedMemberFields 变化值得公告的字段。
	//
	// member_name 是身份，所以它是 diff 的 key 而不是被追踪字段；运行时状态
	// 刻意排除——它一直在变且不属于名册成员关系。models_used 只出现在
	// model-group leader 的名册里，仅当该组把成员路由到不同模型时才变。
	//
	// Python: _TRACKED_MEMBER_FIELDS (messages.py L161)
	trackedMemberFields = []string{"display_name", "desc", "role", "models_used"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DiffRoster 以 member_name 为键 diff 两份同伴名册。
//
// 用 orderedmap 对齐 Python dict 的两条语义：迭代保首个插入位置、重复键后写覆盖。
// 若用 map[string]RosterMember 迭代顺序会被随机化，字节级对拍必失败。
//
// Python: diff_roster (messages.py L164-189)
func DiffRoster(old, new []RosterMember) RosterDelta {
	oldByName := orderedmap.New[string, RosterMember]()
	for _, m := range old {
		oldByName.Set(m.MemberName, m)
	}
	newByName := orderedmap.New[string, RosterMember]()
	for _, m := range new {
		newByName.Set(m.MemberName, m)
	}

	var joined, left, changed []RosterMember
	for pair := newByName.Oldest(); pair != nil; pair = pair.Next() {
		if _, ok := oldByName.Get(pair.Key); !ok {
			joined = append(joined, pair.Value)
		}
	}
	for pair := oldByName.Oldest(); pair != nil; pair = pair.Next() {
		if _, ok := newByName.Get(pair.Key); !ok {
			left = append(left, pair.Value)
		}
	}
	for pair := newByName.Oldest(); pair != nil; pair = pair.Next() {
		prev, ok := oldByName.Get(pair.Key)
		if !ok {
			continue
		}
		if memberFieldChanged(prev, pair.Value) {
			changed = append(changed, pair.Value)
		}
	}
	return RosterDelta{Joined: joined, Left: left, Changed: changed}
}

// IsEmpty 无任何变化、无可公告内容时返回 true。
//
// Python: RosterDelta.is_empty (messages.py L151-153)
func (d RosterDelta) IsEmpty() bool {
	return len(d.Joined) == 0 && len(d.Left) == 0 && len(d.Changed) == 0
}

// FormatMemberLine 渲染一行名册。
//
// markHumans 为真时给人类成员（human_agent 或 passive_human）追加 [human]。
// prefix 是放在字段前的方括号标记，名册增量正文用它说明该成员是加入/退出/更新。
//
// Python: format_member_line (messages.py L197-237)
func FormatMemberLine(member RosterMember, markHumans bool, prefix string) string {
	displayName := member.DisplayName
	if displayName == "" {
		displayName = "unknown"
	}
	head := "- "
	if prefix != "" {
		head = "- [" + prefix + "] "
	}
	line := head + "member_name=" + member.MemberName + " display_name=" + displayName
	if member.ModelGroup != "" {
		line += " model_group=" + member.ModelGroup
	}
	if member.ModelsUsed != "" {
		line += " models_used=" + member.ModelsUsed
	}
	// 两种人类形态都是真人——同伴必须知道要相应放慢沟通节奏
	// （且工具可能代表他们行动）。
	if markHumans &&
		(member.Role == atschema.TeamRoleHumanAgent || member.Role == atschema.TeamRolePassiveHuman) {
		line += " [human]"
	}
	if member.Desc != "" {
		line += " :: " + member.Desc
	}
	return line
}

// BuildIdentityText 渲染成员自己的身份正文，无可用字段时返回 nil。
//
// 携带这个成员特有的一切：它被两个名字知晓，以及它的私有工作约定（公开 desc
// 的成员私有对应物，从不进入任何同伴的名册）。这些都在 spawn 时固定、之后不变，
// 所以本正文只投递一次。
//
// displayName 在这里，是因为同伴的名册用**两个**名字列出成员：没有自己的标签，
// 成员就分不清名册里哪一行是自己，也无法像团队其他人那样称呼自己。
//
// memberWorkspacePath 是成员自己的产物目录，逐成员且恒定，因此与名字同处一个正文。
//
// fork 处理（仅 enable_fork=True 的团队）：forkCapable 为真时前置一段恒定的能力
// 声明，让成员知道自己的身份可能被 fork 转换通知重新指派。转换通知本身由
// BuildIdentityConversion 渲染，作为嵌套 <identity-conversion> 元素投递。
//
// Python: build_identity_text (messages.py L247-315)
func BuildIdentityText(memberName, displayName, memberWorkspacePath, memberPrompt, language string, forkCapable bool) *string {
	privatePrompt := strings.TrimSpace(memberPrompt)
	label := strings.TrimSpace(displayName)
	workspace := strings.TrimSpace(memberWorkspacePath)
	if memberName == "" && label == "" && workspace == "" && privatePrompt == "" {
		return nil
	}
	lbl := messageLabelsFor(language)
	var lines []string
	if forkCapable {
		lines = append(lines, lbl["identity_capability"], "")
	}
	lines = append(lines, lbl["identity_heading"], "")
	if memberName != "" {
		lines = append(lines, lbl["member_name_line"]+": "+memberName)
	}
	if label != "" {
		lines = append(lines, lbl["display_name_line"]+": "+label)
	}
	if workspace != "" {
		purpose := parenthesized(lbl["member_workspace_purpose"], language)
		lines = append(lines, lbl["member_workspace_line"]+": `"+workspace+"`"+purpose)
	}
	if privatePrompt != "" {
		lines = append(lines, "", lbl["private_prompt_heading"], "", privatePrompt)
	}
	out := strings.Join(lines, "\n") + "\n"
	return &out
}

// BuildIdentityPromptDelta 只渲染身份正文里的私有工作约定小节。
//
// 恒定的身份字段由 BuildIdentityText 一次性投递、从不变化，所以每次编辑后
// 重新公告它们是噪音。但私有工作约定（member_prompt.md）**可以**在会话中途
// 手工演进，而它在模型侧的唯一通道就是身份正文——那是一次性的。
// 本 delta 只渲染该小节，让身份正文能在不重述恒定字段的前提下重新公告演进后的约定。
//
// 空白 memberPrompt 返回 nil（无可重新公告的内容）。
//
// Python: build_identity_prompt_delta (messages.py L318-347)
func BuildIdentityPromptDelta(memberPrompt, language string) *string {
	privatePrompt := strings.TrimSpace(memberPrompt)
	if privatePrompt == "" {
		return nil
	}
	lbl := messageLabelsFor(language)
	out := "\n" + lbl["private_prompt_heading"] + "\n\n" + privatePrompt + "\n"
	return &out
}

// BuildIdentityConversion 渲染 fork 转换通知。
//
// 声明当前身份，并明确指出继承来的、更早的身份块里记录的私有工作约定与
// 私有工作区不再适用。作为成员自己 <team-context> 里嵌套 <identity-conversion>
// 元素的正文投递（见 RenderTeamContextWithIdentity）。
//
// memberName 为空时填 "?"（对齐上游 `member_name or "?"`）。
//
// Python: build_identity_conversion (messages.py L350-376)
func BuildIdentityConversion(source, memberName, language string) string {
	lbl := messageLabelsFor(language)
	name := memberName
	if name == "" {
		name = "?"
	}
	tpl := lbl["identity_conversion"]
	tpl = strings.ReplaceAll(tpl, "{source}", source)
	return strings.ReplaceAll(tpl, "{member_name}", name)
}

// BuildTeamInfoText 渲染团队元数据正文，无可用字段时返回 nil。
//
// teamInfo 取 team_name / display_name / desc 三键（TeamBackend.GetTeamInfo 的形状）。
// teamWorkspacePath 渲染为共享工作空间条目下的绝对路径行，外加一条「这是团队
// 配置与内部数据目录，按绝对路径访问、不要新建 .team 子目录」的说明。
// teamOutputsDir 只对无项目目录的成员渲染为独立条目——绑定了项目的成员把
// 交付物留在项目里，传空串。
//
// Python: build_team_info_text (messages.py L379-428)
func BuildTeamInfoText(teamInfo map[string]string, teamWorkspacePath, teamOutputsDir, language string) *string {
	lbl := messageLabelsFor(language)
	teamName := teamInfo["team_name"]
	displayName := teamInfo["display_name"]
	desc := teamInfo["desc"]
	path := strings.TrimSpace(teamWorkspacePath)
	outputs := strings.TrimSpace(teamOutputsDir)
	if teamName == "" && displayName == "" && desc == "" && path == "" && outputs == "" {
		return nil
	}

	lines := []string{lbl["info_heading"], ""}
	if teamName != "" {
		lines = append(lines, "- "+lbl["team_name_label"]+": "+teamName)
	}
	if displayName != "" {
		lines = append(lines, "- "+lbl["display_name_label"]+": "+displayName)
	}
	if desc != "" {
		lines = append(lines, "- "+lbl["team_desc"]+": "+desc)
	}
	if path != "" {
		lines = append(lines, "- "+lbl["team_workspace"]+": `"+path+"`")
		lines = append(lines, "  - "+lbl["team_workspace_purpose"])
		lines = append(lines, "  - "+lbl["team_workspace_config_note"])
	}
	if outputs != "" {
		lines = append(lines, "- "+lbl["team_outputs_dir"]+": `"+outputs+"`")
		lines = append(lines, "  - "+lbl["team_outputs_dir_purpose"])
	}
	out := strings.Join(lines, "\n") + "\n"
	return &out
}

// BuildRosterSnapshotText 渲染同伴名册全量正文，无同伴时返回 nil。
//
// 在成员第一次有同伴需要被告知时投递一次；此后每次变化都用增量公告
// （见 BuildRosterDeltaText）。
//
// markHumans 为真时给人类成员标 [human]。调用方按「查看者角色 +
// expose_human_agents_to_teammates」决定是否开启，好让同伴的同伴默认保持角色匿名。
//
// Python: build_roster_snapshot_text (messages.py L431-458)
func BuildRosterSnapshotText(members []RosterMember, markHumans bool, language string) *string {
	if len(members) == 0 {
		return nil
	}
	lbl := messageLabelsFor(language)
	rows := make([]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, FormatMemberLine(m, markHumans, ""))
	}
	out := lbl["members_heading"] + "\n\n" + strings.Join(rows, "\n") + "\n"
	return &out
}

// BuildRosterDeltaText 渲染只列出变动项的名册变更正文，无变更时返回 nil。
//
// Python: build_roster_delta_text (messages.py L461-487)
func BuildRosterDeltaText(delta RosterDelta, markHumans bool, language string) *string {
	if delta.IsEmpty() {
		return nil
	}
	lbl := messageLabelsFor(language)
	rows := make([]string, 0, len(delta.Joined)+len(delta.Left)+len(delta.Changed))
	for _, m := range delta.Joined {
		rows = append(rows, FormatMemberLine(m, markHumans, lbl["roster_joined"]))
	}
	for _, m := range delta.Left {
		rows = append(rows, FormatMemberLine(m, markHumans, lbl["roster_left"]))
	}
	for _, m := range delta.Changed {
		rows = append(rows, FormatMemberLine(m, markHumans, lbl["roster_updated"]))
	}
	out := lbl["roster_change_heading"] + "\n\n" + strings.Join(rows, "\n") + "\n"
	return &out
}

// messageLabelsFor 返回某语言的标签表，缺失时回退 cn。
//
// Python: labels_for (messages.py L133-135)
func messageLabelsFor(language string) map[string]string {
	if tbl, ok := messageLabels[language]; ok {
		return tbl
	}
	return messageLabels["cn"]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parenthesized 用该语言实际使用的括号包裹一个尾随说明。
//
// Python: _parenthesized (messages.py L240-244) —— cn 用全角，en 用半角且带前导空格。
func parenthesized(text, language string) string {
	if language == "cn" {
		return "（" + text + "）"
	}
	return " (" + text + ")"
}

// memberFieldChanged 判断两个同名成员在任一追踪字段上是否不同。
//
// Python: any(member.get(f,"") != previous.get(f,"") for f in _TRACKED_MEMBER_FIELDS)
func memberFieldChanged(prev, cur RosterMember) bool {
	if cur.DisplayName != prev.DisplayName {
		return true
	}
	if cur.Desc != prev.Desc {
		return true
	}
	if cur.Role != prev.Role {
		return true
	}
	return cur.ModelsUsed != prev.ModelsUsed
}

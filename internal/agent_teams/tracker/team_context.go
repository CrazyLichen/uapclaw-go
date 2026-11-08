package tracker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamContextTracker 决定「这个成员还有什么团队状态没被告知」，并记住。
//
// 团队状态（自己的身份、团队元数据、同伴名册、协作政策）过去走每轮提示词附件：
// 每次模型调用重新收集、重新渲染、重新追加。附件挂在窗口尾部且不写进历史，
// 因此永远无法从 KV 缓存中命中，却是每一次调用都要重新编码的固定开销。
//
// 本类型驱动替代方案：状态在**出现时**写进对话历史，且只写新增部分。
// 两条结论随之而来：
//
//   - 时机由数据驱动，不由启动驱动。leader 首次模型调用时还没有团队
//     （build_team 尚未执行），因此无内容可播；每个通道在自己的探针首次产出
//     内容的那次调用触发，此后探针推进才再触发。
//   - 基线必须持久化。Rail 每轮重建，内存基线会每轮重播全部状态，
//     故基线落在成员自己的子 AgentSession state（键 team_prompt_context），
//     与对话历史同一个 checkpoint bucket，pre_run 时一起恢复。
//
// Python: TeamContextTracker (team_context.py L129-672)
type TeamContextTracker struct {
	// teamBackend 元数据/名册探针与读取来源；nil 时退化为只有身份通道
	teamBackend *tools.TeamBackend
	// policyText 已渲染的 <team-policy> 块（framework 建队，F_122）；nil 关闭通道
	policyText *string
	// memberName 本成员语义标识
	memberName string
	// role 本成员团队角色，决定名册是否标 [human]
	role atschema.TeamRole
	// displayName 本成员人类可读标签
	displayName string
	// memberWorkspacePath 本成员私有产物目录
	memberWorkspacePath string
	// memberPrompt 本成员私有工作约定
	memberPrompt string
	// teamWorkspacePath 团队共享工作空间绝对路径
	teamWorkspacePath string
	// teamOutputsDir 最终产物目录；有项目目录的成员传空串以抑制该条目
	teamOutputsDir string
	// markHumans 名册是否标 [human]
	markHumans bool
	// language 渲染语言
	language string
	// forkSource 本成员 fork 自哪个成员；空串表示普通 spawn
	forkSource string
	// forkCapable enable_fork 是否为真，决定身份块是否带能力声明并嵌 <identity>
	forkCapable bool
	// uncommitted 最后一次 PendingText 算出的基线，等调用方确认投递
	uncommitted map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// TeamContextStateKey 成员子会话 state 里存投递基线的键。
// Python: TEAM_CONTEXT_STATE_KEY (team_context.py L92)
const TeamContextStateKey = "team_prompt_context"

const (
	// baselineIdentityEmitted 身份正文是否已投递（只投一次）
	// Python: _IDENTITY_EMITTED (L95)
	baselineIdentityEmitted = "identity_emitted"
	// baselineTeamInfoMtime 团队元数据探针的基线值
	// Python: _TEAM_INFO_MTIME (L96)
	baselineTeamInfoMtime = "team_info_mtime"
	// baselineRosterMtime 名册探针的基线值
	// Python: _ROSTER_MTIME (L97)
	baselineRosterMtime = "roster_mtime"
	// baselineRoster 上次宣告的名册快照，用于 diff
	// Python: _ROSTER (L98)
	baselineRoster = "roster"
	// baselineMemberPromptMtime 成员 prompt mtime 基线
	// Python: _MEMBER_PROMPT_MTIME (L99)
	baselineMemberPromptMtime = "member_prompt_mtime"
	// baselinePolicyHash 上次投递的政策文本 sha256
	// Python: _POLICY_HASH (L103)
	baselinePolicyHash = "policy_hash"
	// baselineLastDeliveredText 上次注册到「当前状态」通道的内容
	// Python: _LAST_DELIVERED_TEXT (L105)
	baselineLastDeliveredText = "last_delivered_text"
)

// RosterEventKind <team-event> 的 kind 契约 token（首次全量名册）。
// Python: ROSTER_EVENT_KIND (team_context.py L108)
const RosterEventKind = "roster"

// RosterChangeEventKind <team-event> 的 kind 契约 token（名册增量）。
// Python: ROSTER_CHANGE_EVENT_KIND (team_context.py L109)
const RosterChangeEventKind = "roster-change"

// RosterNoteKind 每条名册消息携带的 <team-note> kind。
// Python: ROSTER_NOTE_KIND (team_context.py L112)
const RosterNoteKind = "announcement-only"

// logComponent 本包日志组件（团队状态属于 agentcore 团队域）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamContextTracker 构造 tracker。
//
// Python: TeamContextTracker.__init__ (team_context.py L163-203)
func NewTeamContextTracker(
	teamBackend *tools.TeamBackend,
	memberName string,
	role atschema.TeamRole,
	displayName, memberWorkspacePath, memberPrompt string,
	teamWorkspacePath, teamOutputsDir string,
	exposeHumanAgentsToTeammates bool,
	language, forkSource string,
	policyText *string,
) *TeamContextTracker {
	// Python: role in (LEADER, HUMAN_AGENT) or expose_human_agents_to_teammates
	markHumans := role == atschema.TeamRoleLeader ||
		role == atschema.TeamRoleHumanAgent ||
		exposeHumanAgentsToTeammates
	// Python: bool(team_backend and getattr(team_backend, "fork_enabled", lambda: False)())
	// 偏差 D-8：Go 的 TeamAgentSpec 无 enable_fork 字段，ForkEnabled() 恒 false
	forkCapable := false
	if teamBackend != nil {
		forkCapable = teamBackend.ForkEnabled()
	}
	return &TeamContextTracker{
		teamBackend:         teamBackend,
		policyText:          policyText,
		memberName:          memberName,
		role:                role,
		displayName:         displayName,
		memberWorkspacePath: memberWorkspacePath,
		memberPrompt:        memberPrompt,
		teamWorkspacePath:   teamWorkspacePath,
		teamOutputsDir:      teamOutputsDir,
		markHumans:          markHumans,
		language:            language,
		forkSource:          forkSource,
		forkCapable:         forkCapable,
	}
}

// SnapshotText 渲染本成员当前完整团队状态，忽略任何基线。
//
// PendingText 只给「相对基线新增」的部分；当历史里已不再持有此前说过的内容
// （首投，或 compaction 丢掉了早先的块），当前状态通道需要完整状态。
// 本方法不读也不写任何持久化数据，第二个返回值表示是否有内容。
//
// Python: snapshot_text (team_context.py L205-225)
func (t *TeamContextTracker) SnapshotText() (string, bool) {
	identityBody, _ := t.fullIdentityBody()
	infoBody, _ := t.fullTeamInfoBody()
	var rosterBlock *string
	if t.teamBackend != nil {
		roster, _ := t.listRoster()
		rosterBlock = t.renderRosterSnapshot(roster)
	}
	blocks := t.composeBlocks(identityBody, infoBody, rosterBlock, t.policyText)
	if len(blocks) == 0 {
		return "", false
	}
	return strings.Join(blocks, "\n\n"), true
}

// PendingText 渲染本成员尚未被告知的一切，不推进任何基线。
//
// 调用方必须在文本**真正投递之后**调用 Commit。当探针推进但没有可说的文本
// （空名册、团队尚不存在）时，基线就地推进——那些内容不可能投递失败，
// 不推进会导致每次调用都重新查 DB。第二个返回值表示是否有内容。
//
// Python: pending_text (team_context.py L227-259)
func (t *TeamContextTracker) PendingText(sess sessioninterfaces.SessionFacade) (string, bool) {
	t.uncommitted = nil
	if sess == nil {
		return "", false
	}
	baseline := t.readBaseline(sess)
	updated := copyBaseline(baseline)
	identityBody := t.identityBody(baseline, updated)
	infoBody := t.teamInfoBody(baseline, updated)
	policyBlock := t.policyBlock(baseline, updated)
	rosterBlock := t.rosterBlock(baseline, updated)
	blocks := t.composeBlocks(identityBody, infoBody, rosterBlock, policyBlock)
	if len(blocks) > 0 {
		t.uncommitted = updated
		return strings.Join(blocks, "\n\n"), true
	}
	// 探针推进但无文本可说：就地推进基线，避免每次调用重查 DB
	if !baselineEqual(updated, baseline) {
		t.persist(sess, updated)
	}
	return "", false
}

// Commit 为刚投递的文本持久化基线。
//
// 最后一次 PendingText 无输出且未给 deliveredText 时是 no-op，调用方无需判空。
// deliveredText 非 nil 时记入基线，供重建后的成员注册同一份内容
// （内容 hash 不变 → 不产生附件更新）。
//
// Python: commit (team_context.py L261-283)
func (t *TeamContextTracker) Commit(sess sessioninterfaces.SessionFacade, deliveredText *string) {
	pending := t.uncommitted
	t.uncommitted = nil
	if sess == nil {
		return
	}
	if deliveredText != nil {
		if pending == nil {
			pending = copyBaseline(t.readBaseline(sess))
		}
		pending[baselineLastDeliveredText] = *deliveredText
	}
	if pending == nil {
		return
	}
	t.persist(sess, pending)
}

// LastDeliveredText 返回上次经 deliveredText 提交的内容。
//
// 「当前状态」通道一旦某 section 未重新注册就视为撤回，而 PendingText 在成员
// 已最新时无话可说。原样重注册这份内容能让 section 保持活跃且内容 hash 不变，
// 从而不产生更新宣告。第二个返回值表示是否记录过。
//
// Python: last_delivered_text (team_context.py L285-302)
func (t *TeamContextTracker) LastDeliveredText(sess sessioninterfaces.SessionFacade) (string, bool) {
	if sess == nil {
		return "", false
	}
	v, ok := t.readBaseline(sess)[baselineLastDeliveredText].(string)
	return v, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// composeBlocks 按顺序拼装各通道正文：<team-context> → <team-policy> → 名册。
//
// 顺序有语义：模型先读到「我是谁、这是什么团队」（身份与团队元数据同属团队
// 既成事实，合并进**一个** <team-context>，而不是两个相邻的同类块），
// 再读到「这个团队怎么运转」，最后读到「还有谁可以派活」。
//
// fork 分支要求 identityBody 本身存在：仅团队元数据更新（身份已投递过）时
// 不能渲染一个空 <identity>，必须落到 flat 路径，保持与 pre-fork 版字节一致。
//
// Python: _compose_blocks (team_context.py L304-357)
func (t *TeamContextTracker) composeBlocks(
	identityBody, infoBody, rosterBlock, policyBlock *string,
) []string {
	blocks := make([]string, 0, 3)
	hasIdentity := identityBody != nil && *identityBody != ""
	hasInfo := infoBody != nil && *infoBody != ""
	if hasIdentity || hasInfo {
		if t.forkCapable && hasIdentity {
			var conversion *string
			if t.forkSource != "" {
				c := prompts.BuildIdentityConversion(t.forkSource, t.memberName, t.language)
				conversion = &c
			}
			blocks = append(blocks, inboundtags.RenderTeamContextWithIdentity(*identityBody, conversion, infoBody))
		} else {
			standing := make([]string, 0, 2)
			if hasIdentity {
				standing = append(standing, *identityBody)
			}
			if hasInfo {
				standing = append(standing, *infoBody)
			}
			blocks = append(blocks, inboundtags.RenderTeamContext(strings.Join(standing, "\n")))
		}
	}
	if policyBlock != nil && *policyBlock != "" {
		blocks = append(blocks, *policyBlock)
	}
	if rosterBlock != nil && *rosterBlock != "" {
		blocks = append(blocks, *rosterBlock)
	}
	return blocks
}

// policyBlock 政策文本未投递过时返回该块（基线记 sha256）。
//
// 用哈希而非布尔标志：演化后的政策模板重渲染出不同文本，那是重新投递的理由。
//
// Python: _policy_block (team_context.py L359-373)
func (t *TeamContextTracker) policyBlock(baseline, updated map[string]any) *string {
	if t.policyText == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(*t.policyText))
	digest := hex.EncodeToString(sum[:])
	if prev, ok := baseline[baselinePolicyHash].(string); ok && prev == digest {
		return nil
	}
	updated[baselinePolicyHash] = digest
	return t.policyText
}

// readBaseline 读本成员已持久化的基线，空或类型不符返回空 map。
// Python: _read_baseline (team_context.py L648-654)
func (t *TeamContextTracker) readBaseline(sess sessioninterfaces.SessionFacade) map[string]any {
	if sess == nil {
		return map[string]any{}
	}
	v, err := sess.GetState(sessionstate.StringKey(TeamContextStateKey))
	if err != nil || v == nil {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// persist 把基线写进成员 checkpoint 并 flush。
//
// commit 是让「中途崩溃 / 暂停」落在一致状态的那一步：成员的对话与本基线
// 共享同一个 agent-session state bucket，由同一次 AgentStorage.save 写入。
// 写失败只告警——绝不因为一次 checkpoint 写失败而中断模型调用。
//
// Python: _persist (team_context.py L656-671)
func (t *TeamContextTracker) persist(sess sessioninterfaces.SessionFacade, st map[string]any) {
	if sess == nil {
		return
	}
	sess.UpdateState(map[string]any{TeamContextStateKey: st})
	committer, ok := sess.(interface {
		Commit(ctx context.Context) error
	})
	if !ok {
		return
	}
	if err := committer.Commit(context.Background()); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "team_context_baseline_persist_failed").
			Str("member_name", t.memberName).
			Err(err).
			Msg("failed to persist team context baseline")
	}
}

// copyBaseline 浅拷贝基线。
//
// 值多为标量或 RosterMember 值类型切片，浅拷贝足够；每轮 updated 都是新 map，
// 因此不会跨通道改写已持久化的基线。
func copyBaseline(src map[string]any) map[string]any {
	out := make(map[string]any, len(src)+1)
	for k, v := range src {
		out[k] = v
	}
	return out
}

// baselineEqual 比较两份基线是否等价。
func baselineEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		other, ok := b[k]
		if !ok || !reflect.DeepEqual(v, other) {
			return false
		}
	}
	return true
}

// sortRoster 按 member_name 排序名册，保证跨调用的比较与渲染顺序稳定。
//
// Python: list_members 的返回顺序由 DB 决定（按 member_name 主键）；Go 的
// in-memory 实现遍历 map，顺序随机，故此处显式排序以对齐可比较、可对拍的行为。
func sortRoster(roster []prompts.RosterMember) {
	sort.SliceStable(roster, func(i, j int) bool {
		return roster[i].MemberName < roster[j].MemberName
	})
}

// ──────────────────────────── 通道：identity ────────────────────────────

// identityBody 渲染身份正文；已投递过则只在 prompt 变化时重播该小节。
//
// 恒定字段（member_name / display_name / member_workspace_path）在 spawn 时
// 固定、永不变化，因此由 BuildIdentityText 一次性投递，identity_emitted 门控。
// 私有工作约定（member_prompt）是唯一可能中途被手工演进的字段，且它在模型侧
// 的唯一通道就是身份正文——故一旦投递过，正文改为探测 member_prompt 的
// updated_at，变了**只**重播该小节（BuildIdentityPromptDelta），恒定字段从不
// 重述。无单成员探针的后端（演化关闭 / 旧 fake）保持一次性行为。
//
// **要等成员自己的 DB 行**，因为 display_name 必须来自那一行：构造参数只是
// spec 时的默认值，leader 由 build_team 按调用方**当时**给的名字注册，告诉它
// spec 默认值等于告诉它一个全队不用的名字。teammate 在 spawn 时行已存在，
// 这个门不花代价；leader 在建队后那次调用同时收到「我是谁」与团队元数据。
//
// Python: _identity_body (team_context.py L379-455)
func (t *TeamContextTracker) identityBody(baseline, updated map[string]any) *string {
	if emitted, _ := baseline[baselineIdentityEmitted].(bool); emitted {
		// 已投递：只在 prompt 被演进时重播该小节，恒定字段从不重述
		if t.teamBackend == nil || t.memberName == "" {
			return nil
		}
		mtime, _ := t.teamBackend.GetMemberUpdatedAtState(context.Background(), t.memberName, "prompt")
		// 偏差 D-1：present 恒 true，故只保留「已戳过时间戳 → 比 mtime」分支；
		// 上游 present=false 的「强制重投一次 + 回戳」需要 workspace_cache。
		if prev, ok := baseline[baselineMemberPromptMtime].(int64); ok && prev == mtime {
			return nil
		}
		member, err := t.teamBackend.GetMember(context.Background(), t.memberName)
		if err != nil || member == nil {
			return nil
		}
		delta := prompts.BuildIdentityPromptDelta(member.Prompt, t.language)
		if delta == nil {
			return nil
		}
		updated[baselineMemberPromptMtime] = mtime
		return delta
	}
	body, _ := t.fullIdentityBody()
	if body == nil || *body == "" {
		return nil
	}
	updated[baselineIdentityEmitted] = true
	// 记下 prompt mtime，使后续手工演进能让上面的 delta 分支重播该小节
	if t.teamBackend != nil && t.memberName != "" {
		mtime, _ := t.teamBackend.GetMemberUpdatedAtState(context.Background(), t.memberName, "prompt")
		updated[baselineMemberPromptMtime] = mtime
	}
	return body
}

// fullIdentityBody 从成员当前 DB 行渲染完整身份正文。
//
// 构造快照是 spec 时的 DB 基线（演化前）；GetMember 返回叠加后的行，其 prompt
// 可能携带演进值，优先用它，否则演化后的约定会在最后一步被丢弃（F_84 gap #1）。
// 成员自身行不存在时返回 (nil,false)——无后端时用构造参数原样渲染。
//
// Python: _full_identity_body (team_context.py L457-485)
func (t *TeamContextTracker) fullIdentityBody() (*string, bool) {
	displayName := t.displayName
	memberPrompt := t.memberPrompt
	if t.teamBackend != nil && t.memberName != "" {
		member, err := t.teamBackend.GetMember(context.Background(), t.memberName)
		if err != nil || member == nil {
			return nil, false
		}
		displayName = member.DisplayName
		if member.Prompt != "" {
			memberPrompt = member.Prompt
		}
	}
	return prompts.BuildIdentityText(
		t.memberName, displayName, t.memberWorkspacePath, memberPrompt, t.language, t.forkCapable,
	), true
}

// ──────────────────────────── 通道：team_info ────────────────────────────

// teamInfoBody 在团队元数据探针推进时渲染团队信息正文。
//
// 变化时重播而非替换：先前的块留在历史里，作为当时的事实。
//
// **团队行不存在时什么都不宣告。** leader 在 build_team 之前没有团队，而工作
// 空间路径是构造参数、总是可用的；不加这道门就会先渲染一个「空团队信息」，
// 真正的信息随即到达，成员被同一件事告知两次且第一次是错的。团队行缺失时
// 探针读 0，行一建出来探针自然推进。
//
// Python: _team_info_body (team_context.py L487-538)
func (t *TeamContextTracker) teamInfoBody(baseline, updated map[string]any) *string {
	if t.teamBackend == nil {
		return nil
	}
	mtime, _ := t.teamBackend.GetTeamUpdatedAtState(context.Background())
	// 偏差 D-1：present 恒 true，只保留 mtime 比较分支
	if prev, ok := baseline[baselineTeamInfoMtime].(int64); ok && prev == mtime {
		return nil
	}
	updated[baselineTeamInfoMtime] = mtime
	body, _ := t.fullTeamInfoBody()
	return body
}

// fullTeamInfoBody 渲染团队元数据正文，团队行缺失时返回 (nil,false)。
// Python: _full_team_info_body (team_context.py L540-556)
func (t *TeamContextTracker) fullTeamInfoBody() (*string, bool) {
	if t.teamBackend == nil {
		return nil, false
	}
	info, err := t.teamBackend.GetTeamInfo(context.Background())
	if err != nil || info == nil {
		return nil, false
	}
	teamInfo := map[string]string{
		"team_name":    info.TeamName,
		"display_name": info.DisplayName,
		"desc":         info.Desc,
	}
	return prompts.BuildTeamInfoText(teamInfo, t.teamWorkspacePath, t.teamOutputsDir, t.language), true
}

// ──────────────────────────── 通道：roster ────────────────────────────

// rosterBlock 首次渲染名册快照，之后渲染增量。
//
// ListMembers 已排除成员自己，故存下的名册就是「同伴列表」，增量即相对它计算。
// 基线里没有 roster 键表示从未宣告过——**空的名册**是另一回事，仍走增量路径。
//
// Python: _roster_block (team_context.py L558-594)
func (t *TeamContextTracker) rosterBlock(baseline, updated map[string]any) *string {
	if t.teamBackend == nil {
		return nil
	}
	mtime := t.teamBackend.GetMembersMaxUpdatedAt(context.Background())
	if prev, ok := baseline[baselineRosterMtime].(int64); ok && prev == mtime {
		return nil
	}
	roster, _ := t.listRoster()
	previous, hasPrevious := baseline[baselineRoster].([]prompts.RosterMember)
	updated[baselineRosterMtime] = mtime
	if !hasPrevious {
		block := t.renderRosterSnapshot(roster)
		// 没有值得宣告的内容（还没有同伴）。只在真的投递了快照之后才记
		// roster，这样第一个出现的同伴会作为**全量名册**而非增量被宣告。
		if block != nil {
			updated[baselineRoster] = roster
		}
		return block
	}
	body := prompts.BuildRosterDeltaText(prompts.DiffRoster(previous, roster), t.markHumans, t.language)
	// 探针在成员看不到的字段上推进时，也要把名册写回基线，
	// 让下一次增量相对最新状态做 diff。
	updated[baselineRoster] = roster
	if body == nil {
		return nil
	}
	return t.renderRosterEvent(RosterChangeEventKind, *body)
}

// listRoster 读同伴名册（backend 已排除本成员）。
//
// 模型池场景下 leader 的名册还要列每个成员被路由到的模型与接管时间
// （members.py 的 model_group_label / get_member_served_models）——
// 偏差 D-2：Go 模型池章节未落，ModelGroup / ModelsUsed 恒空，随 25.x 回填。
//
// Python: _list_roster (team_context.py L596-621)
func (t *TeamContextTracker) listRoster() ([]prompts.RosterMember, bool) {
	if t.teamBackend == nil {
		return nil, false
	}
	members, err := t.teamBackend.ListMembers(context.Background())
	if err != nil {
		return nil, false
	}
	roster := make([]prompts.RosterMember, 0, len(members))
	for _, m := range members {
		roster = append(roster, prompts.RosterMember{
			MemberName:  m.MemberName,
			DisplayName: m.DisplayName,
			Desc:        m.Desc,
			Role:        atschema.TeamRole(m.Role),
			// ModelGroup / ModelsUsed 留空（偏差 D-2）
		})
	}
	// Go 的 in-memory 库遍历 map，顺序随机；显式排序保证 delta 与渲染稳定
	sortRoster(roster)
	return roster, true
}

// renderRosterSnapshot 渲染全量名册事件，无同伴时返回 nil。
// Python: _render_roster_snapshot (team_context.py L623-632)
func (t *TeamContextTracker) renderRosterSnapshot(roster []prompts.RosterMember) *string {
	body := prompts.BuildRosterSnapshotText(roster, t.markHumans, t.language)
	if body == nil || *body == "" {
		return nil
	}
	return t.renderRosterEvent(RosterEventKind, *body)
}

// renderRosterEvent 把名册正文包进 <team-event> 并附「仅公告」note。
// Python: _render_roster_event (team_context.py L634-642)
func (t *TeamContextTracker) renderRosterEvent(kind, body string) *string {
	noteText, err := atschema.T("team_context.roster_announcement_note")
	if err != nil {
		noteText = ""
	}
	out := inboundtags.RenderEvent(kind, body, inboundtags.EventOptions{
		NoteKind: RosterNoteKind,
		NoteText: noteText,
	})
	return &out
}

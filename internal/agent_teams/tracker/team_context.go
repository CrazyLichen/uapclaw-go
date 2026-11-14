package tracker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
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

// PolicyTextForTest 返回已渲染的 <team-policy> 块与是否开启。
//
// 仅供其它包的测试断言 policyText 通道是否开启（framework 建队时非 nil）。
// 生产代码不应读它——政策块的投递一律走 PendingText。
func (t *TeamContextTracker) PolicyTextForTest() (string, bool) {
	if t == nil || t.policyText == nil {
		return "", false
	}
	return *t.policyText, true
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
// **不做回滚**：上游 _persist 同样只告警不回滚。回滚看似更安全，实则有害
// ——Commit 失败的原因可能与会话环境有关（例如会话尚未注册 agent store），
// 此时内存态的 UpdateState 其实已经生效，回滚会让基线永远推不动，
// 每轮都重投同一段状态。真正该保证的是「投递失败时调用方不调 Commit」，
// 那条由 PendingText/Commit 的两段式契约承担，不在本函数。
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
		// 偏差 D-1：present 恒 true，故只保留「已戳过时间戳 → 比 mtime」分支。
		//
		// ⤴️ 上游 present=false 分支被整段删掉，但它承载一个必须保留的不变量：
		//
		//	present=false 表示「md 的 frontmatter 里 updated_at 字段为空」——
		//	演化方改了正文却没戳时间戳，这是显式的「必须更新一次」信号。
		//	上游的处理是：不看基线、直接重投，随后把同一个时间戳 T 回戳进文件
		//	（仅 meta，正文保留）并记入基线。因为 T 同时是「文件里的值」与
		//	「基线里的值」，下次探测得到 present=true, mtime=T，比较相等，
		//	不再触发——空字段因此**恰好强制一次重投，永不形成循环**。
		//
		//	Go 侧没有 md mtime 覆盖层（workspace_cache），这个状态不可能出现，
		//	所以分支不可达。但它是「不回戳就会每轮重投」这一防循环设计的唯一
		//	记录：将来补上 workspace_cache、让 present 能返回 false 时，必须
		//	照着上面把「重投 + 回戳 + 记基线」三步一起补回来，只补前两步会死循环。
		//
		// 回收条件：25.x 落地 workspace_cache 时按 Python L421-438 完整恢复。
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
// 模型组池下 leader 的名册还要列组名与每个成员被路由到的模型
// （members.py 的 model_group_label / get_member_served_models）；
// label 为空时两字段都不写（Python None → 键不存在，而非写空串）。
//
// Python: _list_roster (team_context.py L596-621)
func (t *TeamContextTracker) listRoster() ([]prompts.RosterMember, bool) {
	if t.teamBackend == nil {
		return nil, false
	}
	// Python: py:608——仅 leader 携带 label
	label := ""
	if t.role == atschema.TeamRoleLeader {
		label = t.teamBackend.ModelGroupLabel()
	}
	members, err := t.teamBackend.ListMembers(context.Background())
	if err != nil {
		return nil, false
	}
	roster := make([]prompts.RosterMember, 0, len(members))
	for _, m := range members {
		rm := prompts.RosterMember{
			MemberName:  m.MemberName,
			DisplayName: m.DisplayName,
			Desc:        m.Desc,
			Role:        atschema.TeamRole(m.Role),
		}
		if label != "" {
			rm.ModelGroup = label
			rm.ModelsUsed = renderServedModels(loadMemberServedModels(m.Options))
		}
		roster = append(roster, rm)
	}
	// 不在此处排序：上游 _list_roster 也按后端返回顺序原样组装，渲染层
	// （BuildRosterSnapshotText / DiffRoster）保持「按输入顺序」不改写。
	// 顺序确定性由 DAO 层保证（GetTeamMembers 带 ORDER BY member_name）。
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

// ──────────────────────────── 任务通知三兄弟 ────────────────────────────

// TaskOutcomeStateKey 已告知该成员「通过验收」的任务 id 基线键。
// Python: TASK_OUTCOME_STATE_KEY (team_context.py L675)
const TaskOutcomeStateKey = "task_outcome_announced"

// TaskVerifiedEventKind <team-event> 的 kind 契约 token（评审通过）。
// Python: TASK_VERIFIED_EVENT_KIND (team_context.py L678)
const TaskVerifiedEventKind = "task-verified"

// TaskRevisionStateKey 编辑通知的基线键。
// Python: TASK_REVISION_STATE_KEY (team_context.py L680)
const TaskRevisionStateKey = "task_revision_announced"

// TaskRevisedEventKind 自主成员编辑通知的 kind token（F_129）。
// Python: TASK_REVISED_EVENT_KIND (team_context.py L681)
const TaskRevisedEventKind = "task-revised"

// ClaimedNotice 已认领待投递的一条任务通知。
// Python: ClaimedNotice (team_context.py L684-689)
type ClaimedNotice struct {
	// Text 要投递的 <team-event> 块
	Text string
	// Entries 本次覆盖的基线条目
	Entries []string
}

// TaskEventTracker 任务通知器的最小对外面（rail 注入用）。
//
// Python 侧 rail 直接持有 tracker 对象并调 claim/persist；Go 用一个窄接口
// 让 rail 与具体子类解耦，测试可注入替身。
type TaskEventTracker interface {
	// Claim 认领未宣告条目并渲染；无新内容返回 nil
	Claim(sess sessioninterfaces.SessionFacade) *ClaimedNotice
	// Persist 把基线写进会话状态
	Persist(sess sessioninterfaces.SessionFacade)
	// Announce 把待发通知作为独立输入投递：Claim → send → persist。
	// send 失败时归还认领（允许下次重发）。Wakes 为假时直接返回 false。
	Announce(sess sessioninterfaces.SessionFacade, send func(text string) error) (bool, error)
	// Wakes 该通知能否作为独立输入唤醒空闲成员（评审通过不得唤醒作者）
	Wakes() bool
	// Release 归还已认领条目（投递失败后调用）
	Release(notice *ClaimedNotice)
}

// UnimplementedTaskEventTracker 空实现基类（测试替身嵌入用，对齐规则 10 的同款做法）。
//
// 生产实现不得用它。测试替身只需嵌入本类型，再覆写关心的那一个方法。
type UnimplementedTaskEventTracker struct{}

// Claim 空实现。
func (UnimplementedTaskEventTracker) Claim(sessioninterfaces.SessionFacade) *ClaimedNotice {
	return nil
}

// Persist 空实现。
func (UnimplementedTaskEventTracker) Persist(sessioninterfaces.SessionFacade) {}

// Announce 空实现。
func (UnimplementedTaskEventTracker) Announce(
	sessioninterfaces.SessionFacade, func(string) error,
) (bool, error) {
	return false, nil
}

// Wakes 空实现。
func (UnimplementedTaskEventTracker) Wakes() bool { return false }

// Release 空实现。
func (UnimplementedTaskEventTracker) Release(*ClaimedNotice) {}

// taskNoticeTracker 下一次输入的任务通知共享基线（F_119 / F_129）。
//
// 每成员一个实例，由它的 TeamAgent 持有、被所有能「告诉成员某事」的路径借用：
// 策略 rail 在每次受理输入时、成员运行时在任务事件要求它立刻说话时、外部
// 运行时在每次 send 时。哪条路径先 Claim 到条目就由它投递，其余路径看到空。
//
// 基线是「已宣告条目」的有序集合，绑定到**一个成员会话**：每次调用都指名它为
// 哪个会话服务，换成别的会话就重置集合并从那个会话的状态重新加载。每个运行
// 周期、恢复进另一个团队会话、重启、checkpoint 恢复都会递一个新的子会话，
// 因此基线总是与该会话的历史配套——一个会话的宣告不会压制另一个的，回滚后的
// 状态也不会被回滚之后的条目覆盖。已 Claim 的内容在投递后立即持久化；持久化
// 失败的条目会被带到同一 session id 的下一个会话（下一个运行周期）再写，
// 不同 session 则丢弃。
//
// 偏差 D-7：Python 靠单事件循环保证 claim 与 mark 之间无 await（不重复）；
// Go 多协程，故用 sync.Mutex 把「绑定 + 检查 + 标记」包成一步。
//
// Python: _TaskNoticeTracker (team_context.py L692-868)
type taskNoticeTracker struct {
	// mu 保护下列全部字段
	mu sync.Mutex
	// teamBackend 后端；nil 时 tracker 关闭
	teamBackend *tools.TeamBackend
	// memberName 本成员语义标识
	memberName string
	// announced 已宣告条目（保序）
	announced []string
	// announcedSet announced 的快速查表
	announcedSet map[string]struct{}
	// sessionID 当前绑定的会话 id
	sessionID string
	// sessionBound 是否已绑定过会话
	sessionBound bool
	// persisted 上次写入会话状态的条目快照
	persisted []string
	// pending 已 Claim 但尚未写入任何会话状态的条目
	pending []string
	// stateKey 本 tracker 的状态键
	stateKey string
	// wakes 该通知是否可作为独立输入发送（唤醒空闲成员）
	wakes bool
	// hookCandidates 候选行查询钩子（子类在构造时注入）
	//
	// **必须用函数指针而非方法重写**：Go 的 struct embedding 没有虚方法派发，
	// 基类方法体里写 t.candidates(...) 会静态绑定到基类的空实现，子类覆盖
	// 永远不生效。构造期把子类实现装进来是最直接的等价手段。
	hookCandidates func(tm *tools.TeamTaskManager) []*database.TeamTaskBase
	// hookEntry 基线条目生成钩子
	hookEntry func(task *database.TeamTaskBase) string
	// hookRender 渲染钩子
	hookRender func(tasks []*database.TeamTaskBase) string
	// hookSuperseded 条目淘汰钩子（nil 表示不淘汰）
	hookSuperseded func(entries []string) []string
}

// claim 把本成员尚未宣告的条目标记为已宣告并渲染。
//
// sess 为 nil 时不做任何 Claim：没有基线就可能重复通知。
// 返回 nil 表示没有新内容（或 tracker 未绑定 / 关闭）。
//
// Python: claim (team_context.py L766-793)
func (t *taskNoticeTracker) claim(sess sessioninterfaces.SessionFacade) *ClaimedNotice {
	if sess == nil || t.teamBackend == nil || t.memberName == "" {
		return nil
	}
	taskManager := t.teamBackend.TaskManager()
	if taskManager == nil {
		return nil
	}
	// 候选查询在锁外（要查 DB）；之后绑定 + 检查 + 标记在锁内一步完成
	rows := t.callCandidates(taskManager)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bindLocked(sess)
	fresh := make([]*database.TeamTaskBase, 0, len(rows))
	entries := make([]string, 0, len(rows))
	for _, row := range rows {
		e := t.callEntry(row)
		if _, seen := t.announcedSet[e]; seen {
			continue
		}
		fresh = append(fresh, row)
		entries = append(entries, e)
	}
	if len(fresh) == 0 {
		return nil
	}
	for _, e := range entries {
		t.announcedSet[e] = struct{}{}
		t.announced = append(t.announced, e)
		t.pending = append(t.pending, e)
	}
	t.compactLocked()
	return &ClaimedNotice{Text: t.callRender(fresh), Entries: entries}
}

// release 投递失败时把条目还回去。
// Python: release (team_context.py L795-799)
func (t *taskNoticeTracker) release(notice *ClaimedNotice) {
	if notice == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range notice.Entries {
		delete(t.announcedSet, e)
		t.announced = removeString(t.announced, e)
		t.pending = removeString(t.pending, e)
	}
}

// persist 基线自上次写入以来有变化时写进会话状态。
// Python: persist (team_context.py L801-821)
func (t *taskNoticeTracker) persist(sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bindLocked(sess)
	current := append([]string(nil), t.announced...)
	if stringSliceEqual(current, t.persisted) {
		return
	}
	sess.UpdateState(map[string]any{t.stateKey: current})
	committer, ok := sess.(interface {
		Commit(ctx context.Context) error
	})
	if !ok {
		t.persisted = current
		t.pending = nil
		return
	}
	if err := committer.Commit(context.Background()); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "task_notice_persist_failed").
			Str("state_key", t.stateKey).
			Str("member_name", t.memberName).
			Err(err).
			Msg("failed to persist task notice baseline")
		return
	}
	t.persisted = current
	t.pending = nil
}

// announce 把待发通知作为独立输入投递：claim → send → persist。
//
// wakes 为假的通知只 Claim 不发（通过的评审不该唤醒作者），由下一次输入携带。
// 返回是否真的发出。send 抛错时归还 Claim——取消的投递也归还，因为它可能发出
// 也可能没发出，重复一条比丢一条便宜。
//
// Python: announce (team_context.py L823-846)
func (t *taskNoticeTracker) announce(
	sess sessioninterfaces.SessionFacade,
	send func(text string) error,
) (bool, error) {
	if !t.wakes {
		return false, nil
	}
	notice := t.claim(sess)
	if notice == nil {
		return false, nil
	}
	if err := send(notice.Text); err != nil {
		t.release(notice)
		return false, err
	}
	t.persist(sess)
	return true, nil
}

// bindLocked 让 sess 存的基线成为当前基线。
//
// 同一个 session id 再次进来只补 pending（上一个会话已持久化的部分不重读）；
// 换成不同会话则从它的状态重新加载，待写条目只在本会话 id 相同时继承。
//
// Python: _bind (team_context.py L750-764)
func (t *taskNoticeTracker) bindLocked(sess sessioninterfaces.SessionFacade) {
	sessID := sess.GetSessionID()
	if t.sessionBound && sessID == t.sessionID {
		return
	}
	// 同 session id 换对象（下一个运行周期）时继承待写条目；不同 session 丢弃
	var carried []string
	if t.sessionBound && sessID == t.sessionID {
		carried = append([]string(nil), t.pending...)
	}
	stored := t.readAnnounced(sess)
	t.sessionID = sessID
	t.sessionBound = true
	t.pending = carried
	merged := make([]string, 0, len(stored)+len(carried))
	merged = append(merged, stored...)
	merged = append(merged, carried...)
	t.announced = merged
	t.announcedSet = make(map[string]struct{}, len(merged))
	for _, e := range merged {
		t.announcedSet[e] = struct{}{}
	}
	t.compactLocked()
	t.persisted = append([]string(nil), stored...)
}

// compactLocked 丢掉 superseded 判定为「永不再匹配」的条目。
// Python: _compact (team_context.py L848-854)
func (t *taskNoticeTracker) compactLocked() {
	dropped := t.callSuperseded(t.announced)
	if len(dropped) == 0 {
		return
	}
	drop := make(map[string]struct{}, len(dropped))
	for _, e := range dropped {
		drop[e] = struct{}{}
	}
	kept := make([]string, 0, len(t.announced))
	for _, e := range t.announced {
		if _, ok := drop[e]; ok {
			delete(t.announcedSet, e)
			continue
		}
		kept = append(kept, e)
	}
	t.announced = kept
	pending := make([]string, 0, len(t.pending))
	for _, e := range t.pending {
		if _, ok := drop[e]; ok {
			continue
		}
		pending = append(pending, e)
	}
	t.pending = pending
}

// readAnnounced 读本成员在该键下已宣告的条目。
//
// Python 侧 state 是 list；Go 侧经 JSON/checkpoint 往返后可能是 []any，
// 两种都接受（与 readAnnounced 上游容忍非 list 的语义一致）。
//
// Python: _read_announced (team_context.py L910-915)
func (t *taskNoticeTracker) readAnnounced(sess sessioninterfaces.SessionFacade) []string {
	v, err := sess.GetState(sessionstate.StringKey(t.stateKey))
	if err != nil || v == nil {
		return nil
	}
	if list, ok := v.([]string); ok {
		return list
	}
	listAny, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(listAny))
	for _, item := range listAny {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// callSuperseded 调淘汰钩子（未注入则不淘汰）。
// Python: _superseded (team_context.py L856-859)
func (t *taskNoticeTracker) callSuperseded(entries []string) []string {
	if t.hookSuperseded == nil {
		return nil
	}
	return t.hookSuperseded(entries)
}

// callCandidates 调候选查询钩子（未注入则无候选）。
// Python: _candidates (team_context.py L861-862)
func (t *taskNoticeTracker) callCandidates(tm *tools.TeamTaskManager) []*database.TeamTaskBase {
	if t.hookCandidates == nil {
		return nil
	}
	return t.hookCandidates(tm)
}

// callEntry 调条目生成钩子（未注入则空串）。
// Python: _entry (team_context.py L864-865)
func (t *taskNoticeTracker) callEntry(task *database.TeamTaskBase) string {
	if t.hookEntry == nil {
		return ""
	}
	return t.hookEntry(task)
}

// callRender 调渲染钩子（未注入则空串）。
// Python: _render (team_context.py L867-868)
func (t *taskNoticeTracker) callRender(tasks []*database.TeamTaskBase) string {
	if t.hookRender == nil {
		return ""
	}
	return t.hookRender(tasks)
}

// TaskOutcomeTracker 在下一次输入时告知成员「你提交的哪些任务通过了验收」。
//
// 调度模式下通过的评审不唤醒作者（F_119）：它无事可做，唤醒只会推迟它退休。
// 事实仍然到达——晚一点，由下一次唤醒它的东西（启动通知、leader 或用户消息）
// 携带。
//
// 这是**事件**而非持续状态，因此折进被受理的输入本身，永不进 team.context
// 附件（那是当前状态通道）。真相来源是任务表：成员名下**带验收人**的已完成
// 任务；一个条目就是一个 task_id。没有验收人而完成的永不宣告——那是成员自己
// 交的差。
//
// 通知文案跟进程级运行时语言（schema.T），与名册 note 一致。
//
// Python: TaskOutcomeTracker (team_context.py L871-907)
type TaskOutcomeTracker struct {
	// taskNoticeTracker 共享基线机制
	taskNoticeTracker
}

// newTaskOutcomeTracker 构造评审通过通知器。
func newTaskOutcomeTracker(tb *tools.TeamBackend, memberName string) *TaskOutcomeTracker {
	tr := &TaskOutcomeTracker{}
	tr.teamBackend = tb
	tr.memberName = memberName
	tr.stateKey = TaskOutcomeStateKey
	tr.wakes = false
	tr.announcedSet = map[string]struct{}{}
	tr.initOutcomeHooks()
	return tr
}

// initOutcomeHooks 装上本子类的四个钩子。
func (t *TaskOutcomeTracker) initOutcomeHooks() {
	t.hookCandidates = t.candidateRows
	t.hookEntry = func(task *database.TeamTaskBase) string { return task.TaskID }
	t.hookRender = t.renderVerified
}

// Claim 见 taskNoticeTracker.claim。
func (t *TaskOutcomeTracker) Claim(sess sessioninterfaces.SessionFacade) *ClaimedNotice {
	return t.claim(sess)
}

// Announce 见 taskNoticeTracker.announce。
func (t *TaskOutcomeTracker) Announce(
	sess sessioninterfaces.SessionFacade, send func(text string) error,
) (bool, error) {
	return t.announce(sess, send)
}

// Wakes 返回该通知器能否作为独立输入唤醒空闲成员。
// Python: _TaskNoticeTracker.WAKES (team_context.py:749)
func (t *TaskOutcomeTracker) Wakes() bool { return t.wakes }

// Release 见 taskNoticeTracker.release。
func (t *TaskOutcomeTracker) Release(notice *ClaimedNotice) { t.release(notice) }

// Persist 见 taskNoticeTracker.persist。
func (t *TaskOutcomeTracker) Persist(sess sessioninterfaces.SessionFacade) { t.persist(sess) }

// candidateRows 取成员名下带验收人的已完成任务。
// Python: TaskOutcomeTracker._candidates (team_context.py L897-899)
func (t *TaskOutcomeTracker) candidateRows(tm *tools.TeamTaskManager) []*database.TeamTaskBase {
	completed, err := tm.GetTasksByAssignee(context.Background(), t.memberName, string(atschema.TaskStatusCompleted))
	if err != nil {
		return nil
	}
	out := make([]*database.TeamTaskBase, 0, len(completed))
	for _, task := range completed {
		if task.HasReviewers() {
			out = append(out, task)
		}
	}
	return out
}

// renderVerified 渲染 kind=task-verified 的事件块。
// Python: TaskOutcomeTracker._render (team_context.py L904-907)
func (t *TaskOutcomeTracker) renderVerified(tasks []*database.TeamTaskBase) string {
	intro, err := atschema.T("team_context.task_verified_intro")
	if err != nil {
		intro = ""
	}
	lines := []string{intro}
	for _, task := range tasks {
		lines = append(lines, "- "+task.TaskID+" · "+task.Title)
	}
	return inboundtags.RenderEvent(TaskVerifiedEventKind, strings.Join(lines, "\n"), inboundtags.EventOptions{})
}

// TaskRevisionTracker 告诉自主成员哪些在手任务被编辑过（F_129）。
//
// 编辑通知的唯一来源：一个 <team-event kind="task-revised">，列出被编辑的任务、
// 它错过的修订与编辑说明，并要求重读任务。编辑的 TASK_UPDATED 事件只促成一次
// 提前 Claim；leader 编辑时恰好离线的成员则由下一次受理的输入告知，因为编辑
// 仍以 revision > observed_revision 立在行上。调度模式不需要它：调度器把最新
// 内容作为持久邮件推送。
//
// 通知不计入「已看到」——它不携带正文。「已看到」始终是 DB 的
// observed_revision；条目是 task_id@revision，只用于阻止同一修订播两次，
// 因此每个任务只保留最新那条。
//
// Python: TaskRevisionTracker (team_context.py L918-971)
type TaskRevisionTracker struct {
	// taskNoticeTracker 共享基线机制
	taskNoticeTracker
}

// newTaskRevisionTracker 构造编辑通知器。
func newTaskRevisionTracker(tb *tools.TeamBackend, memberName string) *TaskRevisionTracker {
	tr := &TaskRevisionTracker{}
	tr.teamBackend = tb
	tr.memberName = memberName
	tr.stateKey = TaskRevisionStateKey
	tr.wakes = true
	tr.announcedSet = map[string]struct{}{}
	tr.hookCandidates = tr.candidateRows
	tr.hookEntry = func(task *database.TeamTaskBase) string {
		return task.TaskID + "@" + strconv.FormatInt(task.Revision, 10)
	}
	tr.hookRender = tr.renderRevised
	tr.hookSuperseded = tr.supersededEntries
	return tr
}

// Claim 见 taskNoticeTracker.claim。
func (t *TaskRevisionTracker) Claim(sess sessioninterfaces.SessionFacade) *ClaimedNotice {
	return t.claim(sess)
}

// Announce 见 taskNoticeTracker.announce。
func (t *TaskRevisionTracker) Announce(
	sess sessioninterfaces.SessionFacade, send func(text string) error,
) (bool, error) {
	return t.announce(sess, send)
}

// Wakes 返回该通知器能否作为独立输入唤醒空闲成员。
// Python: _TaskNoticeTracker.WAKES (team_context.py:749)
func (t *TaskRevisionTracker) Wakes() bool { return t.wakes }

// Release 见 taskNoticeTracker.release。
func (t *TaskRevisionTracker) Release(notice *ClaimedNotice) { t.release(notice) }

// Persist 见 taskNoticeTracker.persist。
func (t *TaskRevisionTracker) Persist(sess sessioninterfaces.SessionFacade) { t.persist(sess) }

// candidateRows 取成员名下「改过但未读」的任务。
// Python: TaskRevisionTracker._candidates (team_context.py L945-946)
func (t *TaskRevisionTracker) candidateRows(tm *tools.TeamTaskManager) []*database.TeamTaskBase {
	rows, err := tm.UnseenEdits(context.Background(), t.memberName)
	if err != nil {
		return nil
	}
	return rows
}

// supersededEntries 每个任务只留最新修订（修订号只增，旧的永不再匹配）。
// Python: TaskRevisionTracker._superseded (team_context.py L948-959)
func (t *TaskRevisionTracker) supersededEntries(entries []string) []string {
	type newest struct {
		num   int64
		entry string
	}
	latest := make(map[string]newest, len(entries))
	for _, e := range entries {
		idx := strings.LastIndex(e, "@")
		if idx < 0 {
			continue
		}
		taskID, revStr := e[:idx], e[idx+1:]
		rev, err := strconv.ParseInt(revStr, 10, 64)
		if err != nil {
			rev = -1
		}
		cur, ok := latest[taskID]
		if !ok || rev > cur.num {
			latest[taskID] = newest{num: rev, entry: e}
		}
	}
	keep := make(map[string]struct{}, len(latest))
	for _, v := range latest {
		keep[v.entry] = struct{}{}
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, ok := keep[e]; !ok {
			out = append(out, e)
		}
	}
	return out
}

// renderRevised 渲染 kind=task-revised 的事件块，含每个任务的未读编辑说明。
// Python: TaskRevisionTracker._render (team_context.py L964-971)
func (t *TaskRevisionTracker) renderRevised(tasks []*database.TeamTaskBase) string {
	intro, err := atschema.T("team_context.task_revised_intro")
	if err != nil {
		intro = ""
	}
	lines := []string{intro}
	for _, task := range tasks {
		lines = append(lines, fmt.Sprintf("- %s · %s · r%d → r%d",
			task.TaskID, task.Title, task.ObservedRevision, task.Revision))
		for _, note := range prompts.ChangeNotesLines(task) {
			lines = append(lines, "  "+note)
		}
	}
	return inboundtags.RenderEvent(TaskRevisedEventKind, strings.Join(lines, "\n"), inboundtags.EventOptions{})
}

// BuildTaskEventTracker 按 dispatch 模式构造非 leader 成员需要的任务通知器。
//
// 调度成员被告知延迟的「评审通过」（F_119）；自主成员被告知它错过的编辑
// （F_129）。每种模式恰好需要一个，另一种情况由该模式自己的通道覆盖。
//
// Python: build_task_event_tracker (team_context.py L974-990)
func BuildTaskEventTracker(
	dispatchMode string,
	tb *tools.TeamBackend,
	memberName string,
) TaskEventTracker {
	if tb == nil {
		return nil
	}
	if dispatchMode == "scheduled" {
		return newTaskOutcomeTracker(tb, memberName)
	}
	return newTaskRevisionTracker(tb, memberName)
}

// removeString 从切片中移除首个匹配项。
func removeString(list []string, target string) []string {
	for i, v := range list {
		if v == target {
			out := make([]string, 0, len(list)-1)
			out = append(out, list[:i]...)
			out = append(out, list[i+1:]...)
			return out
		}
	}
	return list
}

// stringSliceEqual 比较两个字符串切片。
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 编译期兜住：两个具体类型都满足 rail 注入用的窄接口。
var (
	_ TaskEventTracker = (*TaskOutcomeTracker)(nil)
	_ TaskEventTracker = (*TaskRevisionTracker)(nil)
)

// renderServedModels 渲染成员被路由到的模型历史（leader 名册用）。
// 旧的在前，每条带接管的绝对时间（名册进入对话历史并长期驻留，不用相对
// 时间）；最新一条标注为成员最近一次调用的模型。空历史渲染 "none yet"。
// Python: _render_served_models (team_context.py:115-123)
func renderServedModels(history []database.MemberServedModel) string {
	if len(history) == 0 {
		return "none yet"
	}
	parts := make([]string, 0, len(history))
	for _, item := range history {
		parts = append(parts, fmt.Sprintf("%s (since %s)",
			item.Model, inboundtags.FormatAbsoluteTime(item.Since)))
	}
	// 最新一条：替换结尾括号为 ", latest call)"
	last := parts[len(parts)-1]
	parts[len(parts)-1] = last[:len(last)-1] + ", latest call)"
	return strings.Join(parts, ", ")
}

// loadMemberServedModels 从成员 options JSON 读 served_models 历史。
// Python: get_member_served_models (member_options.py:290-292)
func loadMemberServedModels(rawOptions string) []database.MemberServedModel {
	opts := database.LoadMemberOptions(rawOptions)
	return opts.ServedModels
}

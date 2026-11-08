package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamPolicyRail 将团队策略作为有序 PromptSection 注入到系统提示词构建器。
// Python: TeamPolicyRail(DeepAgentRail) (rails/team_policy_rail.py)
//
// Section 分为两类：
// 两条车道，且这个切分就是设计本身：
//
//   - **system prompt builder**（缓存稳定的前缀）——角色、HITT 契约、工作流、
//     生命周期、额外提示词。全部静态**且同一角色的成员之间完全相同**：构造时
//     建一次，每次 BeforeModelCall 重新 AddSection（廉价的 map 插入）。团队状态
//     的变动与逐成员内容永不触碰这段前缀。
//   - **对话历史**——本成员自己的身份、团队元数据、同伴名册。这些要么逐成员、
//     要么团队存在后才出现，因此不能进共享前缀。tracker.TeamContextTracker
//     决定「还有什么没说」；本 rail 把它注册成一个动态附件 section，无变更时
//     重注册上次的内容让 section 保持活跃；ReAct 再在受理的用户消息之前、或
//     状态在工具循环中途出现时在工具结果之后，追加一条 role=system 的快照/
//     增量消息。历史里已有的内容永不重写。
type TeamPolicyRail struct {
	harnessrails.DeepAgentRail
	// role 团队角色
	role atschema.TeamRole
	// teammateMode 队友模式
	teammateMode string
	// lifecycle 生命周期
	lifecycle string
	// teamMode 团队模式
	teamMode string
	// dispatchMode 调度模式（决定注入哪个任务通知器）
	dispatchMode string
	// basePrompt 基础提示词（调用方给这个 leader 的指令，不是团队政策）
	basePrompt string
	// language 语言
	language string
	// memberName 成员名
	memberName string
	// displayName 成员人类可读标签（身份块）
	displayName string
	// memberWorkspacePath 成员私有产物目录（身份块）
	memberWorkspacePath string
	// memberPrompt 成员私有工作约定（身份块 + 演进 delta）
	memberPrompt string
	// teamOutputsDir 最终产物目录（团队信息块，无项目目录的成员才有）
	teamOutputsDir string
	// forkSource 该成员 fork 自谁；空串表示普通 spawn
	forkSource string
	// policyText framework 建队的政策块；nil 关闭政策通道（偏差 D-3，⤵️ 25.0g）
	policyText *string
	// steerBatchSize 一次模型调用最多吸收的 steering 条数
	steerBatchSize int
	// teamBackend 团队后端
	teamBackend *tools.TeamBackend
	// teamWorkspaceMount 工作空间挂载路径
	teamWorkspaceMount string
	// teamWorkspacePath 工作空间绝对路径
	teamWorkspacePath string
	// exposeHumanAgentsToTeammates 是否暴露人类成员给队友
	exposeHumanAgentsToTeammates bool
	// hittEnabled HITT 能力开关（门控 HITT section，不依赖实时人类名册）
	hittEnabled bool
	// systemPromptBuilder 系统提示词构建器（Init 时缓存）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// staticSections 静态 PromptSection 列表（构造时构建）
	staticSections []saprompt.PromptSection
	// tracker 团队状态决策层（四通道增量宣告）
	tracker *tracker.TeamContextTracker
	// taskEventTracker 任务通知（非 leader 才有；nil 关闭通知）
	taskEventTracker tracker.TaskEventTracker
}

// TeamPolicyRailOption TeamPolicyRail 构造选项。
type TeamPolicyRailOption func(*TeamPolicyRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamPolicyRailPriority TeamPolicyRail 优先级，对齐 Python priority=12
	teamPolicyRailPriority = 12
	// defaultSteerBatchSize 一次模型调用默认吸收的 steering 条数
	// Python: steer_batch_size: int = 2 (team_policy_rail.py L147)
	defaultSteerBatchSize = 2
	// teamContextAttachmentSection 承载团队状态的附件 section 名
	// Python: TEAM_CONTEXT_ATTACHMENT_SECTION (team_policy_rail.py L85)
	teamContextAttachmentSection = "team.context"
	// teamContextAttachmentPriority 该附件的优先级
	// Python: priority=80 (team_policy_rail.py L437)
	teamContextAttachmentPriority = 80
	// teamContextAttachmentSource 该附件的来源标记
	// Python: source="agent_core.team_policy_rail"
	teamContextAttachmentSource = "agent_core.team_policy_rail"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证：确保 TeamPolicyRail 满足 DeepAgentRailProvider 接口
var _ harnessrails.DeepAgentRailProvider = (*TeamPolicyRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamPolicyRail 创建 TeamPolicyRail 实例。
// 静态 Section 在构造期构建，模板缺失时返回错误（对齐 Python __init__ 抛 FileNotFoundError）。
// Python: TeamPolicyRail.__init__(role, persona, ...)
func NewTeamPolicyRail(opts ...TeamPolicyRailOption) (*TeamPolicyRail, error) {
	r := &TeamPolicyRail{
		DeepAgentRail:  *harnessrails.NewDeepAgentRail(),
		steerBatchSize: defaultSteerBatchSize,
	}
	r.WithPriority(teamPolicyRailPriority)
	for _, opt := range opts {
		opt(r)
	}

	// Python: human_names = sorted(team_backend.human_agent_names()) if team_backend else []
	var humanNames []string
	if r.teamBackend != nil {
		humanNames = r.teamBackend.HumanAgentNames()
	}

	// Python: self._static_sections = self._build_static_sections(...)
	staticSections, err := r.buildStaticSections(humanNames)
	if err != nil {
		return nil, err
	}
	r.staticSections = staticSections

	// 团队状态决策层：四通道探针 + 基线持久化
	r.tracker = tracker.NewTeamContextTracker(
		r.teamBackend,
		r.memberName,
		r.role,
		r.displayName,
		r.memberWorkspacePath,
		r.memberPrompt,
		r.teamWorkspacePath,
		r.teamOutputsDir,
		r.exposeHumanAgentsToTeammates,
		r.language,
		r.forkSource,
		r.policyText,
	)
	// 非 leader 才需要任务通知（Python: task_event_tracker if role != LEADER）
	if r.role == atschema.TeamRoleLeader {
		r.taskEventTracker = nil
	}

	return r, nil
}

// Init 缓存 Agent 的共享 prompt builder。
// Python: TeamPolicyRail.init(agent) — 调用 super().init(agent) 后设置 system_prompt_builder
// Go 侧 DeepAgentRail.Init 是 no-op（BaseRail 默认实现），因此不调用 super init
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 从共享 builder 移除所有团队 Section。
// Python: TeamPolicyRail.uninit(agent)
func (r *TeamPolicyRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		for _, section := range r.staticSections {
			r.systemPromptBuilder.RemoveSection(section.Name)
		}
	}
	r.systemPromptBuilder = nil
	return nil
}

// BeforeModelCall 注入静态 Section，然后补投无处搭车的团队状态。
//
// 静态 Section 是廉价的 map 插入，每轮重加即可（builder 按名称覆盖）。
// 状态车道优先走附件注册；没有附件管理器时退化为「追加一条历史消息」。
//
// Python: before_model_call (team_policy_rail.py L381-387)
func (r *TeamPolicyRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder != nil {
		for _, section := range r.staticSections {
			r.systemPromptBuilder.AddSection(section)
		}
	}
	if cbc != nil {
		r.announceUnattachedState(ctx, cbc)
	}
	return nil
}

// StaticSections 返回静态 Section 列表（仅用于测试）。
func (r *TeamPolicyRail) StaticSections() []saprompt.PromptSection {
	return r.staticSections
}

// GetCallbacks 声明本 rail 覆盖的事件映射。
//
// **必须重写**：BaseRail.GetCallbacks 返回空 map，不声明的事件永不触发。
// BeforeModelCall 此前靠装配侧隐式生效，此处一并显式声明，三条在同一张表可见。
//
// Python: DeepAgentRail.get_callbacks() 的 TeamPolicyRail 部分
func (r *TeamPolicyRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackOnUserMessage] = func(ctx context.Context, railCtx any) error {
		return r.OnUserMessage(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeSteeringDrain] = func(ctx context.Context, railCtx any) error {
		return r.BeforeSteeringDrain(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// OnUserMessage 丢弃被取代的输入、打时间锚点，然后折入团队状态。
//
// 三件事按序作用在「即将合成一条消息」的输入列表上：先剪枝，再在存活项之前
// 插入 <team-clock>，最后把团队状态插在更前面。
//
// <team-clock> 是让每条 <team-inbound> 上的绝对 time 可读成「多久以前」的锚点。
// 入站块本身不带相对时间，因为它们留在历史里：渲染一次「刚刚」，一小时后成员
// 再读同一块仍写「刚刚」。锚点每次受理输入写一次、永不刷新，因此每条都忠实
// 记录该输入到达的时刻，最新的一条就是成员的当前时间。
//
// 三件事都在这里做，因为这是它们还是**输入**的唯一时刻。再往后一步它们就是一条
// 普通历史消息——按位置再也定位不到（compaction 会重写它背后的历史），也不再
// 可以改（改一条消息会让其后每个 KV 缓存条目失效）。所以忙碌期间堆积的被取代
// 任务板必须在这里丢，否则永远丢不掉；待发的团队状态必须在这里折入，否则只能
// 由 announceUnattachedState 作为独立消息追加。
//
// 调度模式下尚未告知的「评审通过」也在这里折入（F_119）。它们是事件而非持续
// 状态，因此永远 inline 进这次输入，绝不进 team.context 附件。
//
// Python: on_user_message (team_policy_rail.py L268-319)
func (r *TeamPolicyRail) OnUserMessage(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.UserMessageInputs)
	if !ok || inputs == nil {
		return nil
	}

	// 步骤 1：剪枝（切片长度可变，必须回写）
	inputs.Parts = r.dropSuperseded(inputs.Parts)
	// 步骤 2：时间锚点插在最前
	if len(inputs.Parts) > 0 {
		clock := inboundtags.RenderClock(inboundtags.FormatAbsoluteTime(database.GetCurrentTime()))
		inputs.Parts = append([]string{clock}, inputs.Parts...)
	}

	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	// 步骤 3：任务通知（事件，永远 inline）
	inputs.Parts = r.foldTaskOutcomes(inputs.Parts, sess)
	// 步骤 4：附件车道优先——注册成功就由它负责投递
	if r.queueTeamContextAttachment(ctx, cbc, sess) {
		return nil
	}
	// 步骤 5：兜底——把待发状态前插进本批输入
	if text, ok := r.tracker.PendingText(sess); ok {
		inputs.Parts = append([]string{text}, inputs.Parts...)
		r.tracker.Commit(sess, nil)
	}
	return nil
}

// foldTaskOutcomes 把尚未告知成员的任务事实前插到它的输入上。
//
// 顺序是「先 insert 进本批输入，再 persist 基线」，与上游一致。这个顺序不是
// 随意的：persist 只写会话 state 并 commit，它成功不代表这条 notice 真的会成为
// 一条历史消息。先把 text 放进 parts，本轮就一定会投递出去；persist 若失败，
// 基线留在旧值，下次会再 claim 一次（重复一条总比丢一条便宜）。反过来先
// persist 再插入，一旦「基线已推进、但 parts 没来得及交付」，这条通知就永久丢了。
//
// Python: _fold_task_outcomes (team_policy_rail.py L321-328)
func (r *TeamPolicyRail) foldTaskOutcomes(parts []string, sess sessioninterfaces.SessionFacade) []string {
	if r.taskEventTracker == nil {
		return parts
	}
	notice := r.taskEventTracker.Claim(sess)
	if notice == nil {
		return parts
	}
	// 先插入（Python: parts.insert(0, notice.text)），再持久化基线
	out := append([]string{notice.Text}, parts...)
	r.taskEventTracker.Persist(sess)
	return out
}

// BeforeSteeringDrain 限制一次模型调用吸收多少 steering 积压。
//
// 这是 dropSuperseded 处理的问题的另一半，早一步：框架给忙碌成员排队的一切会
// 一次性交出去，而这里排队的是邮箱流量——一条消息一条，谁也不取代谁，全都得读。
// 因此丢弃不是选项：唯一能避免这一轮变成「一墙糊在一起的消息」的办法就是少拿
// 几条，剩下的留给后续模型调用。
//
// **leader 豁免**，理由与它豁免任务板剪枝相同：它按顺序读到达，拆碎了看到的
// 序列就得自己重新拼。
//
// Python: before_steering_drain (team_policy_rail.py L330-350)
func (r *TeamPolicyRail) BeforeSteeringDrain(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.role == atschema.TeamRoleLeader || cbc == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.SteeringDrainInputs)
	if !ok || inputs == nil {
		return nil
	}
	limit := r.steerBatchSize
	inputs.Limit = &limit
	return nil
}

// dropSuperseded 移除被后续条目取代的排队输入，返回存活列表。
//
// 两个输入队列都会把成员忙碌期间堆积的一切交出来，所以忙碌一段后被唤醒的成员
// 会看到期间渲染的每一份任务板——每份都是完整调查，除最后一份全是过期的。
// 每份板就是一个完整排队输入，因此过期的按整条移除；不解析、不重写。
//
// **leader 的板不动。** teammate 的板是工作队列，列的是此刻可认领的任务，只有
// 当前那份可执行。leader 的板是全队未完成工作，它读的是**序列**（哪个任务出现、
// 哪个移动了）来决定重规划还是收尾。剪掉它等于删掉它正在盯的那些变迁。
//
// Python: _drop_superseded (team_policy_rail.py L352-379)
func (r *TeamPolicyRail) dropSuperseded(parts []string) []string {
	if r.role == atschema.TeamRoleLeader || len(parts) < 2 {
		return parts
	}
	kept := inboundtags.DropSupersededSnapshots(parts)
	if len(kept) == len(parts) {
		return parts
	}
	logger.Debug(logComponent).
		Str("member_name", r.memberName).
		Int("dropped", len(parts)-len(kept)).
		Int("total", len(parts)).
		Msg("dropped superseded inputs")
	return kept
}

// queueTeamContextAttachment 把本成员团队状态注册为一个动态附件 section。
//
// 附件管理器是**当前状态**通道：每个边界把注册区与历史里记录的状态做 diff，
// 未注册的 section 读作已撤回。管理器挂在 agent 实例上，因此重建的成员从空注册
// 区开始，team.context 必须在每个边界重新注册：
//   - 历史里没有附件快照（首投，或 compaction 丢掉了它）→ 完整当前状态
//   - tracker 有新内容 → 那段增量，名册变化因此保持 join/leave 增量
//   - 什么都没变 → 上次提交的同一份内容，内容 hash 与历史一致，不产生宣告
//
// tracker 基线与注册内容只在 ReAct 历史同步成功后提交（25.0a 的 commit 回调）。
// 返回 true 表示附件车道接管投递；false（无管理器，或注册失败）时回落到历史
// 消息车道。
//
// Python: _queue_team_context_attachment (team_policy_rail.py L389-452)
func (r *TeamPolicyRail) queueTeamContextAttachment(
	_ context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	sess sessioninterfaces.SessionFacade,
) bool {
	manager := hprompts.ManagerFor(cbc.Agent())
	modelCtx := cbc.ModelContext()
	if manager == nil || modelCtx == nil {
		return false
	}
	sessionID := sess.GetSessionID()
	hasSnapshot := manager.HasHistorySnapshot(modelCtx, sessionID)
	text, hasText := r.tracker.PendingText(sess)
	lastText, hasLast := r.tracker.LastDeliveredText(sess)

	var content string
	switch {
	case !hasSnapshot:
		if snap, ok := r.tracker.SnapshotText(); ok {
			content = snap
		}
	case hasText:
		content = text
	default:
		// 内容被记住之前就投递过的会话：一次性回落到全量，之后持续重注册
		if hasLast {
			content = lastText
		} else if snap, ok := r.tracker.SnapshotText(); ok {
			content = snap
		}
	}
	if content == "" {
		// 车道拥有投递权，但无内容可送——不能落到兜底路径（否则会重复投递）
		return true
	}
	if !hprompts.DeliverText(cbc.Agent(), sessionID, teamContextAttachmentSection, content,
		hprompts.AttachmentRuntime, teamContextAttachmentSource, teamContextAttachmentPriority) {
		logger.Warn(logComponent).
			Str("member_name", r.memberName).
			Msg("failed to queue team context attachment")
		return false
	}

	if hasText || content != lastText {
		delivered := content
		appendCommitCallback(cbc, func() (err error) {
			// 回调在 ReAct 侧被 recover 兜住，但这里仍防一手：commit 不该抛
			defer func() {
				if rec := recover(); rec != nil {
					err = nil
				}
			}()
			r.tracker.Commit(sess, &delivered)
			return nil
		})
	}
	return true
}

// announceUnattachedState 追加「无处搭车」的团队状态。
//
// 状态通常随输入到达并搭车（见 OnUserMessage），但也可能出现在工具循环中途：
// leader 调 build_team 在轮次中间建出团队、自己的成员行与名册，而下一次输入可能
// 很远。因此模型调用时刻仍待发的状态就变成一条独立消息。
//
// **追加，绝不插入**：尾部是唯一不需要索引、也不会被 compaction 重写背后历史
// 而失效的位置。
//
// Python: _announce_unattached_state (team_policy_rail.py L454-477)
func (r *TeamPolicyRail) announceUnattachedState(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	modelCtx := cbc.ModelContext()
	sess := cbc.Session()
	if modelCtx == nil || sess == nil {
		return
	}
	if r.queueTeamContextAttachment(ctx, cbc, sess) {
		return
	}
	text, ok := r.tracker.PendingText(sess)
	if !ok {
		return
	}
	_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewUserMessage(text)})
	r.tracker.Commit(sess, nil)
}

// TaskEventTracker 返回本 rail 注入的任务通知器（装配校验用）。
func (r *TeamPolicyRail) TaskEventTracker() tracker.TaskEventTracker {
	return r.taskEventTracker
}

// WithPolicyRole 设置角色。
func WithPolicyRole(role atschema.TeamRole) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.role = role }
}

// WithPolicyDisplayName 设置成员展示名。
func WithPolicyDisplayName(name string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.displayName = name }
}

// WithPolicyMemberWorkspacePath 设置成员私有工作区路径。
func WithPolicyMemberWorkspacePath(path string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.memberWorkspacePath = path }
}

// WithPolicyMemberPrompt 设置成员私有工作约定。
func WithPolicyMemberPrompt(prompt string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.memberPrompt = prompt }
}

// WithPolicyTeamOutputsDir 设置团队最终产物目录。
func WithPolicyTeamOutputsDir(dir string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamOutputsDir = dir }
}

// WithPolicyForkSource 设置 fork 来源成员名。
func WithPolicyForkSource(source string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.forkSource = source }
}

// WithPolicySteerBatchSize 设置 steering 批上限。
func WithPolicySteerBatchSize(size int) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.steerBatchSize = size }
}

// WithPolicyPolicyText 设置 framework 建队的政策块（已渲染）。
func WithPolicyPolicyText(text *string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.policyText = text }
}

// WithPolicyTaskEventTracker 注入任务通知器。
func WithPolicyTaskEventTracker(t tracker.TaskEventTracker) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.taskEventTracker = t }
}

// WithPolicyDispatchMode 设置调度模式。
func WithPolicyDispatchMode(mode string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.dispatchMode = mode }
}

// WithPolicyMemberName 设置成员名。
func WithPolicyMemberName(name string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.memberName = name }
}

// WithPolicyLifecycle 设置生命周期。
func WithPolicyLifecycle(lifecycle string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.lifecycle = lifecycle }
}

// WithPolicyTeammateMode 设置队友模式。
func WithPolicyTeammateMode(mode string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teammateMode = mode }
}

// WithPolicyLanguage 设置语言。
func WithPolicyLanguage(lang string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.language = lang }
}

// WithPolicyTeamMode 设置团队模式。
func WithPolicyTeamMode(mode string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamMode = mode }
}

// WithPolicyBasePrompt 设置基础提示词。
func WithPolicyBasePrompt(prompt string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.basePrompt = prompt }
}

// WithPolicyTeamWorkspaceMount 设置工作空间挂载路径。
func WithPolicyTeamWorkspaceMount(mount string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamWorkspaceMount = mount }
}

// WithPolicyTeamWorkspacePath 设置工作空间绝对路径。
func WithPolicyTeamWorkspacePath(path string) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamWorkspacePath = path }
}

// WithPolicyTeamBackend 设置团队后端。
func WithPolicyTeamBackend(tb *tools.TeamBackend) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.teamBackend = tb }
}

// WithPolicyExposeHumanAgents 设置是否暴露人类成员。
func WithPolicyExposeHumanAgents(v bool) TeamPolicyRailOption {
	return func(r *TeamPolicyRail) { r.exposeHumanAgentsToTeammates = v }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildStaticSections 构造不变的静态 Section 列表。
// 模板缺失时返回错误（对齐 Python _build_static_sections 内 load_template 抛异常）。
//
// humanNames 保留入参位但不再使用：HITT 已改为门控 hittEnabled 能力开关，
// 人类名册走状态车道（标 [human]）。保留参数只为与 NewTeamPolicyRail 的
// 调用形态一致（Task 13 会改为直接传 nil）。
//
// Python: TeamPolicyRail._build_static_sections(...)
func (r *TeamPolicyRail) buildStaticSections(_ []string) ([]saprompt.PromptSection, error) {
	var sections []saprompt.PromptSection

	// Role Section（总是存在）
	roleSection, err := prompts.BuildTeamRoleSection(r.role, r.teammateMode, r.workspacePromptVariant(), r.language)
	if err != nil {
		return nil, err
	}
	if roleSection != nil {
		sections = append(sections, *roleSection)
	}
	// HITT Section（门控在 hittEnabled 能力开关上，不在实时名册上）
	hittSection, err := prompts.BuildTeamHITTSection(r.role, r.hittEnabled, r.language, r.memberName, r.exposeHumanAgentsToTeammates)
	if err != nil {
		return nil, err
	}
	if hittSection != nil {
		sections = append(sections, *hittSection)
	}
	// Workflow Section（仅 Leader）
	workflowSection, err := prompts.BuildTeamWorkflowSection(r.role, r.teamMode, r.language)
	if err != nil {
		return nil, err
	}
	if workflowSection != nil {
		sections = append(sections, *workflowSection)
	}
	// Lifecycle Section（仅 Leader）
	lifecycleSection, err := prompts.BuildTeamLifecycleSection(r.role, r.lifecycle, r.language)
	if err != nil {
		return nil, err
	}
	if lifecycleSection != nil {
		sections = append(sections, *lifecycleSection)
	}
	// Extra Section（仅当 basePrompt 非空）
	if s := prompts.BuildTeamExtraSection(r.basePrompt, r.language); s != nil {
		sections = append(sections, *s)
	}
	return sections, nil
}

// workspacePromptVariant 返回工作区措辞变体。
//
// 外部 CLI 成员拿到的是路径式共享工作区说明，进程内成员拿 .team 挂载说明。
// 上游由 rail 工厂按成员类型判定后传入；Go 侧角色即判定依据。
//
// Python: team_policy_rail.py —— rail 工厂按成员类型判定
func (r *TeamPolicyRail) workspacePromptVariant() string {
	if r.role == atschema.TeamRoleExternalCLI {
		return "external"
	}
	return "native"
}

// appendCommitCallback 把回调追加进本次调用的投递成功队列。
//
// 25.0a 的 runCommitCallbacks 会取走并逐条执行；按约定只在附件 Sync 成功后
// 才走到那里，因此基线推进与「内容真的进了历史」是同一件事。
func appendCommitCallback(cbc *agentinterfaces.AgentCallbackContext, fn func() error) {
	if cbc == nil || fn == nil {
		return
	}
	raw, _ := cbc.GetExtra(hprompts.CommitCallbacksExtraKey)
	list, _ := raw.([]func() error)
	cbc.SetExtra(hprompts.CommitCallbacksExtraKey, append(list, fn))
}

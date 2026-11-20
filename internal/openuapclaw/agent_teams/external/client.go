// 本文件是外部团队客户端 ExternalTeamClient：经共享 db + messager 把一个
// 外部 agent 挂到一个运行中的团队上。从 TeamJoinDescriptor 构造后调 Connect
// 即可使用；所有操作运行在调用方 ctx（携带团队 session_id）上，发布的事件
// 落在正确的 topic 上。
//
// 归包偏差（沿 external → runtime/externalcli 既有归包，D-15.12-e）：
// Python openjiuwen/agent_teams/external/client.py（593 行）一比一移植。
//
// 已知降级（详见 spec 偏差登记）：
//   - D-15.16-b9：workspace 演化缓存面（WorkspaceCache/WorkspaceStore/
//     WorkspaceMetaTool + MakeTranslator 的 ws_cache 参数）未移植，member
//     scope connect 时跳过 workspace_meta 工具与 C 类描述演化绑定；
//   - D-15.16-b10：group_chat 广播正文扩展（context_for/group_metadata）与
//     框架模板正文扩展（expand_message/task_links/revision_getters，
//     ⤴️25.6）未移植，fetch_inbox 广播消息保留原始 content。
//
// Python: openjiuwen/agent_teams/external/client.py
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	inboundtags "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	atevents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件常量。

// ──────────────────────────── 常量 ────────────────────────────

// broadcastTarget 广播目标成员名。
// Python: BROADCAST_TARGET = "*"（tools/tool_member.py）
const broadcastTarget = "*"

// ──────────────────────────── 结构体 ────────────────────────────

// InboxView 当前成员需要关注内容的快照。
// Python: InboxView (client.py:77-91)
type InboxView struct {
	// Messages 收件人未读的直发 + 广播消息
	Messages []*database.TeamMessageBase
	// Tasks 全部非终态团队任务（可行动任务板）
	Tasks []*database.TeamTaskBase
}

// IsEmpty 是否没有成员需要处理的内容。
// Python: InboxView.is_empty (client.py:89-91)
func (v *InboxView) IsEmpty() bool {
	return len(v.Messages) == 0 && len(v.Tasks) == 0
}

// InboxObserver 收件箱观察回调（watch 用）。
// Python: InboxObserver（async callback，client.py __all__）
type InboxObserver func(ctx context.Context, view *InboxView) error

// ExternalTeamClient 经共享 db + messager 把外部 agent 挂到运行中的团队。
// Python: ExternalTeamClient (client.py:94-117)
type ExternalTeamClient struct {
	descriptor   *TeamJoinDescriptor
	messager     messager.Messager
	backend      *tools.TeamBackend
	tasks        *tools.TeamTaskManager
	messages     *tools.TeamMessageManager
	db           database.TeamDatabase
	workspaceMgr *team_workspace.TeamWorkspaceManager
	// teamTools member scope 真团队工具，按 card.Name 索引。member scope 的
	// connect() 时构建，外部 CLI 成员调用与原生进程内 teammate 完全相同的
	// TeamTool 实例（同 schema + 同渲染文本）。operator scope 为空。
	// Python: self._tools (client.py:112-116)
	teamTools map[string]tool.Tool
	connected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExternalTeamClient 绑定入团描述符，不打开任何资源。
// Python: __init__ (client.py:103-117)
func NewExternalTeamClient(descriptor *TeamJoinDescriptor) *ExternalTeamClient {
	return &ExternalTeamClient{
		descriptor: descriptor,
		teamTools:  map[string]tool.Tool{},
	}
}

// SessionID 本客户端绑定的团队会话 id。
// Python: session_id property (client.py:119-128)
func (c *ExternalTeamClient) SessionID() string { return c.descriptor.SessionID }

// Language 本客户端渲染用的团队运行语言。
// Python: language property (client.py:130-133)
func (c *ExternalTeamClient) Language() string { return c.descriptor.Language }

// MemberName 本客户端服务的成员身份。
// Python: member_name property (client.py:135-138)
func (c *ExternalTeamClient) MemberName() string { return c.descriptor.MemberName }

// TeamName 目标团队标识。
// Python: team_name property (client.py:140-143)
func (c *ExternalTeamClient) TeamName() string { return c.descriptor.TeamName }

// IsLeader 本成员是否携带 leader 角色。
// Python: is_leader property (client.py:145-148)
func (c *ExternalTeamClient) IsLeader() bool { return c.descriptor.Role == "leader" }

// IsHumanAgent 是否为 human-agent 头像（驱动入站 note）。
// Python: is_human_agent property (client.py:150-153)
func (c *ExternalTeamClient) IsHumanAgent() bool { return c.descriptor.Role == "human_agent" }

// Scope 外部访问场景："member" 或 "operator"。
// Python: scope property (client.py:155-158)
func (c *ExternalTeamClient) Scope() string { return c.descriptor.Scope }

// Tools member scope 的真团队工具（按 card.Name 索引）。
// 仅 scope=="member" 时由 Connect 填充；operator scope 为空。
// Python: tools property (client.py:160-170)
func (c *ExternalTeamClient) Tools() map[string]tool.Tool { return c.teamTools }

// BindSessionContext 为本次调用重新落 session_id + language 绑定。
// 在装饰方法之外直接调暴露的 TeamTool 对象的分发方（MCP server），
// 每次操作前必须调用。
// Python: bind_session_context (client.py:172-181)
func (c *ExternalTeamClient) BindSessionContext(ctx context.Context) context.Context {
	state := sessionctx.InitSessionState()
	state.SetSessionID(c.descriptor.SessionID)
	_ = agentteams.SetLanguage(atschema.Language(c.descriptor.Language))
	return sessionctx.WithSessionState(ctx, state)
}

// Connect 打开团队数据库与 messager，接好各管理器。
// member scope 还会经 CreateTeamTools 构建真 teammate 团队工具，外部成员
// 与进程内成员不可区分。幂等：二次调用为空操作。
// Python: connect (client.py:183-271)
func (c *ExternalTeamClient) Connect(ctx context.Context) error {
	if c.connected {
		return nil
	}
	ctx = c.BindSessionContext(ctx)

	db := sharedDBFactory(c.descriptor.DBConfig)
	if err := db.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize team database: %w", err)
	}

	transport := c.descriptor.TransportConfig
	var msg messager.Messager
	if transport != nil && transport.Backend == "hybrid" {
		// Python: hybrid 分支（client.py:202-216）
		if transport.ExternalPublishURL == "" {
			return fmt.Errorf("hybrid messager requires external_publish_url")
		}
		publisher := messager.NewWebSocketEventPublisher(
			transport.ExternalPublishURL,
			c.SessionID(),
			c.TeamName(),
			transport.RequestTimeout,
		)
		msg = messager.NewHybridMessager(publisher, c.MemberName())
	} else {
		cfg := atschema.MessagerTransportConfig{}
		if transport != nil {
			cfg = *transport
		}
		var err error
		msg, err = messager.CreateMessager(cfg)
		if err != nil {
			return err
		}
	}
	if err := msg.Start(ctx); err != nil {
		return fmt.Errorf("start messager: %w", err)
	}
	c.messager = msg

	var opts []tools.TeamBackendOption
	opts = append(opts, tools.WithMemberMessaging(c.descriptor.EnableMemberMessaging))
	opts = append(opts, tools.WithMaxSubtaskDepth(c.descriptor.MaxSubtaskDepth))
	backend := tools.NewTeamBackend(
		c.TeamName(), c.MemberName(), c.IsLeader(),
		db, msg, opts...,
	)
	// Python: backend.group_chat_spec = SimpleNamespace(...) + bind_group_session
	// —— group_chat 子系统未移植（⤴️），跳过（D-15.16-b10）。
	c.backend = backend
	c.tasks = backend.TaskManager()
	c.messages = backend.MessageManager()
	c.db = db

	if c.descriptor.Scope == "member" {
		// Python: workspace manager + resident cache 先于 create_team_tools
		// 构建（client.py:242-249）——演化缓存面未移植（D-15.16-b9），跳过
		// workspace_meta 工具 attach。
		// 同一工厂、同一 dispatch mode：外部 CLI 成员看到的工具集必须与
		// spawn 时注入的 system prompt 匹配（client.py:250-263）。
		exclude := map[string]struct{}{"checkpoint": {}}
		realTools := tools.CreateTeamTools(
			backend,
			tools.TeamToolOptions{
				Role:         "teammate",
				TeammateMode: c.descriptor.TeammateMode,
				DispatchMode: c.descriptor.DispatchMode,
				Lifecycle:    "temporary",
				Language:     c.descriptor.Language,
			},
			nil,
			exclude,
		)
		for _, tl := range realTools {
			card := tl.Card()
			if card != nil {
				c.teamTools[card.Name] = tl
			}
		}
	}

	c.connected = true
	logger.Info(logComponent).
		Str("team_name", c.TeamName()).
		Str("member_name", c.MemberName()).
		Str("scope", c.descriptor.Scope).
		Msg("ExternalTeamClient connected")
	return nil
}

// Close 停止 messager 并释放自有资源。幂等。
// Python: close (client.py:273-283)
func (c *ExternalTeamClient) Close(ctx context.Context) {
	if c.messager != nil {
		_ = c.messager.Stop(ctx)
		c.messager = nil
	}
	c.tasks = nil
	c.messages = nil
	c.backend = nil
	c.workspaceMgr = nil
	c.teamTools = map[string]tool.Tool{}
	c.connected = false
}

// SendMessage 发直发消息；to == "*" 时广播。
// 返回新消息 id，持久化失败返回 nil。
// Python: send_message (client.py:332-346)
func (c *ExternalTeamClient) SendMessage(ctx context.Context, to, content string) (*string, error) {
	ctx = c.BindSessionContext(ctx)
	messages := c.requireMessages()
	if to == broadcastTarget {
		id, err := messages.BroadcastMessage(ctx, content, c.MemberName())
		if err != nil {
			return nil, err
		}
		return &id, nil
	}
	id, err := messages.SendMessage(ctx, content, to, c.MemberName())
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// CreateTask 创建团队任务（operator scope——外部团队控制）。
// 走与进程内 leader 相同的 TeamTaskManager.Add，发布同样的 task-created 事件。
// Python: create_task (client.py:350-370)
func (c *ExternalTeamClient) CreateTask(ctx context.Context, title, content string, taskID *string, dependencies []string) (*atschema.TaskCreateResult, error) {
	ctx = c.BindSessionContext(ctx)
	var opts []tools.TaskAddOption
	if taskID != nil {
		opts = append(opts, tools.WithTaskID(*taskID))
	}
	if len(dependencies) > 0 {
		opts = append(opts, tools.WithDependencies(dependencies))
	}
	created, err := c.requireTasks().Add(ctx, title, content, opts...)
	if err != nil {
		return nil, err
	}
	return &atschema.TaskCreateResult{Task: created}, nil
}

// ListTasks 列出团队任务，可按状态过滤。
// Python: list_tasks (client.py:372-375)
func (c *ExternalTeamClient) ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error) {
	ctx = c.BindSessionContext(ctx)
	return c.requireTasks().ListTasks(ctx, status)
}

// ClaimableTasks 列出可认领的 pending 任务。
// Python: claimable_tasks (client.py:377-380)
func (c *ExternalTeamClient) ClaimableTasks(ctx context.Context) ([]*database.TeamTaskBase, error) {
	ctx = c.BindSessionContext(ctx)
	return c.requireTasks().GetClaimableTasks(ctx)
}

// GetTask 取单个任务的完整详情，缺失返回 nil。
// Python: get_task (client.py:382-385)
func (c *ExternalTeamClient) GetTask(ctx context.Context, taskID string) (*tools.TaskDetail, error) {
	ctx = c.BindSessionContext(ctx)
	return c.requireTasks().GetTaskDetail(ctx, taskID)
}

// ClaimTask 为本成员认领一个 pending 任务。
// Python: claim_task (client.py:387-390)
func (c *ExternalTeamClient) ClaimTask(ctx context.Context, taskID string) (*atschema.TaskOpResult, error) {
	ctx = c.BindSessionContext(ctx)
	res, err := c.requireTasks().Claim(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// CompleteTask 完成一个已认领任务。
// Python: complete_task (client.py:392-395)
func (c *ExternalTeamClient) CompleteTask(ctx context.Context, taskID string) (*atschema.TaskOpResult, error) {
	ctx = c.BindSessionContext(ctx)
	res, err := c.requireTasks().Complete(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// UpdateTask 编辑任务标题和/或正文（仅 pending / blocked 任务）。
// Python: update_task (client.py:397-406)
func (c *ExternalTeamClient) UpdateTask(ctx context.Context, taskID string, title, content *string) (*atschema.TaskOpResult, error) {
	ctx = c.BindSessionContext(ctx)
	updateTitle, updateContent := "", ""
	hasTitle, hasContent := title != nil, content != nil
	if hasTitle {
		updateTitle = *title
	}
	if hasContent {
		updateContent = *content
	}
	if err := c.requireTasks().UpdateTask(ctx, taskID, updateTitle, updateContent); err != nil {
		return nil, err
	}
	return &atschema.TaskOpResult{OK: true}, nil
}

// ListMembers 列出全部团队成员行。
// Python: list_members (client.py:410-413)
func (c *ExternalTeamClient) ListMembers(ctx context.Context) ([]*database.TeamMember, error) {
	ctx = c.BindSessionContext(ctx)
	return c.requireDB().Member().GetTeamMembers(ctx, c.TeamName(), "")
}

// FetchInbox 读未读消息与当前任务板。
// markRead 为 true（默认）时把未读标记已读，后续轮询不重投。
// Python: fetch_inbox (client.py:417-444)
func (c *ExternalTeamClient) FetchInbox(ctx context.Context, markRead bool) (*InboxView, error) {
	ctx = c.BindSessionContext(ctx)
	messages := c.requireMessages()
	direct, err := messages.GetMessages(ctx, c.MemberName(), true, "")
	if err != nil {
		return nil, err
	}
	broadcast, err := messages.GetBroadcastMessages(ctx, c.MemberName(), true, "")
	if err != nil {
		return nil, err
	}
	// 最旧在前——进程内成员被投递的顺序；单查询各自时间有序，拼接后不是。
	// Python: sorted([*direct, *broadcast], key=timestamp)（client.py:435）
	unread := make([]*database.TeamMessageBase, 0, len(direct)+len(broadcast))
	unread = append(unread, direct...)
	unread = append(unread, broadcast...)
	sort.SliceStable(unread, func(i, j int) bool { return unread[i].Timestamp < unread[j].Timestamp })
	for _, msg := range unread {
		// Python: group_metadata(msg) 分支的广播正文扩展（client.py:437-439）
		// ——group_chat 子系统未移植（D-15.16-b10），保留原始 content。
		if markRead {
			messages.MarkMessageRead(ctx, msg.MessageID, c.MemberName())
		}
	}

	tasks, err := c.ListTasks(ctx, "")
	if err != nil {
		return nil, err
	}
	return &InboxView{Messages: unread, Tasks: tasks}, nil
}

// ReadInbox 把未读消息与任务板渲染成一段文本。
// 进程内成员由协调层「推」入站消息与任务板，外部成员必须「拉」——这是唯一
// 没有原生对应物的工具。文本镜像进程内分发器（render_messages +
// render_task_board），成员两侧读到相同形状。
// Python: read_inbox (client.py:446-479)
func (c *ExternalTeamClient) ReadInbox(ctx context.Context, markRead bool) (string, error) {
	view, err := c.FetchInbox(ctx, markRead)
	if err != nil {
		return "", err
	}
	nowMs := database.GetCurrentTime()
	var parts []string
	if len(view.Messages) > 0 {
		// Python: _expand_template_bodies（client.py:460）——模板正文扩展
		// ⤴️25.6（D-15.16-b10），bodies 为空，普通消息渲染自己的 content。
		bodies := map[string]string{}
		// 消息只带绝对发送时间；前面的时钟锚点让读者知道每条是多久前发的。
		parts = append(parts, inboundtags.RenderClock(inboundtags.FormatAbsoluteTime(nowMs)))
		messagesText, err := RenderMessages(
			toMessageLikes(view.Messages),
			RenderMessagesOptions{
				IsHumanAgent: c.IsHumanAgent(),
				Bodies:       bodies,
			},
		)
		if err != nil {
			return "", err
		}
		parts = append(parts, messagesText)
	}
	board, err := RenderTaskBoard(
		toTaskLikes(view.Tasks), c.IsLeader(), nowMs, c.MemberName(),
	)
	if err != nil {
		return "", err
	}
	if board != "" {
		parts = append(parts, board)
	}
	if len(parts) == 0 {
		return "(inbox empty)", nil
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n\n"
		}
		out += p
	}
	return out, nil
}

// Watch 订阅 MESSAGE 与 TASK topic，团队事件到达即用新收件箱回调 observer，
// 直到 ctx 取消。每个相关事件都当作唤醒触发一次收件箱重取——绕过
// 自事件过滤：重取只会返回发给本成员的消息加实时任务板。
// Python: watch (client.py:526-555)
func (c *ExternalTeamClient) Watch(ctx context.Context, observer InboxObserver) error {
	msg := c.requireMessager()
	sessionID := c.descriptor.SessionID
	messageTopic := atevents.TeamTopicMessage.Build(sessionID, c.TeamName())
	taskTopic := atevents.TeamTopicTask.Build(sessionID, c.TeamName())

	onEvent := func(_ context.Context, _ *atevents.EventMessage) error {
		view, err := c.FetchInbox(ctx, true)
		if err != nil {
			return err
		}
		if !view.IsEmpty() {
			return observer(ctx, view)
		}
		return nil
	}
	if err := msg.Subscribe(ctx, messageTopic, onEvent); err != nil {
		return err
	}
	if err := msg.Subscribe(ctx, taskTopic, onEvent); err != nil {
		return err
	}
	<-ctx.Done()
	_ = msg.Unsubscribe(ctx, messageTopic)
	_ = msg.Unsubscribe(ctx, taskTopic)
	return ctx.Err()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requireTasks 返回任务管理器；未连接报错。
// Python: _require_tasks (client.py:567-570)
func (c *ExternalTeamClient) requireTasks() *tools.TeamTaskManager {
	if c.tasks == nil {
		raiseNotConnected()
	}
	return c.tasks
}

// requireMessages 返回消息管理器；未连接报错。
// Python: _require_messages (client.py:572-575)
func (c *ExternalTeamClient) requireMessages() *tools.TeamMessageManager {
	if c.messages == nil {
		raiseNotConnected()
	}
	return c.messages
}

// requireMessager 返回 messager；未连接报错。
// Python: _require_messager (client.py:577-580)
func (c *ExternalTeamClient) requireMessager() messager.Messager {
	if c.messager == nil {
		raiseNotConnected()
	}
	return c.messager
}

// requireDB 返回团队数据库；未连接报错。
// Python: _require_db (client.py:582-585)
func (c *ExternalTeamClient) requireDB() database.TeamDatabase {
	if c.db == nil {
		raiseNotConnected()
	}
	return c.db
}

// raiseNotConnected 未连接的统一错误。
// Python: _raise_not_connected (client.py:559-565)
func raiseNotConnected() {
	_ = exception.RaiseError(exception.StatusAgentTeamStateInvalid,
		exception.WithMsg("ExternalTeamClient is not connected; call connect() first"))
}

// sharedDBFactory 共享数据库工厂（Python: get_shared_db）。
// 注入缝：spawn.shared_resources.GetSharedDB 属 spawn 包，而本包被 spawn
// 反向消费会成环（externalcli ↔ spawn），由 runtime 包 init 注入。
var sharedDBFactory func(config database.DBConfigProvider) database.TeamDatabase

// SetSharedDBFactory 注入共享数据库工厂（runtime 包 init 接线）。
func SetSharedDBFactory(fn func(config database.DBConfigProvider) database.TeamDatabase) {
	sharedDBFactory = fn
}

// toMessageLikes 把数据库消息行适配成 format 包的结构化视图。
// Python 侧行对象直接满足 _MessageLike；Go 侧以薄适配器对齐。
func toMessageLikes(rows []*database.TeamMessageBase) []MessageLike {
	out := make([]MessageLike, 0, len(rows))
	for _, r := range rows {
		row := r
		out = append(out, &teamMessageLike{row: row})
	}
	return out
}

// toTaskLikes 把数据库任务行适配成 format 包的结构化视图。
func toTaskLikes(rows []*database.TeamTaskBase) []TaskLike {
	out := make([]TaskLike, 0, len(rows))
	for _, r := range rows {
		row := r
		out = append(out, &teamTaskLike{row: row})
	}
	return out
}

// teamMessageLike 数据库消息行的 format.MessageLike 适配。
type teamMessageLike struct {
	row *database.TeamMessageBase
}

// MessageID 消息 id。
func (a *teamMessageLike) MessageID() string { return a.row.MessageID }

// FromMemberName 发送者成员名。
func (a *teamMessageLike) FromMemberName() string { return a.row.FromMemberName }

// Content 消息正文。
func (a *teamMessageLike) Content() string { return a.row.Content }

// Broadcast 是否广播。
func (a *teamMessageLike) Broadcast() bool { return a.row.Broadcast }

// TimestampMs 毫秒 UTC epoch 发送时间。
func (a *teamMessageLike) TimestampMs() int64 { return a.row.Timestamp }

// teamTaskLike 数据库任务行的 format.TaskLike 适配。
type teamTaskLike struct {
	row *database.TeamTaskBase
}

// TaskID 任务 id。
func (a *teamTaskLike) TaskID() string { return a.row.TaskID }

// Status 任务状态。
func (a *teamTaskLike) Status() string { return a.row.Status }

// Title 任务标题。
func (a *teamTaskLike) Title() string { return a.row.Title }

// Content 任务正文。
func (a *teamTaskLike) Content() string { return a.row.Content }

// Assignee 执行者成员名；空表示未指派。
func (a *teamTaskLike) Assignee() string {
	if a.row.Assignee == nil {
		return ""
	}
	return *a.row.Assignee
}

// UpdatedAtMs 最后状态迁移的毫秒 UTC epoch；零值视为从未迁移。
func (a *teamTaskLike) UpdatedAtMs() *int64 {
	if a.row.UpdatedAt == 0 {
		return nil
	}
	t := a.row.UpdatedAt
	return &t
}

// 编译期引用守卫（workspace 字段在 D-15.16-b9 回收前保持挂起形态）。
var (
	_ = team_workspace.TeamWorkspaceConfig{}
	_ = json.Marshal
)

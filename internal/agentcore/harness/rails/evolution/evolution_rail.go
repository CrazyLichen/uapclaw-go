package evolution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionRail 所有演化轨道的基类。
//
// 嵌入 DeepAgentRail，4 个 final 回调自动完成轨迹收集，
// 通过 ext EvolutionExtension 接口实现子类多态分派。
//
// 核心设计（对齐 Python EvolutionRail）：
//   - 轨迹收集是自动的（由基类 4 个 final 回调处理）
//   - 扩展点：OnBeforeInvoke / OnAfterModelCall / OnAfterToolCall /
//     OnAfterInvoke / OnAfterTaskIteration / RunEvolution
//   - 演化触发时机通过 evolution_trigger 参数配置
//
// Python: EvolutionRail(DeepAgentRail)
type EvolutionRail struct {
	rails.DeepAgentRail
	// ext 扩展点接口，实现多态分派
	ext EvolutionExtension
	// trajectoryStore 轨迹存储
	trajectoryStore trajectory.TrajectoryStore
	// builder 轨迹构造器（跨 invoke 复用）
	builder *trajectory.TrajectoryBuilder
	// maxTrajectorySteps 最大轨迹步骤数
	maxTrajectorySteps int
	// evolutionTrigger 演化触发时机
	evolutionTrigger EvolutionTriggerPoint
	// asyncEvolution 是否异步执行演化
	asyncEvolution bool
	// evolutionSem 演化并发控制信号量
	evolutionSem chan struct{}
	// disabledSkills 禁用的技能名称集合
	disabledSkills map[string]bool
	// trajectorySink 轨迹快照写入端点
	trajectorySink trajectory.TrajectorySink
	// teamID 团队标识
	teamID string
	// memberRole 成员角色
	memberRole string
	// defaultMemberRole 默认成员角色。
	// Python: EvolutionRail._default_member_role
	defaultMemberRole string
	// bgTasks 后台任务集合
	bgTasks map[*utils.BackgroundTask]bool
	// pendingHostEvents 待排空的主机事件缓冲
	pendingHostEvents []*stream.OutputSchema
}

// EvolutionRailOption EvolutionRail 构造选项函数。
type EvolutionRailOption func(*EvolutionRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionRail 创建演化轨道基类实例。
//
// ext 参数为扩展点接口，不能为 nil；opts 可选覆盖默认配置。
//
// Python: EvolutionRail.__init__(
//
//	trajectory_store=None, team_trajectory_store=None(已废弃),
//	max_trajectory_steps=200, evolution_trigger=AFTER_INVOKE,
//	async_evolution=True, max_concurrent_evolution=1, disabled_skills=None
//
// )
func NewEvolutionRail(ext EvolutionExtension, opts ...EvolutionRailOption) *EvolutionRail {
	r := &EvolutionRail{
		ext:                ext,
		trajectoryStore:    trajectory.NewInMemoryTrajectoryStore(),
		maxTrajectorySteps: 200,
		evolutionTrigger:   TriggerAfterInvoke,
		asyncEvolution:     true,
		evolutionSem:       make(chan struct{}, 1),
		disabledSkills:     make(map[string]bool),
		bgTasks:            make(map[*utils.BackgroundTask]bool),
		pendingHostEvents:  make([]*stream.OutputSchema, 0),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithTrajectoryStore 设置轨迹存储。
func WithTrajectoryStore(store trajectory.TrajectoryStore) EvolutionRailOption {
	return func(r *EvolutionRail) { r.trajectoryStore = store }
}

// WithMaxTrajectorySteps 设置最大轨迹步骤数。
func WithMaxTrajectorySteps(n int) EvolutionRailOption {
	return func(r *EvolutionRail) { r.maxTrajectorySteps = n }
}

// WithEvolutionTrigger 设置演化触发时机。
func WithEvolutionTrigger(trigger EvolutionTriggerPoint) EvolutionRailOption {
	return func(r *EvolutionRail) { r.evolutionTrigger = trigger }
}

// WithAsyncEvolution 设置是否异步执行演化。
func WithAsyncEvolution(async bool) EvolutionRailOption {
	return func(r *EvolutionRail) { r.asyncEvolution = async }
}

// WithMaxConcurrentEvolution 设置最大并发演化数。
func WithMaxConcurrentEvolution(n int) EvolutionRailOption {
	return func(r *EvolutionRail) { r.evolutionSem = make(chan struct{}, n) }
}

// WithDisabledSkills 设置禁用的技能名称列表。
func WithDisabledSkills(names []string) EvolutionRailOption {
	return func(r *EvolutionRail) { r.disabledSkills = normalizeSkillNames(names) }
}

// WithDefaultMemberRole 设置默认成员角色。
// Python: EvolutionRail(default_member_role=...)
func WithDefaultMemberRole(role string) EvolutionRailOption {
	return func(r *EvolutionRail) {
		r.defaultMemberRole = role
	}
}

// Priority 返回优先级 60。
// Python: EvolutionRail.priority = 60
func (r *EvolutionRail) Priority() int { return 60 }

// TrajectoryStore 返回轨迹存储。
func (r *EvolutionRail) TrajectoryStore() trajectory.TrajectoryStore {
	return r.trajectoryStore
}

// DisabledSkills 返回禁用技能集合。
func (r *EvolutionRail) DisabledSkills() map[string]bool {
	return r.disabledSkills
}

// Builder 返回当前轨迹构造器（子类需要）。
// Python: EvolutionRail.builder property
func (r *EvolutionRail) Builder() *trajectory.TrajectoryBuilder {
	return r.builder
}

// SetTrajectorySink 绑定轨迹写入端点。
//
// Python: EvolutionRail.set_trajectory_sink(sink, *, team_id, member_role)
// Python 在 sink 非 nil 且 team_id 为空时抛出 ValueError。
func (r *EvolutionRail) SetTrajectorySink(sink trajectory.TrajectorySink, teamID string, memberRole ...string) error {
	if sink != nil && teamID == "" {
		return fmt.Errorf("team_id is required when binding a trajectory sink")
	}
	r.trajectorySink = sink
	r.teamID = teamID
	if len(memberRole) > 0 && memberRole[0] != "" {
		role := normalizeMemberRole(memberRole[0])
		if role != nil {
			r.memberRole = *role
		}
	}
	return nil
}

// EmitHostEvent 缓存一个主机事件。
// Python: EvolutionRail.emit_host_event(event)
func (r *EvolutionRail) EmitHostEvent(event *stream.OutputSchema) {
	if event != nil {
		r.pendingHostEvents = append(r.pendingHostEvents, event)
	}
}

// DrainPendingHostEvents 返回并清空主机事件缓冲。
//
// 如果 wait=true，先等待所有未完成的后台任务；timeout 控制最大等待时间。
// timeout <= 0 表示不限制等待时间；timeout > 0 时精确超时，超时后返回已收集的事件。
//
// Python: EvolutionRail.drain_pending_host_events(wait, timeout)
func (r *EvolutionRail) DrainPendingHostEvents(wait bool, timeout *time.Duration) []*stream.OutputSchema {
	if wait && len(r.bgTasks) > 0 {
		// 筛选未完成的任务
		pending := make([]*utils.BackgroundTask, 0)
		for task := range r.bgTasks {
			select {
			case <-task.Done():
				// 已完成，从集合中移除
				delete(r.bgTasks, task)
			default:
				pending = append(pending, task)
			}
		}

		if len(pending) > 0 {
			if timeout != nil && *timeout > 0 {
				// 精确超时等待
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				defer cancel()
				for _, task := range pending {
					select {
					case <-task.Done():
						// 任务在超时前完成
					case <-ctx.Done():
						// 超时，停止等待后续任务
						goto collectEvents
					}
				}
			} else {
				// 无超时限制，等待所有任务完成
				for _, task := range pending {
					_ = task.Wait()
				}
			}
			// 清理已完成任务
			for t := range r.bgTasks {
				select {
				case <-t.Done():
					delete(r.bgTasks, t)
				default:
				}
			}
		}
	}

collectEvents:
	drainedList := r.collectPendingHostEvents()

	logger.Debug(logComponent).
		Int("drained_count", len(drainedList)).
		Msg("DrainPendingHostEvents 完成")

	return drainedList
}

// DrainPendingApprovalEvents 兼容别名，返回并清空主机事件缓冲。
// Python: EvolutionRail.drain_pending_approval_events(wait, timeout)
func (r *EvolutionRail) DrainPendingApprovalEvents(wait bool, timeout *time.Duration) []*stream.OutputSchema {
	return r.DrainPendingHostEvents(wait, timeout)
}

// CleanupBackgroundTasks 取消并清空所有后台任务。
// Python: EvolutionRail.cleanup_background_tasks()
func (r *EvolutionRail) CleanupBackgroundTasks() error {
	for task := range r.bgTasks {
		select {
		case <-task.Done():
			// 任务已完成
		default:
			_ = task.Stop(5 * time.Second)
		}
		delete(r.bgTasks, task)
	}
	return nil
}

// GetCallbacks 合并 DeepAgent 基础事件 + Evolution 扩展事件的回调映射。
//
// 注册 5 个事件：BeforeInvoke / AfterModelCall / AfterToolCall / AfterInvoke / AfterTaskIteration。
// Python: EvolutionRail 注册 before_invoke / after_model_call / after_tool_call / after_invoke / after_task_iteration
func (r *EvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// BeforeInvoke 在每次 invoke 开始时初始化轨迹收集。
//
// Final 回调——子类不应覆写，通过 ext.OnBeforeInvoke 扩展。
//
// Python: EvolutionRail.before_invoke(ctx)
func (r *EvolutionRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil
	}

	sessionID := r.resolveSessionID(cbc, inputs)

	// Python: 复用同 session 的 builder
	if r.builder != nil && r.builder.SessionID() == sessionID {
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("复用已有轨迹构造器")
		return r.ext.OnBeforeInvoke(ctx, cbc)
	}

	// Python: 捕获 member_id 用于团队轨迹聚合
	memberID := ""
	if agent := cbc.Agent(); agent != nil {
		if card := agent.Card(); card != nil {
			memberID = card.GetID()
		}
	}

	// 构建 meta
	meta := map[string]any{}
	if r.memberRole != "" {
		meta["member_role"] = r.memberRole
	}

	opts := []trajectory.TrajectoryBuilderOption{
		trajectory.WithMemberID(memberID),
		trajectory.WithMeta(meta),
	}
	if r.maxTrajectorySteps > 0 {
		opts = append(opts, trajectory.WithMaxSteps(r.maxTrajectorySteps))
	}

	r.builder = trajectory.NewTrajectoryBuilder(sessionID, "online", opts...)

	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Str("member_id", memberID).
		Str("member_role", r.memberRole).
		Msg("创建新轨迹构造器")

	return r.ext.OnBeforeInvoke(ctx, cbc)
}

// AfterModelCall 在每次模型调用后记录 LLM 步骤并触发演化扩展点。
//
// Final 回调——子类不应覆写，通过 ext.OnAfterModelCall 扩展。
//
// Python: EvolutionRail.after_model_call(ctx)
func (r *EvolutionRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}

	// Python: 构建 LLMCallDetail
	var detail *trajectory.LLMCallDetail
	var promptTokenIDs []int
	var completionTokenIDs []int
	var logprobs []map[string]any

	if len(inputs.Messages) > 0 || inputs.Response != nil {
		// 获取模型名称
		modelName := "unknown"
		if cbc.Agent() != nil {
			if config := cbc.Agent().Config(); config != nil {
				if m := config.ModelName(); m != "" {
					modelName = m
				}
			}
		}

		// 分离 token 级字段
		var responseDict map[string]any
		if inputs.Response != nil {
			responseDict, promptTokenIDs, completionTokenIDs, logprobs = splitResponseTokenFields(inputs.Response)
		}

		// 直接存储原始 BaseMessage 对象，与 Python List[Any] 对齐
		messages := inputs.Messages

		// 构建 tools
		var tools []map[string]any
		if len(inputs.Tools) > 0 {
			tools = make([]map[string]any, 0, len(inputs.Tools))
			for _, tool := range inputs.Tools {
				tools = append(tools, toolInfoToMap(tool))
			}
		}

		detail = &trajectory.LLMCallDetail{
			Model:    modelName,
			Messages: messages,
			Response: ensureNonNilMap(responseDict),
			Tools:    tools,
		}
	}

	// 获取 agent_id
	agentIDStr := "unknown"
	if cbc.Agent() != nil {
		if card := cbc.Agent().Card(); card != nil {
			agentIDStr = card.GetID()
		}
	}

	step := &trajectory.TrajectoryStep{
		Kind:               trajectory.StepKindLLM,
		Detail:             detail,
		PromptTokenIDs:     promptTokenIDs,
		CompletionTokenIDs: completionTokenIDs,
		Logprobs:           logprobs,
		Meta: map[string]any{
			"operator_id": agentIDStr + "/llm_main",
			"agent_id":    agentIDStr,
		},
	}
	r.builder.RecordStep(step)

	// Python: 触发扩展点
	if err := r.ext.OnAfterModelCall(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_MODEL_CALL and _allow_evolution_trigger → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterModelCall &&
		r.ext.AllowEvolutionTrigger(TriggerAfterModelCall, cbc) {
		traj := r.buildTrajectory()
		if traj != nil {
			return r.triggerEvolution(traj, cbc)
		}
	}

	return nil
}

// AfterToolCall 在每次工具调用后记录 Tool 步骤并触发演化扩展点。
//
// Final 回调——子类不应覆写，通过 ext.OnAfterToolCall 扩展。
//
// Python: EvolutionRail.after_tool_call(ctx)
func (r *EvolutionRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		logger.Debug(logComponent).Msg("after_tool_call 跳过：轨迹构造器为空")
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	// Python: 构建 ToolCallDetail
	var detail *trajectory.ToolCallDetail
	if inputs.ToolName != "" {
		// 构建 callArgs（Go 中 ToolArgs 已是 map[string]any）
		callArgs := inputs.ToolArgs
		if callArgs == nil {
			callArgs = map[string]any{}
		}

		// 构建 callResult
		callResult := map[string]any{}
		if inputs.ToolResult != nil {
			if m, ok := inputs.ToolResult.(map[string]any); ok {
				callResult = m
			} else {
				callResult = map[string]any{"raw": inputs.ToolResult}
			}
		}

		// Python: tool_call_id=None → Go 中为空字符串
		toolCallID := ""

		detail = &trajectory.ToolCallDetail{
			ToolName:   inputs.ToolName,
			CallArgs:   callArgs,
			CallResult: callResult,
			ToolCallID: toolCallID,
		}
	}

	step := &trajectory.TrajectoryStep{
		Kind:   trajectory.StepKindTool,
		Detail: detail,
		Meta: map[string]any{
			"operator_id": inputs.ToolName,
		},
	}
	r.builder.RecordStep(step)

	// Python: 触发扩展点
	if err := r.ext.OnAfterToolCall(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_TOOL_CALL → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterToolCall &&
		r.ext.AllowEvolutionTrigger(TriggerAfterToolCall, cbc) {
		traj := r.buildTrajectory()
		if traj != nil {
			return r.triggerEvolution(traj, cbc)
		}
	}

	return nil
}

// AfterTaskIteration 在每次任务循环迭代后触发扩展点。
//
// Python: EvolutionRail.after_task_iteration(ctx)
func (r *EvolutionRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: await self._on_after_task_iteration(ctx)
	if err := r.ext.OnAfterTaskIteration(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_TASK_ITERATION → _trigger_evolution
	if r.evolutionTrigger == TriggerAfterTaskIteration &&
		r.ext.AllowEvolutionTrigger(TriggerAfterTaskIteration, cbc) {
		traj := r.buildTrajectory()
		if traj != nil {
			return r.triggerEvolution(traj, cbc)
		}
	}

	return nil
}

// AfterInvoke 在每次 invoke 结束后保存轨迹并触发演化。
//
// Final 回调——子类不应覆写，通过 ext.OnAfterInvoke 扩展。
//
// Python: EvolutionRail.after_invoke(ctx)
func (r *EvolutionRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}

	// Python: trajectory = self._build_trajectory()
	traj := r.buildTrajectory()
	if traj == nil {
		return nil
	}

	// Python: self._trajectory_store.save(trajectory)
	if err := r.saveTrajectory(ctx, traj); err != nil {
		return err
	}

	// Python: self._publish_trajectory_snapshot(trajectory)
	r.publishTrajectorySnapshot(traj)

	// Python: await self._on_after_invoke(ctx)
	if err := r.ext.OnAfterInvoke(ctx, cbc); err != nil {
		return err
	}

	// Python: if trigger == AFTER_INVOKE and _allow_evolution_trigger:
	//     await self._trigger_evolution(trajectory, ctx)
	//     await self._on_after_evolution_triggered(trajectory, ctx)
	if r.evolutionTrigger == TriggerAfterInvoke &&
		r.ext.AllowEvolutionTrigger(TriggerAfterInvoke, cbc) {
		if err := r.triggerEvolution(traj, cbc); err != nil {
			return err
		}
		if err := r.ext.OnAfterEvolutionTriggered(ctx, traj, cbc); err != nil {
			return err
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSessionID 解析运行时 session ID。
//
// Python: _resolve_trajectory_session_id(ctx, inputs)
// Python 实现：从 ctx.session 获取 session_id，回退到 inputs.conversation_id
func (r *EvolutionRail) resolveSessionID(cbc *agentinterfaces.AgentCallbackContext, inputs *agentinterfaces.InvokeInputs) string {
	sess := cbc.Session()
	if sess != nil {
		if sid := sess.GetSessionID(); sid != "" {
			return sid
		}
	}
	if inputs != nil && inputs.ConversationID != "" {
		return inputs.ConversationID
	}
	return ""
}

// buildTrajectory 从 builder 构建轨迹并 snapshot steps。
//
// Python: _build_trajectory()
// Python 实现：trajectory = self._builder.build(); trajectory.steps = list(trajectory.steps)
func (r *EvolutionRail) buildTrajectory() *trajectory.Trajectory {
	if r.builder == nil {
		return nil
	}
	traj := r.builder.Build()
	if traj == nil {
		return nil
	}
	// Python: snapshot steps 避免共享引用变异
	steps := make([]*trajectory.TrajectoryStep, len(traj.Steps))
	copy(steps, traj.Steps)
	traj.Steps = steps
	return traj
}

// saveTrajectory 保存轨迹到存储。
// Python: EvolutionRail._save_trajectory(traj)
func (r *EvolutionRail) saveTrajectory(_ context.Context, traj *trajectory.Trajectory) error {
	if r.trajectoryStore == nil {
		return nil
	}
	r.trajectoryStore.Save(traj, "")
	return nil
}

// publishTrajectorySnapshot 通过 Sink 发布成员轨迹快照。
//
// Python: _publish_trajectory_snapshot(trajectory)
// Python 实现：if sink and team_id → sink.publish_member_trajectory(snapshot)
func (r *EvolutionRail) publishTrajectorySnapshot(traj *trajectory.Trajectory) {
	if r.trajectorySink == nil || r.teamID == "" {
		return
	}
	// 从 trajectory meta 中获取 member_id
	memberID := ""
	if traj.Meta != nil {
		if id, ok := traj.Meta["member_id"].(string); ok {
			memberID = id
		}
	}
	if memberID == "" {
		return
	}

	// Python: member_role = _normalize_member_role(trajectory.meta.get("member_role")) or self._member_role
	memberRole := ""
	if traj.Meta != nil {
		if role, ok := traj.Meta["member_role"].(string); ok && role != "" {
			memberRole = role
		}
	}
	if memberRole == "" {
		memberRole = r.memberRole
	}

	snapshot := &trajectory.MemberTrajectorySnapshot{
		TeamID:     r.teamID,
		SessionID:  traj.SessionID,
		MemberID:   memberID,
		MemberRole: memberRole,
		Trajectory: traj,
	}
	r.trajectorySink.PublishMemberTrajectory(snapshot)
}

// triggerEvolution 根据配置异步或同步触发演化。
//
// Python: _trigger_evolution(trajectory, ctx)
// Python 实现：
//   - async=True: snapshot → create_background_task → _safe_run_evolution
//   - async=False: 直接调用 run_evolution(trajectory, ctx)
func (r *EvolutionRail) triggerEvolution(traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.asyncEvolution {
		// Python: Phase 1 — 同步捕获快照
		snapshot := r.ext.SnapshotForEvolution(context.Background(), traj, cbc)
		if snapshot == nil {
			return nil
		}

		// Python: Phase 2 — 启动后台任务
		skillName := formatSkillName(snapshot)
		bgTask, err := utils.CreateBackgroundTask(
			context.Background(),
			func(bgCtx context.Context) error {
				return r.safeRunEvolution(bgCtx, snapshot)
			},
			"evolution-"+skillName, "evolution",
		)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("创建演化后台任务失败")
			return err
		}
		r.bgTasks[bgTask] = true

		// Python: 清理已完成任务
		for t := range r.bgTasks {
			select {
			case <-t.Done():
				delete(r.bgTasks, t)
			default:
				// 任务仍在运行
			}
		}
	} else {
		// Python: 同步模式 — 直接调用 run_evolution(trajectory, ctx)
		return r.ext.RunEvolution(context.Background(), traj, nil)
	}
	return nil
}

// safeRunEvolution 后台安全执行演化（信号量 + 异常捕获）。
//
// Python: _safe_run_evolution(snapshot)
// Python 实现：
//   - 获取 evolution_sem 信号量
//   - 调用 run_evolution(trajectory, ctx=None, snapshot=snapshot)
//   - 捕获异常 → _emit_background_outcome_event
func (r *EvolutionRail) safeRunEvolution(ctx context.Context, snapshot *EvolutionSnapshot) error {
	// Python: total_timeout = self._get_evolution_total_timeout_secs()
	timeoutSecs := r.ext.GetEvolutionTotalTimeoutSecs()
	if timeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs*float64(time.Second)))
		defer cancel()
	}

	// Python: async with self._evolution_sem
	r.evolutionSem <- struct{}{}
	defer func() { <-r.evolutionSem }()

	traj := snapshot.Trajectory
	err := r.ext.RunEvolution(ctx, traj, snapshot)
	if err != nil {
		// Python: except TimeoutError → outcome = {"status": "timed_out", "message": f"background evolution timed out after {timeout}s"}
		if ctx.Err() == context.DeadlineExceeded {
			timeoutText := formatTimeoutSecs(timeoutSecs)
			outcome := map[string]string{
				"status":  "timed_out",
				"message": "background evolution timed out after " + timeoutText + "s",
			}
			logger.Warn(logComponent).Str("timeout_secs", timeoutText).Msg("后台演化执行超时")
			r.emitBackgroundOutcomeEvent(outcome)
			return err
		}
		// Python: except Exception as exc → outcome = {"status": "failed", "message": str(exc)}
		outcome := map[string]string{
			"status":  "failed",
			"message": err.Error(),
		}
		logger.Warn(logComponent).Err(err).Msg("后台演化执行失败")
		r.emitBackgroundOutcomeEvent(outcome)
	}
	return err
}

// emitBackgroundOutcomeEvent 将后台执行结果写入主机事件缓冲。
//
// Python: _emit_background_outcome_event(outcome)
// Python 实现：构建 EvolutionHostEventMeta + OutputSchema → emit_host_event
func (r *EvolutionRail) emitBackgroundOutcomeEvent(outcome map[string]string) {
	status := outcome["status"]
	if status == "" {
		status = "unknown"
	}
	meta := EvolutionHostEventMeta{
		EventKind:  EvolutionEventKindOutcome,
		RailKind:   stringPtr(outcome["rail_kind"]),
		Stage:      stringPtr(outcome["stage"]),
		SkillName:  stringPtr(outcome["skill_name"]),
		RequestID:  stringPtr(outcome["request_id"]),
		SignalType: stringPtr(outcome["signal_type"]),
		Source:     stringPtr(outcome["source"]),
		Status:     &status,
	}

	// Python: content = f"[Evolution] {outcome['message']}\n"
	message := outcome["message"]
	if message == "" {
		message = "background evolution completed with unknown outcome"
	}

	r.EmitHostEvent(&stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content":         "[Evolution] " + message + "\n",
			"_evolution_meta": meta.ToPayload(),
		},
	})
}

// collectPendingHostEvents 返回并清空主机事件缓冲。
//
// Python: _collect_pending_host_events()
func (r *EvolutionRail) collectPendingHostEvents() []*stream.OutputSchema {
	events := r.pendingHostEvents
	r.pendingHostEvents = make([]*stream.OutputSchema, 0)
	return events
}

// resetTrajectoryBuilder 重置当前轨迹构造器。
//
// Python: _reset_trajectory_builder()
// Python 注释：子类在自身生命周期边界使用此方法，例如上传 RL episode 后。
func (r *EvolutionRail) resetTrajectoryBuilder() {
	r.builder = nil
}

// stringPtr 返回字符串指针，空字符串返回 nil。
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isSkillDisabled 检查技能是否被禁用。
// Python: skill_name in self._disabled_skills
func (r *EvolutionRail) isSkillDisabled(skillName string) bool {
	return r.disabledSkills[skillName]
}

// getAgentIDStr 从回调上下文获取 agent ID 字符串。
func getAgentIDStr(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil || cbc.Agent() == nil {
		return "unknown"
	}
	if card := cbc.Agent().Card(); card != nil {
		return card.GetID()
	}
	return "unknown"
}

// formatTimeoutSecs 将超时秒数格式化为字符串，去除尾部无意义的零。
// Python: f"{total_timeout:.2f}".rstrip("0").rstrip(".")
func formatTimeoutSecs(secs float64) string {
	s := fmt.Sprintf("%.2f", secs)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}


package evolution

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability/demand"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/trajectory"
)

// rail_scope.go 演化 rail 的订阅与 scope 窗口机械。
// 对齐 Python: harness/rails/evolution/evolution_rail.py L312-613（_InvokeCapture/
// _scope_windows/_scope_locks/get_trajectory/_project_window/_with_scope_metadata，
// 2026-12-04 快照）。
//
// 偏差 D-25.2b-11：Go 侧 span 源 = rail collector 增量（AfterModelCall/
// AfterToolCall 经 builder.RecordSpan 累积），而非全局 otel provider 的
// processor drain——Go harness 的 LLM/工具 span 尚未全量经 otel 落地。
// 窗口合并/裁剪/投影与上游同构（MergeTrajectories+TrimTrajectoryWindow）。
// 回收条件：观测链 GenAI span 全量经 otel 落地后切换为 processor drain。

// ──────────────────────────── 结构体 ────────────────────────────

// invokeCapture 一次 invoke 的采集登记。
// Python: evolution_rail.py _InvokeCapture。
type invokeCapture struct {
	subscription *trajectory.SubscriptionHandle
	scopeKey     string
	sessionID    string
	memberID     string
	teamID       string
}

// scopeWindowState 一个 scope 的窗口与其游标。
// Python: _scope_windows[key] = Trajectory；cursor 为 Go 侧 builder 增量游标
// （D-25.2b-11 附属状态，Python 无对应）。
type scopeWindowState struct {
	trajectory *trajectory.Trajectory
	// consumed 已并入窗口的 spanId 集（增量游标；builder 列表会重排，
	// 索引游标不可用——D-25.2b-11 附属状态）。
	consumed map[string]struct{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// scopeKeyOf 窗口键（team/agent 互斥）。
// Python: evolution_rail.py _scope_key。
func scopeKeyOf(scope, sessionID, memberID string) string {
	return scope + "\x00" + sessionID + "\x00" + memberID
}

// trajectoryMetadata 构造规范资源元数据信封。
// Python: evolution_rail.py _trajectory_metadata。
func trajectoryMetadata(sessionID, memberID, teamID, memberRole string) map[string]any {
	metadata := map[string]any{
		trajectory.TrajectoryID:                    uuid.NewString(),
		trajectory.TrajectoryProjectionVersionAttr: trajectory.TrajectoryProjectionVersion,
		trajectory.TrajectorySource:                "online",
		trajectory.SessionID:                       sessionID,
	}
	if memberID != "" {
		metadata[trajectory.MemberID] = memberID
	}
	if teamID != "" {
		metadata[trajectory.TeamID] = teamID
	}
	if memberRole != "" {
		metadata["agentteam.agent.role"] = memberRole
	}
	return metadata
}

// withScopeMetadata 用规范信封替换生产方资源属性。
// Python: evolution_rail.py _with_scope_metadata。
func withScopeMetadata(traj *trajectory.Trajectory, metadata map[string]any) (*trajectory.Trajectory, error) {
	payload := traj.ToOTLP()
	resourceSpans, _ := payload["resourceSpans"].([]any)
	for _, rsItem := range resourceSpans {
		resourceSpan, isMap := rsItem.(map[string]any)
		if !isMap {
			continue
		}
		resource, _ := resourceSpan["resource"].(map[string]any)
		if resource == nil {
			resource = map[string]any{}
		}
		resource["attributes"] = trajectory.AttributesFromMap(metadata)
		resourceSpan["resource"] = resource
	}
	return trajectory.FromOTLP(payload)
}

// ──────────────────────────── EvolutionRail 窗口方法 ────────────────────────────

// beginScopeCapture 登记 scope 采集（订阅 + 游标初始化）。
// Python: evolution_rail.py before_invoke 的订阅段（L355-371）。
func (r *EvolutionRail) beginScopeCapture(sessionID, memberID, teamID string) *invokeCapture {
	capture := &invokeCapture{
		scopeKey:  scopeKeyOf("agent", sessionID, memberID),
		sessionID: sessionID,
		memberID:  memberID,
		teamID:    teamID,
	}
	if processor := r.scopeProcessor(); processor != nil {
		// 订阅当前 rail 关注的类别；trace 未绑定时按活跃订阅语义接收
		//（processor.go OnEnd 路由，D-25.2b-4）。
		if handle, err := processor.Subscribe([]string{"llm", "tool", "event"}, ""); err == nil {
			capture.subscription = handle
		}
	}
	r.scopeMu.Lock()
	defer r.scopeMu.Unlock()
	if r.scopeWindows == nil {
		r.scopeWindows = map[string]*scopeWindowState{}
	}
	if _, exists := r.scopeWindows[capture.scopeKey]; !exists {
		r.scopeWindows[capture.scopeKey] = &scopeWindowState{}
	}
	if r.activeCaptures == nil {
		r.activeCaptures = map[*invokeCapture]struct{}{}
	}
	r.activeCaptures[capture] = struct{}{}
	r.currentCapture = capture
	return capture
}

// endScopeCapture 收口：释放订阅，窗口保留（上游同语义）。
// Python: evolution_rail.py after_invoke finally 段 + uninit。
func (r *EvolutionRail) endScopeCapture(capture *invokeCapture) {
	if capture == nil {
		return
	}
	if processor := r.scopeProcessor(); processor != nil && capture.subscription != nil {
		processor.Unsubscribe(capture.subscription)
	}
	r.scopeMu.Lock()
	defer r.scopeMu.Unlock()
	delete(r.activeCaptures, capture)
	if r.currentCapture == capture {
		r.currentCapture = nil
	}
}

// scopeProcessor 返回进程级 processor（demand 懒构造；evolution→demand
// 无环，直接引用。Python: rail 持有注入的 processor 实例）。
func (r *EvolutionRail) scopeProcessor() ScopeProcessorLike {
	if p := demand.GetTrajectorySpanProcessor(); p != nil {
		if tp, ok := p.(*trajectory.TrajectorySpanProcessor); ok {
			return tp
		}
	}
	return nil
}

// mergeScopeIncrement 把 builder 增量合并进 scope 窗口并裁剪。
// Python: evolution_rail.py _merge_clean_increment（L732-740）。
func (r *EvolutionRail) mergeScopeIncrement(capture *invokeCapture) (*trajectory.Trajectory, error) {
	if capture == nil || r.builder == nil {
		return nil, nil
	}
	spans := r.builder.Spans()
	r.scopeMu.Lock()
	defer r.scopeMu.Unlock()
	state := r.scopeWindows[capture.scopeKey]
	if state == nil {
		state = &scopeWindowState{}
		r.scopeWindows[capture.scopeKey] = state
	}
	// 12-04：builder 列表按 span_sort_key 排序（Python builder.record_span 同款），
	// 索引游标失效——改用已消费 spanId 集合做增量游标。
	additions := make([]map[string]any, 0, len(spans))
	for _, span := range spans {
		spanID := fmt.Sprint(span["spanId"])
		if _, consumed := state.consumed[spanID]; consumed {
			continue
		}
		if state.consumed == nil {
			state.consumed = map[string]struct{}{}
		}
		state.consumed[spanID] = struct{}{}
		additions = append(additions, span)
	}
	merged, err := r.mergeWindowLocked(state.trajectory, additions)
	if err != nil {
		return nil, err
	}
	state.trajectory = merged
	return state.trajectory, nil
}

// mergeWindowLocked 窗口合并 + 裁剪（持锁调用）。
// Python: _merge_clean_increment 的 merge_trajectories + trim_trajectory_window。
func (r *EvolutionRail) mergeWindowLocked(current *trajectory.Trajectory, additions []map[string]any) (*trajectory.Trajectory, error) {
	var merged *trajectory.Trajectory
	var err error
	if current != nil {
		merged, err = trajectory.MergeTrajectories(current, additionsToPayload(additions))
	} else {
		merged, err = trajectory.MergeTrajectories(additionsToPayload(additions))
	}
	if err != nil {
		return nil, err
	}
	trimmed, err := trajectory.TrimTrajectoryWindow(merged, r.maxTrajectorySteps)
	if err != nil {
		return nil, err
	}
	return trimmed, nil
}

// additionsToPayload 把 span 列表包成 OTLP 载荷（合并输入）。
func additionsToPayload(spans []map[string]any) map[string]any {
	spanList := make([]any, 0, len(spans))
	for _, span := range spans {
		spanList = append(spanList, span)
	}
	// 增量载荷注入 trajectory_id 资源属性（NewTrajectory 校验要求；
	// 合并后窗口的规范信封由 projectScopeWindow/GetTrajectory 重写）。
	return map[string]any{"resourceSpans": []any{
		map[string]any{"resource": map[string]any{"attributes": []any{
			map[string]any{"key": trajectory.TrajectoryID, "value": map[string]any{"stringValue": uuid.NewString()}},
		}}, "scopeSpans": []any{
			map[string]any{"scope": map[string]any{}, "spans": spanList},
		}},
	}}
}

// projectScopeWindow 返回带规范元数据的窗口脱离快照。
// Python: evolution_rail.py _project_window。
func (r *EvolutionRail) projectScopeWindow(capture *invokeCapture) (*trajectory.Trajectory, error) {
	if capture == nil {
		return nil, nil
	}
	r.scopeMu.Lock()
	state := r.scopeWindows[capture.scopeKey]
	var payload map[string]any
	if state != nil && state.trajectory != nil {
		payload = state.trajectory.ToOTLP()
	}
	r.scopeMu.Unlock()
	if payload == nil {
		return nil, nil
	}
	projected, err := trajectory.FromOTLP(payload)
	if err != nil {
		return nil, err
	}
	return withScopeMetadata(projected, trajectoryMetadata(
		capture.sessionID, capture.memberID, capture.teamID, r.memberRole))
}

// GetTrajectory 返回 Agent/Team scope 的当前干净窗口。
// Python: evolution_rail.py get_trajectory（L589-613）。
// team_id 与 member_id 互斥（同报 ValueError 语义）。
func (r *EvolutionRail) GetTrajectory(sessionID, memberID, teamID string) (*trajectory.Trajectory, error) {
	if teamID != "" && memberID != "" {
		return nil, fmt.Errorf("member_id must be omitted for a Team trajectory scope")
	}
	var key string
	if teamID != "" {
		key = scopeKeyOf("team", sessionID, "")
	} else {
		key = scopeKeyOf("agent", sessionID, memberID)
	}
	r.scopeMu.Lock()
	state := r.scopeWindows[key]
	var payload map[string]any
	if state != nil && state.trajectory != nil {
		payload = state.trajectory.ToOTLP()
	}
	r.scopeMu.Unlock()
	if payload == nil {
		return nil, nil
	}
	projected, err := trajectory.FromOTLP(payload)
	if err != nil {
		return nil, err
	}
	member := memberID
	if teamID != "" {
		member = ""
	}
	return withScopeMetadata(projected, trajectoryMetadata(sessionID, member, teamID, r.memberRole))
}

// clearScopeWindows 丢弃全部窗口（rail uninit 语义）。
// Python: evolution_rail.py uninit 的窗口清理段。
func (r *EvolutionRail) clearScopeWindows() {
	r.scopeMu.Lock()
	defer r.scopeMu.Unlock()
	r.scopeWindows = map[string]*scopeWindowState{}
}

// ScopeProcessorLike rail 需要的进程级 processor 面（订阅/退订）。
type ScopeProcessorLike interface {
	Subscribe(includeCategories []string, traceID string) (*trajectory.SubscriptionHandle, error)
	Unsubscribe(handle *trajectory.SubscriptionHandle)
}

// scopeProcessorLike 内部别名。
type scopeProcessorLike = ScopeProcessorLike

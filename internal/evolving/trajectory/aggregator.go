package trajectory

import (
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamTrajectory 聚合的团队轨迹，属于单个会话。
//
// 对应 Python: TeamTrajectory dataclass
type TeamTrajectory struct {
	// TeamID 团队标识
	TeamID string
	// SessionID 会话标识
	SessionID string
	// Combined 合并后的轨迹，所有步骤按 start_time_ms 排序
	Combined *Trajectory
	// Members 成员 ID → 各自轨迹
	Members map[string]*Trajectory
}

// TeamTrajectoryAggregator 从 TrajectoryStore 聚合成员轨迹。
//
// 对应 Python: TeamTrajectoryAggregator
type TeamTrajectoryAggregator struct {
	// store 轨迹存储
	store TrajectoryStore
	// teamID 团队标识
	teamID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// leaderRole Leader 角色标识
	leaderRole = "leader"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// CollaborativeTools 协作工具名称集合。
	// 反映成员间交互行为的工具名称。
	//
	// 注意：spawn_member 是 Leader 专用，不包含在 Teammate 上下文中。
	//
	// 对应 Python: COLLABORATIVE_TOOLS
	CollaborativeTools = map[string]bool{
		"view_task":      true,
		"claim_task":     true,
		"send_message":   true,
		"workspace_meta": true,
	}

	// memberRoleMetaKeys 成员角色元数据键。
	//
	// 对应 Python: _MEMBER_ROLE_META_KEYS
	memberRoleMetaKeys = []string{"member_role", "role"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTrajectoryAggregator 创建团队轨迹聚合器。
//
// 对应 Python: TeamTrajectoryAggregator(store=..., team_id=...)
func NewTeamTrajectoryAggregator(store TrajectoryStore, teamID string) *TeamTrajectoryAggregator {
	return &TeamTrajectoryAggregator{
		store:  store,
		teamID: teamID,
	}
}

// Aggregate 聚合指定会话的所有成员轨迹。
//
// 对应 Python: TeamTrajectoryAggregator.aggregate()
func (a *TeamTrajectoryAggregator) Aggregate(sessionID string, filterCollaborative bool) *TeamTrajectory {
	trajectories := a.store.Query("", map[string]any{"session_id": sessionID})
	if len(trajectories) == 0 {
		return a.emptyCombined(sessionID)
	}

	members := memberTrajectoriesByID(trajectories, filterCollaborative)
	if len(members) == 0 {
		return a.emptyCombined(sessionID)
	}

	combined := buildCombinedTrajectory(members, a.teamID, sessionID)
	return &TeamTrajectory{
		TeamID:    a.teamID,
		SessionID: sessionID,
		Members:   members,
		Combined:  combined,
	}
}

// AggregateMemberTrajectories 聚合已加载到内存的成员轨迹。
//
// 对应 Python: aggregate_member_trajectories()
func AggregateMemberTrajectories(trajectories []*Trajectory, teamID, sessionID string, filterCollaborative bool) *Trajectory {
	members := memberTrajectoriesByID(trajectories, filterCollaborative)
	return buildCombinedTrajectory(members, teamID, sessionID)
}

// FilterMemberTrajectory 过滤成员轨迹，仅保留协作相关步骤。
//
// 保留反映成员间行为的步骤：
//   - 包含跨成员元数据键的步骤（invoke_id, parent_invoke_id, child_invokes）
//   - 使用协作工具名称的工具调用（view_task, claim_task 等）
//   - 读写团队技能文件的步骤
//   - 跳过纯内部 LLM 推理和未白名单的工具调用
//
// 对应 Python: filter_member_trajectory()
func FilterMemberTrajectory(trajectory *Trajectory) *Trajectory {
	filteredSteps := make([]*TrajectoryStep, 0, len(trajectory.Steps))
	for _, step := range trajectory.Steps {
		if isCollaborativeStep(step) {
			filteredSteps = append(filteredSteps, step)
		}
	}

	return &Trajectory{
		ExecutionID: trajectory.ExecutionID,
		SessionID:   trajectory.SessionID,
		Source:      trajectory.Source,
		Steps:       filteredSteps,
		Cost:        trajectory.Cost,
		Meta:        trajectory.Meta,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emptyCombined 返回空的合并轨迹。
//
// 对应 Python: TeamTrajectoryAggregator._empty_combined()
func (a *TeamTrajectoryAggregator) emptyCombined(sessionID string) *TeamTrajectory {
	combined := &Trajectory{
		ExecutionID: fmt.Sprintf("team-%s", a.teamID),
		SessionID:   sessionID,
		Source:      "online",
		Steps:       []*TrajectoryStep{},
		Meta:        map[string]any{"member_count": 0},
	}
	return &TeamTrajectory{
		TeamID:    a.teamID,
		SessionID: sessionID,
		Combined:  combined,
	}
}

// memberTrajectoriesByID 按成员 ID 分组轨迹。
//
// 对齐 Python:
//
//	members = {}
//	for trajectory in trajectories:
//	    member_id = str(trajectory.meta.get("member_id", trajectory.execution_id[:8]))
//	    processed = trajectory
//	    if filter_collaborative and not _is_leader_trajectory(trajectory, member_id):
//	        processed = filter_member_trajectory(trajectory)
//	    if processed.steps:
//	        members[member_id] = _merge_member_trajectory(members.get(member_id), processed)
//
// 对应 Python: _member_trajectories_by_id()
func memberTrajectoriesByID(trajectories []*Trajectory, filterCollaborative bool) map[string]*Trajectory {
	members := make(map[string]*Trajectory)
	for _, trajectory := range trajectories {
		memberID := ""
		if v, ok := trajectory.Meta["member_id"]; ok {
			memberID = fmt.Sprint(v)
		}
		if memberID == "" && len(trajectory.ExecutionID) >= 8 {
			memberID = trajectory.ExecutionID[:8]
		}
		processed := trajectory
		if filterCollaborative && !isLeaderTrajectory(trajectory, memberID) {
			processed = FilterMemberTrajectory(trajectory)
		}
		if len(processed.Steps) > 0 {
			if existing, ok := members[memberID]; ok {
				members[memberID] = mergeMemberTrajectory(existing, processed)
			} else {
				members[memberID] = processed
			}
		}
	}
	return members
}

// buildCombinedTrajectory 构建合并后的轨迹。
//
// 对齐 Python:
//
//	all_steps = []
//	for trajectory in members.values():
//	    all_steps.extend(trajectory.steps)
//	all_steps.sort(key=lambda step: step.start_time_ms or 0)
//
// 对应 Python: _build_combined_trajectory()
func buildCombinedTrajectory(members map[string]*Trajectory, teamID, sessionID string) *Trajectory {
	allSteps := make([]*TrajectoryStep, 0)
	for _, trajectory := range members {
		allSteps = append(allSteps, trajectory.Steps...)
	}
	// 按 start_time_ms 排序
	sort.Slice(allSteps, func(i, j int) bool {
		iv := allSteps[i].StartTimeMs
		jv := allSteps[j].StartTimeMs
		if iv == 0 {
			iv = 0
		}
		if jv == 0 {
			jv = 0
		}
		return iv < jv
	})

	// 计算总成本
	totalInput := 0
	totalOutput := 0
	for _, trajectory := range members {
		if trajectory.Cost != nil {
			totalInput += trajectory.Cost["input_tokens"]
			totalOutput += trajectory.Cost["output_tokens"]
		}
	}

	var cost CostInfo
	if totalInput > 0 || totalOutput > 0 {
		cost = CostInfo{"input_tokens": totalInput, "output_tokens": totalOutput}
	}

	return &Trajectory{
		ExecutionID: fmt.Sprintf("team-%s", teamID),
		SessionID:   sessionID,
		Source:      "online",
		Steps:       allSteps,
		Cost:        cost,
		Meta:        map[string]any{"member_count": len(members)},
	}
}

// isLeaderTrajectory 判断轨迹是否属于 Leader 成员。
//
// 对齐 Python:
//
//	for key in _MEMBER_ROLE_META_KEYS:
//	    role = trajectory.meta.get(key)
//	    if role is None:
//	        continue
//	    role_value = getattr(role, "value", role)
//	    return str(role_value).lower() == _LEADER_ROLE
//	return member_id == _LEADER_ROLE
//
// 对应 Python: _is_leader_trajectory()
func isLeaderTrajectory(trajectory *Trajectory, memberID string) bool {
	for _, key := range memberRoleMetaKeys {
		if role, ok := trajectory.Meta[key]; ok {
			roleStr := fmt.Sprint(role)
			return strings.ToLower(roleStr) == leaderRole
		}
	}
	return memberID == leaderRole
}

// mergeMemberTrajectory 合并同一成员的多个持久化轨迹快照。
//
// 对齐 Python:
//
//	if existing is None:
//	    return new
//	if len(new.steps) > len(existing.steps) and _steps_are_prefix(existing.steps, new.steps):
//	    return new
//	if len(existing.steps) > len(new.steps) and _steps_are_prefix(new.steps, existing.steps):
//	    return existing
//	return Trajectory(
//	    execution_id=existing.execution_id,
//	    session_id=existing.session_id or new.session_id,
//	    source=existing.source,
//	    case_id=existing.case_id or new.case_id,
//	    steps=[*existing.steps, *new.steps],
//	    cost=_merge_cost(existing.cost, new.cost),
//	    meta={**existing.meta, **new.meta},
//	)
//
// 对应 Python: _merge_member_trajectory()
func mergeMemberTrajectory(existing, new *Trajectory) *Trajectory {
	if existing == nil {
		return new
	}
	if len(new.Steps) > len(existing.Steps) && stepsArePrefix(existing.Steps, new.Steps) {
		return new
	}
	if len(existing.Steps) > len(new.Steps) && stepsArePrefix(new.Steps, existing.Steps) {
		return existing
	}

	mergedSteps := make([]*TrajectoryStep, 0, len(existing.Steps)+len(new.Steps))
	mergedSteps = append(mergedSteps, existing.Steps...)
	mergedSteps = append(mergedSteps, new.Steps...)

	sessionID := existing.SessionID
	if sessionID == "" {
		sessionID = new.SessionID
	}
	caseID := existing.CaseID
	if caseID == "" {
		caseID = new.CaseID
	}
	mergedMeta := make(map[string]any)
	for k, v := range existing.Meta {
		mergedMeta[k] = v
	}
	for k, v := range new.Meta {
		mergedMeta[k] = v
	}

	return &Trajectory{
		ExecutionID: existing.ExecutionID,
		SessionID:   sessionID,
		Source:      existing.Source,
		CaseID:      caseID,
		Steps:       mergedSteps,
		Cost:        mergeCost(existing.Cost, new.Cost),
		Meta:        mergedMeta,
	}
}

// stepsArePrefix 判断 prefix 是否是 steps 的前缀。
//
// 对应 Python: _steps_are_prefix()
func stepsArePrefix(prefix, steps []*TrajectoryStep) bool {
	if len(prefix) > len(steps) {
		return false
	}
	for i := range prefix {
		if prefix[i] != steps[i] {
			return false
		}
	}
	return true
}

// mergeCost 合并两个 token 成本字典。
//
// 对应 Python: _merge_cost()
func mergeCost(first, second CostInfo) CostInfo {
	if first == nil && second == nil {
		return nil
	}
	merged := make(CostInfo)
	for _, cost := range []CostInfo{first, second} {
		if cost == nil {
			continue
		}
		for key, value := range cost {
			merged[key] += value
		}
	}
	return merged
}

// isCollaborativeStep 判断步骤是否反映成员间协作。
//
// 对齐 Python:
//
//	if step.meta and any(key in step.meta for key in CROSS_MEMBER_META_KEYS):
//	    return True
//	if step.kind != "tool" or not step.detail:
//	    return False
//	tool_name = getattr(step.detail, "tool_name", "").lower()
//	return tool_name in COLLABORATIVE_TOOLS or _is_team_skill_file_access(step, tool_name)
//
// 对应 Python: _is_collaborative_step()
func isCollaborativeStep(step *TrajectoryStep) bool {
	// 检查跨成员元数据键
	if step.Meta != nil {
		for key := range CrossMemberMetaKeys {
			if _, ok := step.Meta[key]; ok {
				return true
			}
		}
	}
	// 非工具步骤或无详情则不是协作步骤
	if step.Kind != StepKindTool || step.Detail == nil {
		return false
	}
	toolDetail, ok := step.Detail.(*ToolCallDetail)
	if !ok {
		return false
	}
	toolName := strings.ToLower(toolDetail.ToolName)
	if CollaborativeTools[toolName] {
		return true
	}
	return isTeamSkillFileAccess(step, toolName)
}

// isTeamSkillFileAccess 判断是否为团队技能文件访问。
//
// 对齐 Python:
//
//	if "read" not in tool_name and "write" not in tool_name:
//	    return False
//	args = str(getattr(step.detail, "call_args", "")).lower()
//	return "skill" in args
//
// 对应 Python: _is_team_skill_file_access()
func isTeamSkillFileAccess(step *TrajectoryStep, toolName string) bool {
	if !strings.Contains(toolName, "read") && !strings.Contains(toolName, "write") {
		return false
	}
	toolDetail, ok := step.Detail.(*ToolCallDetail)
	if !ok {
		return false
	}
	args := strings.ToLower(fmt.Sprint(toolDetail.CallArgs))
	return strings.Contains(args, "skill")
}

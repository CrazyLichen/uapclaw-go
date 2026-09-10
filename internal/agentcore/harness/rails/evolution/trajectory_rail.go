package evolution

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryRail 纯轨迹收集轨道，不触发任何演化逻辑。
//
// 嵌入 EvolutionRail，使用 noOpExtension 作为扩展点实现。
// RunEvolution 为空操作，EvolutionTriggerPoint 默认 AFTER_INVOKE
// 但 RunEvolution 不做任何事。
//
// 使用场景：
//   - 可观测性和调试：记录完整的 Agent 行为轨迹
//   - 离线数据收集：积累数据用于后续离线训练
//   - 行为分析：将轨迹写入存储供外部系统消费
//
// Python: TrajectoryRail(priority=10)
type TrajectoryRail struct {
	*EvolutionRail
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryRail 创建纯轨迹收集轨道。
//
// Python: TrajectoryRail(trajectory_store=None)
func NewTrajectoryRail(opts ...EvolutionRailOption) *TrajectoryRail {
	rail := NewEvolutionRail(noOpExtension{}, opts...)
	return &TrajectoryRail{EvolutionRail: rail}
}

// Priority 返回优先级 10。
// Python: TrajectoryRail.priority = 10
func (r *TrajectoryRail) Priority() int { return 10 }

// ──────────────────────────── 非导出函数 ────────────────────────────

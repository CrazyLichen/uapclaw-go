package updater

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Updater 自演化更新器接口，统一单维优化器和多维归因分配为一个接口。
//
// Trainer 不关心实现细节，只通过此接口获取更新映射：
//
//	(trajectories, evaluated_cases) → 更新映射或候选集
//
// 对应 Python: openjiuwen/agent_evolving/updater/protocol.py Updater(Protocol)
type Updater interface {
	// Bind 绑定 Operator 注册表并过滤可优化的 Operator。
	// 返回匹配数量；0 触发 Trainer 软退出。
	Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int

	// RequiresForwardData 判断此 Updater 是否需要框架执行前向推理。
	// 返回 false 的黑盒优化器（如 tool_optimizer）在内部生成/执行/评估，
	// 不依赖框架的前向推理数据。
	RequiresForwardData() bool

	// Update 离线兼容入口，将 evaluated_cases 转换为 signals 后调用 Process。
	Update(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error)

	// Process 信号优先入口，直接消费 EvolutionSignal 列表。
	Process(ctx context.Context, trajectories []*trajectory.Trajectory, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error)

	// GetState 获取 Updater 可序列化状态（用于检查点保存）。
	GetState() map[string]any

	// LoadState 从检查点恢复 Updater 状态。
	LoadState(state map[string]any)
}

package stages

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StageResult 阶段执行结果，由 Pipeline 读取以驱动状态跳转。
type StageResult struct {
	// NextStage 下一个跳转阶段
	NextStage skilldev.SkillDevStage
}

// ──────────────────────────── 接口 ────────────────────────────

// StageHandler SkillDev Pipeline 阶段处理器接口。
//
// 每个阶段独立实现，通过 Execute() 与 Pipeline 交互。
// 处理器不应持有跨请求的状态——所有状态均通过 SkillDevContext 传入。
type StageHandler interface {
	// Execute 执行阶段逻辑。
	//
	// 参数：
	//   - ctx:  上下文（控制生命周期）
	//   - sctx: SkillDevContext，包含 state、workspace、emit、create_stage_agent 等
	//
	// 返回值：
	//   - StageResult：Pipeline 据此跳转到下一阶段
	//   - error：执行错误
	Execute(ctx context.Context, sctx *skilldev.SkillDevContext) (*StageResult, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识。
var logComponent = logger.ComponentAgentServer

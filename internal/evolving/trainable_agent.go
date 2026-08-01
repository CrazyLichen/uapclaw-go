package evolving

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枌举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TrainableAgent 可训练 Agent 接口。
//
// Trainer 需要通过 Agent 获取 Operator 注册表和执行推理，
// 这是 BaseAgent 的最小扩展接口。
// 此接口从 trainer 包迁移至 evolving 根包，
// 解决 checkpointing ↔ trainer 循环依赖问题。
//
// 对应 Python: BaseAgent + get_operators() 方法
type TrainableAgent interface {
	// Invoke 非流式调用 Agent。
	// 对应 Python: BaseAgent.invoke(inputs, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// Card 返回 Agent 身份卡片。
	// 对应 Python: BaseAgent.card 属性
	Card() *agentschema.AgentCard
	// GetOperators 获取 Operator 注册表。
	// 对应 Python: BaseAgent.get_operators()
	GetOperators() map[string]operator.Operator
}

// ──────────────────────────── 非导出函数 ────────────────────────────

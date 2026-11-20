// agent.go 桥两端共享的 agent 装配产物载体与能力面。
// Python: member_optimizer/agents/factory.py 工厂返回的 agent 对象（鸭子类型）。

package agentface

import (
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Agent agents 子包工厂的装配产物统一载体。
// Python: create_*_agent 返回的 DeepAgent 对象（鸭子类型直传）；
// Go 以具体结构体承载，桥两端编译期 checked。
type Agent struct {
	// Deep DeepAgent 真身（构造失败前恒非 nil）
	Deep *harness.DeepAgent
	// Finalizer 开单证据护栏收尾能力（PlannerEvidenceRail；仅 planner 装配产物持有）
	Finalizer EvidenceFinalizer
	// Release 本次调用的原生资源释放（optimizer_agent_resources 句柄；nil 表示无）
	Release func() error
}

// ──────────────────────────── 枚举 ────────────────────────────

// EvidenceFinalizer 开单证据护栏收尾消息构造能力面。
// Python: agents/rails/planner_evidence.py PlannerEvidenceRail.finalization_messages。
type EvidenceFinalizer interface {
	// FinalizationMessages 构造收尾消息序列。
	FinalizationMessages(request, previous string) []schema.BaseMessage
}

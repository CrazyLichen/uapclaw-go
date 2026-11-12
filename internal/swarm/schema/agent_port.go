package schema

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// PlanState plan 状态快照（只取控制器关心的字段）。
// Python: runtime/plan.py 里 load_state 返回的 plan_mode 视图
type PlanState struct {
	// Mode 当前 plan 模式："plan" | "normal"
	Mode string
	// PlanSlug 活跃计划文件短名
	PlanSlug string
}

// ──────────────────────────── 接口 ────────────────────────────

// ChatAgentPort agent 的执行能力面（Runtime 侧调用）。
//
// 上游没有这个接口：Python 是 duck typing，runtime/service.py 直接调
// agent.process_message(...)。Go 需要静态类型，且 agent 的实现在
// server/runtime 包（上层），故契约必须定义在下层——放在 schema 而非
// runtime，是为了让 server/runtime 实现它时不必 import runtime（P-7）。
//
// 四个方法与上游逐一对应：
//
//	interface.py:3367 process_message(self, request)
//	interface.py:3374 execute_message(self, request)
//	interface.py:4147 process_message_stream(self, request)
type ChatAgentPort interface {
	// ProcessMessage 通过门面会话队列非流式执行
	// Python: interface.py process_message（schedule_session=True）
	ProcessMessage(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
	// ProcessMessageStream 通过门面会话队列流式执行
	// Python: interface.py process_message_stream（schedule_session=True）
	ProcessMessageStream(ctx context.Context, req *AgentRequest) (<-chan *AgentResponseChunk, error)
	// ExecuteMessage 由 Runtime 持有调度的非流式执行
	// Python: interface.py execute_message（schedule_session=False）
	ExecuteMessage(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
	// ExecuteMessageStream 由 Runtime 持有调度的流式执行
	// Python: interface.py execute_message_stream（schedule_session=False）
	ExecuteMessageStream(ctx context.Context, req *AgentRequest) (<-chan *AgentResponseChunk, error)
	// PlanStateAccessor 返回 plan 状态读写入口
	PlanStateAccessor() PlanStateAccessor
}

// PlanStateAccessor plan 状态读写入口。
//
// 上游 plan.py:177 ensure_state(request, mode, sub_mode, agent: Any) 用 Any
// 收 agent、内部 duck typing 调 load_state / save_state；Go 用本接口表达
// 同一组能力（4 个方法的调用面与上游 open_state_session + load/save 一致）。
//
// 定义在 schema：它出现在 ChatAgentPort 的方法签名里，而 ChatAgentPort 要
// 被 server/runtime 实现——放 schema 可避免 server/runtime import runtime。
type PlanStateAccessor interface {
	// LoadPlanState 读取会话 plan 状态
	LoadPlanState(ctx context.Context, sessionID string) (PlanState, error)
	// SavePlanState 保存会话 plan 状态
	SavePlanState(ctx context.Context, sessionID string, st PlanState) error
	// SwitchPlanMode 切 plan 模式
	SwitchPlanMode(ctx context.Context, sessionID, mode string) error
	// CommitSession 提交会话
	CommitSession(ctx context.Context, sessionID string) error
}

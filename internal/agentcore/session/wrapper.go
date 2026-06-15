package session

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouterSessionFacade 路由会话门面，安全壳实现。
//
// 对应 Python: openjiuwen/core/session/internal/wrapper.py (RouterSession)
// 在路由函数（add_conditional_edges）中使用，禁止写入状态/流/交互，
// 只保留追踪和读取能力，防止路由函数产生副作用。
type RouterSessionFacade struct {
	// inner 委托的节点会话门面
	inner *NodeSessionFacade
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRouterSessionFacade 创建路由会话门面实例。
func NewRouterSessionFacade(inner *NodeSessionFacade) *RouterSessionFacade {
	return &RouterSessionFacade{inner: inner}
}

// GetWorkflowID 返回工作流 ID（只读，允许）
func (r *RouterSessionFacade) GetWorkflowID() string {
	return r.inner.GetWorkflowID()
}

// GetComponentID 返回组件 ID（只读，允许）
func (r *RouterSessionFacade) GetComponentID() string {
	return r.inner.GetComponentID()
}

// GetComponentType 返回组件类型（只读，允许）
func (r *RouterSessionFacade) GetComponentType() string {
	return r.inner.GetComponentType()
}

// GetComponentDescription 返回组件描述（只读，允许）
func (r *RouterSessionFacade) GetComponentDescription() string {
	return r.inner.GetComponentDescription()
}

// GetExecutableID 返回全局唯一可执行路径 ID（只读，允许）
func (r *RouterSessionFacade) GetExecutableID() string {
	return r.inner.GetExecutableID()
}

// GetSessionID 返回会话唯一标识（只读，允许）
func (r *RouterSessionFacade) GetSessionID() string {
	return r.inner.GetSessionID()
}

// GetState 获取组件状态值（只读，允许）
// 对齐 Python RouterSession.get_state：继承 StateSession 的读取能力
func (r *RouterSessionFacade) GetState(key state.StateKey) (any, error) {
	return r.inner.GetState(key)
}

// UpdateState 更新组件状态 — 禁止操作（路由函数不允许修改状态）
// 对齐 Python RouterSession.update_state: pass
func (r *RouterSessionFacade) UpdateState(data map[string]any) {
	// 路由场景禁止写入状态，静默忽略
}

// GetGlobalState 获取全局状态值（只读，允许）
// 对齐 Python RouterSession.get_global_state：继承 StateSession 的读取能力
func (r *RouterSessionFacade) GetGlobalState(key state.StateKey) (any, error) {
	return r.inner.GetGlobalState(key)
}

// UpdateGlobalState 更新全局状态 — 禁止操作
// 对齐 Python RouterSession.update_global_state: pass
func (r *RouterSessionFacade) UpdateGlobalState(data map[string]any) {
	// 路由场景禁止写入全局状态，静默忽略
}

// DumpState 导出完整状态快照（只读，允许）
func (r *RouterSessionFacade) DumpState() map[string]any {
	return r.inner.DumpState()
}

// Trace 记录组件追踪数据（路由场景保留追踪能力）
// 对齐 Python RouterSession.trace: await TracerWorkflowUtils.trace(self._inner, data)
func (r *RouterSessionFacade) Trace(ctx context.Context, data map[string]any) error {
	return r.inner.Trace(ctx, data)
}

// TraceError 记录组件错误追踪（路由场景保留追踪能力）
// 对齐 Python RouterSession.trace_error: await TracerWorkflowUtils.trace_error(self._inner, error)
func (r *RouterSessionFacade) TraceError(ctx context.Context, err error) error {
	return r.inner.TraceError(ctx, err)
}

// Interact 请求用户输入 — 禁止操作
// 对齐 Python RouterSession.interact: pass
func (r *RouterSessionFacade) Interact(ctx context.Context, value any) (any, error) {
	// 路由场景禁止交互，静默返回 nil
	return nil, nil
}

// WriteStream 写入标准输出流 — 禁止操作
// 对齐 Python RouterSession.write_stream: pass
func (r *RouterSessionFacade) WriteStream(ctx context.Context, data any) error {
	// 路由场景禁止流写入，静默忽略
	return nil
}

// WriteCustomStream 写入自定义流 — 禁止操作
// 对齐 Python RouterSession.write_custom_stream: pass
func (r *RouterSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	// 路由场景禁止流写入，静默忽略
	return nil
}

// GetEnv 获取环境变量值 — 禁止操作
// 对齐 Python RouterSession.get_env: pass
func (r *RouterSessionFacade) GetEnv(key string) any {
	// 路由场景禁止读取配置，返回 nil
	return nil
}

// GetNodeConfig 获取节点级配置 — 禁止操作
// 对齐 Python RouterSession.get_workflow_config / get_agent_config: pass
func (r *RouterSessionFacade) GetNodeConfig() any {
	// 路由场景禁止读取配置，返回 nil
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

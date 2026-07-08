package rails

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentRail DeepAgent 扩展 Rail 基类。
//
// 在核心层 AgentRail（10 个钩子）基础上增加：
//   - workspace：工作空间引用（供子类访问工作空间信息）
//   - sysOperation：系统操作引用（供子类执行系统级操作）
//   - GetCallbacks()：合并基础事件 + DeepAgent 扩展事件
//
// 子类嵌入 DeepAgentRail 后覆盖关心的钩子方法，
// 并在 GetCallbacks() 中通过 CallbackFrom + BuildCallbacks 声明映射。
//
// 对应 Python: DeepAgentRail (openjiuwen/harness/rails/base.py L28-107)
type DeepAgentRail struct {
	agentinterfaces.BaseRail
	// workspace 工作空间引用
	workspace *workspace.Workspace
	// sysOperation 系统操作引用（接口值，无需取地址）
	sysOperation sys_operation.SysOperation
}

// ──────────────────────────── 接口 ────────────────────────────

// DeepAgentRailProvider DeepAgentRail 提供者接口。
// 对齐 Python: isinstance(rail_inst, DeepAgentRail) 类型检查。
// 嵌入 DeepAgentRail 的子类自动满足此接口。
type DeepAgentRailProvider interface {
	SetSysOperation(op sys_operation.SysOperation)
	SetWorkspace(w *workspace.Workspace)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAgentRail 创建 DeepAgentRail 实例（默认优先级 50）。
//
// 对应 Python: DeepAgentRail.__init__()
func NewDeepAgentRail() *DeepAgentRail {
	return &DeepAgentRail{
		BaseRail: *agentinterfaces.NewBaseRail(),
	}
}

// SetWorkspace 设置工作空间引用。
//
// 对应 Python: DeepAgentRail.set_workspace(workspace)
func (d *DeepAgentRail) SetWorkspace(w *workspace.Workspace) {
	d.workspace = w
}

// Workspace 返回工作空间引用。
func (d *DeepAgentRail) Workspace() *workspace.Workspace {
	return d.workspace
}

// SetSysOperation 设置系统操作引用。
//
// 对应 Python: DeepAgentRail.set_sys_operation(sys_operation)
func (d *DeepAgentRail) SetSysOperation(op sys_operation.SysOperation) {
	d.sysOperation = op
}

// SysOperation 返回系统操作引用。
func (d *DeepAgentRail) SysOperation() sys_operation.SysOperation {
	return d.sysOperation
}

// GetCallbacks 合并基础事件 + DeepAgent 扩展事件的回调映射。
//
// 先获取 BaseRail 的基础回调（8 个核心事件），
// 再遍历 DeepEventMethodMap 检查子类是否覆盖了 task-iteration 钩子。
// 对齐 Python: DeepAgentRail.get_callbacks() = super().get_callbacks() + DEEP_EVENT_METHOD_MAP
func (d *DeepAgentRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := d.BaseRail.GetCallbacks()

	// 遍历 DeepAgent 扩展事件，将 task-iteration 钩子加入回调映射
	for event := range agentinterfaces.DeepEventMethodMap {
		if fn, ok := d.getDeepCallback(event); ok {
			callbacks[event] = fn
		}
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getDeepCallback 获取 DeepAgent 扩展事件的回调函数。
//
// 将 BeforeTaskIteration/AfterTaskIteration 包装为 PerAgentCallbackFunc。
func (d *DeepAgentRail) getDeepCallback(event agentinterfaces.AgentCallbackEvent) (cb.PerAgentCallbackFunc, bool) {
	switch event {
	case agentinterfaces.CallbackBeforeTaskIteration:
		return func(ctx context.Context, railCtx any) error {
			return d.BeforeTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}, true
	case agentinterfaces.CallbackAfterTaskIteration:
		return func(ctx context.Context, railCtx any) error {
			return d.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}, true
	default:
		return nil, false
	}
}

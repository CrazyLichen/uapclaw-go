package handlers

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseCoordinationHandler 场景级协调事件处理器基类。
//
// 子类声明 EVENT_METHOD_MAP（event_key → 方法名），通过 GetCallbacks() 输出
// event_key → bound method 注册给 CallbackFramework。
// Python: BaseCoordinationHandler
type BaseCoordinationHandler struct {
	// round Round 级控制面（与 TeamHarness 打交道）
	round coordination.AgentRoundController
	// lifecycle TeamAgent 级生命周期效果
	lifecycle coordination.TeamLifecycleController
	// poll EventBus 自身的 poll 控制
	poll coordination.PollController
	// blueprint 静态身份
	blueprint coordination.DispatcherBlueprint
	// infra per-process 容器
	infra coordination.DispatcherInfra
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseCoordinationHandler 创建基类实例。
// Python: BaseCoordinationHandler.__init__
func NewBaseCoordinationHandler(
	host coordination.DispatcherHost,
	bp coordination.DispatcherBlueprint,
	inf coordination.DispatcherInfra,
	pollCtrl coordination.PollController,
) BaseCoordinationHandler {
	return BaseCoordinationHandler{
		round:     host,
		lifecycle: host,
		poll:      pollCtrl,
		blueprint: bp,
		infra:     inf,
	}
}

// GetCallbacks 返回 event_key → bound method，供 framework 注册。
// 子类应覆盖此方法返回自身的 EVENT_METHOD_MAP。
// Python: BaseCoordinationHandler.get_callbacks
func (h *BaseCoordinationHandler) GetCallbacks() map[string]coordination.EventCallbackFunc {
	return map[string]coordination.EventCallbackFunc{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 自动审查层包装权限轨时使用的协作面。
//
// 对应 Python 代码：无独立文件——Python 侧是鸭子类型
// （`_auto_permission/lifecycle.py` / `before_tool.py` / `runtime_bridge.py`
// 用 getattr(base_rail, "xxx", None) + callable() 探测可选方法）。
// Go 用显式接口替代鸭子类型（规则 9.6 语言层差异）。
//
// 依赖倒置判定（规则 10.2）：本接口描述「9.90d-2 的 AutoPermissionInterruptRail
// 能调用它包装的权限 rail 的哪些方法」，属于「DeepAgent 用来调用别人的」
// 协作方能力，不是 DeepAgent 自己的能力，因此允许单独定义窄接口，
// 不进 DeepAgentInterface。
//
// ⚠️ 命名说明：本接口与 agentinterfaces.BaseRail **无关**。
//   - agentinterfaces.BaseRail 是「所有 rail 的公共基类」（继承链，is-a）
//   - BasePermissionRail 是「外层包装者看内层的视角」（组合链，has-a）
// 两者名字都含 Base 纯属巧合。
//
// 方法分两类：
//   - 回调类：BeforeToolCall —— 框架（ReAct）触发后由外层转发给内层
//   - 业务类：其余 6 个 —— 外层（AutoPermissionInterruptRail）在决策过程中
//     主动向内层查询配置/快照/上下文，或向内层写回配置/审批绑定
//
// 实际实现：JiuwenSwarmPermissionInterruptRail（permission_interrupt_rail.go）。

package permissions

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BasePermissionRail 自动审查层包装的权限轨协作面。
//
// Python: 鸭子类型 getattr(self.base_rail, "before_tool_call", None) + callable()
//
// 方法签名以 JiuwenSwarmPermissionInterruptRail 的实际实现为准（保留 error
// 返回以对齐 Python 的 ValueError 语义），不采用计划文档的简化版。
type BasePermissionRail interface {
	// BeforeToolCall 工具调用前拦截（回调类，由外层转发）。
	BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
	// UpdateConfig 热更新静态权限配置（业务类）。
	UpdateConfig(config map[string]any, toolNames []string)
	// SetTrustedDirs 设置受信任目录列表（业务类）。
	SetTrustedDirs(trustedDirs []string)
	// InstalledPermissionConfig 返回已安装的全局权限配置（业务类）。
	InstalledPermissionConfig() map[string]any
	// GetPermissionSnapshot 返回指定会话的权限快照（业务类）。
	//
	// 返回 error 对齐 Python 的 `raise ValueError("Installed permission
	// snapshot unavailable")`：宿主无法提供快照时外层应拒绝而非静默降级。
	GetPermissionSnapshot(sessionID string) (map[string]any, error)
	// PermissionCheckContext 绑定权限检查上下文，返回清理函数（业务类）。
	//
	// 对应 Python 的 @contextmanager permission_check_context(session_id)：
	// 进入时读一次快照，返回的 cleanup 在退出时清理。
	PermissionCheckContext(sessionID string) (cleanup func())
	// BindApprovalRequest 把审批范围冻结到既有待处理请求上（业务类）。
	//
	// 返回 error 对齐 Python 的 `raise ValueError("permission_snapshot_unavailable")`。
	BindApprovalRequest(request saschema.InterruptRequester, toolCall *llmschema.ToolCall) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：JiuwenSwarmPermissionInterruptRail 必须满足 BasePermissionRail。
var _ BasePermissionRail = (*JiuwenSwarmPermissionInterruptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

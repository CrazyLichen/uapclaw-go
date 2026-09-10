package security

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var factoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPermissionInterruptRail 若 permissions.enabled 为真则创建护栏，否则返回 nil。
//
// Python: build_permission_interrupt_rail(permissions, llm, model_name, engine, host, workspace_root) -> PermissionInterruptRail | None (factory.py)
// 通过 PermissionRailConstructor 回调解耦 security 与 rails/security 的循环依赖。
func BuildPermissionInterruptRail(
	permissions map[string]any,
	engine *PermissionEngine,
	host *ToolPermissionHost,
	workspaceRoot string,
	model *llm.Model,
	modelName string,
	ctor PermissionRailConstructor,
) agentinterfaces.AgentRail {
	if permissions == nil {
		return nil
	}

	// Python: if not isinstance(permissions, dict) or not permissions.get("enabled", False)
	enabled := false
	if v, ok := permissions["enabled"]; ok {
		if b, ok := v.(bool); ok {
			enabled = b
		}
	}
	if !enabled {
		logger.Debug(factoryLogComponent).Msg("build_permission_interrupt_rail: permissions.enabled=false，跳过创建")
		return nil
	}

	// Python: h = host or ToolPermissionHost()
	h := host
	if h == nil {
		h = &ToolPermissionHost{}
	}

	// Python: if h.resolve_workspace_dir is None and workspace_root is not None
	if h.ResolveWorkspaceDir == nil && workspaceRoot != "" {
		root := workspaceRoot // 捕获到闭包
		h.ResolveWorkspaceDir = func() string { return root }
	}

	// Python: return PermissionInterruptRail(config=deepcopy(permissions), engine=engine, tool_names=None, llm=llm, model_name=model_name, host=h)
	// deepcopy 由构造器内部 NewPermissionEngine 处理（PermissionEngine.NewPermissionEngine 会拷贝 config）
	return ctor(permissions, engine, nil, model, modelName, h)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

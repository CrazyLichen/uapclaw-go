package security

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var factoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPermissionInterruptRail 若 permissions.enabled 为真则创建 PermissionEngine，
// 否则返回 nil。
//
// 对齐 Python: build_permission_interrupt_rail(permissions, llm, model_name, engine, host, workspace_root) (factory.py)
func BuildPermissionInterruptRail(
	permissions map[string]any,
	engine *PermissionEngine,
	host *ToolPermissionHost,
	workspaceRoot string,
) (*PermissionEngine, *ToolPermissionHost) {
	if permissions == nil {
		return nil, nil
	}

	// 检查 enabled
	enabled := false
	if v, ok := permissions["enabled"]; ok {
		if b, ok := v.(bool); ok {
			enabled = b
		}
	}
	if !enabled {
		logger.Debug(factoryLogComponent).Msg("build_permission_interrupt_rail: permissions.enabled=false，跳过创建")
		return nil, nil
	}

	h := host
	if h == nil {
		h = &ToolPermissionHost{}
	}

	// 若 host 缺少 ResolveWorkspaceDir 且有 workspaceRoot，则补充
	if h.ResolveWorkspaceDir == nil && workspaceRoot != "" {
		root := workspaceRoot // 捕获到闭包
		h.ResolveWorkspaceDir = func() string { return root }
	}

	return engine, h
}

// ──────────────────────────── 非导出函数 ────────────────────────────

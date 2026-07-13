package sysop_builder

import (
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateLocalSysOpCard 创建本地模式的 SysOperationCard。
// 对齐 Python: _create_local_sysop_card()
// ⤵️ 10.3.7-11: 本地模式卡片具体配置待回填
func CreateLocalSysOpCard(projectDir string, workspaceDir string, isCodeAgent bool) *sysop.SysOperationCard {
	card := sysop.NewSysOperationCard(sysop.WithSysOpMode(sysop.OperationModeLocal))

	// 构建本地工作配置
	// ⤵️ 10.3.7-11: 根据 isCodeAgent 决定主写入根
	// code-agent → projectDir，deep-agent → workspaceDir
	workDir := workspaceDir
	if isCodeAgent && projectDir != "" {
		workDir = projectDir
	}

	// 设置工作目录配置
	// ⤵️ 10.3.7-11: card.WorkConfig = BuildLocalWorkConfig(workDir, projectDir, workspaceDir)
	_ = workDir

	logger.Info(logComponent).
		Str("mode", "local").
		Str("project_dir", projectDir).
		Str("workspace_dir", workspaceDir).
		Bool("is_code_agent", isCodeAgent).
		Msg("CreateLocalSysOpCard 完成（待 10.3.7-11 回填具体配置）")

	return card
}

// CreateSandboxSysOpCard 创建沙箱模式的 SysOperationCard。
// 对齐 Python: _create_sandbox_sysop_card()
// ⤵️ 10.3.7-11: 沙箱模式卡片具体配置待回填
func CreateSandboxSysOpCard(projectDir string, workspaceDir string, isCodeAgent bool) *sysop.SysOperationCard {
	card := sysop.NewSysOperationCard(sysop.WithSysOpMode(sysop.OperationModeSandbox))

	// ⤵️ 10.3.7-11: 构建沙箱网关配置
	// card.GatewayConfig = BuildSandboxGatewayConfig(...)

	logger.Info(logComponent).
		Str("mode", "sandbox").
		Msg("CreateSandboxSysOpCard 完成（待 10.3.7-11 回填具体配置）")

	return card
}

// BuildFilesystemPolicy 构建文件系统策略。
// 对齐 Python: _build_filesystem_policy()
// ⤵️ 10.3.7-11: 文件系统策略具体配置待回填
func BuildFilesystemPolicy(projectDir string, workspaceDir string, readOnly bool) any {
	// ⤵️ 10.3.7-11: 根据 readOnly 和目录构建 FilesystemPolicy
	logger.Info(logComponent).
		Str("project_dir", projectDir).
		Bool("read_only", readOnly).
		Msg("BuildFilesystemPolicy 完成（待 10.3.7-11 回填）")
	return nil
}

// ListAutoManagedSandboxPaths 列出自动管理的沙箱路径。
// 对齐 Python: _list_auto_managed_sandbox_paths()
// ⤵️ 10.3.7-11: 自动管理沙箱路径待回填
func ListAutoManagedSandboxPaths(projectDir string) []string {
	// ⤵️ 10.3.7-11: 返回自动管理的沙箱路径列表
	logger.Info(logComponent).Str("project_dir", projectDir).Msg("ListAutoManagedSandboxPaths 完成（待 10.3.7-11 回填）")
	return nil
}

// CreateSysOperationFromCard 从 SysOperationCard 创建 SysOperation 实例。
// 对齐 Python: _create_sys_operation_from_card(card)
// ⤵️ 10.3.7-11: 具体实例化待回填
func CreateSysOperationFromCard(card *sysop.SysOperationCard) sysop.SysOperation {
	if card == nil {
		return nil
	}

	// 根据 card.Mode 创建对应实例
	switch card.Mode {
	case sysop.OperationModeLocal:
		// ⤵️ 10.3.7-11: 创建 LocalSysOperation 实例
		return nil
	case sysop.OperationModeSandbox:
		// ⤵️ 10.3.7-11: 创建 SandboxSysOperation 实例
		return nil
	default:
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveOperationMode 解析操作模式。
// 对齐 Python: _resolve_operation_mode(config_base)
// ⤵️ 10.3.7-11: 从配置解析操作模式
func resolveOperationMode(configBase map[string]any) sysop.OperationMode {
	// ⤵️ 10.3.7-11: 从 configBase["sys_operation"]["mode"] 解析
	return sysop.OperationModeLocal
}

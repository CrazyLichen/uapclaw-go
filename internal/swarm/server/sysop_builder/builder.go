package sysop_builder

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// preserveFileSharingMode 固有文件共享模式，当前只支持 "mount"。
// 对齐 Python: PreserveFileSharingMode = Literal["mount"]
const preserveFileSharingMode = "mount"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateLocalSysOpCard 创建本地模式 SysOperationCard。
// 对齐 Python: create_local_sysop_card() (sysop_builder.py L770-776)
//
// Python 实现：
//
//	return SysOperationCard(
//	    mode=OperationMode.LOCAL,
//	    work_config=LocalWorkConfig(shell_allowlist=None),
//	)
//
// shell_allowlist=None 表示允许所有命令，Go 中 nil []string 等价。
func CreateLocalSysOpCard() *sysop.SysOperationCard {
	card := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{
			ShellAllowlist: nil, // 对齐 Python: shell_allowlist=None，允许所有命令
		}),
	)

	logger.Info(logComponent).Str("mode", "LOCAL").Msg("本地 SysOperationCard 已创建")

	return card
}

// CreateSandboxSysOpCard 创建沙箱模式 SysOperationCard。
// 对齐 Python: create_sandbox_sysop_card(sandbox_url, sandbox_type, *, files_runtime,
//
//	excluded_commands, idle_ttl_seconds, idle_check_interval, project_dir, is_code_agent)
//
// 失败时返回 nil（Python 返回 None），异常被捕获记 warning。
// Python 实现中 work_config 同样使用 LocalWorkConfig(shell_allowlist=None)，
// 安全边界由沙箱自身保证。
func CreateSandboxSysOpCard(
	sandboxURL string,
	sandboxType string,
	filesRuntime map[string]any,
	excludedCommands []string,
	idleTTLSecs *int,
	idleCheckInterval *int,
	projectDir string,
	isCodeAgent bool,
) *sysop.SysOperationCard {
	policy, uploadList, err := BuildFilesystemPolicy(filesRuntime, projectDir, isCodeAgent)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("BuildFilesystemPolicy 失败，无法创建沙箱 SysOperationCard")
		return nil
	}

	// 构建 extraParams
	excludedCmds := make([]string, 0)
	if excludedCommands != nil {
		excludedCmds = excludedCommands
	}
	extraParams := map[string]any{
		"policy":                     policy,
		"policy_mode":                "append",
		"excluded_commands":          excludedCmds,
		"preserve_file_sharing_mode": preserveFileSharingMode,
		"preserve_files_upload":      uploadList,
	}
	// idle_check_interval 走 extraParams 而非 launcher_config 独立字段
	if idleCheckInterval != nil {
		extraParams["idle_check_interval"] = *idleCheckInterval
	}

	// 构建 SandboxGatewayConfig
	gatewayConfig := sysop.NewSandboxGatewayConfig()
	gatewayConfig.Isolation = sysop.NewSandboxIsolationConfig()
	gatewayConfig.Isolation.ContainerScope = sysop.ContainerScopeSystem

	launcherConfig := sysop.NewPreDeployLauncherConfig(sandboxURL)
	launcherConfig.SandboxType = sandboxType
	launcherConfig.IdleTTLSeconds = idleTTLSecs
	launcherConfig.ExtraParams = extraParams
	gatewayConfig.LauncherConfig = launcherConfig

	// 构建 SysOperationCard
	card := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeSandbox),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{
			ShellAllowlist: nil, // 对齐 Python: shell_allowlist=None，安全边界由沙箱保证
		}),
		sysop.WithSysOpGatewayConfig(gatewayConfig),
	)

	// 日志输出 policy 详情（对齐 Python L730-763）
	fsPolicy := make(map[string]any)
	if p, ok := policy["filesystem_policy"]; ok {
		fsPolicy, _ = p.(map[string]any)
	}
	bindMountsCount := 0
	if bm, ok := fsPolicy["bind_mounts"]; ok {
		if bmSlice, ok := bm.([]map[string]any); ok {
			bindMountsCount = len(bmSlice)
		}
	}
	idleCheckVal := -1
	if idleCheckInterval != nil {
		idleCheckVal = *idleCheckInterval
	}
	logger.Info(logComponent).
		Str("base_url", sandboxURL).
		Str("sandbox_type", sandboxType).
		Int("idle_ttl", func() int {
			if idleTTLSecs != nil {
				return *idleTTLSecs
			}
			return -1
		}()).
		Int("idle_check_interval", idleCheckVal).
		Str("preserve_file_sharing_mode", preserveFileSharingMode).
		Int("excluded_commands", len(excludedCmds)).
		Int("filesystem_policy.files", func() int {
			if f, ok := fsPolicy["files"]; ok {
				if s, ok := f.([]map[string]any); ok {
					return len(s)
				}
			}
			return 0
		}()).
		Int("filesystem_policy.directories", func() int {
			if d, ok := fsPolicy["directories"]; ok {
				if s, ok := d.([]map[string]any); ok {
					return len(s)
				}
			}
			return 0
		}()).
		Int("bind_mounts", bindMountsCount).
		Int("filesystem_policy.read_write", func() int {
			if rw, ok := fsPolicy["read_write"]; ok {
				if s, ok := rw.([]string); ok {
					return len(s)
				}
			}
			return 0
		}()).
		Int("filesystem_policy.read_only", func() int {
			if ro, ok := fsPolicy["read_only"]; ok {
				if s, ok := ro.([]string); ok {
					return len(s)
				}
			}
			return 0
		}()).
		Int("preserve_files_upload", len(uploadList)).
		Str("policy_mode", "append").
		Msg("沙箱 SysOperationCard 已创建")

	return card
}

// CreateSysOperationFromCard 从 SysOperationCard 创建 SysOperation 实例并注册到 ResourceMgr。
// 对齐 Python: Runner.resource_mgr.add_sys_operation(card)
// 包含隔离键复用检查、注册、并发重试逻辑。
func CreateSysOperationFromCard(card *sysop.SysOperationCard) sysop.SysOperation {
	if card == nil {
		return nil
	}

	// 1. 从 card 创建 SysOperation 实例
	instance, err := sysop.NewSysOperation(card)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("NewSysOperation 创建失败")
		return nil
	}

	// 2. 计算隔离键模板，检查是否可复用
	isolationKey := instance.IsolationKeyTemplate()
	if isolationKey != "" {
		existing := getRegisteredSysOpByIsolationKey(isolationKey)
		if existing != nil {
			logger.Info(logComponent).
				Str("isolation_key", isolationKey).
				Msg("复用已注册 SysOperation")
			return existing
		}
	}

	// 3. 注册到 ResourceMgr
	rm := runner.GetResourceMgr()
	if rm != nil {
		if addErr := rm.AddSysOperation(card.ID, instance); addErr != nil {
			// 注册失败，再查一次（并发场景）
			if isolationKey != "" {
				existing := getRegisteredSysOpByIsolationKey(isolationKey)
				if existing != nil {
					logger.Info(logComponent).
						Str("isolation_key", isolationKey).
						Msg("注册失败后复用已注册 SysOperation")
					return existing
				}
			}
			logger.Warn(logComponent).
				Err(addErr).
				Msg("SysOperation 注册失败")
			return nil
		}
		logger.Info(logComponent).
			Str("card_id", card.ID).
			Msg("SysOperation 已注册到资源管理器")
	} else {
		logger.Warn(logComponent).Msg("全局资源管理器未初始化，跳过 SysOperation 注册")
	}

	return instance
}

// ResolveOperationMode 从配置解析操作模式。
// 对齐 Python: _resolve_operation_mode(config_base) — 从 configBase["sys_operation"]["mode"] 解析
func ResolveOperationMode(configBase map[string]any) sysop.OperationMode {
	if configBase == nil {
		return sysop.OperationModeLocal
	}
	if sysOpSection, ok := configBase["sys_operation"].(map[string]any); ok {
		if m, ok := sysOpSection["mode"].(string); ok {
			return sysop.FromOperationModeString(m)
		}
	}
	return sysop.OperationModeLocal
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getRegisteredSysOpByIsolationKey 按隔离键模板查找已注册的 SysOperation。
// 对齐 Python: _get_registered_sys_operation_by_isolation_key()
func getRegisteredSysOpByIsolationKey(key string) sysop.SysOperation {
	if key == "" {
		return nil
	}
	rm := runner.GetResourceMgr()
	if rm == nil {
		return nil
	}
	instance, err := rm.GetSysOperationByIsolationKey(key)
	if err != nil {
		return nil
	}
	return instance
}

package security

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PathAction 一次需要落盘的路径访问（fileguard 的 AskAccess 在 security 侧的表示）。
type PathAction struct {
	// Path 绝对路径（posix 形式）
	Path string
	// Action 访问动作：read / write / exec
	Action string
}

// ──────────────────────────── 枚举 ────────────────────────────

// FileGuardEvaluator 路径防护判定器。
//
// 由 fileguard 子包实现，security 包只依赖此接口——避免 security ↔ fileguard
// 循环依赖（fileguard 需要 security 的 PermissionLevel 等符号）。
// 判定语义见 fileguard.FileGuardChecker。
type FileGuardEvaluator interface {
	// Enabled 路径层是否启用
	Enabled() bool
	// Mode 判定模式（"legacy" / "native"）
	Mode() string
	// Evaluate 返回路径层判定结果；无意见或全 ALLOW 时返回 nil
	Evaluate(toolName string, toolArgs map[string]any) *PermissionResult
	// CollectAskAccesses 返回本次判定为 ASK 的访问，供「总是允许」按轴落盘
	CollectAskAccesses(toolName string, toolArgs map[string]any) []PathAction
}

// NetGuardEvaluator 网络防护判定器。由 netguard 子包实现，语义见
// netguard.NetGuardChecker。只有 allow/deny，无 ask。
type NetGuardEvaluator interface {
	// Evaluate 返回网络层判定结果；无意见或 ALLOW 时返回 nil
	Evaluate(toolName string, toolArgs map[string]any) *PermissionResult
}

// ──────────────────────────── 常量 ────────────────────────────

// FileGuardFactory 构造路径防护判定器。路径层关闭时应返回 nil。
type FileGuardFactory func(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator

// NetGuardFactory 构造网络防护判定器。网络层关闭时应返回 nil。
type NetGuardFactory func(permissions map[string]any) NetGuardEvaluator

// SensitivePathsMerger 把包内敏感路径注入配置的 file_guard.paths。
type SensitivePathsMerger func(permissions map[string]any, inject *bool) map[string]any

// NetURLsMerger 把包内 net_urls 注入配置的 net_guard.urls。
type NetURLsMerger func(permissions map[string]any, inject *bool) map[string]any

// HasPackageNetURLsFunc 判定配置的 urls 是否已含全部包内内置项。
type HasPackageNetURLsFunc func(urls any) bool

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileGuardFactoryMu 工厂注册锁
	fileGuardFactoryMu sync.RWMutex
	// fileGuardFactory 路径防护工厂，由 fileguard 子包 init 注入
	fileGuardFactory FileGuardFactory

	// netGuardFactoryMu 工厂注册锁
	netGuardFactoryMu sync.RWMutex
	// netGuardFactory 网络防护工厂，由 netguard 子包 init 注入
	netGuardFactory NetGuardFactory

	// bridgeMu 三个配置注入函数注册锁
	bridgeMu sync.RWMutex
	// sensitivePathsMerger 敏感路径注入函数
	sensitivePathsMerger SensitivePathsMerger
	// netURLsMerger net_urls 注入函数
	netURLsMerger NetURLsMerger
	// hasPackageNetURLs 内置 net_urls 存在性判定
	hasPackageNetURLs HasPackageNetURLsFunc
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterFileGuardFactory 注册路径防护判定器工厂。仅供 fileguard 子包 init 调用。
func RegisterFileGuardFactory(f FileGuardFactory) {
	fileGuardFactoryMu.Lock()
	fileGuardFactory = f
	fileGuardFactoryMu.Unlock()
}

// RegisterNetGuardFactory 注册网络防护判定器工厂。仅供 netguard 子包 init 调用。
func RegisterNetGuardFactory(f NetGuardFactory) {
	netGuardFactoryMu.Lock()
	netGuardFactory = f
	netGuardFactoryMu.Unlock()
}

// NewFileGuardEvaluator 构造路径防护判定器。未注册工厂或路径层关闭时返回 nil。
//
// 供 rails 侧在 persist 链路构造判定器使用——那里要用「刚合并的配置」而非
// 引擎当前配置，故不能走引擎的 fileGuard 字段。
func NewFileGuardEvaluator(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator {
	return newFileGuardEvaluator(permissions, workspaceRoot, trustedDirs)
}

// NewNetGuardEvaluator 构造网络防护判定器。未注册工厂或网络层关闭时返回 nil。
func NewNetGuardEvaluator(permissions map[string]any) NetGuardEvaluator {
	return newNetGuardEvaluator(permissions)
}

// RegisterPermissionBridges 注册三个配置注入函数。仅供 fileguard / netguard
// 子包 init 调用。
func RegisterPermissionBridges(
	sensitiveMerger SensitivePathsMerger,
	netMerger NetURLsMerger,
	hasNetURLs HasPackageNetURLsFunc,
) {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	if sensitiveMerger != nil {
		sensitivePathsMerger = sensitiveMerger
	}
	if netMerger != nil {
		netURLsMerger = netMerger
	}
	if hasNetURLs != nil {
		hasPackageNetURLs = hasNetURLs
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFileGuardEvaluator 用已注册的工厂构造判定器，未注册返回 nil。
func newFileGuardEvaluator(permissions map[string]any, workspaceRoot string, trustedDirs []string) FileGuardEvaluator {
	fileGuardFactoryMu.RLock()
	f := fileGuardFactory
	fileGuardFactoryMu.RUnlock()
	if f == nil {
		return nil
	}
	return f(permissions, workspaceRoot, trustedDirs)
}

// newNetGuardEvaluator 用已注册的工厂构造判定器，未注册返回 nil。
func newNetGuardEvaluator(permissions map[string]any) NetGuardEvaluator {
	netGuardFactoryMu.RLock()
	f := netGuardFactory
	netGuardFactoryMu.RUnlock()
	if f == nil {
		return nil
	}
	return f(permissions)
}

// mergeWithSensitivePaths 注入内置敏感路径。注册函数缺失时原样返回并 WARN。
func mergeWithSensitivePaths(cfg map[string]any) map[string]any {
	bridgeMu.RLock()
	f := sensitivePathsMerger
	bridgeMu.RUnlock()
	if f == nil {
		logger.Warn(logComponent).Msg("permission.prepare.sensitive_paths_merger_missing")
		return cfg
	}
	return f(cfg, nil)
}

// mergeWithNetURLs 注入内置 net_urls。注册函数缺失时原样返回并 WARN。
func mergeWithNetURLs(cfg map[string]any) map[string]any {
	bridgeMu.RLock()
	f := netURLsMerger
	bridgeMu.RUnlock()
	if f == nil {
		logger.Warn(logComponent).Msg("permission.prepare.net_urls_merger_missing")
		return cfg
	}
	return f(cfg, nil)
}

// checkPackageNetURLs 判定 urls 是否已含全部包内内置项。
// 注册函数缺失时返回 false（保守：触发注入）。
func checkPackageNetURLs(urls any) bool {
	bridgeMu.RLock()
	f := hasPackageNetURLs
	bridgeMu.RUnlock()
	if f == nil {
		return false
	}
	return f(urls)
}

// truthyAny Python 的 bool() 语义。
func truthyAny(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

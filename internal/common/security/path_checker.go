package security

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PathChecker 敏感路径检查器（Python path_checker.PathChecker 单例等价）。
// Go 无 __new__ 单例惯用法，用 sync.Once + 包级实例复刻惰性初始化语义。
type PathChecker struct {
	// sensitivePaths 归一化后的敏感路径清单
	sensitivePaths map[string]struct{}
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pathCheckerInstance 单例实例（Python _instance）
	pathCheckerInstance *PathChecker
	// pathCheckerOnce 惰性初始化（Python _lock + _initialized）
	pathCheckerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPathChecker 返回单例（Python PathChecker() 隐式 __new__ 等价）
func GetPathChecker() *PathChecker {
	pathCheckerOnce.Do(func() {
		instance := &PathChecker{}
		instance.loadConfig()
		pathCheckerInstance = instance
	})
	return pathCheckerInstance
}

// IsSensitivePath 检查路径是否敏感（Python 模块级 is_sensitive_path）
func IsSensitivePath(path string) bool {
	// Python: if not path or not isinstance(...): return False
	if path == "" {
		return false
	}
	return GetPathChecker().isSensitivePath(path)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadConfig 加载敏感路径清单（Python _load_config）。
// Go 侧无 UserConfig.get_sensitive_paths 可用配置（未移植），恒走 Python 异常兜底默认清单；
// 回收条件：UserConfig 移植后改为读取配置。
func (p *PathChecker) loadConfig() {
	// Python: sensitive_paths 清空后从 UserConfig 读取，异常时兜底默认清单；
	// Go 侧无该配置来源，直接取兜底清单（Python except 分支的字面清单）
	sensitivePaths := []string{
		"/etc/passwd", "/etc/shadow", "/etc/hosts", "/etc/hostname",
		"/etc/ssh/", "/proc/", "/sys/", "/dev/",
		"C:\\Windows\\System32\\", "C:\\Windows\\SysWOW64\\", "C:\\Windows\\System\\",
	}
	p.sensitivePaths = make(map[string]struct{}, len(sensitivePaths))
	for _, sp := range sensitivePaths {
		// Python: normalized_path = os.path.realpath(os.path.expanduser(path.strip()))
		normalized := normalizeRealPath(expandUser(strings.TrimSpace(sp)))
		if normalized == "" {
			// Python: except (OSError, ValueError) 时回退原串
			normalized = strings.TrimSpace(sp)
		}
		p.sensitivePaths[normalized] = struct{}{}
	}
}

// isSensitivePath 前缀匹配判定（Python PathChecker.is_sensitive_path）
func (p *PathChecker) isSensitivePath(path string) bool {
	// Python: normalized_path = os.path.abspath(os.path.expanduser(str(path)))
	normalized := filepath.Clean(expandUser(path))
	if !filepath.IsAbs(normalized) {
		if abs, err := filepath.Abs(normalized); err == nil {
			normalized = abs
		}
	}
	for sensitive := range p.sensitivePaths {
		// Python: if normalized_path.startswith(sensitive_path): return True
		if strings.HasPrefix(normalized, sensitive) {
			return true
		}
	}
	return false
}

// expandUser 展开 ~ 前缀（Python os.path.expanduser 等价）
func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// normalizeRealPath 解析符号链接（Python os.path.realpath 等价）；
// 解析失败返回空串，由调用方决定回退
func normalizeRealPath(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(real); err == nil {
		return abs
	}
	return real
}

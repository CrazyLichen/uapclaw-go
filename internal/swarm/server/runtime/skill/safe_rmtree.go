package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRmtreeRetries 安全删除最大重试次数
	maxRmtreeRetries = 3
	// rmtreeBaseDelay 重试基础延迟（毫秒）
	rmtreeBaseDelay = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeRmtree 安全删除目录（对齐 Python: _safe_rmtree）。
// 最多 3 次重试，Windows 上修改文件权限为可写，指数退避延迟。
//
// 对齐 Python: jiuwenswarm/server/runtime/skill/skill_manager.py (_safe_rmtree)
func safeRmtree(path string) error {
	var lastErr error
	for attempt := 0; attempt < maxRmtreeRetries; attempt++ {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		lastErr = err

		// Windows: 递归修改文件权限为可写（对齐 Python: os.chmod 为只读文件解锁）
		if runtime.GOOS == "windows" {
			_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				_ = os.Chmod(p, 0o777)
				return nil
			})
		}

		// 指数退避延迟：200ms, 400ms（对齐 Python: time.sleep(0.2 * (2 ** attempt))）
		if attempt < maxRmtreeRetries-1 {
			delayMs := rmtreeBaseDelay * (1 << attempt)
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}
	return lastErr
}

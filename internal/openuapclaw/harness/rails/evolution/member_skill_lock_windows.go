//go:build windows

package evolution

import (
	"os"
	"time"
)

// acquireFileLockDefault Windows 侧进程内互斥降级实现（无 flock；
// Python portalocker 在 Windows 走 msvcrt，Go 侧暂以互斥目录哨兵承载）。
func acquireFileLockDefault(lockPath string) (func(), error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := os.Mkdir(lockPath + ".lockdir", 0o755); err == nil {
			return func() { _ = os.RemoveAll(lockPath + ".lockdir") }, nil
		}
		if time.Now().After(deadline) {
			return nil, os.ErrPermission
		}
		time.Sleep(50 * time.Millisecond)
	}
}

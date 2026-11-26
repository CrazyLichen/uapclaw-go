//go:build !windows

package evolution

import (
	"os"
	"syscall"
	"time"
)

// acquireFileLockDefault 成员拷贝互斥文件锁（syscall.Flock 独占）。
// 对齐 Python portalocker.Lock(timeout=30)。
func acquireFileLockDefault(lockPath string) (func(), error) {
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = fd.Close()
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN)
		_ = fd.Close()
	}, nil
}

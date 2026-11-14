package processcli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// 本文件是 channels/process_cli/session_guard.py 的 Go 对应物。
//
// Python: channels/process_cli/session_guard.py（119 行）
//
// 职责：一个 Process CLI machine Session 的跨进程归属。
// SessionLease 持有 OS 锁直到 Runtime 与其 Session 关闭；
// BindAgent 把会话钉到其声明的根 Agent。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// leaseLockDirName 锁文件目录名。
	// Python: session_guard.py:34 ".process_cli_locks"
	leaseLockDirName = ".process_cli_locks"
	// bindingDirName 绑定文件目录名。
	// Python: session_guard.py:16 _BINDING_DIR
	bindingDirName = ".process_cli_bindings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionGuardError 会话归属冲突的安全机器错误。
// Python: session_guard.py:19-26 SessionGuardError
type SessionGuardError struct {
	// Message 错误消息（保留 Python 原文，协议字符串不本地化）
	Message string
	// Code 稳定错误码
	Code string
	// Retryable 是否可重试
	Retryable bool
}

// sessionLease 一个会话的跨进程排它租约。
// Python: session_guard.py:28-58 SessionLease
type SessionLease struct {
	// sessionID 会话标识（Python :31 同名字段，存而不用）
	sessionID string
	// lockPath 锁文件路径
	lockPath string
	// handle 已打开的锁文件句柄（acquire 成功后非 nil）
	handle *os.File
	// acquired 是否当前持有（对齐 Python _acquired，release 幂等）
	acquired bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *SessionGuardError) Error() string { return e.Message }

// CodeValue 稳定错误码（exceptionInfo 的提取接口，Python error.code）。
func (e *SessionGuardError) CodeValue() string { return e.Code }

// NewSessionLease 构造租约（构造不做 I/O，锁在 Acquire 才加，对齐 Python 构造语义）。
// Python: session_guard.py:31-42
func NewSessionLease(sessionID string) *SessionLease {
	digest := sha256.Sum256([]byte(sessionID))
	lockPath := filepath.Join(lifecycle.GetAgentSessionsDir(), leaseLockDirName,
		fmt.Sprintf("%s.lock", hex.EncodeToString(digest[:])))
	return &SessionLease{sessionID: sessionID, lockPath: lockPath}
}

// Acquire 非阻塞加排它锁；冲突返回 SESSION_BUSY（可重试）。
// Python: session_guard.py:44-53
func (l *SessionLease) Acquire() error {
	// Python: :35 lock_dir.mkdir(parents=True, exist_ok=True)
	if err := os.MkdirAll(filepath.Dir(l.lockPath), 0o755); err != nil {
		return err
	}
	// Python: :36-41 portalocker.Lock(..., mode="a", timeout=0, LOCK_EX|LOCK_NB)
	handle, err := os.OpenFile(l.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := lockSessionFileNonBlocking(handle); err != nil {
		_ = handle.Close()
		// Python: :47-52 except LockException → SessionGuardError("Session is already executing in another Process CLI call.", code="SESSION_BUSY", retryable=True)
		return &SessionGuardError{
			Message:   "Session is already executing in another Process CLI call.",
			Code:      "SESSION_BUSY",
			Retryable: true,
		}
	}
	l.handle = handle
	l.acquired = true
	return nil
}

// Release 幂等解锁。
// Python: session_guard.py:55-58
func (l *SessionLease) Release() {
	if l.acquired {
		unlockSessionFile(l.handle)
		_ = l.handle.Close()
		l.handle = nil
		l.acquired = false
	}
}

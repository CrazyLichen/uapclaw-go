package processcli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
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

// ──────────────────────────── 导出函数（BindAgent） ────────────────────────────

// BindAgent 把会话钉到其声明的根 Agent（在会话租约之下调用）。
// Python: session_guard.py:61-116 bind_agent
//
// definition 为 nil 时写入空指纹（fingerprint=null），仍占位防后续混用。
func BindAgent(sessionID string, definition map[string]any, resumed bool) error {
	// Python: :69-73 directory = get_agent_sessions_dir()/".process_cli_bindings"; target = <digest>.json
	digest := sha256.Sum256([]byte(sessionID))
	directory := filepath.Join(lifecycle.GetAgentSessionsDir(), bindingDirName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	target := filepath.Join(directory, hex.EncodeToString(digest[:])+".json")

	// Python: :74-79 fingerprint = from_mapping(definition).fingerprint if definition else None
	fingerprint := ""
	if definition != nil {
		mapped, err := swruntime.RuntimeAgentDefinitionFromMapping(definition)
		if err != nil {
			return err
		}
		value, err := swruntime.RuntimeAgentDefinitionFingerprint(mapped)
		if err != nil {
			return err
		}
		fingerprint = value
	}
	// Python: :79 expected = {"schema": 1, "fingerprint": fingerprint}
	// fingerprint 为空时用 nil，marshal 出 null，对齐 Python None
	var fingerprintValue any = nil
	if fingerprint != "" {
		fingerprintValue = fingerprint
	}
	expected := map[string]any{"schema": 1, "fingerprint": fingerprintValue}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return err
	}

	// Python: :80-92 if target.exists(): 比对
	if _, statErr := os.Stat(target); statErr == nil {
		raw, readErr := os.ReadFile(target)
		if readErr != nil {
			// Python: :83-86 OSError → AGENT_BINDING_INVALID
			return &SessionGuardError{Message: "Session Agent binding is unreadable.", Code: "AGENT_BINDING_INVALID"}
		}
		var stored map[string]any
		if jsonErr := json.Unmarshal(raw, &stored); jsonErr != nil {
			// Python: :83-86 UnicodeError/JSONDecodeError → AGENT_BINDING_INVALID
			return &SessionGuardError{Message: "Session Agent binding is unreadable.", Code: "AGENT_BINDING_INVALID"}
		}
		// Python: :87-91 stored != expected → AGENT_DEFINITION_SESSION_CONFLICT
		storedJSON, marshalErr := json.Marshal(stored)
		if marshalErr != nil || string(storedJSON) != string(expectedJSON) {
			return &SessionGuardError{Message: "Session is bound to another Agent definition.", Code: "AGENT_DEFINITION_SESSION_CONFLICT"}
		}
		return nil
	}

	// Python: :94-106 if resumed and definition is not None: 已有消息会话不得收养自定义 Agent
	// metadata 读取用绕缓存直读（cache_bust=True + enable_writeback=False 的等价物）。
	if resumed && definition != nil {
		metadata := sessionpkg.ReadSessionMetadata(sessionpkg.GetSessionsDir(), sessionID)
		if metadataMessageCount(metadata) > 0 {
			return &SessionGuardError{Message: "An existing unbound Session cannot adopt a custom Agent.", Code: "AGENT_DEFINITION_SESSION_CONFLICT"}
		}
	}

	// Python: :108-116 tmp(uuid 串后缀) 原子写 + finally 清理
	temporary := filepath.Join(directory,
		fmt.Sprintf("%s.%s.tmp", filepath.Base(target), uuid.NewString()))
	cleanup := func() { _ = os.Remove(temporary) }
	stream, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		cleanup()
		return err
	}
	if _, err := stream.Write(expectedJSON); err != nil {
		_ = stream.Close()
		cleanup()
		return err
	}
	if err := stream.Sync(); err != nil {
		_ = stream.Close()
		cleanup()
		return err
	}
	if err := stream.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		cleanup()
		return err
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// metadataMessageCount 读 metadata 的 message_count（JSON 数值解出 float64）。
// Python: :102 int(metadata.get("message_count") or 0)
func metadataMessageCount(metadata map[string]any) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata["message_count"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

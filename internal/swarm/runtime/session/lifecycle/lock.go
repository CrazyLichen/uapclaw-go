package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sharingRetryAttempts 原子替换窗口的读取重试上限。
	// Python: lifecycle.py _SHARING_RETRY_ATTEMPTS = 100
	//
	// 原子写者用 tmp+rename 发布；未加锁的读者若落在替换窗口内会收到
	// 共享冲突（Windows 上表现为 PermissionError），此时应重试而不是
	// 让整个请求失败。上限存在是为了让真正不可读的文件快速失败。
	sharingRetryAttempts = 100
	// sharingRetryMaxSleep 单次重试最大退避
	sharingRetryMaxSleep = 10 * time.Millisecond
	// sharingRetryBaseSleep 退避基数
	sharingRetryBaseSleep = time.Millisecond
)

// ──────────────────────────── 结构体 ────────────────────────────

// processFileLock 进程内 + 跨进程文件锁。
type processFileLock struct {
	// mu 进程内互斥
	mu sync.Mutex
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// goroutineLockState 协程 → 已持有的锁路径集合。
	//
	// 可重入必须按"持有者"判定：Python 用 threading.local，Go 没有协程局部存储，
	// 因此用协程 ID 显式分表。若只用一个全局集合，A 协程持锁时 B 协程会误判
	// "已持有"而直接放行，锁形同虚设。
	goroutineLockState sync.Map
	// processLocks 进程内锁表（键为资源路径）
	processLocks sync.Map
	// goroutineSeq 协程 ID 分配器
	goroutineSeq atomic.Uint64
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadJSON 读 JSON 文件；不存在返回空 map。
// Python: lifecycle.py read_json
//
// 内容不是对象时返回 CONFLICT（上层据此判定数据损坏）。
func ReadJSON(path string) (map[string]any, error) {
	if !pathExists(path) {
		return map[string]any{}, nil
	}
	raw, err := readFileRideOutSharing(path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &LifecycleError{
			Code:    "CONFLICT",
			Message: fmt.Sprintf("invalid lifecycle data: %s", filepath.Base(path)),
		}
	}
	return out, nil
}

// AtomicJSON 原子写 JSON：临时文件 + fsync + rename。
// Python: lifecycle.py atomic_json
func AtomicJSON(path string, value map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path),
		fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), time.Now().UnixNano()))
	handle, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = handle.Close()
		_ = os.Remove(temporary)
	}()
	if _, err := handle.Write(raw); err != nil {
		return err
	}
	if err := handle.Sync(); err != nil {
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// ResourceLock 获取资源跨进程锁，返回释放函数。
// Python: lifecycle.py resource_lock（同路径可重入）
//
// 可重入语义与上游一致：同一协程重复获取同一资源不重复加锁，
// 否则 save_locked 在其调用方已持锁时会自锁。
func ResourceLock(kind, resourceID string) (func(), error) {
	path, err := ResourcePath(kind, resourceID)
	if err != nil {
		return nil, err
	}
	gid := currentGoroutineID()
	if held, ok := goroutineLockState.Load(gid); ok {
		if _, exists := held.(*sync.Map).Load(path); exists {
			return func() {}, nil
		}
	}
	value, _ := processLocks.LoadOrStore(path, &processFileLock{})
	lock := value.(*processFileLock)
	lock.mu.Lock()

	handle, openErr := openLockFile(path)
	if openErr != nil {
		lock.mu.Unlock()
		return nil, openErr
	}
	if lockErr := lockFileHandle(handle); lockErr != nil {
		_ = handle.Close()
		lock.mu.Unlock()
		return nil, lockErr
	}
	setGoroutineHeld(gid, path, true)
	return func() {
		setGoroutineHeld(gid, path, false)
		unlockFileHandle(handle)
		_ = handle.Close()
		lock.mu.Unlock()
	}, nil
}

// FenceWrites 给资源加写围栏；project 类型还会级联加围栏到其下会话。
// Python: lifecycle.py fence_writes
//
// 级联原因：项目的写围栏必须同时隔离其成员会话的写入，否则项目删除期间
// 成员会话仍能落盘。级联时同时递增会话代次，使在飞的旧代写入被拒。
func FenceWrites(kind, resourceID string) error {
	release, err := ResourceLock(kind, resourceID)
	if err != nil {
		return err
	}
	value, readErr := stateLocked(kind, resourceID)
	if readErr != nil {
		release()
		return readErr
	}
	value["write_blocked"] = true
	SaveLocked(kind, resourceID, value)
	release()

	if kind != "project" {
		return nil
	}
	active := GetAgentSessionsDir()
	archived := filepath.Join(filepath.Dir(active), "sessions_archived")
	for _, root := range []string{active, archived} {
		entries, readDirErr := os.ReadDir(root)
		if readDirErr != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionID := entry.Name()
			meta, metaErr := RawMetadata(sessionID)
			if metaErr != nil {
				continue
			}
			projectID := ProjectIDFor(meta, nil)
			if projectID != resourceID {
				continue
			}
			childRelease, childErr := ResourceLock("session", sessionID)
			if childErr != nil {
				continue
			}
			child, childReadErr := stateLocked("session", sessionID)
			if childReadErr == nil {
				generation := asInt(child["generation"]) + 1
				child["generation"] = generation
				if operation, ok := child["operation"].(map[string]any); ok {
					operation["generation"] = generation
				}
				SaveLocked("session", sessionID, child)
			}
			childRelease()
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readFileRideOutSharing 读文件内容，重试瞬时共享冲突。
// Python: lifecycle.py _read_text_ride_out_sharing
func readFileRideOutSharing(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < sharingRetryAttempts; attempt++ {
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !os.IsPermission(err) {
			return nil, err
		}
		sleep := time.Duration(attempt+1) * sharingRetryBaseSleep
		if sleep > sharingRetryMaxSleep {
			sleep = sharingRetryMaxSleep
		}
		time.Sleep(sleep)
	}
	return nil, lastErr
}

// openLockFile 打开（必要时创建）锁文件。
//
// 锁文件放在 resources/<kind>_<id>.lock，与 JSON 同目录不同名，
// 避免原子替换 JSON 时把锁 inode 换掉。
func openLockFile(resourcePathValue string) (*os.File, error) {
	lockPath := resourcePathValue + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
}

// asInt 把 JSON 解出的数值转 int。
func asInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

// currentGoroutineID 取当前协程标识。
//
// Go 不暴露 goroutine ID，标准做法是从 runtime.Stack 的首行解析
// "goroutine <id> ["。这里只用于锁归属判定，不参与任何业务逻辑，
// 解析失败时退化为 0（等价于"不认为已持有"，只是失去可重入）。
func currentGoroutineID() uint64 {
	var buffer [64]byte
	n := runtime.Stack(buffer[:], false)
	text := string(buffer[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(text, prefix) {
		return 0
	}
	rest := text[len(prefix):]
	spaceIndex := strings.IndexByte(rest, ' ')
	if spaceIndex <= 0 {
		return 0
	}
	id, err := strconv.ParseUint(rest[:spaceIndex], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// setGoroutineHeld 记录/清除某协程对某路径的持有状态。
func setGoroutineHeld(gid uint64, path string, held bool) {
	if held {
		value, _ := goroutineLockState.LoadOrStore(gid, &sync.Map{})
		value.(*sync.Map).Store(path, struct{}{})
		return
	}
	value, ok := goroutineLockState.Load(gid)
	if !ok {
		return
	}
	paths := value.(*sync.Map)
	paths.Delete(path)
	empty := true
	paths.Range(func(any, any) bool {
		empty = false
		return false
	})
	if empty {
		goroutineLockState.Delete(gid)
	}
}

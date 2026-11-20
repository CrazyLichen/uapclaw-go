package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeLockTimeout 仓库锁在超时内未获取。
// Python: lock.py WorktreeLockTimeout(TimeoutError)
//
// 显式报错而非降级为无锁——降级会重新引入本锁要防的竞态。
type WorktreeLockTimeout struct {
	// Message 超时描述（含 repo root 与秒数）
	Message string
}

// Error 实现 error 接口。
func (e *WorktreeLockTimeout) Error() string { return e.Message }

// Unwrap 使 errors.Is 判定哨兵 ErrWorktreeLockTimeout 成立。
func (e *WorktreeLockTimeout) Unwrap() error { return ErrWorktreeLockTimeout }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// lockFileName 跨进程锁文件名（对齐上游字面量，勿改品牌改名规则例外保留）
	lockFileName = "openjiuwen-worktree.lock"
	// lockPollInterval 跨进程锁轮询间隔
	lockPollInterval = 50 * time.Millisecond
)

// ErrWorktreeLockTimeout WorktreeLockTimeout 的哨兵，供 errors.Is 判定。
var ErrWorktreeLockTimeout = errors.New("worktree lock timeout")

// ──────────────────────────── 全局变量 ────────────────────────────

// lockBuckets 按 repo_root 分桶的进程内锁。
// Python: lock.py 模块级 _INPROCESS_LOCKS dict + _BUCKET_GUARD；
// 模块级原因：with_worktrees_dir / with_base_dir 经 config 拷贝构建新 manager 实例，
// 实例级锁无法跨这些视图共享。
var lockBuckets sync.Map // map[string]*sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// RepoLock 对 repo_root 的 worktree 关键段做串行化。
// Python: lock.py repo_lock(repo_root, *, timeout, enabled)
//
// 双层互斥锁：先进程内、后跨进程，按相反顺序释放（无死锁可能）。
// 返回释放闭包，调用方 `defer release()`（Python async with 上下文管理器的 Go 等价）。
//
// 偏差 D-25.7-d：跨进程锁 Python 用 filelock.AsyncFileLock，Go 用
// syscall.Flock 非阻塞重试——锁文件路径与文件名一比一，但锁文件字节内容
// 格式不互通；同机全 Go 进程无影响。
//
// 参数：
//   - repoRoot: 规范 git 仓库根。不同 root 永不争用。
//   - timeoutSeconds: 双层获取的总等待秒数，超时返回 WorktreeLockTimeout。
//   - enabled: false 时为 no-op（远端后端自行管理隔离时禁用）。
func RepoLock(repoRoot string, timeoutSeconds float64, enabled bool) (release func(), err error) {
	if !enabled {
		return func() {}, nil
	}

	inproc := inprocessLock(repoRoot)
	lockFile := lockFilePath(repoRoot)

	// 父目录不存在时尽力创建；真正失败由后续加锁报错
	// Python: os.makedirs(parent, exist_ok=True) 吞 OSError
	if parent := filepath.Dir(lockFile); parent != "" {
		_ = os.MkdirAll(parent, 0o755)
	}

	deadline := time.Now().Add(time.Duration(timeoutSeconds * float64(time.Second)))

	// 第 1 层：进程内（Python asyncio.timeout 包裹 acquire 的等价：
	// TryLock 轮询保证 deadline 可生效，阻塞 Lock 会无限等待错过超时）
	inprocMu := inproc
	inprocAcquired := false
	for {
		if inprocMu.TryLock() {
			inprocAcquired = true
			break
		}
		if time.Now().After(deadline) {
			return nil, &WorktreeLockTimeout{Message: fmt.Sprintf(
				"Timed out waiting for in-process worktree lock on %s after %gs", repoRoot, timeoutSeconds)}
		}
		time.Sleep(lockPollInterval)
	}

	defer func() {
		if err != nil && inprocAcquired {
			inprocMu.Unlock()
		}
	}()

	// 第 2 层：跨进程 flock 重试
	fd, ferr := acquireFileLock(lockFile, deadline)
	if ferr != nil {
		return nil, &WorktreeLockTimeout{Message: ferr.Error()}
	}

	return func() {
		// 逆序释放：先跨进程、后进程内（Python finally 两段语义）
		releaseFileLock(fd, lockFile)
		if inprocAcquired {
			inprocMu.Unlock()
		}
	}, nil
}

// ClearLockBuckets 清空进程内锁桶。仅测试用；不得在有操作在飞时调用。
// Python: lock.py clear_lock_buckets
func ClearLockBuckets() {
	lockBuckets.Range(func(k, _ any) bool {
		lockBuckets.Delete(k)
		return true
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lockFilePath 跨进程锁文件路径：<repo_root>/.git/ 下；无 .git 目录时放 repo_root 旁。
// Python: lock.py _lock_file_path(repo_root)
func lockFilePath(repoRoot string) string {
	gitDir := filepath.Join(repoRoot, ".git")
	if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
		return filepath.Join(gitDir, lockFileName)
	}
	return filepath.Join(repoRoot, "."+lockFileName)
}

// inprocessLock 返回 repo_root 共享的进程内锁。
// Python: lock.py _get_inprocess_lock(repo_root)
func inprocessLock(repoRoot string) *sync.Mutex {
	v, _ := lockBuckets.LoadOrStore(repoRoot, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// acquireFileLock 以 flock 非阻塞重试获取跨进程排它锁直至 deadline。
// Python: filelock.AsyncFileLock.acquire 的 Go 等价（D-25.7-d）。
// PermissionError 语义归并到超时错误：锁文件不可创建/写入时同样显式报错
// 而非降级无锁（Python 将 PermissionError 转 WorktreeLockTimeout，:105-111）。
func acquireFileLock(lockFile string, deadline time.Time) (*os.File, error) {
	for {
		fd, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, fmt.Errorf(
				"Cannot acquire cross-process worktree lock (%s): permission denied. "+
					"Set repo_lock=False if this repo_root has no cross-process contention: %w",
				lockFile, err)
		}
		if err := flockExclusive(fd); err == nil {
			return fd, nil
		}
		_ = fd.Close()
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"Timed out waiting for cross-process worktree lock (%s)", lockFile)
		}
		time.Sleep(lockPollInterval)
	}
}

// releaseFileLock 释放跨进程锁；释放失败仅记 debug（Python release best-effort，:117-119）。
func releaseFileLock(fd *os.File, lockFile string) {
	if fd == nil {
		return
	}
	if err := flockRelease(fd); err != nil {
		logger.Debug(logComponent).Str("lock_file", lockFile).Err(err).
			Msg("Failed to release cross-process worktree lock")
	}
	_ = fd.Close()
}

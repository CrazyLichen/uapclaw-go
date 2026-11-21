package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestRepoLock_并发串行化 同 repo 的锁必须互斥，释放后第二个持有者才能进入。
// Python: lock.py repo_lock 双层互斥语义
func TestRepoLock_并发串行化(t *testing.T) {
	root := t.TempDir()

	release1, err := RepoLock(root, 120.0, true)
	if err != nil {
		t.Fatalf("首次获取失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		release2, err := RepoLock(root, 5.0, true)
		if err == nil {
			defer release2()
		}
		done <- err
	}()

	time.Sleep(100 * time.Millisecond) // 第二个锁应阻塞在等待
	select {
	case err := <-done:
		t.Fatalf("持有期间不应能获取锁: %v", err)
	default:
	}

	release1() // 释放后第二个锁才能拿到
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("释放后应能获取: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("释放后 2s 内未获取到锁")
	}
}

// TestRepoLock_超时返回WorktreeLockTimeout 超时必须显式报错而非静默降级无锁。
// Python: lock.py WorktreeLockTimeout("Timed out waiting ...")
func TestRepoLock_超时返回WorktreeLockTimeout(t *testing.T) {
	root := t.TempDir()

	release, err := RepoLock(root, 10.0, true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = RepoLock(root, 0.1, true)
	var te *WorktreeLockTimeout
	if !errors.As(err, &te) {
		t.Fatalf("期望 WorktreeLockTimeout, got %v", err)
	}
	if !errors.Is(err, ErrWorktreeLockTimeout) {
		t.Fatalf("应包裹哨兵 ErrWorktreeLockTimeout: %v", err)
	}
}

// TestRepoLock_禁用时直通 enabled=False 时为 no-op。
// Python: lock.py repo_lock "if not enabled: yield; return"
func TestRepoLock_禁用时直通(t *testing.T) {
	root := t.TempDir()

	release1, err := RepoLock(root, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	release1()

	// 同 root 禁用期间另一持有者可直接进入（无互斥）
	release2, err := RepoLock(root, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

// TestRepoLock_不同Repo不互斥 不同 repo_root 永不争用。
// Python: lock.py "Different roots never contend"
func TestRepoLock_不同Repo不互斥(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	releaseA, err := RepoLock(rootA, 5.0, true)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()

	releaseB, err := RepoLock(rootB, 1.0, true)
	if err != nil {
		t.Fatalf("不同 repo 不应互斥: %v", err)
	}
	releaseB()
}

// TestLockFilePath_Git目录存在时锁文件在其下 Python: _lock_file_path(repo_root)
func TestLockFilePath_Git目录存在时锁文件在其下(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := lockFilePath(root)
	want := filepath.Join(root, ".git", lockFileName)
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// TestLockFilePath_无Git目录时锁文件在repo旁 无 .git 目录时放 repo_root 下。
func TestLockFilePath_无Git目录时锁文件在repo旁(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plain")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	got := lockFilePath(root)
	want := filepath.Join(root, "."+lockFileName)
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// TestClearLockBuckets 清空进程内锁桶（测试专用）。
// Python: lock.py clear_lock_buckets
func TestClearLockBuckets(t *testing.T) {
	root := t.TempDir()
	release, err := RepoLock(root, 5.0, true)
	if err != nil {
		t.Fatal(err)
	}
	release()

	ClearLockBuckets()

	var n int
	lockBuckets.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		t.Fatalf("清空后桶数应为 0, got %d", n)
	}
}

// TestRepoLock_并发多持有者竞争 只有一个能赢，其余超时或排队成功。
func TestRepoLock_并发多持有者竞争(t *testing.T) {
	root := t.TempDir()
	const n = 5
	var wg sync.WaitGroup
	winners := make(chan struct{}, n)
	release, err := RepoLock(root, 10.0, true)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := RepoLock(root, 10.0, true)
			if err != nil {
				return
			}
			winners <- struct{}{}
			time.Sleep(10 * time.Millisecond)
			r()
		}()
	}
	release()
	wg.Wait()
	close(winners)

	count := 0
	for range winners {
		count++
	}
	if count != n {
		t.Fatalf("全部持有者最终都应串行进入, got %d/%d", count, n)
	}
}

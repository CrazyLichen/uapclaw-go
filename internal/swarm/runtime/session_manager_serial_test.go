package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSessionManager_同会话两次rename串行 spec §6 要求的并发回归。
//
// 场景：前端连点两次重命名，两条请求都是"非 chat RPC"，由门面 SessionManager
// 排队。若串行失守，两次读-改-写会互相覆盖，后写的标题丢失。
// 这里用并发提交 + 共享可变状态来验证：只有在同一时刻只有一个任务在跑，
// 计数才不会出现两个任务同时进入临界区。
func TestSessionManager_同会话两次rename串行(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()
	const sessionID = "s1"

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var titles []string
	var mu sync.Mutex

	rename := func(title string) func(context.Context) (any, error) {
		return func(context.Context) (any, error) {
			current := inFlight.Add(1)
			for {
				observed := maxInFlight.Load()
				if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
					break
				}
			}
			// 模拟读-改-写窗口
			time.Sleep(15 * time.Millisecond)
			mu.Lock()
			titles = append(titles, title)
			mu.Unlock()
			inFlight.Add(-1)
			return title, nil
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, title := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			_, _ = manager.SubmitAndWait(ctx, sessionID, rename(name))
		}(title)
	}
	close(start)
	wg.Wait()

	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("同会话任务必须串行，实测最大并发 %d", got)
	}
	if len(titles) != 2 {
		t.Fatalf("两个重命名都应完成，实际 %d", len(titles))
	}
	manager.CloseAllSessions(ctx)
}

// TestSessionManager_不同会话rename可并发 反证串行只针对同会话。
func TestSessionManager_不同会话rename可并发(t *testing.T) {
	manager := NewSessionManager()
	ctx := context.Background()

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	work := func(context.Context) (any, error) {
		current := inFlight.Add(1)
		for {
			observed := maxInFlight.Load()
			if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inFlight.Add(-1)
		return nil, nil
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, sid := range []string{"a", "b"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			<-start
			_, _ = manager.SubmitAndWait(ctx, sessionID, work)
		}(sid)
	}
	close(start)
	wg.Wait()

	if got := maxInFlight.Load(); got < 2 {
		t.Errorf("不同会话应可并发，实测最大并发 %d", got)
	}
	manager.CloseAllSessions(ctx)
}

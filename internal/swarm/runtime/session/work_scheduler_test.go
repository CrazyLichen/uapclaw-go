package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

// newTestScheduler 构造调度器。
func newTestScheduler() *SessionWorkScheduler {
	return NewSessionWorkScheduler()
}

// newSchedHandle 构造调度用句柄。
func newSchedHandle(eid, sid string, gen int, kind SessionWorkKind) *SessionExecutionHandle {
	h := NewSessionExecutionHandle(eid, sid, "r-"+eid, gen, kind, "web")
	return h
}

// TestScheduler_提交并等待返回结果 校验基本执行。
func TestScheduler_提交并等待返回结果(t *testing.T) {
	s := newTestScheduler()
	h := newSchedHandle("e1", "s1", 1, SessionWorkKindChatStream)
	got, err := s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
		return "ok", nil
	})
	if err != nil || got != "ok" {
		t.Fatalf("SubmitAndWait = (%v, %v), want (ok, nil)", got, err)
	}
	s.Close(time.Second)
}

// TestScheduler_同会话串行 校验同一 lane 不并发。
func TestScheduler_同会话串行(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	var mu sync.Mutex
	var order []string
	var inFlight int
	var maxInFlight int
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(nm string) {
			defer wg.Done()
			h := newSchedHandle("e-"+nm, "s1", 1, SessionWorkKindHeartbeat)
			_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
				mu.Lock()
				inFlight++
				if inFlight > maxInFlight {
					maxInFlight = inFlight
				}
				order = append(order, nm)
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				inFlight--
				mu.Unlock()
				return nm, nil
			})
		}(name)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("三个任务都应执行，实际 %v", order)
	}
	if maxInFlight != 1 {
		t.Fatalf("同会话必须串行，最大并发应为 1，实际 %d", maxInFlight)
	}
}

// TestScheduler_不同会话并行 校验跨 lane 不互相阻塞。
func TestScheduler_不同会话并行(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, sid := range []string{"s1", "s2"} {
		wg.Add(1)
		go func(session string) {
			defer wg.Done()
			h := newSchedHandle("e-"+session, session, 1, SessionWorkKindHeartbeat)
			_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
				started <- struct{}{}
				<-gate // 等两边都起来
				return nil, nil
			})
		}(sid)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("不同会话应并行执行，实际被串行化")
		}
	}
	close(gate)
	wg.Wait()
}

// TestScheduler_两级优先级 校验 latest_first 类先执行、普通类 FIFO。
//
// spec §8 D-5：这是相对旧 SessionManager（一律 LIFO）的行为变化。
func TestScheduler_两级优先级(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context) (any, error) {
		return func(context.Context) (any, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil, nil
		}
	}

	// 先占住 lane
	release := make(chan struct{})
	blockingDone := make(chan struct{})
	go func() {
		defer close(blockingDone)
		h := newSchedHandle("e-block", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			<-release
			return nil, nil
		})
	}()
	time.Sleep(80 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		h := newSchedHandle("e-h1", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, record("hb1"))
	}()
	time.Sleep(15 * time.Millisecond)
	go func() {
		defer wg.Done()
		h := newSchedHandle("e-h2", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, record("hb2"))
	}()
	time.Sleep(15 * time.Millisecond)
	go func() {
		defer wg.Done()
		h := newSchedHandle("e-c1", "s1", 1, SessionWorkKindChatStream)
		_, _ = s.SubmitAndWait(context.Background(), h, record("chat"))
	}()
	time.Sleep(80 * time.Millisecond)
	close(release)
	wg.Wait()
	<-blockingDone

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("三个任务都应执行，实际 %v", order)
	}
	if order[0] != "chat" {
		t.Fatalf("latest_first 类应先执行（group=0），实际顺序 %v", order)
	}
	if order[1] != "hb1" || order[2] != "hb2" {
		t.Fatalf("普通类应按 FIFO，实际 %v", order)
	}
}

// TestScheduler_取消排队项 校验排队中的能被摘掉不执行。
func TestScheduler_取消排队项(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	release := make(chan struct{})
	go func() {
		h := newSchedHandle("e-block", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			<-release
			return nil, nil
		})
	}()
	time.Sleep(80 * time.Millisecond)

	queued := newSchedHandle("e-q", "s1", 1, SessionWorkKindHeartbeat)
	var executed bool
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.SubmitAndWait(context.Background(), queued, func(context.Context) (any, error) {
			mu.Lock()
			executed = true
			mu.Unlock()
			return nil, nil
		})
	}()
	time.Sleep(40 * time.Millisecond)
	s.CancelHandles(context.Background(), []*SessionExecutionHandle{queued}, time.Second)
	close(release)
	<-done

	mu.Lock()
	defer mu.Unlock()
	if executed {
		t.Fatal("取消的排队项不应被执行")
	}
}

// TestScheduler_取消运行项返回超时标识 校验运行中的能被取消。
func TestScheduler_取消运行项返回超时标识(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	h := newSchedHandle("e1", "s1", 1, SessionWorkKindHeartbeat)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			close(started)
			<-release // 故意不检查 ctx，模拟「不响应取消」
			return nil, nil
		})
	}()
	<-started

	timedOut := s.CancelHandles(context.Background(), []*SessionExecutionHandle{h}, 100*time.Millisecond)
	if len(timedOut) != 1 || timedOut[0] != "e1" {
		t.Fatalf("不响应取消的执行应返回超时标识，实际 %v", timedOut)
	}
	close(release)
	<-done
}

// TestScheduler_CloseSession摘掉排队并停lane 校验关闭语义。
func TestScheduler_CloseSession摘掉排队并停lane(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	blocking := newSchedHandle("e-block", "s1", 1, SessionWorkKindHeartbeat)
	go func() {
		_, _ = s.SubmitAndWait(context.Background(), blocking, func(context.Context) (any, error) {
			time.Sleep(300 * time.Millisecond)
			return nil, nil
		})
	}()
	time.Sleep(80 * time.Millisecond)

	var mu sync.Mutex
	var queuedRan bool
	queued := newSchedHandle("e-q", "s1", 1, SessionWorkKindHeartbeat)
	queuedDone := make(chan struct{})
	go func() {
		defer close(queuedDone)
		_, _ = s.SubmitAndWait(context.Background(), queued, func(context.Context) (any, error) {
			mu.Lock()
			queuedRan = true
			mu.Unlock()
			return nil, nil
		})
	}()
	time.Sleep(40 * time.Millisecond)

	existed, _ := s.CloseSession("s1", nil, 200*time.Millisecond)
	if !existed {
		t.Fatal("lane 应存在")
	}
	<-queuedDone
	mu.Lock()
	defer mu.Unlock()
	if queuedRan {
		t.Fatal("关闭 lane 时排队的任务不应执行")
	}
}

// TestScheduler_CloseSession代次不符返回不存在 校验代次判定。
func TestScheduler_CloseSession代次不符返回不存在(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	h := newSchedHandle("e1", "s1", 1, SessionWorkKindHeartbeat)
	go func() {
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			time.Sleep(100 * time.Millisecond)
			return nil, nil
		})
	}()
	time.Sleep(30 * time.Millisecond)
	other := 9
	existed, _ := s.CloseSession("s1", &other, time.Second)
	if existed {
		t.Fatal("代次不符时应返回 existed=false")
	}
}

// TestScheduler_Close后拒绝提交 校验关闭语义。
func TestScheduler_Close后拒绝提交(t *testing.T) {
	s := newTestScheduler()
	s.Close(time.Second)
	_, err := s.SubmitAndWait(context.Background(), newSchedHandle("e1", "s1", 1, SessionWorkKindHeartbeat),
		func(context.Context) (any, error) { return nil, nil })
	if err == nil {
		t.Fatal("关闭后提交应报错")
	}
	if err.Error() != "session scheduler is closed" {
		t.Fatalf("错误文案 = %q", err.Error())
	}
}

// TestScheduler_代次不符提交报错 校验 lane 代次绑定。
func TestScheduler_代次不符提交报错(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	h1 := newSchedHandle("e1", "s1", 1, SessionWorkKindHeartbeat)
	go func() {
		_, _ = s.SubmitAndWait(context.Background(), h1, func(context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		})
	}()
	time.Sleep(50 * time.Millisecond)

	h2 := newSchedHandle("e2", "s1", 2, SessionWorkKindHeartbeat)
	_, err := s.SubmitAndWait(context.Background(), h2, func(context.Context) (any, error) { return nil, nil })
	if err == nil {
		t.Fatal("代次不符应报错")
	}
	if want := "session generation mismatch: s1 expected=1 actual=2"; err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
}

// TestScheduler_提交方ctx取消时放弃等待 校验调用方上下文。
func TestScheduler_提交方ctx取消时放弃等待(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	release := make(chan struct{})
	h := newSchedHandle("e1", "s1", 1, SessionWorkKindHeartbeat)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := s.SubmitAndWait(ctx, h, func(context.Context) (any, error) {
			<-release
			return nil, nil
		})
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("ctx 取消后应返回错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 SubmitAndWait 应立即返回")
	}
	close(release)
}

// TestScheduler_旧LIFO到两级规则的行为变化 固化 spec §8 D-5。
//
// 旧 SessionManager 对所有任务一律 LIFO（后到先跑）；改为上游两级规则后：
// CHAT_UNARY / CHAT_STREAM 走 group=0 且 priority 递减（仍是后到先跑），
// 其余走 group=1、priority=0、按 sequence FIFO（**变先进先跑**）。
// 本用例锁死后半句，防止回归。
func TestScheduler_旧LIFO到两级规则的行为变化(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context) (any, error) {
		return func(context.Context) (any, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil, nil
		}
	}

	release := make(chan struct{})
	blockingDone := make(chan struct{})
	go func() {
		defer close(blockingDone)
		h := newSchedHandle("e-block", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			<-release
			return nil, nil
		})
	}()
	time.Sleep(80 * time.Millisecond)

	var wg sync.WaitGroup
	for _, name := range []string{"hb1", "hb2", "hb3"} {
		wg.Add(1)
		go func(nm string) {
			defer wg.Done()
			h := newSchedHandle("e-"+nm, "s1", 1, SessionWorkKindHeartbeat)
			_, _ = s.SubmitAndWait(context.Background(), h, record(nm))
		}(name)
		time.Sleep(15 * time.Millisecond) // 保证提交顺序确定
	}
	time.Sleep(80 * time.Millisecond)
	close(release)
	wg.Wait()
	<-blockingDone

	mu.Lock()
	defer mu.Unlock()
	want := []string{"hb1", "hb2", "hb3"}
	if len(order) != 3 {
		t.Fatalf("三个任务都应执行，实际 %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("普通类应 FIFO（旧行为是 LIFO），实际 %v，期望 %v", order, want)
		}
	}
}

// TestScheduler_聊天类仍为后到先跑 固化 latest_first 分支。
func TestScheduler_聊天类仍为后到先跑(t *testing.T) {
	s := newTestScheduler()
	defer s.Close(time.Second)

	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context) (any, error) {
		return func(context.Context) (any, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil, nil
		}
	}

	release := make(chan struct{})
	blockingDone := make(chan struct{})
	go func() {
		defer close(blockingDone)
		h := newSchedHandle("e-block", "s1", 1, SessionWorkKindHeartbeat)
		_, _ = s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
			<-release
			return nil, nil
		})
	}()
	time.Sleep(80 * time.Millisecond)

	var wg sync.WaitGroup
	for _, name := range []string{"chat1", "chat2"} {
		wg.Add(1)
		go func(nm string) {
			defer wg.Done()
			h := newSchedHandle("e-"+nm, "s1", 1, SessionWorkKindChatStream)
			_, _ = s.SubmitAndWait(context.Background(), h, record(nm))
		}(name)
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	close(release)
	wg.Wait()
	<-blockingDone

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 {
		t.Fatalf("两个任务都应执行，实际 %v", order)
	}
	if order[0] != "chat2" {
		t.Fatalf("聊天类应后到先跑，实际 %v", order)
	}
}

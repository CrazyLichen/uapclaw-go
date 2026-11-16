package tasks

import (
	"context"
	"testing"
	"time"
)

// TestDbgResumeRepro 复现挂起恢复调度间歇失败。
func TestDbgResumeRepro(t *testing.T) {
	for iter := 0; iter < 30; iter++ {
		func() {
			gate1, gate2 := make(chan struct{}), make(chan struct{})
			executor := &fakeExecutor{runResult: map[string]any{"answer": "done"}, runGates: []chan struct{}{gate1, gate2}}
			store := newTestStore(t)
			svc, err := NewTaskService(store, executor, nil, 2, 2*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Close()
			if err := svc.Start(); err != nil {
				t.Fatal(err)
			}
			task, _ := svc.Submit("u", "s", "b1", "任务", nil)
			// 等首轮 running
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				current, _ := store.Read(toString(task["id"]), nil, nil)
				if toString(current["status"]) == "running" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			close(gate1)
			// 等 completed
			deadline = time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				current, _ := store.Read(toString(task["id"]), nil, nil)
				if toString(current["status"]) == "completed" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			current, _ := store.Read(toString(task["id"]), nil, nil)
			if toString(current["status"]) != "completed" {
				t.Fatalf("iter=%d 未完成: %v", iter, current["status"])
			}
			_, _ = store.Update(toString(task["id"]), func(current map[string]any) {
				current["status"] = "waiting_user"
				current["output_closed"] = true
				current["execution_settled"] = true
				current["interaction"] = map[string]any{
					"id": "i2", "source": "ask_user_interrupt", "state": "pending", "request_id": "r1",
					"questions": []any{map[string]any{"question": "确认？"}},
				}
			})
			_, err = svc.Answer(context.Background(), "u", "s", toString(task["id"]), "b2", "i2",
				[]any{map[string]any{"question": "确认？", "answer": "确认"}}, false)
			if err != nil {
				t.Fatalf("iter=%d answer 错误: %v", iter, err)
			}
			// 等 running（带 Kick 泵）
			deadline = time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				svc.Kick()
				current, _ := store.Read(toString(task["id"]), nil, nil)
				st := toString(current["status"])
				if st == "running" || st == "completed" {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			curFinal, _ := store.Read(toString(task["id"]), nil, nil)
			t.Fatalf("iter=%d 未重派发: %v resume=%v workers=%d", iter,
				curFinal["status"], curFinal["resume_answer"], func() int { svc.mu.Lock(); defer svc.mu.Unlock(); return len(svc.workers) }())
		}()
	}
}

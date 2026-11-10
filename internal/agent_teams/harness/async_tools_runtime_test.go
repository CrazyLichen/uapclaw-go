package harness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// injectionRecorder 线程安全地收集完成注入文本（后台 goroutine 写、测试读）。
type injectionRecorder struct {
	mu    sync.Mutex
	items []string
}

func (r *injectionRecorder) add(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, text)
}

func (r *injectionRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

func newTestRuntime(t *testing.T, rec *injectionRecorder) *AsyncToolRuntime {
	t.Helper()
	return NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	})
}

func waitStatus(t *testing.T, rt *AsyncToolRuntime, id, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec := rt.Get(id); rec != nil && rec.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %s 未到状态 %s", id, want)
}

// waitInjected 等注入条数达到 n 并返回快照。
func waitInjected(t *testing.T, rec *injectionRecorder, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(rec.snapshot()) >= n {
			return rec.snapshot()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("注入条数未达到 %d，实际 %d", n, len(rec.snapshot()))
	return nil
}

// TestRuntime_成功完成注入 验证完成结果注入。
func TestRuntime_成功完成注入(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	rt.Launch("t1", func(context.Context) (any, error) {
		return map[string]any{"answer": 42}, nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo", Description: "d"})
	waitStatus(t, rt, "t1", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	require.Len(t, injected, 1)
	assert.Contains(t, injected[0], "42")
	assert.Contains(t, injected[0], "demo")
}

// TestRuntime_失败注入 验证失败记录与注入。
func TestRuntime_失败注入(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	rt.Launch("t2", func(context.Context) (any, error) {
		return nil, errors.New("nope")
	}, tools.AsyncToolLaunchOptions{ToolName: "demo", Description: "d"})
	waitStatus(t, rt, "t2", AsyncToolStatusError)
	assert.Contains(t, rt.Get("t2").Error, "nope")
	injected := waitInjected(t, rec, 1)
	require.Len(t, injected, 1)
	assert.Contains(t, injected[0], "nope")
}

// TestRuntime_自定义formatter 验证 FormatCompleted 覆盖默认文案。
func TestRuntime_自定义formatter(t *testing.T) {
	rec := &injectionRecorder{}
	rt := newTestRuntime(t, rec)
	rt.Launch("fmt", func(context.Context) (any, error) { return "payload", nil },
		tools.AsyncToolLaunchOptions{
			ToolName:        "demo",
			FormatCompleted: func(v any) string { return "custom-ok:" + v.(string) },
		})
	waitStatus(t, rt, "fmt", AsyncToolStatusCompleted)
	assert.Equal(t, []string{"custom-ok:payload"}, waitInjected(t, rec, 1))
}

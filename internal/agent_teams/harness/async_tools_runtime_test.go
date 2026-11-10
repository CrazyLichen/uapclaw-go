package harness

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

func newTestRuntime(t *testing.T, injected *[]string) *AsyncToolRuntime {
	t.Helper()
	return NewAsyncToolRuntime(func(_ context.Context, text string) error {
		*injected = append(*injected, text)
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

// TestRuntime_成功完成注入 验证完成结果注入。
func TestRuntime_成功完成注入(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	var injected []string
	rt := newTestRuntime(t, &injected)
	rt.Launch("t1", func(context.Context) (any, error) {
		return map[string]any{"answer": 42}, nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo", Description: "d"})
	waitStatus(t, rt, "t1", AsyncToolStatusCompleted)
	require.Len(t, injected, 1)
	assert.Contains(t, injected[0], "42")
	assert.Contains(t, injected[0], "demo")
}

// TestRuntime_失败注入 验证失败记录与注入。
func TestRuntime_失败注入(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	var injected []string
	rt := newTestRuntime(t, &injected)
	rt.Launch("t2", func(context.Context) (any, error) {
		return nil, errors.New("nope")
	}, tools.AsyncToolLaunchOptions{ToolName: "demo", Description: "d"})
	waitStatus(t, rt, "t2", AsyncToolStatusError)
	assert.Contains(t, rt.Get("t2").Error, "nope")
	require.Len(t, injected, 1)
	assert.Contains(t, injected[0], "nope")
}

// TestRuntime_自定义formatter 验证 FormatCompleted 覆盖默认文案。
func TestRuntime_自定义formatter(t *testing.T) {
	var injected []string
	rt := newTestRuntime(t, &injected)
	rt.Launch("fmt", func(context.Context) (any, error) { return "payload", nil },
		tools.AsyncToolLaunchOptions{
			ToolName:        "demo",
			FormatCompleted: func(v any) string { return "custom-ok:" + v.(string) },
		})
	waitStatus(t, rt, "fmt", AsyncToolStatusCompleted)
	assert.Equal(t, []string{"custom-ok:payload"}, injected)
}

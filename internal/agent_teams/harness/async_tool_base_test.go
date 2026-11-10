package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// fakeWorker 实现 AsyncToolWorker，验证 Invoke 动态派发到子类。
type fakeWorker struct {
	called bool
}

func (f *fakeWorker) RunBackground(_ context.Context, _ string, _ map[string]any) (any, error) {
	f.called = true
	return "ok", nil
}

// fakeHost 实现 tools.AsyncToolHost。
type fakeHost struct {
	launched bool
	taskID   string
}

func (f *fakeHost) LaunchAsyncTool(taskID string, _ func(context.Context) (any, error), _ tools.AsyncToolLaunchOptions) error {
	f.launched = true
	f.taskID = taskID
	return nil
}
func (f *fakeHost) SessionID() string                            { return "sess" }
func (f *fakeHost) SetAsyncToolOutputDirResolver(func() string)  {}

// TestAsyncTool_Invoke派发到worker 验证 Go 嵌入虚派发陷阱已规避。
func TestAsyncTool_Invoke派发到worker(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	host := &fakeHost{}
	worker := &fakeWorker{}
	base := NewAsyncTool(tool.NewToolCardWithID("team.demo", "demo", "d", nil, nil), host, schema.LanguageCN, worker)

	out, err := base.Invoke(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.True(t, host.launched)
	data, _ := out["data"].(map[string]any)
	assert.Equal(t, "launched", data["status"])
	assert.NotEmpty(t, host.taskID)
	assert.NotEmpty(t, base.MapResult(out))
}

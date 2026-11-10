package harness

import (
	"context"
	"errors"
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
	desc     string
	err      error
}

func (f *fakeHost) LaunchAsyncTool(taskID string, _ func(context.Context) (any, error), opts tools.AsyncToolLaunchOptions) error {
	f.launched = true
	f.taskID = taskID
	f.desc = opts.Description
	return f.err
}
func (f *fakeHost) SessionID() string                           { return "sess" }
func (f *fakeHost) SetAsyncToolOutputDirResolver(func() string) {}

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

// TestAsyncTool_Stream不支持 验证流式调用返回哨兵错误。
func TestAsyncTool_Stream不支持(t *testing.T) {
	base := NewAsyncTool(tool.NewToolCardWithID("team.demo", "demo", "d", nil, nil),
		&fakeHost{}, schema.LanguageCN, &fakeWorker{})
	_, err := base.Stream(context.Background(), nil)
	require.ErrorIs(t, err, tool.ErrStreamNotSupported)
}

// TestAsyncTool_Parent 验证 Parent / Language 访问器。
func TestAsyncTool_Parent(t *testing.T) {
	host := &fakeHost{}
	base := NewAsyncTool(tool.NewToolCardWithID("team.demo", "demo", "d", nil, nil),
		host, schema.LanguageCN, &fakeWorker{})
	assert.Same(t, host, base.Parent())
	assert.Equal(t, schema.LanguageCN, base.Language())
}

// TestAsyncTool_Invoke启动失败 验证 launch 失败时返回 success=false 且 MapResult 透传错误。
func TestAsyncTool_Invoke启动失败(t *testing.T) {
	host := &fakeHost{err: errors.New("boom")}
	base := NewAsyncTool(tool.NewToolCardWithID("team.demo", "demo", "d", nil, nil),
		host, schema.LanguageCN, &fakeWorker{})
	out, err := base.Invoke(context.Background(), map[string]any{})
	require.NoError(t, err)
	success, _ := out["success"].(bool)
	assert.False(t, success)
	assert.Contains(t, base.MapResult(out), "boom")
}

// descWorker 覆盖 LaunchedDescription（可选接口）。
type descWorker struct {
	fakeWorker
}

func (d *descWorker) LaunchedDescription(map[string]any) string { return "custom-desc" }

// TestAsyncTool_自定义launched描述 验证可选 LaunchedDescription 被使用。
func TestAsyncTool_自定义launched描述(t *testing.T) {
	host := &fakeHost{}
	base := NewAsyncTool(tool.NewToolCardWithID("team.demo", "demo", "d", nil, nil),
		host, schema.LanguageCN, &descWorker{})
	_, err := base.Invoke(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "custom-desc", host.desc)
}

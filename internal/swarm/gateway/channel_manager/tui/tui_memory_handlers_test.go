package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestResolveProjectDir_三级回退 对齐 Python _resolve_project_dir。
func TestResolveProjectDir_三级回退(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"project_dir 优先", map[string]any{"project_dir": "/a", "trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/a"},
		{"trusted_dirs 次之", map[string]any{"trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/b"},
		{"cwd 兜底", map[string]any{"cwd": "/c"}, "/c"},
		{"全缺省返回空", map[string]any{}, ""},
		{"空字符串视为缺省", map[string]any{"project_dir": "", "cwd": ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveProjectDir(tc.params))
		})
	}
}

// TestMemoryMode_缺省为plan 对齐 Python params.get("mode", "plan")。
func TestMemoryMode_缺省为plan(t *testing.T) {
	require.Equal(t, "plan", memoryMode(map[string]any{}))
	require.Equal(t, "plan", memoryMode(map[string]any{"mode": ""}))
	require.Equal(t, "agent.code.normal", memoryMode(map[string]any{"mode": "agent.code.normal"}))
}

// TestWithProjectDir_不修改入参且命中时注入副本 对齐 Python {**params, ...}。
func TestWithProjectDir_不修改入参且命中时注入副本(t *testing.T) {
	params := map[string]any{"cwd": "/c"}

	merged := withProjectDir(params, "/p")
	require.Equal(t, "/p", merged["project_dir"])
	require.Equal(t, "/c", merged["cwd"])
	_, hasKey := params["project_dir"]
	require.False(t, hasKey, "不得就地修改调用方 map")

	unchanged := withProjectDir(params, "")
	require.Equal(t, "/c", unchanged["cwd"])
	_, hasKey = unchanged["project_dir"]
	require.False(t, hasKey)
}

// capturedFrame 捕获的 res 帧
type capturedFrame struct {
	reqID   string
	ok      bool
	payload map[string]any
	errMsg  string
	code    string
}

// fakeResSender 捕获 SendResFrame 调用
type fakeResSender struct {
	frames []capturedFrame
}

// SendResFrame 实现 ResponseSender
func (f *fakeResSender) SendResFrame(_ *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	f.frames = append(f.frames, capturedFrame{reqID: reqID, ok: ok, payload: payload, errMsg: errMsg, code: code})
}

// lastFrame 取最后一帧
func (f *fakeResSender) lastFrame(t *testing.T) capturedFrame {
	t.Helper()
	require.NotEmpty(t, f.frames, "应有 res 帧")
	return f.frames[len(f.frames)-1]
}

// setupAgentWorkspace 设置隔离的 UAPCLAW_DATA_DIR 并返回 agent 工作区路径
func setupAgentWorkspace(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	agentWorkspace := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.MkdirAll(agentWorkspace, 0o755))

	t.Setenv(pathutil.EnvDataDir, dataDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return agentWorkspace
}

// TestMemoryLocalHandler_成功帧透传payload
func TestMemoryLocalHandler_成功帧透传payload(t *testing.T) {
	orig := memoryHandlers["memory.list"]
	defer func() { memoryHandlers["memory.list"] = orig }()
	memoryHandlers["memory.list"] = func(_ context.Context, workspace, mode string, params map[string]any) map[string]any {
		return map[string]any{"echo_workspace": workspace, "echo_mode": mode, "echo_project": params["project_dir"]}
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.list")
	require.NoError(t, handler(nil, "req-1", map[string]any{"mode": "agent.code.normal", "cwd": "/proj"}, "sess_1"))

	frame := sender.lastFrame(t)
	require.True(t, frame.ok)
	require.Empty(t, frame.errMsg)
	require.Equal(t, "agent.code.normal", frame.payload["echo_mode"])
	require.Equal(t, "/proj", frame.payload["echo_project"], "list 应注入 project_dir")
}

// TestMemoryLocalHandler_error字段转失败帧 result["error"] → ok=false + INTERNAL_ERROR
func TestMemoryLocalHandler_error字段转失败帧(t *testing.T) {
	orig := memoryHandlers["memory.edit"]
	defer func() { memoryHandlers["memory.edit"] = orig }()
	memoryHandlers["memory.edit"] = func(_ context.Context, _, _ string, _ map[string]any) map[string]any {
		return map[string]any{"error": "mkdir failed"}
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.edit")
	require.NoError(t, handler(nil, "req-2", map[string]any{}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok)
	require.Equal(t, "mkdir failed", frame.errMsg)
	require.Equal(t, "INTERNAL_ERROR", frame.code)
	require.Nil(t, frame.payload)
}

// TestMemoryLocalHandler_panic转失败帧 覆盖 defer recover 分支
func TestMemoryLocalHandler_panic转失败帧(t *testing.T) {
	orig := memoryHandlers["memory.status"]
	defer func() { memoryHandlers["memory.status"] = orig }()
	memoryHandlers["memory.status"] = func(_ context.Context, _, _ string, _ map[string]any) map[string]any {
		panic("boom")
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.status")
	require.NoError(t, handler(nil, "req-3", map[string]any{}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok)
	require.Equal(t, "boom", frame.errMsg)
	require.Equal(t, "INTERNAL_ERROR", frame.code)
}

// TestMemoryHandlers_默认映射覆盖五个方法 生产映射必须能调通（memory 目录为普通文件，
// 使索引初始化快速失败，仅验证映射与调用链，不做业务断言）。
func TestMemoryHandlers_默认映射覆盖五个方法(t *testing.T) {
	agentWorkspace := setupAgentWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(agentWorkspace, "memory"), []byte("not a dir"), 0o644))

	for _, method := range []string{"memory.list", "memory.edit", "memory.status", "memory.toggle", "memory.open"} {
		handler, ok := memoryHandlers[method]
		require.True(t, ok, "缺少方法 %s", method)
		result := handler(context.Background(), agentWorkspace, "plan", map[string]any{})
		require.NotNil(t, result, "%s 应返回 payload", method)
	}
}

// TestMemoryLocalHandler_edit真失败走失败帧 用生产 handler 触发 mkdir 失败（memory 是普通文件）。
func TestMemoryLocalHandler_edit真失败走失败帧(t *testing.T) {
	agentWorkspace := setupAgentWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(agentWorkspace, "memory"), []byte("not a dir"), 0o644))

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.edit")
	require.NoError(t, handler(nil, "req-4", map[string]any{"path": "memory/new.md"}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok, "父目录不可创建时应为失败帧")
	require.Equal(t, "INTERNAL_ERROR", frame.code)
	require.NotEmpty(t, frame.errMsg)
}

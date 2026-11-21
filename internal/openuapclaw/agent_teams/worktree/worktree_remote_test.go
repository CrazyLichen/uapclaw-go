package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// loopbackMessager 回环 messager：请求直投 handler（mock 只允许 messager 边界）。
type loopbackMessager struct {
	handler *WorktreeRemoteHandler
	mu      sync.Mutex
	payload []map[string]any
}

func (m *loopbackMessager) SendAndWait(ctx context.Context, nodeID string, payload map[string]any) (map[string]any, error) {
	m.mu.Lock()
	m.payload = append(m.payload, payload)
	m.mu.Unlock()
	// json tag 往返（与生产 requestToPayload/responseFromPayload 同一序列化层）
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req := &WorktreeRemoteRequest{}
	if err := json.Unmarshal(raw, req); err != nil {
		return nil, err
	}
	// 直投 handler：响应原样回传（Python messager 返回 response dict 语义）
	resp := m.handler.Handle(ctx, req)
	respRaw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(respRaw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// initBareOrigin 建带初始提交的裸仓库 origin + 本地克隆。
func initBareOrigin(t *testing.T) (originURL, localRepo string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run(base, "init", "--bare", origin)
	local := filepath.Join(base, "work")
	run(base, "clone", origin, local)
	require.NoError(t, os.WriteFile(filepath.Join(local, "README.md"), []byte("init"), 0o644))
	run(local, "add", ".")
	run(local, "commit", "-m", "init")
	run(local, "push", "origin", "HEAD:main")
	return origin, local
}

// ensureCloned 经 handler 的克隆缓存拿到节点本地仓库路径。
func ensureCloned(t *testing.T, origin string) string {
	t.Helper()
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	handler := NewWorktreeRemoteHandler(mgr)
	path, err := handler.ensureRepo(context.Background(), origin)
	require.NoError(t, err)
	return path
}

// ──────────────────────────── 后端三操作 ────────────────────────────

// TestRemoteWorktreeBackend_Create 往返链：leader 经 messager 请求远端建 worktree。
// Python: RemoteWorktreeBackend.create
func TestRemoteWorktreeBackend_Create(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	origin, local := initBareOrigin(t)

	// 远端节点侧：本地 manager + handler
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	// 节点的 target 解析走 base_dir 分支（远端无 workspace ctx）
	view, err := mgr.WithBaseDir(filepath.Join(agent_teams.GetAgentTeamsHome(), "node-worktrees"))
	require.NoError(t, err)
	handler := NewWorktreeRemoteHandler(view)

	msg := &loopbackMessager{handler: handler}
	backend := NewRemoteWorktreeBackend(worktree.NewWorktreeConfig(), msg, "node-1")

	// 远端节点的 ambient cwd：其 DeepAgent workspace 锚定在克隆仓库（Python contextvar 语义）
	nodeCtx := cwd.WithCwdState(context.Background(), cwd.InitCwd(ensureCloned(t, origin)))
	result, err := backend.Create(nodeCtx, "agent-remote-1", local, "/ignored")
	require.NoError(t, err)
	assert.NotEmpty(t, result.WorktreePath)
	assert.DirExists(t, result.WorktreePath)
	// leader 本地 target 被忽略（Python del target_path）
	assert.NotEqual(t, "/ignored", result.WorktreePath)
}

// TestRemoteWorktreeBackend_ExistsRemove 往返 exists / remove。
// Python: RemoteWorktreeBackend.exists / remove
func TestRemoteWorktreeBackend_ExistsRemove(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	origin, local := initBareOrigin(t)

	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	view, err := mgr.WithBaseDir(filepath.Join(agent_teams.GetAgentTeamsHome(), "node-worktrees"))
	require.NoError(t, err)
	handler := NewWorktreeRemoteHandler(view)
	msg := &loopbackMessager{handler: handler}
	backend := NewRemoteWorktreeBackend(worktree.NewWorktreeConfig(), msg, "node-1")

	nodeCtx := cwd.WithCwdState(context.Background(), cwd.InitCwd(ensureCloned(t, origin)))
	created, err := backend.Create(nodeCtx, "agent-remote-2", local, "")
	require.NoError(t, err)

	assert.True(t, backend.Exists(context.Background(), created.WorktreePath))
	assert.True(t, backend.Remove(context.Background(), created.WorktreePath, local, false))
	assert.False(t, backend.Exists(context.Background(), created.WorktreePath))
}

// ──────────────────────────── handler 细节 ────────────────────────────

// TestWorktreeRemoteHandler_未知Action 未知动作返回错误响应。
// Python: handle default 分支 "Unknown action: %s"
func TestWorktreeRemoteHandler_未知Action(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	handler := NewWorktreeRemoteHandler(mgr)
	resp := handler.Handle(context.Background(), &WorktreeRemoteRequest{Action: "nope"})
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "Unknown action: nope")
}

// TestWorktreeRemoteHandler_远端基准模式 handler 构造把 GitBackend 切到远端基准。
// Python: __init__ "backend.remote_base = True" (:247-249)
func TestWorktreeRemoteHandler_远端基准模式(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	_ = NewWorktreeRemoteHandler(mgr)
	assert.True(t, worktree.BackendIsRemoteBase(mgr.Backend()), "GitBackend 应被切到 remote_base")
}

// TestWorktreeRemoteHandler_ensureRepo缓存 同 URL 二次克隆命中缓存目录。
// Python: _ensure_repo :322-352
func TestWorktreeRemoteHandler_ensureRepo缓存(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	handler := NewWorktreeRemoteHandler(mgr)

	// 本地 file:// 裸仓库作为 clone 源（真 URL 语义，不走网络）
	origin, _ := initBareOrigin(t)

	path1, err := handler.ensureRepo(context.Background(), origin)
	require.NoError(t, err)
	// 内容寻址目录：{home}/remote_repos/{hash12}
	assert.Contains(t, path1, filepath.Join("remote_repos"))
	hashDir := filepath.Base(path1)
	assert.Len(t, hashDir, 12)

	path2, err := handler.ensureRepo(context.Background(), origin)
	require.NoError(t, err)
	assert.Equal(t, path1, path2)
}

// TestWorktreeRemoteHandler_Remove无仓库报错 remove 目标无 git 元数据时报错响应。
// Python: _handle_remove "Cannot find repo root for worktree"
func TestWorktreeRemoteHandler_Remove无仓库报错(t *testing.T) {
	agent_teams.ConfigureHome(t.TempDir())
	t.Cleanup(agent_teams.ResetHome)
	mgr := worktree.NewWorktreeManager(worktree.NewWorktreeConfig(), mustBackend(t))
	handler := NewWorktreeRemoteHandler(mgr)
	resp := handler.Handle(context.Background(), &WorktreeRemoteRequest{
		Action:       "remove",
		WorktreePath: filepath.Join(t.TempDir(), "not-a-repo"),
	})
	assert.False(t, resp.Success)
	assert.True(t, strings.Contains(resp.Error, "Cannot find repo root"))
}

// TestResponseFromPayload_类型不符显式报错 载荷键类型漂移 → error 而非静默零值
//（Python model_validate strict 语义；json tag 往返层防御）。
func TestResponseFromPayload_类型不符显式报错(t *testing.T) {
	// success 键是字符串 → Unmarshal 报错
	_, err := responseFromPayload(map[string]any{"success": "yes"})
	require.Error(t, err)
	// 键缺失 → 合法（omitted），零值即缺省
	resp, err := responseFromPayload(map[string]any{})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}

// TestRequestToPayload_roundtrip 请求经 json tag 往返字段无损。
func TestRequestToPayload_roundtrip(t *testing.T) {
	payload, err := requestToPayload(&WorktreeRemoteRequest{
		Action: "create", Slug: "agent-x", RepoURL: "https://x/y.git",
		BaseBranch: "main", Config: map[string]any{"enabled": true},
	})
	require.NoError(t, err)
	assert.Equal(t, "create", payload["action"])
	assert.Equal(t, "https://x/y.git", payload["repo_url"])
	// 空 WorktreePath omitempty → 不出现
	_, ok := payload["worktree_path"]
	assert.False(t, ok)
}

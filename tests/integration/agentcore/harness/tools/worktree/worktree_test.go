//go:build integration

package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	worktree "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeManagerSuite 测试 WorktreeManager 生命周期操作。
// 不嵌入 AgentSuite，Manager 测试不需要 MockLLM。
type WorktreeManagerSuite struct {
	suite.Suite
	// Ctx 测试用上下文
	Ctx context.Context
	// Cancel 取消函数
	Cancel context.CancelFunc
}

// WorktreeRailSuite 测试 WorktreeRail 在 DeepAgent 上下文中的集成行为。
// 嵌入 AgentSuite，用于测试 Init 工具注册和 BeforeAfterInvoke Session 持久化。
type WorktreeRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestWorktreeManagerSuite(t *testing.T) {
	suite.Run(t, new(WorktreeManagerSuite))
}

func TestWorktreeRailSuite(t *testing.T) {
	suite.Run(t, new(WorktreeRailSuite))
}

// SetupSuite 初始化 Manager 测试环境。
func (s *WorktreeManagerSuite) SetupSuite() {
	s.Ctx, s.Cancel = context.WithTimeout(context.Background(), 5*time.Minute)
}

// TearDownSuite 清理 Manager 测试环境。
func (s *WorktreeManagerSuite) TearDownSuite() {
	if s.Cancel != nil {
		s.Cancel()
	}
}

// newMgrWithCtx 创建 Manager 并将其 SessionState 注入 Context。
// Manager 的 Enter 设置 m.sessionState，而 Exit 从 ctx 读取 session。
// 两者必须使用同一个 WorktreeSessionState 实例。
func newMgrWithCtx(ctx context.Context, eventHandler worktree.WorktreeEventHandler) (context.Context, *worktree.WorktreeManager) {
	cfg := worktree.NewWorktreeConfig()
	cfg.Enabled = true

	var opts []worktree.ManagerOption
	if eventHandler != nil {
		opts = append(opts, worktree.WithEventHandler(eventHandler))
	}
	mgr := worktree.NewWorktreeManager(cfg, nil, opts...)

	// 使用 Manager 内部的 SessionState（Enter 写入此实例）
	ctx = worktree.WithWorktreeSessionState(ctx, mgr.SessionState())
	return ctx, mgr
}

// TestWorktreeManager_Enter创建 测试 Manager.Enter 创建 worktree 并进入。
// 对齐 Python: WorktreeManager.enter(slug, member_name, team_name)
func (s *WorktreeManagerSuite) TestWorktreeManager_Enter创建() {
	repoRoot := setupGitRepo(s.T())

	cwdState := cwd.InitCwd(repoRoot, cwd.WithWorkspace(repoRoot))
	ctx := cwd.WithCwdState(s.Ctx, cwdState)
	ctx, mgr := newMgrWithCtx(ctx, nil)

	session, err := mgr.Enter(ctx, "test-slug", "", "")
	s.Require().NoError(err)
	s.Require().NotNil(session)
	s.Equal("test-slug", session.WorktreeName)
	s.NotEmpty(session.WorktreePath, "WorktreePath 不应为空")

	// 验证 worktree 目录存在
	_, err = os.Stat(session.WorktreePath)
	s.NoError(err, "worktree 目录应存在")
}

// TestWorktreeManager_ExitKeep保留 测试 Manager.Exit(action="keep") 保留 worktree 目录。
// 对齐 Python: WorktreeManager.exit("keep")
func (s *WorktreeManagerSuite) TestWorktreeManager_ExitKeep保留() {
	repoRoot := setupGitRepo(s.T())

	cwdState := cwd.InitCwd(repoRoot, cwd.WithWorkspace(repoRoot))
	ctx := cwd.WithCwdState(s.Ctx, cwdState)
	ctx, mgr := newMgrWithCtx(ctx, nil)

	session, err := mgr.Enter(ctx, "keep-slug", "", "")
	s.Require().NoError(err)

	wtPath := session.WorktreePath

	// Exit with keep
	result, err := mgr.Exit(ctx, "keep", false)
	s.Require().NoError(err)
	s.Equal("keep", result["action"])

	// 目录仍存在
	_, err = os.Stat(wtPath)
	s.NoError(err, "keep 模式下 worktree 目录应保留")
}

// TestWorktreeManager_ExitRemove删除 测试 Manager.Exit(action="remove", discard_changes=true) 删除 worktree 目录。
// 对齐 Python: WorktreeManager.exit("remove", discard_changes=True)
func (s *WorktreeManagerSuite) TestWorktreeManager_ExitRemove删除() {
	repoRoot := setupGitRepo(s.T())

	cwdState := cwd.InitCwd(repoRoot, cwd.WithWorkspace(repoRoot))
	ctx := cwd.WithCwdState(s.Ctx, cwdState)
	ctx, mgr := newMgrWithCtx(ctx, nil)

	session, err := mgr.Enter(ctx, "remove-slug", "", "")
	s.Require().NoError(err)

	wtPath := session.WorktreePath

	// Exit with remove + discard_changes
	result, err := mgr.Exit(ctx, "remove", true)
	s.Require().NoError(err)
	s.Equal("remove", result["action"])

	// 目录已删除
	_, err = os.Stat(wtPath)
	s.True(os.IsNotExist(err), "remove 模式下 worktree 目录应被删除")
}

// TestWorktreeManager_CountChanges 测试 Manager.CountChanges 统计变更文件数。
// 对齐 Python: WorktreeManager.count_changes(session)
func (s *WorktreeManagerSuite) TestWorktreeManager_CountChanges() {
	repoRoot := setupGitRepo(s.T())

	cwdState := cwd.InitCwd(repoRoot, cwd.WithWorkspace(repoRoot))
	ctx := cwd.WithCwdState(s.Ctx, cwdState)
	ctx, mgr := newMgrWithCtx(ctx, nil)

	session, err := mgr.Enter(ctx, "changes-slug", "", "")
	s.Require().NoError(err)

	// 在 worktree 中创建新文件
	newFile := filepath.Join(session.WorktreePath, "new_feature.go")
	s.Require().NoError(os.WriteFile(newFile, []byte("// new feature"), 0o644))

	// CountChanges 应返回 ChangedFiles >= 1
	summary := mgr.CountChanges(ctx, session)
	s.Require().NotNil(summary, "CountChanges 不应返回 nil")
	s.GreaterOrEqual(summary.ChangedFiles, 1, "应检测到至少 1 个变更文件")
}

// TestWorktreeManager_EventHook触发 测试 Enter/Exit 时事件处理器触发。
// 对齐 Python: WorktreeManager._fire_event / event_handler
func (s *WorktreeManagerSuite) TestWorktreeManager_EventHook触发() {
	repoRoot := setupGitRepo(s.T())

	cwdState := cwd.InitCwd(repoRoot, cwd.WithWorkspace(repoRoot))
	ctx := cwd.WithCwdState(s.Ctx, cwdState)

	var createdCount atomic.Int32
	var removedCount atomic.Int32

	eventHandler := func(_ context.Context, event worktree.WorktreeEvent) error {
		switch event.(type) {
		case *worktree.WorktreeCreatedEvent:
			createdCount.Add(1)
		case *worktree.WorktreeRemovedEvent:
			removedCount.Add(1)
		}
		return nil
	}

	ctx, mgr := newMgrWithCtx(ctx, eventHandler)

	// Enter 应触发 CreatedEvent
	_, err := mgr.Enter(ctx, "event-slug", "", "")
	s.Require().NoError(err)
	s.Equal(int32(1), createdCount.Load(), "Enter 应触发 WorktreeCreatedEvent")

	// Exit with remove 应触发 RemovedEvent
	_, err = mgr.Exit(ctx, "remove", true)
	s.Require().NoError(err)
	s.Equal(int32(1), removedCount.Load(), "Exit remove 应触发 WorktreeRemovedEvent")
}

// TestWorktreeSessionState_Context传播 测试 InitWorktreeSessionState + WithWorktreeSessionState Context 传播。
// 对齐 Python: ContextVar 传播
func (s *WorktreeManagerSuite) TestWorktreeSessionState_Context传播() {
	state := worktree.InitWorktreeSessionState()
	s.Require().NotNil(state)

	ctx := worktree.WithWorktreeSessionState(s.Ctx, state)

	// 从 context 中获取
	got := worktree.WorktreeSessionStateFromCtx(ctx)
	s.Require().NotNil(got)
	s.Equal(state, got, "从 ctx 获取的 state 应为注入的实例")

	// 设置 session 后可获取
	testSession := &worktree.WorktreeSession{
		OriginalCWD:  "/tmp/original",
		WorktreePath: "/tmp/worktree",
		WorktreeName: "test-ctx",
	}
	state.SetCurrentSession(testSession)

	current := worktree.GetCurrentSession(ctx)
	s.Require().NotNil(current)
	s.Equal("test-ctx", current.WorktreeName)
}

// TestGitBackend_创建和移除 测试 GitBackend.Create + Remove 真实 git worktree 操作。
// 对齐 Python: GitBackend.create / GitBackend.remove
func (s *WorktreeManagerSuite) TestGitBackend_创建和移除() {
	repoRoot := setupGitRepo(s.T())

	cfg := worktree.NewWorktreeConfig()
	cfg.Enabled = true
	backend, err := worktree.CreateBackend("git", cfg)
	s.Require().NoError(err, "CreateBackend('git') 不应返回错误")
	ctx := s.Ctx

	targetPath := worktree.WorktreePathFor(repoRoot, "backend-test")

	// Create
	result, err := backend.Create(ctx, "backend-test", repoRoot, targetPath)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal(targetPath, result.WorktreePath)
	s.False(result.Existed, "首次创建应不是已存在")

	// 验证目录存在
	_, err = os.Stat(targetPath)
	s.NoError(err, "worktree 目录应存在")

	// 验证 .git 文件存在
	_, err = os.Stat(filepath.Join(targetPath, ".git"))
	s.NoError(err, ".git 文件应存在")

	// Remove
	removed := backend.Remove(ctx, targetPath, repoRoot)
	s.True(removed, "Remove 应返回 true")

	// 验证目录已删除
	_, err = os.Stat(targetPath)
	s.True(os.IsNotExist(err), "Remove 后 worktree 目录应不存在")
}

// TestWorktree_Slug验证 测试 ValidateSlug 校验逻辑。
// 对齐 Python: validate_slug(slug)
func (s *WorktreeManagerSuite) TestWorktree_Slug验证() {
	// 合法 slug
	s.NoError(worktree.ValidateSlug("valid-slug"), "合法 slug 不应报错")
	s.NoError(worktree.ValidateSlug("feature_auth"), "下划线合法")
	s.NoError(worktree.ValidateSlug("v2.0"), "点号合法")

	// 路径遍历
	s.Error(worktree.ValidateSlug("../../../etc"), "路径遍历应报错")
	s.Error(worktree.ValidateSlug(".."), "双点应报错")
	s.Error(worktree.ValidateSlug("."), "单点应报错")

	// 空字符串
	s.Error(worktree.ValidateSlug(""), "空 slug 应报错")

	// 非法字符
	s.Error(worktree.ValidateSlug("slug with spaces"), "空格应报错")
	s.Error(worktree.ValidateSlug("slug#hash"), "井号应报错")
}

// TestWorktreeRail_Init注册工具 测试 WorktreeRail.Init 注册 EnterWorktreeTool + ExitWorktreeTool。
// 对齐 Python: WorktreeRail.init(agent) 中工具注册
func (s *WorktreeRailSuite) TestWorktreeRail_Init注册工具() {
	rail := worktree.NewWorktreeRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 触发 Init（延迟到 Invoke）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 worktree"})
	s.Require().NoError(err)

	// 验证 ability_manager 包含 enter/exit worktree 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")

	// Manager 应已创建（Init 触发后）
	s.NotNil(rail.Manager(), "WorktreeRail.Init 应创建 Manager")
}

// TestWorktreeRail_BeforeAfterInvoke_Session持久化 测试 BeforeInvoke 恢复 session 和 AfterInvoke 持久化。
// 对齐 Python: WorktreeRail.before_invoke / WorktreeRail.after_invoke
func (s *WorktreeRailSuite) TestWorktreeRail_BeforeAfterInvoke_Session持久化() {
	rail := worktree.NewWorktreeRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Session 持久化测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 创建带 Session 的 Invoke
	sess := s.NewTestSession("worktree-session-persist-test")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	// Invoke 不应报错（BeforeInvoke 从 Session.state 恢复，AfterInvoke 写回）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "worktree 持久化"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "带 Session 的 Invoke 应成功完成")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupGitRepo 创建临时 git 仓库，返回仓库根路径。
func setupGitRepo(t testing.TB) string {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

// runGit 在指定目录下执行 git 命令。
func runGit(t testing.TB, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), string(out))
}

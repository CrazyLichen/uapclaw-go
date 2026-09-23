package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeRail 注入 enter_worktree / exit_worktree 工具的 Rail。
// Python: WorktreeRail (rails.py)
//
// 拥有每个 Agent 的 WorktreeManager，在 Init 时注册工具，Uninit 时移除。
// 桥接 ContextVar session 和 Agent 的 Session.state：
// BeforeInvoke 从 Session.state 恢复，AfterInvoke 持久化。
type WorktreeRail struct {
	rails.DeepAgentRail
	// userConfig 用户配置
	userConfig WorktreeConfig
	// eventHandler 事件处理器
	eventHandler WorktreeEventHandler
	// lifecycleRails 生命周期 hook 列表
	lifecycleRails []WorktreeLifecycleRail
	// manager 管理器（Init 时创建）
	manager *WorktreeManager
	// tools 注册的工具列表
	tools []tool.Tool
}

// worktreeRailOptions WorktreeRail 内部构造选项
type worktreeRailOptions struct {
	config         *WorktreeConfig
	eventHandler   WorktreeEventHandler
	lifecycleRails []WorktreeLifecycleRail
}

// AutoSetupRail 自动检测项目类型并运行 setup 的 LifecycleRail。
// Python: AutoSetupRail
type AutoSetupRail struct {
	// commands 预设的 setup 命令列表；为空时调用 detectSetup 自动检测
	commands []string
}

// DiffSummaryRail action=keep 时记录 git diff --stat 的 LifecycleRail。
// Python: DiffSummaryRail
type DiffSummaryRail struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// WorktreeRailOption WorktreeRail 构造选项
type WorktreeRailOption func(*worktreeRailOptions)

// AutoSetupRailOption AutoSetupRail 构造选项
type AutoSetupRailOption func(*AutoSetupRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// sessionStateKey Session state 持久化 key。
// Python: _SESSION_STATE_KEY = "_worktree_session"
const sessionStateKey = "_worktree_session"

// defaultWorktreeNameKey 默认 worktree 名持久化 key。
// Python: _DEFAULT_WORKTREE_NAME_KEY = "_worktree_default_name"
const defaultWorktreeNameKey = "_worktree_default_name"

// worktreeRailPriority WorktreeRail 优先级。
// 与 SysOperationRail 同级（100）。
const worktreeRailPriority = 100

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorktreeRail 创建 WorktreeRail 实例。
// Python: WorktreeRail(*, config, event_handler, lifecycle_rails)
func NewWorktreeRail(opts ...WorktreeRailOption) *WorktreeRail {
	o := &worktreeRailOptions{}
	for _, opt := range opts {
		opt(o)
	}
	cfg := o.config
	if cfg == nil {
		defaultCfg := NewWorktreeConfig()
		defaultCfg.Enabled = true
		cfg = &defaultCfg
	}
	return &WorktreeRail{
		userConfig:     *cfg,
		eventHandler:   o.eventHandler,
		lifecycleRails: o.lifecycleRails,
	}
}

// WithWorktreeRailConfig 设置配置。
func WithWorktreeRailConfig(cfg WorktreeConfig) WorktreeRailOption {
	return func(o *worktreeRailOptions) { o.config = &cfg }
}

// WithWorktreeRailEventHandler 设置事件处理器。
func WithWorktreeRailEventHandler(handler WorktreeEventHandler) WorktreeRailOption {
	return func(o *worktreeRailOptions) { o.eventHandler = handler }
}

// WithWorktreeRailLifecycleRails 设置生命周期 rail。
func WithWorktreeRailLifecycleRails(rails ...WorktreeLifecycleRail) WorktreeRailOption {
	return func(o *worktreeRailOptions) { o.lifecycleRails = rails }
}

// NewAutoSetupRail 创建 AutoSetupRail 实例。
func NewAutoSetupRail(opts ...AutoSetupRailOption) *AutoSetupRail {
	r := &AutoSetupRail{}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithCommands 设置预设的 setup 命令。
func WithCommands(cmds ...string) AutoSetupRailOption {
	return func(r *AutoSetupRail) { r.commands = cmds }
}

// Manager 返回 WorktreeManager 访问器。
func (r *WorktreeRail) Manager() *WorktreeManager {
	return r.manager
}

// Priority 返回 Rail 优先级。
// Python: WorktreeRail.priority = 100
func (r *WorktreeRail) Priority() int {
	return worktreeRailPriority
}

// Init 构建 Manager 并注册 enter/exit 工具。
// Python: WorktreeRail.init(agent)
func (r *WorktreeRail) Init(ctx context.Context, agent interfaces.BaseAgent) error {
	// Python: lang = agent.system_prompt_builder.language
	lang := "cn" // 默认中文
	if agent != nil && agent.SystemPromptBuilder() != nil {
		l := agent.SystemPromptBuilder().Language()
		if l != "" {
			lang = l
		}
	}
	// Python: agent_id = getattr(getattr(agent, "card", None), "id", None)
	agentID := ""
	if agent != nil && agent.Card() != nil {
		agentID = agent.Card().ID
	}

	r.manager = NewWorktreeManager(
		r.userConfig,
		nil,
		WithEventHandler(r.eventHandler),
		WithLifecycleRails(r.lifecycleRails...),
	)

	enterTool, err := NewEnterWorktreeTool(r.manager, lang, agentID)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("创建 EnterWorktreeTool 失败")
		return err
	}
	exitTool, err := NewExitWorktreeTool(r.manager, lang, agentID)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("创建 ExitWorktreeTool 失败")
		return err
	}

	r.tools = []tool.Tool{enterTool, exitTool}

	// 注册到 ResourceMgr
	// Python: L88: Runner.resource_mgr.add_tool(self._tools)
	// 对齐 SysOperationRail.Init 模式：先移除已存在的同名工具，再批量注册
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for _, t := range r.tools {
			toolID := t.Card().ID
			if toolID != "" {
				existing, err := resourceMgr.GetTool([]string{toolID})
				if err == nil && len(existing) > 0 {
					_, _ = resourceMgr.RemoveTool([]string{toolID})
				}
			}
		}
		for _, t := range r.tools {
			_ = resourceMgr.AddTool(t)
		}
	}

	// Python: agent.ability_manager.add(tool.card)
	if agent != nil && agent.AbilityManager() != nil {
		for _, t := range r.tools {
			card := t.Card()
			if card != nil {
				agent.AbilityManager().Add(card)
			}
		}
	}

	logger.Info(logComponent).Msg("WorktreeRail 初始化完成")
	return nil
}

// Uninit 移除工具并清空 Manager。
// Python: WorktreeRail.uninit(agent)
func (r *WorktreeRail) Uninit(agent interfaces.BaseAgent) error {
	// 从 ResourceMgr 移除
	// Python: L95-102: Runner.resource_mgr.remove_tool(tool_id)
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for _, t := range r.tools {
			func(t tool.Tool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(logComponent).
							Str("event_type", "worktree_rail_uninit").
							Str("tool_name", t.Card().Name).
							Msgf("注销工具失败: %v", rec)
					}
				}()
				toolID := t.Card().ID
				if toolID != "" && resourceMgr != nil {
					_, _ = resourceMgr.RemoveTool([]string{toolID})
				}
			}(t)
		}
	}

	// Python: for tool in self._tools: agent.ability_manager.remove(name)
	if agent != nil && agent.AbilityManager() != nil {
		for _, t := range r.tools {
			card := t.Card()
			if card != nil && card.Name != "" {
				agent.AbilityManager().Remove(card.Name)
			}
		}
	}
	r.tools = nil
	r.manager = nil
	return nil
}

// BeforeInvoke 从 Session.state 恢复 worktree session 到 ContextVar。
// Python: WorktreeRail.before_invoke(ctx)
//
// 读取 Session.state 中 _worktree_session 和 _worktree_default_name，
// 写入 ContextVar，同时恢复 CWD。
func (r *WorktreeRail) BeforeInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Session() == nil {
		return nil
	}

	session := cbc.Session()

	// 恢复 defaultWorktreeName
	defaultName, _ := session.GetState(state.StringKey(defaultWorktreeNameKey))
	if name, ok := defaultName.(string); ok {
		SetDefaultWorktreeName(ctx, name)
	}

	// 恢复 worktree session
	stored, _ := session.GetState(state.StringKey(sessionStateKey))
	if stored == nil {
		SetCurrentSession(ctx, nil)
		return nil
	}

	var ws WorktreeSession
	switch v := stored.(type) {
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("反序列化 worktree session 失败")
			SetCurrentSession(ctx, nil)
			return nil
		}
		if err := json.Unmarshal(data, &ws); err != nil {
			logger.Warn(logComponent).Err(err).Msg("解析 worktree session 失败")
			SetCurrentSession(ctx, nil)
			return nil
		}
	default:
		SetCurrentSession(ctx, nil)
		return nil
	}

	SetCurrentSession(ctx, &ws)

	// 恢复 CWD
	cwdState := cwd.CwdStateFromCtx(ctx)
	if cwdState != nil && ws.WorktreePath != "" {
		cwdState.SetCwd(ws.WorktreePath)
		cwdState.SetOriginalCwd(ws.WorktreePath)
	}

	return nil
}

// AfterInvoke 将 ContextVar session 持久化到 Session.state。
// Python: WorktreeRail.after_invoke(ctx)
func (r *WorktreeRail) AfterInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Session() == nil {
		return nil
	}

	session := cbc.Session()
	current := GetCurrentSession(ctx)

	var payload map[string]any
	if current != nil {
		data, err := json.Marshal(current)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("序列化 worktree session 失败")
			return nil
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			logger.Warn(logComponent).Err(err).Msg("转换 worktree session 失败")
			return nil
		}
	}

	session.UpdateState(map[string]any{
		sessionStateKey:        payload,
		defaultWorktreeNameKey: GetDefaultWorktreeName(ctx),
	})

	return nil
}

// AfterWorktreeCreate AutoSetupRail 的 hook 实现。
// Python: AutoSetupRail.after_worktree_create(ctx, session)
func (a *AutoSetupRail) AfterWorktreeCreate(ctx context.Context, _ *interfaces.AgentCallbackContext, session *WorktreeSession) error {
	commands := a.commands
	if len(commands) == 0 {
		commands = detectSetup(session.WorktreePath)
	}
	for _, cmd := range commands {
		execCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		cmdObj := exec.CommandContext(execCtx, "sh", "-c", cmd)
		cmdObj.Dir = session.WorktreePath
		cmdObj.Stdout = nil
		cmdObj.Stderr = nil
		if err := cmdObj.Run(); err != nil {
			logger.Warn(logComponent).Str("cmd", cmd).Err(err).Msg("Setup 命令执行失败")
		}
		cancel()
	}
	return nil
}

// BeforeWorktreeCreate AutoSetupRail 的空实现，不干预 slug。
func (a *AutoSetupRail) BeforeWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _, _ string) (*string, error) {
	return nil, nil
}

// BeforeWorktreeExit AutoSetupRail 的空实现，不干预 action。
func (a *AutoSetupRail) BeforeWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) (*string, error) {
	return nil, nil
}

// AfterWorktreeExit AutoSetupRail 的空实现。
func (a *AutoSetupRail) AfterWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (a *AutoSetupRail) OnWorktreeFileWrite(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) bool {
	return true
}
func (a *AutoSetupRail) BeforeWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, message string, _ []string) (*string, error) {
	return &message, nil
}
func (a *AutoSetupRail) AfterWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (a *AutoSetupRail) OnWorktreeSync(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, files []string) []string {
	return files
}

// BeforeWorktreeExit DiffSummaryRail 的 hook 实现。
// Python: DiffSummaryRail.before_worktree_exit(ctx, session, action)
func (d *DiffSummaryRail) BeforeWorktreeExit(ctx context.Context, _ *interfaces.AgentCallbackContext, session *WorktreeSession, action string) (*string, error) {
	if action != "keep" {
		return nil, nil
	}
	if session.OriginalHeadCommit == "" {
		return nil, nil
	}
	r := runGit(ctx, []string{"diff", "--stat", session.OriginalHeadCommit + "..HEAD"}, session.WorktreePath)
	if r.OK() && r.Stdout != "" {
		logger.Info(logComponent).Str("worktree_name", session.WorktreeName).
			Str("diff_stat", r.Stdout).Msg("Worktree 变更摘要")
	}
	return nil, nil
}

// BeforeWorktreeCreate DiffSummaryRail 的空实现，不干预 slug。
func (d *DiffSummaryRail) BeforeWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _, _ string) (*string, error) {
	return nil, nil
}

// AfterWorktreeCreate DiffSummaryRail 的空实现。
func (d *DiffSummaryRail) AfterWorktreeCreate(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession) error {
	return nil
}

// AfterWorktreeExit DiffSummaryRail 的空实现。
func (d *DiffSummaryRail) AfterWorktreeExit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (d *DiffSummaryRail) OnWorktreeFileWrite(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) bool {
	return true
}
func (d *DiffSummaryRail) BeforeWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, message string, _ []string) (*string, error) {
	return &message, nil
}
func (d *DiffSummaryRail) AfterWorktreeCommit(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string) error {
	return nil
}
func (d *DiffSummaryRail) OnWorktreeSync(_ context.Context, _ *interfaces.AgentCallbackContext, _ *WorktreeSession, _ string, files []string) []string {
	return files
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// detectSetup 检测项目类型并返回 setup 命令。
// Python: AutoSetupRail._detect_setup(path)
func detectSetup(path string) []string {
	if _, err := os.Stat(filepath.Join(path, "pyproject.toml")); err == nil {
		return []string{"uv sync --quiet"}
	}
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		return []string{"npm install --silent"}
	}
	return nil
}

package team_workspace

import (
	"context"
	"fmt"
	"strings"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamWorkspaceRail 团队工作空间 Rail。
// 拦截 .team/ 路径下的标准文件工具调用，透明施加锁检查和版本控制。
// Agent 使用标准 read_file/write_file — 本 Rail 添加行为。
//
// 对齐 Python: TeamWorkspaceRail (team_workspace/rails.py)
type TeamWorkspaceRail struct {
	rails.DeepAgentRail
	// ws 工作空间管理器
	ws *TeamWorkspaceManager
	// memberName 当前成员名
	memberName string
	// lastPullTime 上次拉取时间（仅 DISTRIBUTED 模式使用）
	lastPullTime float64
	// pullInterval 拉取间隔秒数
	pullInterval float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamPrefix 团队路径前缀
	teamPrefix = ".team/"
	// pullIntervalDefault 默认拉取间隔秒数
	pullIntervalDefault = 5.0
	// eventWorkspaceArtifactUpdated 工件更新事件类型
	// 对齐 Python: TeamEvent.WORKSPACE_ARTIFACT_UPDATED（因循环依赖不导入 schema 包）
	eventWorkspaceArtifactUpdated = "workspace_artifact_updated"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// writeTools 写操作工具集合
	writeTools = map[string]bool{"write_file": true, "edit_file": true}
	// readTools 读操作工具集合
	readTools = map[string]bool{"read_file": true, "glob": true, "grep": true, "list_files": true}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamWorkspaceRail 创建 TeamWorkspaceRail 实例。
// 对齐 Python: TeamWorkspaceRail.__init__(workspace_manager, member_name)
func NewTeamWorkspaceRail(ws *TeamWorkspaceManager, memberName string) *TeamWorkspaceRail {
	return &TeamWorkspaceRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		ws:            ws,
		memberName:    memberName,
		pullInterval:  pullIntervalDefault,
	}
}

// Init 初始化 Rail，设置 CwdState 的 team workspace。
// 对齐 Python: TeamWorkspaceRail.init(agent)
//
// TODO: 待 AgentConfigurator 在 SetupAgent 中统一调用 cwd.InitCwd(WithTeamWorkspace) 后，
// 此方法仅需保留 Rail 自身的初始化逻辑（当前由上层已处理，此处为 no-op）。
func (r *TeamWorkspaceRail) Init(_ interfaces.BaseAgent) error {
	return nil
}

// BeforeToolCall 工具调用前拦截。
// 对齐 Python: TeamWorkspaceRail.before_tool_call(ctx)
// 读操作→maybePull；写操作→maybePull + 锁检查（设 Extra 标记，不阻断）。
func (r *TeamWorkspaceRail) BeforeToolCall(_ context.Context, cbc *interfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*interfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	toolName := inputs.ToolName
	toolArgs := inputs.ToolArgs
	if toolArgs == nil {
		toolArgs = make(map[string]any)
	}

	path, _ := toolArgs["file_path"].(string)
	if path == "" || !strings.HasPrefix(path, teamPrefix) {
		return nil
	}

	// 读操作路径：拉取后放行
	if readTools[toolName] {
		r.maybePull()
		return nil
	}

	if !writeTools[toolName] {
		return nil
	}

	// 写操作路径：拉取 + 锁检查
	r.maybePull()

	if r.ws.Config().ConflictStrategy == ConflictStrategyLock {
		lock := r.ws.GetLock(path)
		if lock != nil && lock.HolderID != r.memberName && !lock.IsExpired() {
			toolMsgText := fmt.Sprintf("文件 '%s' 被 %s (%s) 锁定", path, lock.HolderName, lock.HolderID)
			logger.Warn(logComponent).
				Str("event_type", "workspace_lock_rejected").
				Str("file_path", path).
				Str("holder_id", lock.HolderID).
				Str("holder_name", lock.HolderName).
				Msg(toolMsgText)
			// 在 extra 中存储拒绝信息，供下游处理
			cbc.Extra()["workspace_lock_rejected"] = toolMsgText
		}
	}

	return nil
}

// AfterToolCall 工具调用后拦截。
// 对齐 Python: TeamWorkspaceRail.after_tool_call(ctx)
// 写操作→autoCommit + publishEvent。
func (r *TeamWorkspaceRail) AfterToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*interfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	toolName := inputs.ToolName
	if !writeTools[toolName] {
		return nil
	}

	toolArgs := inputs.ToolArgs
	if toolArgs == nil {
		return nil
	}

	path, _ := toolArgs["file_path"].(string)
	if path == "" || !strings.HasPrefix(path, teamPrefix) {
		return nil
	}

	realPath := resolveWorkspaceRelative(path, r.ws.TeamName())

	// 自动版本控制（分布式模式下含推送）
	if r.ws.Config().VersionControl {
		if _, err := r.ws.AutoCommit(ctx, realPath, r.memberName); err != nil {
			logger.Error(logComponent).
				Str("event_type", "workspace_auto_commit_failed").
				Str("file_path", realPath).
				Err(err).
				Msg("工作空间自动提交失败")
		}
	}

	// 通过回调发布事件
	if r.ws.publishEvent != nil {
		r.ws.publishEvent(
			eventWorkspaceArtifactUpdated,
			map[string]any{
				"team_name":     r.ws.TeamName(),
				"member_name":   r.memberName,
				"artifact_path": realPath,
			},
		)
	}

	return nil
}

// GetCallbacks 返回本 Rail 覆盖的回调映射。
// 仅声明 BeforeToolCall 和 AfterToolCall 两个钩子。
func (r *TeamWorkspaceRail) GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(interfaces.CallbackBeforeToolCall, func(ctx context.Context, railCtx any) error {
			return r.BeforeToolCall(ctx, railCtx.(*interfaces.AgentCallbackContext))
		}),
		r.CallbackFrom(interfaces.CallbackAfterToolCall, func(ctx context.Context, railCtx any) error {
			return r.AfterToolCall(ctx, railCtx.(*interfaces.AgentCallbackContext))
		}),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybePull 节流拉取（LOCAL 模式下 no-op）。
// 对齐 Python: TeamWorkspaceRail._maybe_pull
func (r *TeamWorkspaceRail) maybePull() {
	if r.ws.Mode() != WorkspaceModeDistributed {
		return
	}
	// TODO: 实现分布式模式的节流拉取逻辑
	// 对齐 Python: now := time.Now().Sub(epoch).Seconds()
	// 对齐 Python: if now - r.lastPullTime < r.pullInterval { return }
	// 对齐 Python: r.lastPullTime = now
	// r.ws.Pull(ctx)
}

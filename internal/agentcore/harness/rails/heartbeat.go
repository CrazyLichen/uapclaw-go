package rails

import (
	"context"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeartbeatRail 心跳护栏，在心跳运行时注入 HEARTBEAT.md 内容到系统提示词。
//
// 职责：
//   - Init:               获取 systemPromptBuilder、sysOperation、workspace、heartbeatDir
//   - BeforeModelCall:    若 run_kind=heartbeat，读取 HEARTBEAT.md 并注入心跳提示词节
//   - Uninit:             移除心跳提示词节
//
// 心跳调度器（11.11）周期性触发 RunKind=HEARTBEAT 的 Agent 调用，
// HeartbeatRail 在 BeforeModelCall 中将 HEARTBEAT.md 的内容注入系统提示词，
// 使 LLM 根据内容是否为空决定输出 HEARTBEAT_OK（存活确认）还是执行具体任务指令。
//
// 对齐 Python: openjiuwen/harness/rails/heartbeat_rail.py HeartbeatRail
type HeartbeatRail struct {
	DeepAgentRail
	// systemPromptBuilder 系统提示词构建器
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// heartbeatDir HEARTBEAT.md 文件路径
	heartbeatDir string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// heartbeatRailPriority HeartbeatRail 优先级
	// 对齐 Python: HeartbeatRail.priority = 80
	heartbeatRailPriority = 80
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HeartbeatRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*HeartbeatRail)(nil)

// heartbeatLogComponent 日志组件标识
var heartbeatLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHeartbeatRail 创建心跳护栏实例。
//
// 对齐 Python: HeartbeatRail.__init__()
func NewHeartbeatRail() *HeartbeatRail {
	r := &HeartbeatRail{
		DeepAgentRail: *NewDeepAgentRail(),
	}
	r.WithPriority(heartbeatRailPriority)
	return r
}

// Init 初始化 HeartbeatRail。
//
// 获取 systemPromptBuilder、sysOperation、workspace、heartbeatDir。
// 对齐 Python: HeartbeatRail.init() L25-38
func (r *HeartbeatRail) Init(agent agentinterfaces.BaseAgent) error {
	// 对齐 Python L26: self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 对齐 Python L28-29: if not agent.deep_config → log + return
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok || deepAgent.DeepConfig() == nil {
		logger.Info(heartbeatLogComponent).
			Str("event_type", "heartbeat_no_deep_config").
			Msg("deepConfig 未配置")
		return nil
	}

	// 对齐 Python L31-32: if not self.sys_operation → set_sys_operation
	if r.SysOperation() == nil {
		if deepAgent.DeepConfig().SysOperation != nil {
			r.SetSysOperation(deepAgent.DeepConfig().SysOperation)
		}
	}
	// 对齐 Python L33-34: if not self.workspace → set_workspace
	if r.Workspace() == nil {
		if deepAgent.DeepConfig().Workspace != nil {
			r.SetWorkspace(deepAgent.DeepConfig().Workspace)
		}
	}

	// 对齐 Python L35: self.heartbeat_dir = str(self.workspace.get_node_path(WorkspaceNode.HEARTBEAT_MD))
	if r.Workspace() != nil {
		if path := r.Workspace().GetNodePath(workspace.WorkspaceNodeHEARTBEATMD); path != nil {
			r.heartbeatDir = *path
		}
	}

	return nil
}

// Uninit 移除心跳提示词节。
//
// 对齐 Python: HeartbeatRail.uninit() L40-42
func (r *HeartbeatRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionHeartbeat)
	}
	return nil
}

// BeforeModelCall 心跳运行时注入心跳提示词节。
//
// 对齐 Python: HeartbeatRail.before_model_call() L44-63
func (r *HeartbeatRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L45-46: if system_prompt_builder is None or run_kind != HEARTBEAT → return
	if r.systemPromptBuilder == nil {
		return nil
	}

	// 对齐 Python L46: ctx.extra.get("run_kind") != RunKind.HEARTBEAT
	runKind, _ := cbc.Extra()["run_kind"].(string)
	if runKind != string(agentinterfaces.RunKindHeartbeat) {
		return nil
	}

	// 对齐 Python L48-50: if not self.sys_operation → warning + return
	if r.SysOperation() == nil {
		logger.Warn(heartbeatLogComponent).
			Str("event_type", "heartbeat_no_sys_operation").
			Msg("sysOperation 未配置")
		return nil
	}

	// 对齐 Python L52-57: fs.read_file(heartbeat_dir, mode="text")
	fsOp := r.SysOperation().Fs()
	if fsOp == nil {
		logger.Warn(heartbeatLogComponent).
			Str("event_type", "heartbeat_no_fs_operation").
			Msg("FsOperation 为 nil")
		return nil
	}

	readRes, err := fsOp.ReadFile(ctx, r.heartbeatDir)
	content := ""
	if err == nil && readRes != nil {
		// 对齐 Python L54: content = read_res.data.content
		content = readRes.Data
	} else {
		// 对齐 Python L56: logger.warning("HeartbeatRail: failed to read HEARTBEAT.md")
		logger.Warn(heartbeatLogComponent).
			Str("event_type", "heartbeat_read_failed").
			Str("path", r.heartbeatDir).
			Err(err).
			Msg("读取 HEARTBEAT.md 失败")
	}

	// 对齐 Python L58-64: build_heartbeat_section + add_section/remove_section
	// Go 的 BuildHeartbeatSection 总是返回有效的 PromptSection（与 Python 不同，
	// Python 在 heartbeat_content 为 None 时可能返回 None）。
	// 因此始终调用 AddSection。
	heartbeatSection := sections.BuildHeartbeatSection(content, r.systemPromptBuilder.Language())
	r.systemPromptBuilder.AddSection(heartbeatSection)

	return nil
}

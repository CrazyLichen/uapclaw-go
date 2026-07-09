package rails

import (
	"context"
	"runtime"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/code"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationRail 系统操作护栏，注册文件系统、Shell 和代码工具。
// 对齐 Python: SysOperationRail (sys_operation_rail.py)
type SysOperationRail struct {
	DeepAgentRail
	// tools 已注册的工具实例
	tools []tool.Tool
	// withCodeTool 是否包含 CodeTool
	withCodeTool bool
	// readOnly 只读模式
	readOnly bool
	// enableReadImageMultimodal nil=从配置推断, true/false=显式设置
	enableReadImageMultimodal *bool
}

// SysOperationRailOption 配置选项函数
type SysOperationRailOption func(*SysOperationRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sysOpRailPriority SysOperationRail 优先级
	// 对齐 Python: SysOperationRail.priority = 100
	sysOpRailPriority = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 SysOperationRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*SysOperationRail)(nil)

// sysOpRailLogComponent 日志组件标识
var sysOpRailLogComponent = logger.ComponentAgentCore

// 确保编译时引用 hinterfaces 包（SysOperationRail 需要 DeepAgentInterface 类型断言）
var _ hinterfaces.DeepAgentInterface

// ──────────────────────────── 导出函数 ────────────────────────────

// WithCodeTool 启用 CodeTool
func WithCodeTool(enabled bool) SysOperationRailOption {
	return func(r *SysOperationRail) { r.withCodeTool = enabled }
}

// WithReadOnly 设置只读模式
func WithReadOnly(readOnly bool) SysOperationRailOption {
	return func(r *SysOperationRail) { r.readOnly = readOnly }
}

// WithEnableReadImageMultimodal 设置图片多模态
func WithEnableReadImageMultimodal(enabled bool) SysOperationRailOption {
	return func(r *SysOperationRail) { r.enableReadImageMultimodal = &enabled }
}

// NewSysOperationRail 创建系统操作护栏实例。
// 对齐 Python: SysOperationRail.__init__()
func NewSysOperationRail(opts ...SysOperationRailOption) *SysOperationRail {
	r := &SysOperationRail{
		DeepAgentRail: *NewDeepAgentRail(),
	}
	r.WithPriority(sysOpRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Init 初始化系统操作护栏，创建并注册工具。
// 对齐 Python: SysOperationRail.init (sys_operation_rail.py L42-91)
func (r *SysOperationRail) Init(agent agentinterfaces.BaseAgent) error {
	// 获取 language
	var language string
	sb := agent.SystemPromptBuilder()
	if sb != nil {
		language = sb.Language()
	} else {
		language = "cn"
	}

	// 获取 agentID
	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 解析 enableReadImageMultimodal: 如果为 nil，从 DeepConfig 推断，默认 true
	enableImageMultimodal := true
	if r.enableReadImageMultimodal != nil {
		enableImageMultimodal = *r.enableReadImageMultimodal
	} else {
		// 类型断言获取 DeepConfig
		if deepAgent, ok := agent.(hinterfaces.DeepAgentInterface); ok {
			if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
				enableImageMultimodal = deepCfg.EnableReadImageMultimodal
			}
		}
	}

	// 获取 SysOperation（从 Rail 自身引用）
	op := r.SysOperation()

	// 创建工具实例
	// 对齐 Python L51-67
	readTool := filesystem.NewReadFileTool(op, language, agentID, enableImageMultimodal)
	writeTool := filesystem.NewWriteFileTool(op, language, agentID)
	editTool := filesystem.NewEditFileTool(op, language, agentID)
	globTool := filesystem.NewGlobTool(op, language, agentID)
	listDirTool := filesystem.NewListDirTool(op, language, agentID)
	grepTool := filesystem.NewGrepTool(op, language, agentID)
	permConfig := shell.NewPermissionConfig(shell.PermissionModeBypass, nil, nil)
	bashTool := shell.NewBashTool(op, language, agentID, permConfig)

	// 构建工具列表
	// 对齐 Python L69-78
	shared := []tool.Tool{globTool, listDirTool, grepTool, bashTool}
	if r.readOnly {
		r.tools = append([]tool.Tool{readTool}, shared...)
	} else {
		r.tools = append([]tool.Tool{readTool, writeTool, editTool}, shared...)
	}

	// PowerShellTool — 仅 Windows
	// 对齐 Python L63-67: PowerShellTool(...) if os.name == "nt" else None
	if runtime.GOOS == "windows" {
		powershellTool := shell.NewPowerShellTool(op, language, agentID, permConfig)
		r.tools = append(r.tools, powershellTool)
	}

	// CodeTool — 仅 withCodeTool && !readOnly
	// 对齐 Python L77-78
	if r.withCodeTool && !r.readOnly {
		codeTool := code.NewCodeTool(op, language, agentID)
		r.tools = append(r.tools, codeTool)
	}

	// 幂等注册: 已存在则先 remove, 再 add
	// 对齐 Python L85-88
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		toolID := t.Card().ID
		if resourceMgr != nil && toolID != "" {
			existing, err := resourceMgr.GetTool([]string{toolID})
			if err == nil && len(existing) > 0 {
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}
		}
	}

	// 批量注册到 ResourceMgr
	// 对齐 Python L88: Runner.resource_mgr.add_tool(self.tools)
	if resourceMgr != nil {
		for _, t := range r.tools {
			_ = resourceMgr.AddTool(t)
		}
	}

	// 注册到 AbilityManager
	// 对齐 Python L90-91: agent.ability_manager.add(tool.card)
	am := agent.AbilityManager()
	if am != nil {
		for _, t := range r.tools {
			am.Add(t.Card())
		}
	}

	logger.Info(sysOpRailLogComponent).
		Str("event_type", "sys_operation_rail_init").
		Int("tool_count", len(r.tools)).
		Bool("read_only", r.readOnly).
		Bool("with_code_tool", r.withCodeTool).
		Msg("SysOperationRail 已注册工具")

	return nil
}

// Uninit 注销系统操作护栏，移除所有已注册工具。
// 对齐 Python: SysOperationRail.uninit (sys_operation_rail.py L93-103)
func (r *SysOperationRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if len(r.tools) == 0 {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()

	// 对齐 Python L95-102
	for _, t := range r.tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(sysOpRailLogComponent).
						Str("event_type", "sys_operation_rail_uninit").
						Str("tool_name", t.Card().Name).
						Msgf("注销工具失败: %v", rec)
				}
			}()
			// 从 AbilityManager 移除
			name := t.Card().Name
			if name != "" && am != nil {
				am.Remove(name)
			}
			// 从 ResourceMgr 移除
			toolID := t.Card().ID
			if toolID != "" && resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}
		}(t)
	}
	r.tools = nil

	logger.Info(sysOpRailLogComponent).
		Str("event_type", "sys_operation_rail_uninit").
		Msg("SysOperationRail 注销完成")

	return nil
}

// BeforeInvoke 空实现
func (r *SysOperationRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterInvoke 空实现
func (r *SysOperationRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

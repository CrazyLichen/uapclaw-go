package memory

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	cmt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/coding_memory"
	gmt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// liteToolCtx 记忆工具上下文最小接口
type liteToolCtx interface {
	EnsureManager() bool
	IsClosed() bool
}

// MemberMemoryToolkit 成员记忆工具集。
// 对齐 Python MemberMemoryToolkit (member_memory_toolkit.py)
type MemberMemoryToolkit struct {
	// memberName 成员名称
	memberName string
	// teamName 团队名称
	teamName string
	// workspace 工作空间
	workspace *workspace.Workspace
	// scenario 场景
	scenario TeamScenario
	// embeddingConfig 嵌入配置
	embeddingConfig *embedding.EmbeddingConfig
	// sysOperation 系统操作接口
	sysOperation sysop.SysOperation
	// readOnly 是否只读
	readOnly bool
	// manager 记忆索引管理器
	manager lite.MemoryIndexManager
	// ctx 工具上下文（MemoryToolContext 或 CodingMemoryToolContext）
	ctx liteToolCtx
	// tools 工具列表
	tools []tool.Tool
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// memberMemoryLogComponent 日志组件标识
var memberMemoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberMemoryToolkit 创建成员记忆工具集
func NewMemberMemoryToolkit(memberName string, teamName string, ws *workspace.Workspace, scenario TeamScenario, embCfg *embedding.EmbeddingConfig, sysOp sysop.SysOperation, readOnly bool) *MemberMemoryToolkit {
	return &MemberMemoryToolkit{
		memberName:      memberName,
		teamName:        teamName,
		workspace:       ws,
		scenario:        scenario,
		embeddingConfig: embCfg,
		sysOperation:    sysOp,
		readOnly:        readOnly,
	}
}

// Initialize 初始化工具集。对齐 Python MemberMemoryToolkit.initialize()
func (t *MemberMemoryToolkit) Initialize(ctx context.Context) (bool, error) {
	// 已初始化 + manager 未关闭 → 返回 true
	if t.initialized && t.manager != nil {
		if closed, ok := t.manager.(interface{ IsClosed() bool }); ok && !closed.IsClosed() {
			return true, nil
		}
	}

	// 检查 isMemoryEnabled
	if !lite.IsMemoryEnabled() {
		logger.Info(memberMemoryLogComponent).
			Str("event_type", "member_memory_toolkit_init").
			Msg("Memory system is disabled")
		return false, nil
	}

	// 构造 agentID = "{teamName}.{memberName}"
	agentID := fmt.Sprintf("%s.%s", t.teamName, t.memberName)

	// 根据 scenario 确定 nodeName
	nodeName := "memory"
	if t.scenario == TeamScenarioCoding {
		nodeName = "coding_memory"
	}

	// 获取 memoryDir
	memoryDir := ""
	if t.workspace != nil {
		if nodePath := t.workspace.GetNodePath(nodeName); nodePath != nil {
			memoryDir = *nodePath
		}
	}

	settings := lite.CreateMemorySettings(memoryDir, nil)

	// 获取 MemoryIndexManager
	params := lite.MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       t.workspace,
		Settings:        settings,
		EmbeddingConfig: t.embeddingConfig,
		SysOperation:    t.sysOperation,
		NodeName:        nodeName,
	}
	mgr, err := lite.GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(memberMemoryLogComponent).
			Str("event_type", "member_memory_toolkit_init").
			Str("agent_id", agentID).
			Err(err).
			Msg("获取记忆管理器失败")
		return false, fmt.Errorf("初始化记忆管理器失败: %w", err)
	}
	if mgr == nil {
		return false, nil
	}
	t.manager = mgr

	// 根据 scenario 创建上下文和工具
	if t.scenario == TeamScenarioCoding {
		toolCtx := lite.NewCodingMemoryToolContext().
			WithWorkspace(t.workspace).
			WithSettings(settings).
			WithAgentID(agentID).
			WithEmbeddingConfig(t.embeddingConfig).
			WithSysOperation(t.sysOperation).
			WithManager(mgr).
			WithCodingMemoryDir(memoryDir)
		t.ctx = toolCtx
		t.tools = createCodingMemoryToolsForTeam(toolCtx, t.readOnly, t.teamName, t.memberName)
	} else {
		toolCtx := lite.NewMemoryToolContext().
			WithWorkspace(t.workspace).
			WithSettings(settings).
			WithAgentID(agentID).
			WithEmbeddingConfig(t.embeddingConfig).
			WithSysOperation(t.sysOperation).
			WithManager(mgr)
		t.ctx = toolCtx
		t.tools = createGeneralMemoryToolsForTeam(toolCtx, t.readOnly, t.teamName, t.memberName)
	}

	t.initialized = true

	logger.Info(memberMemoryLogComponent).
		Str("event_type", "member_memory_toolkit_init").
		Str("agent_id", agentID).
		Str("scenario", string(t.scenario)).
		Int("tool_count", len(t.tools)).
		Msg("MemberMemoryToolkit 初始化完成")

	return true, nil
}

// GetTools 返回工具列表
func (t *MemberMemoryToolkit) GetTools() []tool.Tool { return t.tools }

// GetToolCards 返回工具卡片列表
func (t *MemberMemoryToolkit) GetToolCards() []tool.ToolCard {
	cards := make([]tool.ToolCard, len(t.tools))
	for i, t := range t.tools {
		cards[i] = *t.Card()
	}
	return cards
}

// Close 关闭工具集
func (t *MemberMemoryToolkit) Close(_ context.Context) error {
	if t.manager != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(memberMemoryLogComponent).
						Str("event_type", "member_memory_toolkit_close").
						Msgf("关闭管理器失败: %v", rec)
				}
			}()
			_ = t.manager.Close()
		}()
		t.manager = nil
	}
	t.ctx = nil
	t.tools = nil
	t.initialized = false
	return nil
}

// Manager 返回记忆索引管理器
func (t *MemberMemoryToolkit) Manager() lite.MemoryIndexManager { return t.manager }

// Ctx 返回工具上下文
func (t *MemberMemoryToolkit) Ctx() liteToolCtx { return t.ctx }

// TeamName 返回团队名称
func (t *MemberMemoryToolkit) TeamName() string { return t.teamName }

// MemberName 返回成员名称
func (t *MemberMemoryToolkit) MemberName() string { return t.memberName }

// ──────────────────────────── 非导出函数 ────────────────────────────

// createCodingMemoryToolsForTeam 创建编程记忆工具集（带 team 前缀）。
// 对齐 Python _create_coding_tools
func createCodingMemoryToolsForTeam(ctx *lite.CodingMemoryToolContext, readOnly bool, teamName string, memberName string) []tool.Tool {
	// 使用工厂函数创建工具，然后根据 readOnly 裁剪
	allTools := cmt.CreateCodingMemoryTools(ctx, "cn", fmt.Sprintf("%s.%s", teamName, memberName))

	if readOnly {
		// 只保留 coding_memory_read
		for _, t := range allTools {
			if t.Card().Name == "coding_memory_read" {
				return []tool.Tool{t}
			}
		}
		return nil
	}
	return allTools
}

// createGeneralMemoryToolsForTeam 创建通用记忆工具集（带 team 前缀）。
// 对齐 Python _create_general_tools
func createGeneralMemoryToolsForTeam(ctx *lite.MemoryToolContext, readOnly bool, teamName string, memberName string) []tool.Tool {
	// 使用工厂函数创建全部 5 个工具
	allTools := gmt.CreateMemoryTools(ctx, "cn", fmt.Sprintf("%s.%s", teamName, memberName))

	if readOnly {
		// 只保留 search/get/read
		var readOnlyTools []tool.Tool
		for _, t := range allTools {
			name := t.Card().Name
			if name == "memory_search" || name == "memory_get" || name == "read_memory" {
				readOnlyTools = append(readOnlyTools, t)
			}
		}
		return readOnlyTools
	}
	return allTools
}

package memory

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	// manager 记忆索引管理器。⤵️ 回填: 7.1
	manager lite.MemoryIndexManager
	// ctx 工具上下文（MemoryToolContext 或 CodingMemoryToolContext）。⤵️ 回填: 7.3
	ctx any
	// tools 工具列表。⤵️ 回填: 7.2
	tools []tool.Tool
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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

// Initialize 初始化工具集。⤵️ 回填: 7.1+7.2+7.3 — 当前返回 false
func (t *MemberMemoryToolkit) Initialize(_ context.Context) (bool, error) { return false, nil }

// GetTools 返回工具列表。⤵️ 回填: 7.2 — 当前返回 nil
func (t *MemberMemoryToolkit) GetTools() []tool.Tool { return nil }

// GetToolCards 返回工具卡片列表。⤵️ 回填: 7.2 — 当前返回 nil
func (t *MemberMemoryToolkit) GetToolCards() []tool.ToolCard { return nil }

// Close 关闭工具集。⤵️ 回填: 7.1 — 当前空实现
func (t *MemberMemoryToolkit) Close(_ context.Context) error { return nil }

// Manager 返回记忆索引管理器
func (t *MemberMemoryToolkit) Manager() lite.MemoryIndexManager { return t.manager }

// Ctx 返回工具上下文
func (t *MemberMemoryToolkit) Ctx() any { return t.ctx }

// TeamName 返回团队名称
func (t *MemberMemoryToolkit) TeamName() string { return t.teamName }

// MemberName 返回成员名称
func (t *MemberMemoryToolkit) MemberName() string { return t.memberName }

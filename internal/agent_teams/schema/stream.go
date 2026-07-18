package schema

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamOutputSchema 带有来源成员身份和角色的输出流数据。
// 对齐 Python: TeamOutputSchema (openjiuwen/agent_teams/schema/stream.py)
//
// 继承 OutputSchema 并添加 source_member 和 role 字段，
// 使团队层消费者能够将每个 chunk 归属到产生它的成员（leader 或 teammate）。
// 非团队生产者（单 Agent、harness 直接流式）继续产出普通 OutputSchema 实例。
type TeamOutputSchema struct {
	// OutputSchema 嵌入标准输出流数据
	stream.OutputSchema
	// SourceMember 产生此 chunk 的成员名
	SourceMember *string `json:"source_member,omitempty"`
	// Role 产生此 chunk 的成员角色
	Role *TeamRole `json:"role,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ChunkObserver 分块观察者回调。
// 对齐 Python: ChunkObserver = Callable[[OutputSchema], Awaitable[None]]
// 每个分块标注来源成员后触发，用于 SpawnManager 将 Teammate chunk 转发到 Leader 的 streamQueue。
type ChunkObserver func(ctx context.Context, chunk stream.Schema) error

// NewTeamOutputSchema 从普通 OutputSchema 构建带标签的团队 chunk。
// 对齐 Python: TeamOutputSchema.from_output(base, source_member=..., role=...)
//
// 返回新实例指针；原始 base 不会被修改，DeepAgent 内部保留其对象标识。
func NewTeamOutputSchema(base stream.OutputSchema, sourceMember *string, role *TeamRole) *TeamOutputSchema {
	return &TeamOutputSchema{
		OutputSchema: base,
		SourceMember: sourceMember,
		Role:         role,
	}
}

// SchemaType 实现 stream.Schema 接口。
func (s *TeamOutputSchema) SchemaType() string { return s.Type }

// Validate 实现 stream.Schema 接口。
func (s *TeamOutputSchema) Validate() error { return s.OutputSchema.Validate() }

// ──────────────────────────── 非导出函数 ────────────────────────────

package extraction

// 此文件已将 EntityDef/RelationDef/HumanEntity/AIEntity/DefaultEntity/DefaultRelation
// 下移到 registry 子包（解决循环依赖），通过重新导出保持 extraction 包的 API 兼容。
//
// Python: EntityDef/RelationDef 定义在 extraction/entity_type_definition.py
// Go 差异：下移到 registry/ 子包，因为 prompts/entity_extraction/ 需要使用
// EntityDef 而不能反向导入 extraction 包（extraction 通过 registry.go
// 空白导入 prompts/cn 和 prompts/en）。
import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EntityDef 实体类型定义（重新导出，实际定义在 registry 包）
//
// Python: EntityDef (entity_type_definition.py)
type EntityDef = registry.EntityDef

// RelationDef 关系类型定义（重新导出，实际定义在 registry 包）
//
// Python: RelationDef (entity_type_definition.py)
type RelationDef = registry.RelationDef

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// HumanEntity 人类实体类型定义（重新导出）
	HumanEntity = registry.HumanEntity
	// AIEntity AI 实体类型定义（重新导出）
	AIEntity = registry.AIEntity
	// DefaultEntity 默认实体类型定义（重新导出）
	DefaultEntity = registry.DefaultEntity
	// DefaultRelation 默认关系类型定义（重新导出）
	DefaultRelation = registry.DefaultRelation
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

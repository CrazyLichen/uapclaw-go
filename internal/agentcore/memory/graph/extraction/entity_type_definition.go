package extraction

import "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"

// ──────────────────────────── 结构体 ────────────────────────────

// EntityDef 实体类型定义
//
// Python: EntityDef (entity_type_definition.py)
type EntityDef struct {
	// Name 类型名称
	Name string `json:"name"`
	// Description 多语言描述（key 为语言代码）
	Description map[string]string `json:"description"`
}

// RelationDef 关系类型定义
//
// Python: RelationDef (entity_type_definition.py)
type RelationDef struct {
	// Name 类型名称
	Name string `json:"name"`
	// Description 多语言描述（key 为语言代码）
	Description map[string]string `json:"description"`
	// LHS 左侧实体类型
	LHS *EntityDef `json:"lhs"`
	// RHS 右侧实体类型
	RHS *EntityDef `json:"rhs"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// HumanEntity 人类实体类型定义
	//
	// Python: HUMAN_ENTITY
	HumanEntity = &EntityDef{
		Name:        "Human",
		Description: registry.HumanEntityDescription,
	}

	// AIEntity AI 实体类型定义
	//
	// Python: AI_ENTITY
	AIEntity = &EntityDef{
		Name:        "AI",
		Description: registry.AIEntityDescription,
	}

	// DefaultEntity 默认实体类型定义
	//
	// Python: ENTITY_DEFINITION
	DefaultEntity = &EntityDef{
		Name:        "Entity",
		Description: registry.EntityDefinitionDescription,
	}

	// DefaultRelation 默认关系类型定义
	//
	// Python: RELATION_DEFINITION
	DefaultRelation = &RelationDef{
		Name:        "Relation",
		Description: registry.RelationDefinitionDescription,
		LHS:         DefaultEntity,
		RHS:         DefaultEntity,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

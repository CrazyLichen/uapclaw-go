package registry

// ──────────────────────────── 结构体 ────────────────────────────

// EntityDefAttr 实体定义属性模板
// 对齐 Python: EntityDefAttr(MultilingualBaseModel)
type EntityDefAttr struct {
	// Content 实体摘要模板（默认空字符串）
	// 对齐 Python: content: str = Field(default="", description="{{[ent_summary]}}")
	Content string `json:"content"`
}

// EntityDef 实体类型定义
//
// Python: EntityDef (entity_type_definition.py)
type EntityDef struct {
	// Name 类型名称
	Name string `json:"name"`
	// Description 多语言描述（key 为语言代码）
	Description map[string]string `json:"description"`
	// Attributes 实体属性模板
	// 对齐 Python: attributes: MultilingualBaseModel = Field(default_factory=EntityDefAttr)
	Attributes *EntityDefAttr `json:"attributes"`
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
	// DefaultEntityDefAttr 默认实体属性模板实例
	DefaultEntityDefAttr = &EntityDefAttr{Content: ""}
)

var (
	// HumanEntity 人类实体类型定义
	//
	// Python: HUMAN_ENTITY
	HumanEntity = &EntityDef{
		Name:        "Human",
		Description: HumanEntityDescription,
		Attributes:  DefaultEntityDefAttr,
	}

	// AIEntity AI 实体类型定义
	//
	// Python: AI_ENTITY
	AIEntity = &EntityDef{
		Name:        "AI",
		Description: AIEntityDescription,
		Attributes:  DefaultEntityDefAttr,
	}

	// DefaultEntity 默认实体类型定义
	//
	// Python: ENTITY_DEFINITION
	DefaultEntity = &EntityDef{
		Name:        "Entity",
		Description: EntityDefinitionDescription,
		Attributes:  DefaultEntityDefAttr,
	}

	// DefaultRelation 默认关系类型定义
	//
	// Python: RELATION_DEFINITION
	DefaultRelation = &RelationDef{
		Name:        "Relation",
		Description: RelationDefinitionDescription,
		LHS:         DefaultEntity,
		RHS:         DefaultEntity,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

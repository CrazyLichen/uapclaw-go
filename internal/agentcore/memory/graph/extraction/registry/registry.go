package registry

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// SchemaInfoHeader Schema 信息头分隔符
//
// Python: SCHEMA_INFO_HEADER = "\n\n---\n"
const SchemaInfoHeader = "\n\n---\n"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// MultilingualDescription 多语言 Schema description 注册表
	//
	// Python: base.py MULTILINGUAL_DESCRIPTION
	MultilingualDescription = map[string]map[string]string{}

	// RegisteredLanguage 已注册的语言集合
	//
	// Python: entity_extraction/base.py REGISTERED_LANGUAGE
	RegisteredLanguage = map[string]struct{}{}

	// SourceDescription 数据源描述格式化模板
	SourceDescription = map[string]string{}

	// RefJSONObjectDef JSON Object 定义标题
	RefJSONObjectDef = map[string]string{}

	// OutputFormat 输出格式定义标题
	OutputFormat = map[string]string{}

	// DisplayEntity 实体展示格式化模板
	DisplayEntity = map[string]string{}

	// MarkCurrentMsg 当前消息标记模板
	MarkCurrentMsg = map[string]string{}

	// MarkHistoryMsg 历史消息标记模板
	MarkHistoryMsg = map[string]string{}

	// RelationFormat 关系格式化模板
	RelationFormat = map[string]string{}

	// NoRelationGiven 无关系时的默认文本
	NoRelationGiven = map[string]string{}

	// EntityDefinitionDescription 实体类型定义描述
	EntityDefinitionDescription = map[string]string{}

	// HumanEntityDescription 人类实体描述
	HumanEntityDescription = map[string]string{}

	// AIEntityDescription AI 实体描述
	AIEntityDescription = map[string]string{}

	// RelationDefinitionDescription 关系类型定义描述
	RelationDefinitionDescription = map[string]string{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

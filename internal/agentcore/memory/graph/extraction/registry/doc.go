// Package registry 提供图记忆抽取模块的多语言注册表和实体/关系类型定义。
//
// 包含全局字典变量（由 cn/en 子包的 init() 填充）以及 EntityDef/RelationDef
// 类型定义和预定义实例。EntityDef/RelationDef 从 extraction 包下移到此包，
// 以便 prompts/entity_extraction/ 子包可以引用这些类型而不会形成循环依赖。
//
// 文件目录：
//
//	registry/
//	├── doc.go                    # 包文档
//	├── registry.go               # 全局注册表变量（MultilingualDescription/RegisteredLanguage 等）
//	└── entity_type_definition.go # EntityDef/RelationDef 结构体 + HumanEntity/AIEntity/DefaultEntity/DefaultRelation
//
// 对应 Python 代码：
//   - openjiuwen/core/memory/graph/extraction/base.py（MULTILINGUAL_DESCRIPTION 等）
//   - openjiuwen/core/memory/graph/extraction/entity_extraction/base.py（REGISTERED_LANGUAGE 等）
//   - openjiuwen/core/memory/graph/extraction/entity_type_definition.py（EntityDef/RelationDef）
package registry

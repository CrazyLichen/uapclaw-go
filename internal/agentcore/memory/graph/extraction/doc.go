// Package extraction 提供 Graph Extraction 实体/关系抽取模块。
//
// 包含多语言 Schema 描述替换机制、输出模型 Go struct 定义和
// JSON Schema 生成。这些组件被 GraphMemory 的每次 LLM 调用依赖，
// 用于生成结构化输出请求和解析 LLM 响应。
//
// 注意：格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/FormatRelationDefinitions/
// FormatExistingEntities/FormatExistingRelations/EnsureValidLanguage）在
// prompts/entity_extraction/ 子包中，对齐 Python prompts/entity_extraction/base.py 位置。
// EntityDef/RelationDef 类型定义在 registry/ 子包中，extraction 包通过类型别名重新导出。
//
// 文件目录：
//
//	extraction/
//	├── doc.go                     # 包文档
//	├── base.go                    # 多语言 description 替换机制 + ResponseFormat/ReadableSchema 生成
//	├── custom_types.go            # JSONLike 类型别名
//	├── entity_type_definition.go  # EntityDef/RelationDef 重新导出（实际定义在 registry/）
//	├── extraction_models.go       # 输出模型 Go struct（EntityExtraction/EntitySummary/...）
//	├── extraction_prompts.go      # 提示词组装函数（待实现）
//	├── parse_response.go          # JSON 解析（parseJSON/ensureList）（待实现）
//	├── registry.go                # 空白导入触发 cn/en init()
//	└── registry/                  # 多语言注册表 + EntityDef/RelationDef 类型定义
//	    ├── doc.go
//	    ├── registry.go
//	    └── entity_type_definition.go
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/
package extraction

// Package extraction 提供 Graph Extraction 实体/关系抽取模块。
//
// 包含多语言 Schema 描述替换机制、输出模型 Go struct 定义、
// 提示词组装函数和 JSON 解析器。这些组件被 GraphMemory 的
// 每次LLM调用依赖，用于生成结构化输出请求和解析LLM响应。
//
// 文件目录：
//
//	extraction/
//	├── doc.go                     # 包文档
//	├── base.go                    # 多语言 description 替换机制 + ResponseFormat/ReadableSchema 生成
//	├── custom_types.go            # JSONLike 类型别名
//	├── entity_type_definition.go  # EntityDef/RelationDef/HumanEntity/AIEntity
//	├── extraction_models.go       # 输出模型 Go struct（EntityExtraction/EntitySummary/...）
//	├── extraction_prompts.go      # 提示词组装函数
//	└── parse_response.go          # JSON 解析（parseJSON/ensureList）
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/
package extraction

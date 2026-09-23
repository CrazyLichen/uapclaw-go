// Package graph_memory 提供图记忆的核心输入校验和辅助函数。
//
// 包含添加记忆和搜索记忆的输入校验，以及消息转换、
// 实体更新、LLM 调用参数组装和图记忆状态结构等工具函数。
//
// 文件目录：
//
//	graph_memory/
//	├── doc.go                  # 包文档
//	├── parse_llm_response.go   # LLM 响应解析（ParseISO/Dict2Relation/ParseAllRelations/ResolveEntities/ParseRelationMerging）
//	├── postprocess.go          # 后处理函数（ValidateEntitiesEpisodes/CreateEpisode/ProcessRelations/ProcessEntities/ParseRelationUUIDsToRemove）
//	├── states.go               # 图记忆状态结构（LookupTables/GraphMemUpdate/GraphMemState 等）
//	├── utils.go                # 消息转换、实体更新、参数组装
//	└── validate_input.go       # 输入校验函数
//
// 对应 Python 代码：openjiuwen/core/memory/graph/graph_memory/
package graph_memory

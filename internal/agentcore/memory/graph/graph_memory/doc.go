// Package graph_memory 提供图记忆主类 GraphMemory 及其管线方法。
//
// GraphMemory 维护知识图谱的添加和检索：通过 LLM 从内容中抽取实体和关系，
// 合并/去重后存储到图数据库，并支持跨实体、关系和片段的语义搜索。
//
// 文件目录：
//
//	graph_memory/
//	├── doc.go                  # 包文档
//	├── base.go                 # GraphMemory 主类（构造/AddMemory/Search/InvokeLLM/实体合并关系解析等）
//	├── parse_llm_response.go   # LLM 响应解析（ParseISO/Dict2Relation/ParseAllRelations/ResolveEntities/ParseRelationMerging）
//	├── postprocess.go          # 后处理函数（ValidateEntitiesEpisodes/CreateEpisode/ProcessRelations/ProcessEntities/ParseRelationUUIDsToRemove）
//	├── states.go               # 图记忆状态结构（asyncTask/LookupTables/GraphMemUpdate/GraphMemState/PersistToDB 等）
//	├── utils.go                # 消息转换、实体更新、参数组装
//	└── validate_input.go       # 输入校验函数
//
// 对应 Python 代码：openjiuwen/core/memory/graph/graph_memory/
package graph_memory

// Package extract 提供长期记忆提取功能，通过 LLM 从对话中提取用户画像、语义记忆和情景记忆碎片。
//
// 本包对齐 Python openjiuwen/core/memory/process/extract/，包含参数定义和核心提取器。
// 后续 7.19 MemoryAnalyzer 和 Generator 编排器将在此包中补充。
//
// 文件目录：
//
//	extract/
//	├── doc.go           # 包文档
//	├── common.go        # 提取参数结构体（ExtractMemoryParams / MemoryOperationParams）
//	└── extractor.go     # LongTermMemoryExtractor 长期记忆提取器
//
// 对应 Python 代码：openjiuwen/core/memory/process/extract/
package extract

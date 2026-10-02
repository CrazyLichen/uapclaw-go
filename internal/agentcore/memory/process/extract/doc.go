// Package extract 提供长期记忆提取和精炼功能，通过 LLM 从对话中提取用户画像、语义记忆和情景记忆碎片，
// 并编排 MemoryAnalyzer → LongTermMemoryExtractor → 语义校验 → 记忆单元输出的完整流水线。
//
// 本包对齐 Python openjiuwen/core/memory/process/extract/，包含参数定义、核心提取器、分析器和编排器。
//
// 文件目录：
//
//	extract/
//	├── doc.go           # 包文档
//	├── common.go        # 提取参数结构体（ExtractMemoryParams / MemoryOperationParams）
//	├── extractor.go     # ExtractLongTermMemory 长期记忆提取器
//	├── analyzer.go      # MemoryAnalyzer + VariableResult + MemoryAnalyzerResult
//	└── generator.go     # Generator 记忆生成编排器
//
// 对应 Python 代码：openjiuwen/core/memory/process/extract/
package extract

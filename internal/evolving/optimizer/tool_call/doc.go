// Package tool_call 提供工具描述优化器，通过两阶段 Beam Search
// 迭代优化工具的自然语言描述，提升 LLM function calling 准确率。
//
// 两阶段流程：
//  1. Example Stage（APICallToExampleMethod）：生成 API 调用示例，形成正负例集
//  2. Description Stage（ToolDescriptionMethod）：基于正负例批判并增强描述
//
// 最终通过 ToolDescriptionReviewer（clean → cross_check → translate）三步后处理，
// 输出结构化的高质量工具描述。
//
// 文件目录：
//
//	tool_call/
//	├── doc.go                # 包文档
//	├── base.go               # ToolOptimizerBase 核心
//	├── format.go             # ParseJSON / FormatPromptLlama
//	├── schema_extractor.go   # ExtractSchema
//	├── default_configs.go    # DefaultConfigEg / DefaultConfigDesc
//	├── rits.go               # InvokeWithVerify 薄包装
//	├── beam_search.go        # BeamSearch + TreeNode
//	├── api_wrapper.go        # SimpleAPIWrapper / SimpleAPIWrapperFromCallable
//	├── api_wrapper_mcp.go    # MakeSyncMCPCaller（integration build tag）
//	├── base_method.go        # BaseMethod + ProduceAnswerFromAPICall
//	├── eval.go               # SimpleEval
//	├── example_method.go     # APICallToExampleMethod
//	├── description_method.go # ToolDescriptionMethod
//	├── reviewer.go           # ToolDescriptionReviewer
//	└── pipeline.go           # CustomizedPipeline
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/tool_call/
package tool_call

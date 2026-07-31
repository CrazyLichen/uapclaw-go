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
//	├── base.go               # ToolOptimizerBase（工具优化器基类） 核心
//	├── format.go             # ParseJSON（解析JSON）/FormatPromptLlama（格式化Llama提示词）
//	├── schema_extractor.go   # ExtractSchema（提取Schema）
//	├── default_configs.go    # DefaultConfigEg（默认示例配置）/DefaultConfigDesc（默认描述配置）
//	├── rits.go               # InvokeWithVerify（调用并验证） 薄包装
//	├── beam_search.go        # BeamSearch（束搜索） + TreeNode（树节点）
//	├── api_wrapper.go        # SimpleAPIWrapperFromCallable（简单API包装器）
//	├── api_wrapper_mcp.go    # MakeSyncMCPCaller（创建同步MCP调用者）（integration build tag）
//	├── base_method.go        # BaseMethod（基础方法） + ProduceAnswerFromAPICall（从API调用生成答案）
//	├── eval.go               # SimpleEval（简单评估器）
//	├── example_method.go     # APICallToExampleMethod（API调用转示例方法）
//	├── description_method.go # ToolDescriptionMethod（工具描述方法）
//	├── reviewer.go           # ToolDescriptionReviewer（工具描述审查器）
//	└── pipeline.go           # CustomizedPipeline（自定义流水线）
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/tool_call/
package tool_call

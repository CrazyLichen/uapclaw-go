// Package optimizer 提供自演化系统的维度优化器。
//
// BaseOptimizer 定义优化器的公共接口和 Mixin 辅助结构体，
// 子优化器嵌入 Mixin 获得公共字段和方法，自己实现 Backward/Step 等核心方法。
// TextualParameter 是梯度容器，存储 target→梯度值和可选描述。
//
// 文件目录：
//
//	optimizer/
//	├── doc.go                # 包文档
//	├── base.go               # BaseOptimizer（基础优化器） 接口 + BaseOptimizerMixin（优化器混入） + TextualParameter（文本参数）
//	├── llm_call/             # LLM 维度提示词优化器
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # LLMCallOptimizerBase（LLM调用优化器基类） 嵌入结构体
//	│   ├── instruction_optimizer.go # InstructionOptimizer（指令优化器） 核心实现
//	│   └── templates.go      # PromptTemplate（提示词模板） 模板常量
//	├── tool_call/            # 工具描述优化器（两阶段 Beam Search）
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # ToolOptimizerBase（工具优化器基类） 核心
//	│   ├── format.go         # ParseJSON（解析JSON）/FormatPromptLlama（格式化Llama提示词）
//	│   ├── schema_extractor.go # ExtractSchema（提取Schema）
//	│   ├── default_configs.go  # DefaultConfigEg（默认示例配置）/DefaultConfigDesc（默认描述配置）
//	│   ├── rits.go           # InvokeWithVerify（调用并验证） 薄包装
//	│   ├── beam_search.go    # BeamSearch（束搜索） + TreeNode（树节点）
//	│   ├── api_wrapper.go    # SimpleAPIWrapperFromCallable（简单API包装器）
//	│   ├── api_wrapper_mcp.go # MakeSyncMCPCaller（创建同步MCP调用者）（integration tag）
//	│   ├── base_method.go    # BaseMethod（基础方法） + ProduceAnswerFromAPICall（从API调用生成答案）
//	│   ├── eval.go           # SimpleEval（简单评估器） 评估器
//	│   ├── example_method.go # APICallToExampleMethod（API调用转示例方法）
//	│   ├── description_method.go # ToolDescriptionMethod（工具描述方法）
//	│   ├── reviewer.go       # ToolDescriptionReviewer（工具描述审查器）
//	│   └── pipeline.go       # CustomizedPipeline（自定义流水线）
//	├── memory_call/          # 记忆维度优化器
//	│   ├── doc.go            # 包文档
//	│   └── base.go           # MemoryOptimizerBase（记忆优化器基类） 声明式骨架
//	├── skill_call/          # 技能经验优化器
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # SkillExperienceOptimizerBase（技能经验优化器基类） 共享字段/方法/常量
//	│   ├── draft_parser.go   # ParsedExperienceDraft + JSON 提取/解析辅助函数
//	│   ├── experience_optimizer.go # SkillExperienceOptimizer（个体技能经验优化器） Backward/Step/GenerateRecords/RetryParse + 辅助函数
//	│   ├── team_optimizer.go  # TeamSkillExperienceOptimizer（团队技能经验优化器） 双路径 GenerateRecords/UserPatch/TrajectoryPatch/RegenerateBody/callLLM + 辅助函数
//	│   └── templates.go      # 提示词模板（CN+EN 双语，一比一复刻 Python 原文）
//	└── llm_resilience/       # LLM 弹性重试策略
//	    ├── doc.go            # 包文档
//	    └── llm_resilience.go # LLMInvokePolicy（LLM调用策略） + InvokeTextWithRetry（带重试的文本调用）
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/base.py
package optimizer

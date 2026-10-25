// Package utils 提供 devtools agent_builder 共享工具函数与常量。
//
// 本包是 devtools 工具面（agent_builder NL→DSL 生成器）的最底层地基：
// 所有 LLM 调用（Clarifier/Generator/DLGenerator/ResourceRetriever）的输出
// 都经过本包的"剥围栏→安全解析→格式化上下文"管线。
//
// 流程位置：开发者/前端 → AgentBuilder(14.9) → Executor(14.5) →
// BaseAgentBuilder 状态机(14.6) → Clarifier/Generator(14.7) 与
// DLGenerator/Reflector(14.8) → 本包函数（LLM 输出解析咽喉，13 处消费）。
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── enums.go            # 4 枚举类型+25 常量+Parse×4+All×4（对齐 Python enums.py）
//	├── constants.go        # 20 个共享常量（含 13 个死常量，对齐 Python 全量）
//	├── utils.go            # 7 个工具函数 + 2 个预编译正则
//	├── constants_test.go   # 21.5 用例（对齐 Python test_constants.py）
//	├── enums_test.go       # 12 unit+4.5 system 回填+增强用例（对齐 Python test_enums.py + system）
//	└── utils_test.go       # 39.5+17 用例（对齐 Python test_utils.py + system 3 + 增强）
//
// 对应 Python 代码：openjiuwen/dev_tools/agent_builder/utils/
package utils

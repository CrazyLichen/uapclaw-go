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
//	├── constants.go        # 20 个共享常量（含 13 个死常量，对齐 Python 全量）
//	├── utils.go            # 7 个工具函数 + 2 个预编译正则
//	├── constants_test.go   # 21.5 用例（对齐 Python test_constants.py）
//	└── utils_test.go       # 39.5 用例（对齐 Python test_utils.py + system 3 + 增强 4）
//
// 对应 Python 代码：openjiuwen/dev_tools/agent_builder/utils/
package utils

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ── Workflow 相关常量（14.8 workflow 链消费）──

	// WorkflowRequestContent 工作流请求话术
	WorkflowRequestContent = "Please provide your desired workflow description so I can generate " +
		"the corresponding flowchart for you. If unclear, you can reply 'unclear' " +
		"and I will plan the process for you."

	// WorkflowDesignResponseContent 工作流设计响应前缀
	WorkflowDesignResponseContent = "Workflow design content:\n"

	// GenerateDLFromDesignContent 从设计生成 DL 话术
	GenerateDLFromDesignContent = "Please generate the corresponding Process Definition Language (DL) " +
		"description based on the following workflow design content:\n"

	// ModifyDLContent 修正 DL 话术
	ModifyDLContent = "Please correct the Process Definition Language (DL) based on the following error message:\n"

	// ── 默认配置（14.5/14.8 消费）──

	// DefaultMaxHistorySize 历史消息默认上限
	DefaultMaxHistorySize = 50

	// DefaultMaxRetries 默认重试次数
	DefaultMaxRetries = 3

	// DefaultTimeout 默认超时秒数（Python 侧死常量，全量对齐保留）
	DefaultTimeout = 30

	// ── 资源类型（死常量，全量对齐保留）──

	// ResourceTypePlugin 插件资源类型
	ResourceTypePlugin = "plugin"

	// ResourceTypeKnowledge 知识库资源类型
	ResourceTypeKnowledge = "knowledge"

	// ResourceTypeWorkflow 工作流资源类型
	ResourceTypeWorkflow = "workflow"

	// ── 正则模式（本包 utils.go 消费）──

	// JSONExtractPattern JSON 围栏提取正则（对齐 Python JSON_EXTRACT_PATTERN，
	// Go 侧需配合 (?s) 标志使 . 匹配换行，等价 Python [\s\S]）
	JSONExtractPattern = "```(?:json)?\\s*(.*?)\\s*```"

	// ── API 相关（死常量，全量对齐保留）──

	// APIVersion API 版本号
	APIVersion = "v1"

	// APIBasePath API 基础路径（编译期拼接，等价 Python f-string）
	APIBasePath = "/api/" + APIVersion

	// ── 进度相关（死常量，全量对齐保留；14.3 消费与否届时确认）──

	// ProgressUpdateInterval 进度更新间隔秒
	ProgressUpdateInterval = 0.1

	// ProgressHeartbeatInterval 进度心跳间隔秒
	ProgressHeartbeatInterval = 30.0

	// ── 长度限制（死常量，全量对齐保留）──

	// MaxQueryLength 查询最大长度
	MaxQueryLength = 5000

	// MinQueryLength 查询最小长度
	MinQueryLength = 1

	// MaxSessionIDLength 会话 ID 最大长度
	MaxSessionIDLength = 255

	// MaxHistorySize 历史最大条数
	MaxHistorySize = 1000

	// MinHistorySize 历史最小条数
	MinHistorySize = 1
)

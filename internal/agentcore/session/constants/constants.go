package constants

// ──────────────────────────── 结构体 ────────────────────────────

// EnvConfigEntry 环境变量到配置键的映射条目
type EnvConfigEntry struct {
	// EnvKey 环境变量键名（如 "WORKFLOW_EXECUTE_TIMEOUT"）
	EnvKey string
	// ConfigKey 配置键名（如 "_execute_timeout"）
	ConfigKey string
}

// ──────────────────────────── 枚举 ────────────────────────────

// EnvConfigType 环境变量值类型枚举
type EnvConfigType int

const (
	// EnvConfigTypeFloat 浮点类型
	EnvConfigTypeFloat EnvConfigType = iota
	// EnvConfigTypeInt 整数类型
	EnvConfigTypeInt
	// EnvConfigTypeBool 布尔类型
	EnvConfigTypeBool
)

// ──────────────────────────── 常量 ────────────────────────────

// 配置键名（对应 Python 配置键，存入 env 字典的 key）
const (
	// WorkflowExecuteTimeoutKey 工作流执行超时
	WorkflowExecuteTimeoutKey = "_execute_timeout"
	// WorkflowStreamFrameTimeoutKey 工作流流帧超时
	WorkflowStreamFrameTimeoutKey = "_stream_frame_timeout"
	// WorkflowStreamFirstFrameTimeoutKey 工作流首帧超时
	WorkflowStreamFirstFrameTimeoutKey = "_stream_first_frame_timeout"
	// CompStreamCallTimeoutKey 组件流调用超时
	CompStreamCallTimeoutKey = "_comp_stream_call_timeout"
	// StreamInputGenTimeoutKey 流输入生成器超时
	StreamInputGenTimeoutKey = "_stream_input_generator_timeout"
	// EndCompTemplateRenderPositionTimeoutKey 终端组件模板渲染位置超时
	EndCompTemplateRenderPositionTimeoutKey = "_end_comp_template_render_position_timeout"
	// EndCompTemplateBranchRenderTimeoutKey 终端组件模板分支渲染超时
	EndCompTemplateBranchRenderTimeoutKey = "_end_comp_template_branch_render_timeout"
	// LoopNumberMaxLimitKey 循环次数上限
	LoopNumberMaxLimitKey = "_loop_number_max_limit"
	// ForceDelWorkflowStateKey 强制删除工作流状态
	ForceDelWorkflowStateKey = "_force_del_workflow_state"
)

// 环境变量键名（对应 Python 的环境变量键名，从 os.Getenv 读取的 key）
const (
	// WorkflowExecuteTimeoutEnvKey 工作流执行超时环境变量
	WorkflowExecuteTimeoutEnvKey = "WORKFLOW_EXECUTE_TIMEOUT"
	// WorkflowStreamFrameTimeoutEnvKey 工作流流帧超时环境变量
	WorkflowStreamFrameTimeoutEnvKey = "WORKFLOW_STREAM_FRAME_TIMEOUT"
	// WorkflowStreamFirstFrameTimeoutEnvKey 工作流首帧超时环境变量
	WorkflowStreamFirstFrameTimeoutEnvKey = "WORKFLOW_STREAM_FIRST_FRAME_TIMEOUT"
	// CompStreamCallTimeoutEnvKey 组件流调用超时环境变量
	CompStreamCallTimeoutEnvKey = "COMP_STREAM_CALL_TIMEOUT"
	// StreamInputGenTimeoutEnvKey 流输入生成器超时环境变量
	StreamInputGenTimeoutEnvKey = "STREAM_INPUT_GEN_TIMEOUT"
	// LoopNumberMaxLimitEnvKey 循环次数上限环境变量
	LoopNumberMaxLimitEnvKey = "LOOP_NUMBER_MAX_LIMIT"
	// ForceDelWorkflowStateEnvKey 强制删除工作流状态环境变量
	ForceDelWorkflowStateEnvKey = "FORCE_DEL_WORKFLOW_STATE"
)

// 默认值
const (
	// WorkflowExecuteTimeoutDefault 工作流执行超时默认值（秒）
	WorkflowExecuteTimeoutDefault = 60.0
	// WorkflowStreamFrameTimeoutDefault 工作流流帧超时默认值（-1 表示不超时）
	WorkflowStreamFrameTimeoutDefault = -1.0
	// WorkflowStreamFirstFrameTimeoutDefault 工作流首帧超时默认值（-1 表示不超时）
	WorkflowStreamFirstFrameTimeoutDefault = -1.0
	// CompStreamCallTimeoutDefault 组件流调用超时默认值（-1 表示不超时）
	CompStreamCallTimeoutDefault = -1.0
	// StreamInputGenTimeoutDefault 流输入生成器超时默认值（-1 表示不超时）
	StreamInputGenTimeoutDefault = -1.0
	// EndCompTemplateRenderPositionTimeoutDefault 终端组件模板渲染位置超时默认值（秒）
	EndCompTemplateRenderPositionTimeoutDefault = 5.0
	// EndCompTemplateBranchRenderTimeoutDefault 终端组件模板分支渲染超时默认值（秒）
	EndCompTemplateBranchRenderTimeoutDefault = 5.0
	// LoopNumberMaxLimitDefault 循环次数上限默认值
	LoopNumberMaxLimitDefault = 1000
	// ForceDelWorkflowStateDefault 强制删除工作流状态默认值
	ForceDelWorkflowStateDefault = false
)

// 交互输入在 session state 中的键（从 checkpointer/base.go 迁移）
const (
	// InteractiveInputKey 交互输入在 session state 中的键。
	// 对应 Python: openjiuwen/core/common/constants/constant.py (INTERACTIVE_INPUT)
	InteractiveInputKey = "__interactive_input__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// EnvConfigKeys 环境变量键到配置键的映射表。
	// 对应 Python: _ENV_CONFIG_KEYS
	EnvConfigKeys = []EnvConfigEntry{
		{WorkflowExecuteTimeoutEnvKey, WorkflowExecuteTimeoutKey},
		{WorkflowStreamFrameTimeoutEnvKey, WorkflowStreamFrameTimeoutKey},
		{WorkflowStreamFirstFrameTimeoutEnvKey, WorkflowStreamFirstFrameTimeoutKey},
		{CompStreamCallTimeoutEnvKey, CompStreamCallTimeoutKey},
		{StreamInputGenTimeoutEnvKey, StreamInputGenTimeoutKey},
		{LoopNumberMaxLimitEnvKey, LoopNumberMaxLimitKey},
		{ForceDelWorkflowStateEnvKey, ForceDelWorkflowStateKey},
	}

	// EnvConfigTypes 环境变量键到值类型的映射表。
	// 对应 Python: _ENV_CONFIG_TYPES
	EnvConfigTypes = map[string]EnvConfigType{
		WorkflowExecuteTimeoutEnvKey:          EnvConfigTypeFloat,
		WorkflowStreamFrameTimeoutEnvKey:      EnvConfigTypeFloat,
		WorkflowStreamFirstFrameTimeoutEnvKey: EnvConfigTypeFloat,
		CompStreamCallTimeoutEnvKey:           EnvConfigTypeFloat,
		StreamInputGenTimeoutEnvKey:           EnvConfigTypeFloat,
		LoopNumberMaxLimitEnvKey:              EnvConfigTypeInt,
		ForceDelWorkflowStateEnvKey:           EnvConfigTypeBool,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuiltinDefaults 返回内置默认配置的完整字典。
// 对应 Python: Config._load_builtin_configs_ 中的 builtin_configs 字典
func BuiltinDefaults() map[string]any {
	return map[string]any{
		CompStreamCallTimeoutKey:                CompStreamCallTimeoutDefault,
		StreamInputGenTimeoutKey:                StreamInputGenTimeoutDefault,
		EndCompTemplateBranchRenderTimeoutKey:   EndCompTemplateBranchRenderTimeoutDefault,
		EndCompTemplateRenderPositionTimeoutKey: EndCompTemplateRenderPositionTimeoutDefault,
		WorkflowExecuteTimeoutKey:               WorkflowExecuteTimeoutDefault,
		WorkflowStreamFrameTimeoutKey:           WorkflowStreamFrameTimeoutDefault,
		WorkflowStreamFirstFrameTimeoutKey:      WorkflowStreamFirstFrameTimeoutDefault,
		LoopNumberMaxLimitKey:                   LoopNumberMaxLimitDefault,
		ForceDelWorkflowStateKey:                ForceDelWorkflowStateDefault,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

package workflow

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// CompIOConfig 组件输入/输出配置。
// Python: workflow_config.py CompIOConfig；inputs/outputs_schema 可为 dict 或变换函数（Transformer），
// Go 用 any 承载（map[string]any 或 MessageTransformer，MessageTransformer 定义见 session/utils）。
type CompIOConfig struct {
	// InputsSchema 输入 schema（map[string]any 或变换函数）
	InputsSchema any
	// OutputsSchema 输出 schema（map[string]any 或变换函数）
	OutputsSchema any
}

// ExceptionConfig 节点异常处理配置（Python extra="allow" 的自由字段 Go 用 map 承载）。
// Python: workflow_config.py ExceptionConfig。
type ExceptionConfig struct {
	// HandleType 处理类型，默认 "interrupt"
	HandleType string
	// Extra 附加字段（Python model_config extra=allow）
	Extra map[string]any
}

// NodeSpec 工作流节点/组件规格。
// Python: workflow_config.py NodeSpec。
type NodeSpec struct {
	// IOConfigs 常规（非流式）I/O 配置
	IOConfigs *CompIOConfig
	// StreamIOConfigs 流式 I/O 配置
	StreamIOConfigs *CompIOConfig
	// Abilities 组件支持的能力列表（default_factory=list）
	Abilities []components.ComponentAbility
	// MaxRetries 最大重试次数（ge=0）
	MaxRetries int
	// Timeout 节点执行超时秒数；<=0 表示不超时
	Timeout float64
	// ExceptionConfig 异常恢复配置
	ExceptionConfig *ExceptionConfig
	// Name 人类可读组件名；空串在 trace 时回退 comp_id（Python Optional[str]，偏差：nil→空串）
	Name string
}

// WorkflowSpec 工作流结构完整规格。
// Python: workflow_config.py WorkflowSpec。
type WorkflowSpec struct {
	// Edges 常规数据流边（source -> [targets]）
	Edges map[string][]string
	// StreamEdges 流式数据流边（source -> [targets]）
	StreamEdges map[string][]string
	// CompConfigs 每个组件的配置
	CompConfigs map[string]*NodeSpec
	// StreamSourceGroups 流式消费者的 CNF 源组，键为 producer_id-ABILITY
	StreamSourceGroups map[string][][]string
	// StartNodes 起始节点
	StartNodes []string
}

// NewNodeSpec 创建带 Python 默认值的 NodeSpec
func NewNodeSpec() *NodeSpec {
	return &NodeSpec{
		Abilities: []components.ComponentAbility{},
		Timeout:   -1.0,
	}
}

// NewWorkflowSpec 创建带 Python 默认值的 WorkflowSpec（default_factory=dict/list）
func NewWorkflowSpec() *WorkflowSpec {
	return &WorkflowSpec{
		Edges:              map[string][]string{},
		StreamEdges:        map[string][]string{},
		CompConfigs:        map[string]*NodeSpec{},
		StreamSourceGroups: map[string][][]string{},
		StartNodes:         []string{},
	}
}

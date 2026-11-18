package workflow

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CompIOConfig 组件输入/输出配置。
// Python: workflow_config.py CompIOConfig；inputs/outputs_schema 可为 dict 或变换函数（Transformer），
// Go 用 any 承载（偏差 D-15-1：Dict | Transformer Union 无 Go 对应；Transformer 见 session/utils）。
type CompIOConfig struct {
	// InputsSchema 输入 schema（map[string]any 或 Transformer）
	InputsSchema any `json:"inputs_schema"`
	// OutputsSchema 输出 schema（map[string]any 或 Transformer）
	OutputsSchema any `json:"outputs_schema"`
}

// ExceptionConfig 节点异常处理配置。
// Python: workflow_config.py ExceptionConfig；model_config extra=allow 的自由字段
// Go 用 Extra map 承载，MarshalJSON/UnmarshalJSON 平铺序列化（偏差 D-15-3）。
type ExceptionConfig struct {
	// HandleType 处理类型，默认 "interrupt"
	HandleType string `json:"handle_type"`
	// Extra 附加字段（Python extra=allow）
	Extra map[string]any `json:"-"`
}

// NodeSpec 工作流节点/组件规格。
// Python: workflow_config.py NodeSpec。
type NodeSpec struct {
	// IOConfigs 常规（非流式）I/O 配置
	IOConfigs *CompIOConfig `json:"io_configs"`
	// StreamIOConfigs 流式 I/O 配置
	StreamIOConfigs *CompIOConfig `json:"stream_io_configs"`
	// Abilities 组件支持的能力列表（default_factory=list）
	Abilities []components.ComponentAbility `json:"abilities"`
	// MaxRetries 最大重试次数（ge=0，越界拒绝构造）
	MaxRetries int `json:"max_retries"`
	// Timeout 节点执行超时秒数；<=0 表示不超时
	Timeout float64 `json:"timeout"`
	// ExceptionConfig 异常恢复配置
	ExceptionConfig *ExceptionConfig `json:"exception_config"`
	// Name 人类可读组件名；nil 在 trace 时回退 comp_id（Python Optional[str]，偏差 D-15-2）
	Name *string `json:"name"`
}

// WorkflowSpec 工作流结构完整规格。
// Python: workflow_config.py WorkflowSpec。
type WorkflowSpec struct {
	// Edges 常规数据流边（source -> [targets]）
	Edges map[string][]string `json:"edges"`
	// StreamEdges 流式数据流边（source -> [targets]）
	StreamEdges map[string][]string `json:"stream_edges"`
	// CompConfigs 每个组件的配置
	CompConfigs map[string]*NodeSpec `json:"comp_configs"`
	// StreamSourceGroups 流式消费者的 CNF 源组，键为 producer_id-ABILITY
	StreamSourceGroups map[string][][]string `json:"stream_source_groups"`
	// StartNodes 起始节点
	StartNodes []string `json:"start_nodes"`
}

// WorkflowConfig 工作流身份、图规格与嵌套上限。
// Python: workflow_config.py WorkflowConfig。
type WorkflowConfig struct {
	// Card 工作流元数据卡（必填，缺省拒绝构造）
	Card *WorkflowCard `json:"card"`
	// Spec 图结构规格（未传补建空 spec，对齐 default_factory）
	Spec *WorkflowSpec `json:"spec"`
	// WorkflowMaxNestingDepth 嵌套上限（ge=0, le=10，默认 5）
	WorkflowMaxNestingDepth int `json:"workflow_max_nesting_depth"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WorkflowConfigOption WorkflowConfig 构造选项。
type WorkflowConfigOption func(*WorkflowConfig)

// WithWorkflowSpec 设置图结构规格；nil 时构造函数补建空 spec（对齐 default_factory）。
func WithWorkflowSpec(spec *WorkflowSpec) WorkflowConfigOption {
	return func(c *WorkflowConfig) { c.Spec = spec }
}

// WithMaxNestingDepth 设置嵌套上限；[0,10] 之外的值由构造函数拒绝（ge=0, le=10）。
func WithMaxNestingDepth(depth int) WorkflowConfigOption {
	return func(c *WorkflowConfig) { c.WorkflowMaxNestingDepth = depth }
}

// NodeSpecOption NodeSpec 构造选项。
type NodeSpecOption func(*NodeSpec)

// WithMaxRetries 设置最大重试次数；负值由构造函数拒绝（ge=0）。
func WithMaxRetries(n int) NodeSpecOption {
	return func(s *NodeSpec) { s.MaxRetries = n }
}

// NewNodeSpec 创建带 Python 默认值的 NodeSpec（Abilities 空列表、Timeout=-1.0）。
// max_retries ge=0：负值拒绝构造，对齐 pydantic ValidationError 抛出点。
func NewNodeSpec(opts ...NodeSpecOption) (*NodeSpec, error) {
	ns := &NodeSpec{
		Abilities: []components.ComponentAbility{},
		Timeout:   -1.0,
	}
	for _, opt := range opts {
		opt(ns)
	}
	if ns.MaxRetries < 0 {
		return nil, errWorkflowConfig("max_retries 越界: %d（ge=0，对齐 pydantic ValidationError）", ns.MaxRetries)
	}
	return ns, nil
}

// NewWorkflowSpec 创建带 Python 默认值的 WorkflowSpec（default_factory=dict/list）。
func NewWorkflowSpec() *WorkflowSpec {
	return &WorkflowSpec{
		Edges:              map[string][]string{},
		StreamEdges:        map[string][]string{},
		CompConfigs:        map[string]*NodeSpec{},
		StreamSourceGroups: map[string][][]string{},
		StartNodes:         []string{},
	}
}

// NewWorkflowConfig 创建 WorkflowConfig。
// card 必填（nil 拒绝，对齐 pydantic 缺必填 ValidationError）；
// spec 未传补建空 spec（default_factory=WorkflowSpec）；深度默认 5、合法域 [0,10]（越界拒绝）。
// 注意：0 是合法值也是 Go 零值，故「未传默认 5」由选项机制承载、禁止零值判断；
// Python _workflow.py:151 的空值兜底分支（workflow_config 为空时新建）由此收敛。
func NewWorkflowConfig(card *WorkflowCard, opts ...WorkflowConfigOption) (*WorkflowConfig, error) {
	if card == nil {
		return nil, errWorkflowConfig("card 必填（对齐 pydantic 缺必填 ValidationError）")
	}
	c := &WorkflowConfig{Card: card, WorkflowMaxNestingDepth: 5}
	for _, opt := range opts {
		opt(c)
	}
	if c.WorkflowMaxNestingDepth < 0 || c.WorkflowMaxNestingDepth > 10 {
		return nil, errWorkflowConfig("workflow_max_nesting_depth 越界: %d（ge=0, le=10）", c.WorkflowMaxNestingDepth)
	}
	if c.Spec == nil {
		c.Spec = NewWorkflowSpec()
	}
	return c, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errWorkflowConfig 构造校验错误。
func errWorkflowConfig(format string, args ...any) error {
	return fmt.Errorf("workflow_config: "+format, args...)
}

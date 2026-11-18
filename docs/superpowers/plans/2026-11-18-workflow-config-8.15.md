# 8.15 WorkflowConfig 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整同步 `WorkflowConfig` 本体（含 pydantic 校验拒绝语义与 JSON 序列化面），同改动内收口三件回填（① NodeConfig 管路收窄 ② tracer workflow metadata 提取 ③ 8.12 D6 骨架销账）并回收 8.11 偏差 D-3。

**Architecture:** 在 8.12 前置骨架 `internal/agentcore/workflow/workflow_config.go` 上补齐 `WorkflowConfig` 本体与构造校验；随后沿依赖方向 session/config → session/internal → session/tracer → graph/vertex 逐层收窄类型；最后删除 `applyNodeConfig`/`ApplyNodeConfig`（D-3 内联回收）并销账。

**Tech Stack:** Go 1.x、标准库 `encoding/json`、go test；无新第三方依赖。

**Spec:** docs/superpowers/specs/2026-11-18-workflow-config-8.15-design.md

**测试命令：** 全量回归用 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（CI 同款标签，缺标签 memory 包 FTS 假失败）；单包可裸跑。

---

### Task 1: WorkflowConfig 本体 + 构造校验 + json tag

**Files:**
- Modify: `internal/agentcore/workflow/workflow_config.go`
- Test: `internal/agentcore/workflow/workflow_config_test.go`（新建）

- [ ] **Step 1: 写失败测试（构造校验 + 默认值 + 序列化键名）**

```go
package workflow

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

// TestNewWorkflowConfig_默认值 card 必填 + spec 补建 + 深度默认 5
func TestNewWorkflowConfig_默认值(t *testing.T) {
	wfc, err := NewWorkflowConfig(&WorkflowCard{Version: "v1"})
	if err != nil {
		t.Fatalf("合法构造不应报错: %v", err)
	}
	if wfc.Spec == nil || wfc.Spec.Edges == nil {
		t.Fatal("spec 为 nil 应补建空 spec（default_factory）")
	}
	if wfc.WorkflowMaxNestingDepth != 5 {
		t.Fatalf("默认嵌套深度应 5, got %d", wfc.WorkflowMaxNestingDepth)
	}
	if wfc.Card.Version != "v1" {
		t.Fatal("card 应原样保留")
	}
}

// TestNewWorkflowConfig_校验拒绝 复刻 pydantic ValidationError 拒绝语义
func TestNewWorkflowConfig_校验拒绝(t *testing.T) {
	if _, err := NewWorkflowConfig(nil); err == nil {
		t.Fatal("card nil 应 error（缺必填）")
	}
	if _, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(-1)); err == nil {
		t.Fatal("depth -1 应 error（ge=0）")
	}
	if _, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(11)); err == nil {
		t.Fatal("depth 11 应 error（le=10）")
	}
	wfc, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(0))
	if err != nil || wfc.WorkflowMaxNestingDepth != 0 {
		t.Fatalf("depth 0 是合法值: err=%v depth=%d", err, wfc.WorkflowMaxNestingDepth)
	}
}

// TestNewWorkflowConfig_WithWorkflowSpec nil 补建 / 非 nil 保留
func TestNewWorkflowConfig_WithWorkflowSpec(t *testing.T) {
	wfc, _ := NewWorkflowConfig(&WorkflowCard{}, WithWorkflowSpec(nil))
	if wfc.Spec == nil {
		t.Fatal("显式 nil spec 应补建")
	}
	spec := NewWorkflowSpec()
	spec.StartNodes = []string{"a"}
	wfc2, _ := NewWorkflowConfig(&WorkflowCard{}, WithWorkflowSpec(spec))
	if len(wfc2.Spec.StartNodes) != 1 {
		t.Fatal("非 nil spec 应原样保留")
	}
}

// TestNewNodeSpec_校验 max_retries ge=0
func TestNewNodeSpec_校验(t *testing.T) {
	if _, err := NewNodeSpec(WithMaxRetries(-1)); err == nil {
		t.Fatal("负 retries 应 error")
	}
	ns, err := NewNodeSpec()
	if err != nil || ns.MaxRetries != 0 || ns.Timeout != -1.0 || ns.Abilities == nil {
		t.Fatalf("默认值: err=%v retries=%d timeout=%v abilities=%v",
			err, ns.MaxRetries, ns.Timeout, ns.Abilities)
	}
}

// TestNodeSpec_序列化键名 json tag 对齐 pydantic snake_case
func TestNodeSpec_序列化键名(t *testing.T) {
	name := "comp"
	ns, _ := NewNodeSpec(WithMaxRetries(3))
	ns.Name = &name
	ns.IOConfigs = &CompIOConfig{InputsSchema: map[string]any{"x": "1"}}
	bs, err := json.Marshal(ns)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"io_configs"`, `"max_retries"`, `"timeout"`, `"name"`, `"abilities"`, `"stream_io_configs"`, `"exception_config"`} {
		if !contains(string(bs), key) {
			t.Fatalf("缺键 %s: %s", key, bs)
		}
	}
}

// TestWorkflowConfig_序列化键名
func TestWorkflowConfig_序列化键名(t *testing.T) {
	wfc, _ := NewWorkflowConfig(&WorkflowCard{Version: "v1", Name: "wf"}, WithMaxNestingDepth(3))
	bs, _ := json.Marshal(wfc)
	for _, key := range []string{`"card"`, `"spec"`, `"workflow_max_nesting_depth"`} {
		if !contains(string(bs), key) {
			t.Fatalf("缺键 %s: %s", key, bs)
		}
	}
}

// TestCompIOConfig_Transformer可承载 any 槽（D-15-1）
func TestCompIOConfig_Transformer可承载(t *testing.T) {
	io := &CompIOConfig{InputsSchema: utils.MessageTransformer(
		func(m map[string]any) map[string]any { return m })}
	if _, ok := io.InputsSchema.(utils.MessageTransformer); !ok {
		t.Fatal("Transformer 应可放入 any 槽")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
```

> 注：若 `session/utils.MessageTransformer` 的真实签名与上不符，以仓内实际定义为准调整（测试意图是验证 any 槽可放函数型 Transformer）。`contains` 亦可改用 `strings.Contains`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/workflow/ -run 'TestNewWorkflowConfig|TestNewNodeSpec|TestNodeSpec_序列化|TestWorkflowConfig_序列化|TestCompIOConfig' -v
```
预期：编译失败（`NewWorkflowConfig`/`WithMaxNestingDepth`/`WithMaxRetries` 未定义）。

- [ ] **Step 3: 实现本体（重写 workflow_config.go）**

按声明顺序（规范 2：结构体 → 枚举 → 常量 → 全局变量 → 导出函数），完整替换文件内容：

```go
package workflow

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CompIOConfig 组件输入/输出配置。
// Python: workflow_config.py CompIOConfig；inputs/outputs_schema 可为 dict 或变换函数（Transformer），
// Go 用 any 承载（偏差 D-15-1：Dict | Transformer Union 无 Go 对应）。
type CompIOConfig struct {
	// InputsSchema 输入 schema（map[string]any 或 Transformer）
	InputsSchema any `json:"inputs_schema,omitempty"`
	// OutputsSchema 输出 schema（map[string]any 或 Transformer）
	OutputsSchema any `json:"outputs_schema,omitempty"`
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
	IOConfigs *CompIOConfig `json:"io_configs,omitempty"`
	// StreamIOConfigs 流式 I/O 配置
	StreamIOConfigs *CompIOConfig `json:"stream_io_configs,omitempty"`
	// Abilities 组件支持的能力列表（default_factory=list）
	Abilities []components.ComponentAbility `json:"abilities"`
	// MaxRetries 最大重试次数（ge=0，越界拒绝）
	MaxRetries int `json:"max_retries"`
	// Timeout 节点执行超时秒数；<=0 表示不超时
	Timeout float64 `json:"timeout"`
	// ExceptionConfig 异常恢复配置
	ExceptionConfig *ExceptionConfig `json:"exception_config,omitempty"`
	// Name 人类可读组件名；nil 在 trace 时回退 comp_id（Python Optional[str]，偏差 D-15-2）
	Name *string `json:"name,omitempty"`
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
// max_retries ge=0：负值拒绝构造，对齐 pydantic ValidationError。
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
// spec 未传补建空 spec（default_factory）；深度默认 5、合法域 [0,10]（越界拒绝）。
// 注意：0 是合法值也是 Go 零值，故「未传默认 5」由选项机制承载、禁止零值判断。
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

// errWorkflowConfig 构造校验错误。
func errWorkflowConfig(format string, args ...any) error {
	return fmt.Errorf("workflow_config: "+format, args...)
}
```

> import 需补 `"fmt"`；`WorkflowCard` 为 8.16 已有类型（`workflow/base.go`），不得重定义。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/workflow/ -v
```
预期：全部 PASS（含 8.16 base_test.go 既有用例）。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/workflow/workflow_config.go internal/agentcore/workflow/workflow_config_test.go
git commit -m "feat(8.15): WorkflowConfig 本体 + 构造校验拒绝语义 + snake_case json tag"
```

---

### Task 2: ExceptionConfig 平铺序列化（extra=allow）

**Files:**
- Modify: `internal/agentcore/workflow/workflow_config.go`
- Test: `internal/agentcore/workflow/workflow_config_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestExceptionConfig_平铺序列化 extra=allow 平铺进 JSON
func TestExceptionConfig_平铺序列化(t *testing.T) {
	ec := &ExceptionConfig{HandleType: "retry", Extra: map[string]any{"strategy": "fallback"}}
	bs, err := json.Marshal(ec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if m["handle_type"] != "retry" || m["strategy"] != "fallback" {
		t.Fatalf("Extra 应平铺: %s", bs)
	}
	// 往返
	var ec2 ExceptionConfig
	if err := json.Unmarshal(bs, &ec2); err != nil {
		t.Fatal(err)
	}
	if ec2.HandleType != "retry" || ec2.Extra["strategy"] != "fallback" {
		t.Fatalf("往返失败: handle=%q extra=%v", ec2.HandleType, ec2.Extra)
	}
	// 无 Extra 时 Unmarshal 后 Extra 为 nil、handle_type 默认语义由构造承载
	var ec3 ExceptionConfig
	if err := json.Unmarshal([]byte(`{"handle_type":"interrupt"}`), &ec3); err != nil {
		t.Fatal(err)
	}
	if ec3.HandleType != "interrupt" || ec3.Extra != nil {
		t.Fatalf("无附加字段: handle=%q extra=%v", ec3.HandleType, ec3.Extra)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/workflow/ -run TestExceptionConfig -v
```
预期：FAIL（`"strategy"` 键不存在，Extra 被 `json:"-"` 丢弃）。

- [ ] **Step 3: 实现定制 MarshalJSON/UnmarshalJSON**

在 workflow_config.go 导出函数区追加（结构体定义保持不动）：

```go
// MarshalJSON 平铺序列化：handle_type 与 Extra 键同层输出（对齐 pydantic extra=allow）。
func (e *ExceptionConfig) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(e.Extra)+1)
	m["handle_type"] = e.HandleType
	for k, v := range e.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON 平铺反序列化：handle_type 归位、其余键进 Extra（对齐 extra=allow）。
func (e *ExceptionConfig) UnmarshalJSON(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	e.Extra = make(map[string]any, len(m))
	for k, v := range m {
		if k == "handle_type" {
			if s, ok := v.(string); ok {
				e.HandleType = s
			}
			continue
		}
		e.Extra[k] = v
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/workflow/ -v
```
预期：全 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/workflow/workflow_config.go internal/agentcore/workflow/workflow_config_test.go
git commit -m "feat(8.15): ExceptionConfig extra=allow 平铺序列化"
```

---

### Task 3: session/config GetWorkflowConfig / AddWorkflowConfig 收窄

**Files:**
- Modify: `internal/agentcore/session/config/config.go`（接口 ~34-40 行、实现 ~123-141 行、字段 ~58-60 行）
- Test: `internal/agentcore/session/config/config_test.go`（既有用例适配 + 新增取值用例）

- [ ] **Step 1: 写失败测试**

在 config_test.go 追加：

```go
// TestGetWorkflowConfig_收窄 WorkflowConfig 实现后返回具体类型
func TestGetWorkflowConfig_收窄(t *testing.T) {
	cfg := NewSessionConfig(context.Background())
	wfc, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{Version: "v1"})
	cfg.AddWorkflowConfig("wf1", wfc)
	got := cfg.GetWorkflowConfig("wf1")
	if got == nil || got.Card.Version != "v1" {
		t.Fatalf("应返回 WorkflowConfig: %v", got)
	}
	if cfg.GetWorkflowConfig("") != nil || cfg.GetWorkflowConfig("nope") != nil {
		t.Fatal("空 ID / 未知 ID 应 nil")
	}
}
```

（import 补 `workflow` 包；既有 `assert.Nil(cfg.GetWorkflowConfig("wf1"))` 等用例类型不变仍编译通过。）

- [ ] **Step 2: 运行确认编译失败**

```bash
go test ./internal/agentcore/session/config/ -run TestGetWorkflowConfig_收窄 -v
```
预期：编译失败（`got.Card` 对 any 非法）。

- [ ] **Step 3: 实现收窄**

config.go 三处：

```go
// 接口（~34 行）：
	// GetWorkflowConfig 按 workflowID 获取工作流配置。
	// Python: Config.get_workflow_config(workflow_id)
	GetWorkflowConfig(workflowID string) *workflow.WorkflowConfig
	// AddWorkflowConfig 添加工作流配置。
	// Python: Config.add_workflow_config(workflow_id, workflow_config)
	AddWorkflowConfig(workflowID string, workflowConfig *workflow.WorkflowConfig)
```

```go
// 字段（~58 行）：
	// workflowConfigs 按 workflowID 索引的工作流配置
	workflowConfigs map[string]*workflow.WorkflowConfig
```

```go
// 实现（~123 行）：
func (c *defaultSessionConfig) GetWorkflowConfig(workflowID string) *workflow.WorkflowConfig {
	if workflowID == "" {
		return nil
	}
	return c.workflowConfigs[workflowID]
}

func (c *defaultSessionConfig) AddWorkflowConfig(workflowID string, workflowConfig *workflow.WorkflowConfig) {
	if workflowID == "" || workflowConfig == nil {
		return
	}
	c.workflowConfigs[workflowID] = workflowConfig
}
```

> 包依赖核验：workflow → session/stream 单向；session/config → workflow 反向成立，无环。import 补 `workflow` 包。

- [ ] **Step 4: 全仓修编译（消费方适配）**

```bash
go build ./... 2>&1 | head -20
```
预期：session/internal、session/tracer 报 `NodeConfig()/GetWorkflowConfig` 类型不符——本步骤**只修编译不动逻辑**：temp 断言 `wfc := cfg.GetWorkflowConfig(id).(*workflow.WorkflowConfig)` 不需要（返回已是具体类型），直接把消费点按 Task 4/5 的目标签名先行编译放行（本 Task 与 Task 4/5 同批改动期间 `go build` 允许红，以 Task 5 完成为准）。若希望每 Task 独立绿，可将本 Task 与 Task 4、5 合并执行。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/config/
git commit -m "feat(8.15): session config WorkflowConfig 类型收窄"
```

---

### Task 4: NodeSession.NodeConfig 真实实现 + 接口面收窄

**Files:**
- Modify: `internal/agentcore/session/internal/workflow_session.go:398-406`
- Modify: `internal/agentcore/session/node.go:239-242`（NodeSessionFacade.GetNodeConfig）、`node.go:295-296`（接口声明）
- Modify: `internal/agentcore/session/wrapper.go:133-136`（RouterSessionFacade 桩）
- Test: `internal/agentcore/session/internal/workflow_session_test.go`（新增）、`session/node_test.go`、`session/wrapper_test.go`（既有桩用例适配）

- [ ] **Step 1: 写失败测试**

```go
// TestNodeSession_NodeConfig 从 workflow config 取节点规格（Python node_config() 一比一）
func TestNodeSession_NodeConfig(t *testing.T) {
	parent := newFakeInnerSession(t) // 沿用本文件既有 fake 构造
	wfc, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{Version: "v1"})
	ns, _ := workflow.NewNodeSpec()
	ns.Abilities = []components.ComponentAbility{components.ComponentAbilityInvoke}
	wfc.Spec.CompConfigs["node1"] = ns
	parent.cfg.AddWorkflowConfig("wf1", wfc)

	sess := NewNodeSession(parent, "node1", "exec", false)
	got := sess.NodeConfig()
	if got == nil || len(got.Abilities) != 1 {
		t.Fatalf("应取到 node1 的规格: %v", got)
	}
	if sess2 := NewNodeSession(parent, "nodeX", "exec", false); sess2.NodeConfig() != nil {
		t.Fatal("未知节点应 nil（comp_configs.get）")
	}
}
```

> 注意：`NewNodeSession` 的 workflowID 取自 parent 的 `WorkflowID()`（workflow_session.go:512 getWorkflowID）——fake 的 workflowID 须返回 "wf1" 或 "wf1" 语义等价物；以本文件既有 fake 实际字段为准调整。cfg 为 `config.SessionConfig` 时若 `NewSessionConfig` 返回接口，直接调 `AddWorkflowConfig`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/session/... -run TestNodeSession_NodeConfig -v
```
预期：FAIL（当前恒返回 nil）。

- [ ] **Step 3: 实现真实取值**

替换 workflow_session.go:398-406：

```go
// NodeConfig 获取节点级配置。
// Python: NodeSession.node_config() → config.get_workflow_config(workflow_id).spec.comp_configs.get(node_id)
func (n *NodeSession) NodeConfig() *workflow.NodeSpec {
	cfg := n.delegate.Config()
	if cfg == nil {
		return nil
	}
	wfc := cfg.GetWorkflowConfig(n.workflowID)
	if wfc == nil || wfc.Spec == nil {
		return nil
	}
	return wfc.Spec.CompConfigs[n.nodeID]
}
```

node.go 接口与 facade 同步（import 补 workflow）：

```go
// node.go:295 接口：
	// NodeConfig 节点规格（Python: node_config()）
	NodeConfig() *workflow.NodeSpec

// node.go:239 facade：
// GetNodeConfig 获取节点级配置。
func (f *NodeSessionFacade) GetNodeConfig() *workflow.NodeSpec {
	return f.inner.NodeConfig()
}

// wrapper.go:133 桩（RouterSessionFacade 禁止操作，返回 nil）：
// GetNodeConfig 获取节点级配置 — 禁止操作
func (r *RouterSessionFacade) GetNodeConfig() *workflow.NodeSpec {
	return nil
}
```

- [ ] **Step 4: 跑 session 包测试**

```bash
go test ./internal/agentcore/session/... -v 2>&1 | tail -5
```
预期：全 PASS（graph 包此阶段可能编译红，属 Task 5 范围）。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/
git commit -m "feat(8.15): NodeSession.NodeConfig 真实取值 + 接口面收窄"
```

---

### Task 5: tracer getWorkflowMetadata 提取 card.version / card.name

**Files:**
- Modify: `internal/agentcore/session/tracer/workflow.go:245-271`
- Test: `internal/agentcore/session/tracer/workflow_test.go`（新增或追加）

- [ ] **Step 1: 写失败测试**

```go
// TestGetWorkflowMetadata_card提取 WorkflowConfig 实现后提取 card.version/name
func TestGetWorkflowMetadata_card提取(t *testing.T) {
	// 构造带 workflow config 的 session（沿本文件既有 fake WorkflowNodeSession）
	sess := newFakeWfNodeSession(t, "wf1") // 要求 Config() 返回含 wf1 的 SessionConfig
	wfc, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{Version: "v2", Name: "主流程"})
	sess.cfg.AddWorkflowConfig("wf1", wfc)

	md := getWorkflowMetadata(sess)
	if md["WorkflowVersion"] != "v2" || md["WorkflowName"] != "主流程" {
		t.Fatalf("应提取 card 字段: %v", md)
	}
}

// TestGetWorkflowMetadata_无config兜底 nil 配置时空串兜底
func TestGetWorkflowMetadata_无config兜底(t *testing.T) {
	sess := newFakeWfNodeSession(t, "wf1")
	md := getWorkflowMetadata(sess)
	if md["WorkflowVersion"] != "" || md["WorkflowName"] != "" {
		t.Fatalf("无配置应空串兜底: %v", md)
	}
}
```

> fake 形状以 workflow_test.go 既有 `WorkflowNodeSession` fake 为准（需 `WorkflowID()`/`Config()` 两方法）。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/session/tracer/ -run TestGetWorkflowMetadata -v
```
预期：FAIL（WorkflowVersion 恒空串）。

- [ ] **Step 3: 实现提取**

替换 workflow.go:245-271（注释悬挂段删除）：

```go
// getWorkflowMetadata 获取工作流元数据，对应 Python TracerWorkflowUtils._get_workflow_metadata。
// 返回 workflow_id/workflow_version/workflow_name（Python 从
// session.config().get_workflow_config(executable_id).card 提取 version 和 name）。
func getWorkflowMetadata(session WorkflowNodeSession) map[string]any {
	workflowID := session.WorkflowID()
	metadata := map[string]any{
		"WorkflowID":      workflowID,
		"WorkflowVersion": "",
		"WorkflowName":    "",
	}
	cfg := session.Config()
	if cfg == nil {
		return metadata
	}
	wfc := cfg.GetWorkflowConfig(workflowID)
	if wfc == nil || wfc.Card == nil {
		return metadata
	}
	metadata["WorkflowVersion"] = wfc.Card.Version
	metadata["WorkflowName"] = wfc.Card.Name
	return metadata
}
```

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/agentcore/session/... -v 2>&1 | tail -3
```
预期：全 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/tracer/
git commit -m "feat(8.15): tracer getWorkflowMetadata 提取 card.version/name"
```

---

### Task 6: vertex.go D-3 内联回收（删除 applyNodeConfig / ApplyNodeConfig）

**Files:**
- Modify: `internal/agentcore/graph/vertex.go:157-165`（删 applyNodeConfig）、`vertex.go:236-237`（Init 内联）、`vertex.go:768-772`（删 ApplyNodeConfig）、`vertex.go:75` 注释更新
- Modify: `internal/agentcore/graph/vertex_test.go`（6 处 `v.applyNodeConfig(...)` 直测 + `TestApplyNodeConfig与SetEndNode` 适配）
- Modify: `tests/integration/agentcore/workflow_vertex/vertex_stream_test.go:123-128`（ApplyNodeConfig 改 Init 路径）

- [ ] **Step 1: 改 Init 内联 + 删两方法**

Init 步骤 ④⑤⑥⑦（vertex.go:236-237）替换为内联四步（对齐 vertex.py:92-97 内联形态）：

```go
	// ④⑤⑥⑦ node_config 读取与能力派生（vertex.py:92-97，✅ 8.15 D-3 内联回收）
	v.nodeConfig = v.nodeSession.NodeConfig()
	// ⑤ 能力面（vertex.py:93-95）：空则默认 [INVOKE]
	if v.nodeConfig != nil && len(v.nodeConfig.Abilities) > 0 {
		v.componentAbility = v.nodeConfig.Abilities
	} else {
		v.componentAbility = []components.ComponentAbility{components.ComponentAbilityInvoke}
	}
	// ⑥⑦ hasStreamCall（vertex.py:96-97）
	v.hasStreamCall = len(v.streamAbilities()) > 0
```

整段删除 `applyNodeConfig`（157-165）与 `ApplyNodeConfig`（768-772，含注释）；vertex.go:75 注释 `NodeSession.NodeConfig() any 断言` 改 `NodeSession.NodeConfig() 返回 *NodeSpec`。

- [ ] **Step 2: 适配测试**

vertex_test.go 各处直测（~220/232/466/492/1061/1127/1275 行）改为「parent config 注册 WorkflowConfig + 走 Init」驱动：

```go
// 例（原 v.applyNodeConfig(&workflow.NodeSpec{Abilities: ...}) 直测改法）：
v := NewVertex("n", &fakeVertexExecutable{})
parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()),
	st: state.NewInMemoryWorkflowState()}
wfc, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{})
wfc.Spec.CompConfigs["n"], _ = workflow.NewNodeSpec()
wfc.Spec.CompConfigs["n"].Abilities = []components.ComponentAbility{
	components.ComponentAbilityInvoke, components.ComponentAbilityTransform}
parent.cfg.AddWorkflowConfig(parentWFID, wfc) // parentWFID = fake 的 WorkflowID() 返回值
_ = v.Init(parent, nil)
```

`TestApplyNodeConfig与SetEndNode`（1203 行）改名 `TestInit_节点规格派生与SetEndNode`，同样走 Init。集成测试 vertex_stream_test.go:123-128：删 `v.ApplyNodeConfig(...)` 调用，改为在 `cfg` 上 `AddWorkflowConfig(wfID, wfc)` 后由 `v.Init(parent, nil)` 一步驱动（fakeInner 需实现 `WorkflowID() string` 返回该 wfID，检查既有 fake 是否已实现——8.11 集成已建 fakeInner，若无则补一个方法）。

> 注意：`Init` 的能力派生读的是 `v.nodeSession.NodeConfig()`，而 NodeSession.workflowID 来自 parent 的 `WorkflowID()`。fake parent 必须 `WorkflowID()` 返回注册用的 wfID，否则取不到。若同一测试内多次 Init 不同能力组合，每次重建 config 再 Init（Vertex 无 Init 幂等约束，8.11 已验证可重复 Init）。

- [ ] **Step 3: 锚点清零验证（规则 8.3）**

```bash
grep -rn "applyNodeConfig\|ApplyNodeConfig" internal tests cmd --include="*.go"
```
预期：0 命中（spec/计划文档除外）。

- [ ] **Step 4: 跑 graph + 集成测试**

```bash
go test ./internal/agentcore/graph/ ./tests/integration/agentcore/workflow_vertex/ -v 2>&1 | tail -5
```
预期：全 PASS，零行为变化（派生逻辑逐字搬入 Init）。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/graph/ tests/integration/agentcore/workflow_vertex/
git commit -m "refactor(8.15): 回收 8.11 D-3，applyNodeConfig 内联回 Init 并删除装配面"
```

---

### Task 7: 全量回归 + 销账 + 推送

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（8.15 行、8.16 行 ⤵️ 钩子、8.11 D-3 注记、8.12 D6 注记、新实施序注记）

- [ ] **Step 1: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "FAIL|ok  " | grep FAIL; echo "exit=$?"
go build ./... && go vet ./...
```
预期：零 FAIL，build/vet 干净。

- [ ] **Step 2: 销账 IMPLEMENTATION_PLAN.md**

1. 8.15 行改 ✅，产出写 Go 事实（WorkflowConfig 本体/校验/json tag/三件回填/D-3 回收/D-15-1..3 偏差、覆盖率、spec 路径）；
2. 8.16 行四处 ⤵️8.15 钩子改「✅ 已回填（8.15）」；
3. 8.11 行 D-3 注记改「✅ 8.15 内联回收」；
4. 8.12 行 D6 注记改「✅ 8.15 完整同步」；
5. 末尾追加实施序注记：**十四次记录**——8.15 按方案 A 落地（完整同步+三件回填收口，spec docs/superpowers/specs/2026-11-18-workflow-config-8.15-design.md §2），下一节 8.14（Workflow 类，依赖 8.15 已就绪）/8.13 可并行。

- [ ] **Step 3: 覆盖率核对**

```bash
go test -cover ./internal/agentcore/workflow/ ./internal/agentcore/session/... 2>&1 | grep coverage
```
预期：workflow 包 ≥85%（新代码全覆盖），session 各子包不低于基线。

- [ ] **Step 4: Commit + Push**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-11-18-workflow-config-8.15-design.md
git commit -m "docs(8.15): 实施完成销账（三件回填+D-3 回收+偏差 D-15-1..3 归档）"
git push origin main
```

（推送即交付，规则 11：不追踪远端 CI。）

---

## 任务依赖

Task 1 → Task 2（同文件）；Task 1 → Task 3（收窄依赖 WorkflowConfig 类型）；Task 3 → Task 4（NodeConfig 取值依赖 GetWorkflowConfig 具体类型）；Task 4 → Task 5（tracer 收窄依赖 config 收窄）；Task 4 → Task 6（vertex 调 NodeConfig 新签名）；Task 6、7 收尾。Task 3-5 期间允许中间态编译红，Task 5 完成后必须全绿。

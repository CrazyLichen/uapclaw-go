# 9.70c Updater Protocol 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Updater Protocol 接口 + SingleDimUpdater + MultiDimUpdater + signal 最小前置类型 + from_evaluated_case 转换 + Trainer 回填

**Architecture:** Updater 是 Trainer 和 Optimizer 之间的核心协议层，Trainer 只依赖 Updater 接口，不直接依赖 BaseOptimizer。SingleDimUpdater 委托 BaseOptimizer 的 backward→step 链路；MultiDimUpdater 按 domain 分发 signals 到不同 optimizer 并合并 updates。EvolutionSignal 和 from_evaluated_case 作为 9.73 的最小前置，放在 signal 包内。

**Tech Stack:** Go 1.22+，标准库 testing，无外部测试框架

**Python 参考路径：**
- `openjiuwen/agent_evolving/updater/protocol.py` — Updater Protocol
- `openjiuwen/agent_evolving/updater/single_dim.py` — SingleDimUpdater
- `openjiuwen/agent_evolving/updater/multi_dim.py` — MultiDimUpdater
- `openjiuwen/agent_evolving/signal/base.py` — EvolutionSignal
- `openjiuwen/agent_evolving/signal/from_eval.py` — from_evaluated_case

**回填点标注：**
- ⤵️ `SingleDimUpdater.opt` 字段类型为 `any`，9.72e 时替换为 `BaseOptimizer`
- ⤵️ `MultiDimUpdater.domainOptimizers` 值类型为 `any`，9.72e 时替换为 `BaseOptimizer`
- ⤵️ `Updater.Update/Process` 的 `trajectories` 参数类型为 `[]any`，9.77 时替换为 `[]trajectory.Trajectory`

---

## File Structure

```
internal/evolving/
├── signal/                           # 新建包
│   ├── doc.go                        # 包文档
│   ├── signal.go                     # EvolutionSignal 最小 struct
│   ├── signal_test.go                # EvolutionSignal 测试
│   ├── from_eval.go                  # from_evaluated_case / from_evaluated_cases
│   └── from_eval_test.go             # from_eval 测试
├── updater/                          # 新建包
│   ├── doc.go                        # 包文档
│   ├── protocol.go                   # Updater 接口
│   └── protocol_test.go              # Updater 接口兼容性测试
│   ├── single_dim/                   # 新建子包
│   │   ├── doc.go                    # 包文档
│   │   ├── single_dim.go             # SingleDimUpdater
│   │   └── single_dim_test.go        # 测试
│   └── multi_dim/                    # 新建子包
│       ├── doc.go                    # 包文档
│       ├── multi_dim.go              # MultiDimUpdater
│       └── multi_dim_test.go         # 测试
├── trainer/
│   └── trainer.go                    # 修改：updater 字段类型回填
```

---

### Task 1: EvolutionSignal 最小 struct

**Files:**
- Create: `internal/evolving/signal/doc.go`
- Create: `internal/evolving/signal/signal.go`
- Create: `internal/evolving/signal/signal_test.go`

- [ ] **Step 1: 创建 signal/doc.go**

```go
// Package signal 提供自演化信号类型与转换工具。
//
// 信号（EvolutionSignal）标识 Agent 执行过程中的问题类型和诊断信息，
// 驱动优化器决定优化方向。本包同时提供离线评估结果到信号的转换函数。
//
// 文件目录：
//
//	signal/
//	├── doc.go           # 包文档
//	├── signal.go        # EvolutionSignal 最小 struct
//	└── from_eval.go     # EvaluatedCase → EvolutionSignal 转换
//
// 对应 Python 代码：openjiuwen/agent_evolving/signal/
package signal
```

- [ ] **Step 2: 创建 signal/signal.go**

```go
package signal

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionSignal 演化信号，标识 Agent 执行过程中的问题和诊断信息。
//
// 信号由评估结果（离线）或对话监控（在线）产生，
// 驱动优化器决定优化方向和内容。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionSignal
type EvolutionSignal struct {
	// SignalType 信号类型（如 "low_score"、"execution_failure"、"user_correction"）
	SignalType string
	// Section 建议修改的 SKILL.md 区域（如 "Troubleshooting"、"Examples"）
	Section string
	// Excerpt 问题摘要或关键片段
	Excerpt string
	// SkillName 关联的技能名称（可选）
	SkillName *string
	// Context 诊断上下文（如 question/label/answer/reason/score/source/tool_name）
	Context map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 signal/signal_test.go**

```go
package signal

import (
	"testing"
)

func TestEvolutionSignal_字段赋值(t *testing.T) {
	skill := "my_skill"
	sig := EvolutionSignal{
		SignalType: "low_score",
		Section:    "Troubleshooting",
		Excerpt:    "score=0.00",
		SkillName:  &skill,
		Context: map[string]any{
			"score":   0.0,
			"source":  "offline_evaluation",
			"reason":  "wrong answer",
		},
	}

	if sig.SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "low_score")
	}
	if sig.Section != "Troubleshooting" {
		t.Errorf("Section = %q, want %q", sig.Section, "Troubleshooting")
	}
	if sig.Excerpt != "score=0.00" {
		t.Errorf("Excerpt = %q, want %q", sig.Excerpt, "score=0.00")
	}
	if sig.SkillName == nil || *sig.SkillName != "my_skill" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_skill")
	}
	if sig.Context["score"] != 0.0 {
		t.Errorf("Context[score] = %v, want 0.0", sig.Context["score"])
	}
}

func TestEvolutionSignal_可选字段为零值(t *testing.T) {
	sig := EvolutionSignal{
		SignalType: "evaluated",
		Section:    "Examples",
		Excerpt:    "score=0.50",
	}

	if sig.SkillName != nil {
		t.Errorf("SkillName = %v, want nil", sig.SkillName)
	}
	if sig.Context != nil {
		t.Errorf("Context = %v, want nil", sig.Context)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v -run TestEvolutionSignal`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/signal/doc.go internal/evolving/signal/signal.go internal/evolving/signal/signal_test.go
git commit -m "feat(evolving): 添加 EvolutionSignal 最小 struct (9.70c 前置)"
```

---

### Task 2: from_evaluated_case 转换函数

**Files:**
- Create: `internal/evolving/signal/from_eval.go`
- Create: `internal/evolving/signal/from_eval_test.go`

- [ ] **Step 1: 创建 from_eval_test.go（先写失败测试）**

```go
package signal

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

func TestFromEvaluatedCase_低分产生信号(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "what is Go?"},
		map[string]any{"answer": "a programming language"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "a car"})
	ec.SetScore(0.0)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil, want non-nil signal")
	}
	if sig.SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "low_score")
	}
	if sig.Section != "Troubleshooting" {
		t.Errorf("Section = %q, want %q", sig.Section, "Troubleshooting")
	}
	if sig.Context == nil {
		t.Fatal("Context is nil")
	}
	if sig.Context["score"] != 0.0 {
		t.Errorf("Context[score] = %v, want 0.0", sig.Context["score"])
	}
	if sig.Context["source"] != "offline_evaluation" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "offline_evaluation")
	}
}

func TestFromEvaluatedCase_非零分产生Evaluated信号(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "what is Go?"},
		map[string]any{"answer": "a programming language"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "a language"})
	ec.SetScore(0.5)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil, want non-nil signal")
	}
	if sig.SignalType != "evaluated" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "evaluated")
	}
}

func TestFromEvaluatedCase_达到阈值返回nil(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	ec.SetScore(1.0)

	threshold := 1.0
	sig := FromEvaluatedCase(ec, "", &threshold)

	if sig != nil {
		t.Errorf("FromEvaluatedCase = %v, want nil when score >= threshold", sig)
	}
}

func TestFromEvaluatedCase_无阈值不过滤(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	ec.SetScore(1.0)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil with no threshold, want non-nil")
	}
	if sig.SignalType != "evaluated" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "evaluated")
	}
}

func TestFromEvaluatedCase_operatorID传入SkillName(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	sig := FromEvaluatedCase(ec, "my_operator", nil)

	if sig.SkillName == nil || *sig.SkillName != "my_operator" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_operator")
	}
}

func TestFromEvaluatedCases_批量转换(t *testing.T) {
	case1 := dataset.NewCase(
		map[string]any{"query": "q1"},
		map[string]any{"answer": "a1"},
	)
	case2 := dataset.NewCase(
		map[string]any{"query": "q2"},
		map[string]any{"answer": "a2"},
	)
	ec1 := dataset.NewEvaluatedCase(*case1, map[string]any{"output": "bad"})
	ec1.SetScore(0.0)
	ec2 := dataset.NewEvaluatedCase(*case2, map[string]any{"output": "good"})
	ec2.SetScore(1.0)

	threshold := 1.0
	signals := FromEvaluatedCases([]*dataset.EvaluatedCase{ec1, ec2}, "", &threshold)

	if len(signals) != 1 {
		t.Fatalf("FromEvaluatedCases returned %d signals, want 1", len(signals))
	}
	if signals[0].SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "low_score")
	}
}

func TestFromEvaluatedCases_无阈值全部保留(t *testing.T) {
	case1 := dataset.NewCase(
		map[string]any{"query": "q1"},
		map[string]any{"answer": "a1"},
	)
	case2 := dataset.NewCase(
		map[string]any{"query": "q2"},
		map[string]any{"answer": "a2"},
	)
	ec1 := dataset.NewEvaluatedCase(*case1, map[string]any{"output": "bad"})
	ec1.SetScore(0.0)
	ec2 := dataset.NewEvaluatedCase(*case2, map[string]any{"output": "good"})
	ec2.SetScore(1.0)

	signals := FromEvaluatedCases([]*dataset.EvaluatedCase{ec1, ec2}, "", nil)

	if len(signals) != 2 {
		t.Fatalf("FromEvaluatedCases returned %d signals, want 2", len(signals))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v -run TestFromEvaluated`
Expected: FAIL — `FromEvaluatedCase` undefined

- [ ] **Step 3: 创建 from_eval.go 实现**

```go
package signal

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FromEvaluatedCase 将离线评估结果转换为演化信号。
//
// 当 score_threshold 非空且 case.Score >= threshold 时返回 nil（不产生信号）；
// 否则根据分数判断 signal_type：score==0 → "low_score"，其他 → "evaluated"。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_eval.py from_evaluated_case
func FromEvaluatedCase(case_ *dataset.EvaluatedCase, operatorID string, scoreThreshold *float64) *EvolutionSignal {
	if scoreThreshold != nil && case_.Score >= *scoreThreshold {
		return nil
	}

	signalType := "evaluated"
	if case_.Score == 0 {
		signalType = "low_score"
	}

	var skillName *string
	if operatorID != "" {
		skillName = &operatorID
	}

	context := map[string]any{
		"question": fmt.Sprintf("%v", case_.GetInputs()),
		"label":    fmt.Sprintf("%v", case_.GetLabel()),
		"answer":   fmt.Sprintf("%v", case_.Answer),
		"reason":   case_.Reason,
		"score":    case_.Score,
		"source":   "offline_evaluation",
	}

	return &EvolutionSignal{
		SignalType: signalType,
		Section:    "Troubleshooting",
		Excerpt:    fmt.Sprintf("score=%.2f", case_.Score),
		SkillName:  skillName,
		Context:    context,
	}
}

// FromEvaluatedCases 批量将 EvaluatedCase 列表转换为 EvolutionSignal 列表。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_eval.py from_evaluated_cases
func FromEvaluatedCases(cases []*dataset.EvaluatedCase, operatorID string, scoreThreshold *float64) []*EvolutionSignal {
	signals := make([]*EvolutionSignal, 0, len(cases))
	for _, case_ := range cases {
		sig := FromEvaluatedCase(case_, operatorID, scoreThreshold)
		if sig != nil {
			signals = append(signals, sig)
		}
	}
	return signals
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v -run TestFromEvaluated`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/signal/from_eval.go internal/evolving/signal/from_eval_test.go
git commit -m "feat(evolving): 添加 from_evaluated_case 信号转换函数 (9.70c 前置)"
```

---

### Task 3: Updater 接口定义

**Files:**
- Create: `internal/evolving/updater/doc.go`
- Create: `internal/evolving/updater/protocol.go`
- Create: `internal/evolving/updater/protocol_test.go`

- [ ] **Step 1: 创建 updater/doc.go**

```go
// Package updater 提供自演化更新器协议与实现。
//
// Updater 是 Trainer 和 Optimizer 之间的核心协议层。
// Trainer 只依赖 Updater 接口，不直接依赖 BaseOptimizer。
// SingleDimUpdater 委托 BaseOptimizer 的 backward→step 链路；
// MultiDimUpdater 按 domain 分发 signals 到不同 optimizer 并合并 updates。
//
// 文件目录：
//
//	updater/
//	├── doc.go           # 包文档
//	├── protocol.go      # Updater 接口定义
//	├── single_dim/      # SingleDimUpdater（单维更新器）
//	│   ├── doc.go
//	│   └── single_dim.go
//	└── multi_dim/       # MultiDimUpdater（多维更新器）
//	    ├── doc.go
//	    └── multi_dim.go
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/
package updater
```

- [ ] **Step 2: 创建 updater/protocol.go**

```go
package updater

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Updater 自演化更新器接口，统一单维优化器和多维归因分配为一个接口。
//
// Trainer 不关心实现细节，只通过此接口获取更新映射：
//   (trajectories, evaluated_cases) -> update mapping 或 candidate set
//
// 对应 Python: openjiuwen/agent_evolving/updater/protocol.py Updater(Protocol)
type Updater interface {
	// Bind 绑定 Operator 注册表并过滤可优化的 Operator。
	// 返回匹配数量；0 触发 Trainer 软退出。
	Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int

	// RequiresForwardData 判断此 Updater 是否需要框架执行前向推理。
	// 返回 False 的黑盒优化器（如 tool_optimizer）在内部生成/执行/评估，
	// 不依赖框架的前向推理数据。
	RequiresForwardData() bool

	// Update 离线兼容入口，将 evaluated_cases 转换为 signals 后调用 Process。
	//
	// trajectories 参数类型为 []any，9.77 Trajectory 实现后替换为 []trajectory.Trajectory。⤵️
	Update(ctx context.Context, trajectories []any, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error)

	// Process 信号优先入口，直接消费 EvolutionSignal 列表。
	//
	// trajectories 参数类型为 []any，9.77 Trajectory 实现后替换为 []trajectory.Trajectory。⤵️
	Process(ctx context.Context, trajectories []any, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error)

	// GetState 获取 Updater 可序列化状态（用于检查点保存）。
	GetState() map[string]any

	// LoadState 从检查点恢复 Updater 状态。
	LoadState(state map[string]any)
}
```

- [ ] **Step 3: 创建 updater/protocol_test.go**

```go
package updater

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// mockUpdater 用于验证 Updater 接口兼容性
type mockUpdater struct {
	bindCalled      bool
	bindReturn      int
	requireForward  bool
	updateCalled    bool
	processCalled   bool
	getStateCalled  bool
	loadStateCalled bool
	lastSignals     []*signal.EvolutionSignal
	lastConfig      map[string]any
}

func (m *mockUpdater) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	m.bindCalled = true
	return m.bindReturn
}

func (m *mockUpdater) RequiresForwardData() bool {
	return m.requireForward
}

func (m *mockUpdater) Update(ctx context.Context, trajectories []any, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
	m.updateCalled = true
	return map[schema.UpdateKey]any{}, nil
}

func (m *mockUpdater) Process(ctx context.Context, trajectories []any, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error) {
	m.processCalled = true
	m.lastSignals = signals
	m.lastConfig = config
	return map[schema.UpdateKey]any{}, nil
}

func (m *mockUpdater) GetState() map[string]any {
	m.getStateCalled = true
	return map[string]any{}
}

func (m *mockUpdater) LoadState(state map[string]any) {
	m.loadStateCalled = true
}

// 验证 Updater 接口可被实现
func TestUpdater_接口兼容性(t *testing.T) {
	var _ Updater = &mockUpdater{}

	u := &mockUpdater{bindReturn: 3, requireForward: true}

	n := u.Bind(nil, nil, nil)
	if n != 3 {
		t.Errorf("Bind returned %d, want 3", n)
	}
	if !u.bindCalled {
		t.Error("Bind was not called")
	}

	if !u.RequiresForwardData() {
		t.Error("RequiresForwardData returned false, want true")
	}

	_, _ = u.Update(context.Background(), nil, nil, nil)
	if !u.updateCalled {
		t.Error("Update was not called")
	}

	sig := &signal.EvolutionSignal{SignalType: "low_score"}
	_, _ = u.Process(context.Background(), nil, []*signal.EvolutionSignal{sig}, map[string]any{"key": "val"})
	if !u.processCalled {
		t.Error("Process was not called")
	}
	if len(u.lastSignals) != 1 || u.lastSignals[0].SignalType != "low_score" {
		t.Errorf("Process signals = %v, want 1 signal with type low_score", u.lastSignals)
	}

	state := u.GetState()
	if !u.getStateCalled {
		t.Error("GetState was not called")
	}
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}

	u.LoadState(map[string]any{"key": "val"})
	if !u.loadStateCalled {
		t.Error("LoadState was not called")
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/updater/ -v -run TestUpdater`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/updater/doc.go internal/evolving/updater/protocol.go internal/evolving/updater/protocol_test.go
git commit -m "feat(evolving): 添加 Updater 接口定义 (9.70c)"
```

---

### Task 4: SingleDimUpdater 实现

**Files:**
- Create: `internal/evolving/updater/single_dim/doc.go`
- Create: `internal/evolving/updater/single_dim/single_dim.go`
- Create: `internal/evolving/updater/single_dim/single_dim_test.go`

- [ ] **Step 1: 创建 single_dim/doc.go**

```go
// Package single_dim 提供单维更新器实现。
//
// SingleDimUpdater 委托内部 BaseOptimizer 的 backward→step 链路，
// 将 signals 传递给 optimizer 生成梯度，再由 step 返回更新映射。
// 更新映射由 Trainer 统一应用到 Operator 注册表。
//
// 文件目录：
//
//	single_dim/
//	├── doc.go           # 包文档
//	└── single_dim.go    # SingleDimUpdater 实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/single_dim.py
package single_dim
```

- [ ] **Step 2: 创建 single_dim_test.go（先写失败测试）**

```go
package single_dim

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// mockOptimizer 模拟 BaseOptimizer 的关键方法
// 使用 map 记录调用，因为 opt 字段为 any 无法直接 mock 接口
type mockOptimizer struct {
	bindCalled         bool
	bindReturn         int
	bindTargets        []string
	addTrajectoryCalls int
	backwardCalled     bool
	backwardSignals    []*signal.EvolutionSignal
	stepReturn         map[schema.UpdateKey]any
	stepCalled         bool
	requireForward     bool
}

func (m *mockOptimizer) Bind(operators any, targets []string, config map[string]any) int {
	m.bindCalled = true
	m.bindTargets = targets
	return m.bindReturn
}

func (m *mockOptimizer) RequiresForwardData() bool {
	return m.requireForward
}

func (m *mockOptimizer) AddTrajectory(traj any) {
	m.addTrajectoryCalls++
}

func (m *mockOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
	m.backwardCalled = true
	m.backwardSignals = signals
	return nil
}

func (m *mockOptimizer) Step() map[schema.UpdateKey]any {
	m.stepCalled = true
	return m.stepReturn
}

func TestNewSingleDimUpdater(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 3}
	u := NewSingleDimUpdater(opt)

	if u == nil {
		t.Fatal("NewSingleDimUpdater returned nil")
	}
}

func TestSingleDimUpdater_Bind委托给优化器(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 3}
	u := NewSingleDimUpdater(opt)

	n := u.Bind(nil, []string{"system_prompt"}, nil)

	if n != 3 {
		t.Errorf("Bind returned %d, want 3", n)
	}
	if !opt.bindCalled {
		t.Error("optimizer.Bind was not called")
	}
}

func TestSingleDimUpdater_Bind空目标使用Config(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 2}
	u := NewSingleDimUpdater(opt)

	config := map[string]any{"targets": []string{"user_prompt"}}
	n := u.Bind(nil, nil, config)

	if n != 2 {
		t.Errorf("Bind returned %d, want 2", n)
	}
	if !opt.bindCalled {
		t.Error("optimizer.Bind was not called")
	}
}

func TestSingleDimUpdater_RequiresForwardData委托(t *testing.T) {
	opt := &mockOptimizer{requireForward: false}
	u := NewSingleDimUpdater(opt)

	if u.RequiresForwardData() != false {
		t.Error("RequiresForwardData should return false")
	}

	opt.requireForward = true
	if u.RequiresForwardData() != true {
		t.Error("RequiresForwardData should return true")
	}
}

func TestSingleDimUpdater_Process完整链路(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "system_prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	traj1 := "trajectory1"
	traj2 := "trajectory2"
	signals := []*signal.EvolutionSignal{
		{SignalType: "low_score", Section: "Troubleshooting", Excerpt: "score=0.00"},
	}

	result, err := u.Process(context.Background(), []any{traj1, traj2}, signals, map[string]any{})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if opt.addTrajectoryCalls != 2 {
		t.Errorf("add_trajectory called %d times, want 2", opt.addTrajectoryCalls)
	}
	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1", len(opt.backwardSignals))
	}
	if !opt.stepCalled {
		t.Error("step was not called")
	}
	if len(result) != 1 {
		t.Errorf("Process returned %d updates, want 1", len(result))
	}
}

func TestSingleDimUpdater_Process空轨迹(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	_, err := u.Process(context.Background(), nil, nil, map[string]any{})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if opt.addTrajectoryCalls != 0 {
		t.Errorf("add_trajectory called %d times, want 0", opt.addTrajectoryCalls)
	}
	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if !opt.stepCalled {
		t.Error("step was not called")
	}
}

func TestSingleDimUpdater_Update转换EvaluatedCases(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "system_prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	result, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{ec}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1", len(opt.backwardSignals))
	}
	if opt.backwardSignals[0].SignalType != "low_score" {
		t.Errorf("signal type = %q, want %q", opt.backwardSignals[0].SignalType, "low_score")
	}
	if len(result) != 1 {
		t.Errorf("Update returned %d updates, want 1", len(result))
	}
}

func TestSingleDimUpdater_Update尊重ScoreThreshold(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	highScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	highScore.SetScore(1.0)
	lowScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	lowScore.SetScore(0.0)

	threshold := 1.0
	_, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{highScore, lowScore}, map[string]any{"score_threshold": threshold})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1 (filtered by threshold)", len(opt.backwardSignals))
	}
	if opt.backwardSignals[0].SignalType != "low_score" {
		t.Errorf("signal type = %q, want %q", opt.backwardSignals[0].SignalType, "low_score")
	}
}

func TestSingleDimUpdater_GetState返回空(t *testing.T) {
	opt := &mockOptimizer{}
	u := NewSingleDimUpdater(opt)

	state := u.GetState()
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}
}

func TestSingleDimUpdater_LoadState无操作(t *testing.T) {
	opt := &mockOptimizer{}
	u := NewSingleDimUpdater(opt)

	// 不应 panic
	u.LoadState(map[string]any{"key": "value"})
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/updater/single_dim/ -v`
Expected: FAIL — `NewSingleDimUpdater` undefined

- [ ] **Step 4: 创建 single_dim/single_dim.go**

```go
package single_dim

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SingleDimUpdater 单维更新器，委托内部 BaseOptimizer 的 backward→step 链路。
//
// 将 signals 传递给 optimizer 生成梯度（backward），
// 再由 step 返回更新映射，由 Trainer 统一应用。
//
// opt 字段当前类型为 any，9.72e 实现后替换为 BaseOptimizer 接口。⤵️
//
// 对应 Python: openjiuwen/agent_evolving/updater/single_dim.py SingleDimUpdater
type SingleDimUpdater struct {
	// opt 内部优化器实例。
	// ⤵️ 9.72e 时替换 any 为 BaseOptimizer 接口
	opt any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSingleDimUpdater 创建 SingleDimUpdater 实例。
//
// optimizer 参数当前为 any，9.72e 后约束为 BaseOptimizer 接口。⤵️
//
// 对应 Python: SingleDimUpdater(optimizer=BaseOptimizer)
func NewSingleDimUpdater(optimizer any) *SingleDimUpdater {
	return &SingleDimUpdater{opt: optimizer}
}

// Bind 绑定 Operator 注册表，过滤可优化的 Operator。
// 返回匹配数量；0 触发 Trainer 软退出。
// 委托给内部优化器的 bind 方法。
//
// 对应 Python: SingleDimUpdater.bind(operators, targets, **config)
func (u *SingleDimUpdater) Bind(operators map[string]any, targets []string, config map[string]any) int {
	effectiveTargets := targets
	if effectiveTargets == nil && config != nil {
		if t, ok := config["targets"]; ok {
			if ts, ok := t.([]string); ok {
				effectiveTargets = ts
			}
		}
	}

	type binder interface {
		Bind(operators any, targets []string, config map[string]any) int
	}
	if b, ok := u.opt.(binder); ok {
		return b.Bind(operators, effectiveTargets, config)
	}
	return 0
}

// RequiresForwardData 判断是否需要前向推理数据，委托给内部优化器。
//
// 对应 Python: SingleDimUpdater.requires_forward_data()
func (u *SingleDimUpdater) RequiresForwardData() bool {
	type requirer interface {
		RequiresForwardData() bool
	}
	if r, ok := u.opt.(requirer); ok {
		return r.RequiresForwardData()
	}
	return true
}

// Process 信号优先入口，写入轨迹 → 执行 backward → 返回 step 结果。
//
// 对应 Python: SingleDimUpdater.process(trajectories, signals, config)
func (u *SingleDimUpdater) Process(ctx context.Context, trajectories []any, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error) {
	// 写入轨迹
	type trajectoryAdder interface {
		AddTrajectory(traj any)
	}
	if a, ok := u.opt.(trajectoryAdder); ok {
		for _, traj := range trajectories {
			a.AddTrajectory(traj)
		}
	}

	// 执行 backward
	type backwarder interface {
		Backward(ctx context.Context, signals []*signal.EvolutionSignal) error
	}
	if b, ok := u.opt.(backwarder); ok {
		if err := b.Backward(ctx, signals); err != nil {
			return nil, err
		}
	}

	// 执行 step
	type stepper interface {
		Step() map[schema.UpdateKey]any
	}
	if s, ok := u.opt.(stepper); ok {
		return s.Step(), nil
	}

	return map[schema.UpdateKey]any{}, nil
}

// Update 离线兼容入口，将 EvaluatedCase 转换为 EvolutionSignal 后调用 Process。
//
// 对应 Python: SingleDimUpdater.update(trajectories, evaluated_cases, config)
func (u *SingleDimUpdater) Update(ctx context.Context, trajectories []any, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
	// 从 config 中提取 score_threshold
	var scoreThreshold *float64
	if config != nil {
		if st, ok := config["score_threshold"]; ok {
			if f, ok := st.(float64); ok {
				scoreThreshold = &f
			}
		}
	}

	signals := signal.FromEvaluatedCases(evaluatedCases, "", scoreThreshold)
	return u.Process(ctx, trajectories, signals, config)
}

// GetState 获取 Updater 可序列化状态。
// 当前 BaseOptimizer 无稳定可恢复状态，返回空 map。
//
// 对应 Python: SingleDimUpdater.get_state()
func (u *SingleDimUpdater) GetState() map[string]any {
	return map[string]any{}
}

// LoadState 从检查点恢复状态，当前为无操作。
//
// 对应 Python: SingleDimUpdater.load_state(state)
func (u *SingleDimUpdater) LoadState(_ map[string]any) {
	// 当前 BaseOptimizer 无稳定可恢复状态，no-op
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/updater/single_dim/ -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/evolving/updater/single_dim/doc.go internal/evolving/updater/single_dim/single_dim.go internal/evolving/updater/single_dim/single_dim_test.go
git commit -m "feat(evolving): 添加 SingleDimUpdater 实现 (9.70c)"
```

---

### Task 5: MultiDimUpdater 实现

**Files:**
- Create: `internal/evolving/updater/multi_dim/doc.go`
- Create: `internal/evolving/updater/multi_dim/multi_dim.go`
- Create: `internal/evolving/updater/multi_dim/multi_dim_test.go`

- [ ] **Step 1: 创建 multi_dim/doc.go**

```go
// Package multi_dim 提供多维更新器实现。
//
// MultiDimUpdater 按 Operator domain（llm/tool/memory/skill_experience）
// 分配 signals 到对应域的优化器，合并各域的更新映射，由 Trainer 统一应用。
//
// 当前为纯结构体默认实现，bind/process/get_state/load_state 返回零值，
// 后续具体实现时重写。
//
// 文件目录：
//
//	multi_dim/
//	├── doc.go          # 包文档
//	└── multi_dim.go    # MultiDimUpdater 实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/multi_dim.py
package multi_dim
```

- [ ] **Step 2: 创建 multi_dim_test.go（先写失败测试）**

```go
package multi_dim

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// mockDomainOptimizer 模拟域优化器
type mockDomainOptimizer struct {
	requireForward bool
}

func (m *mockDomainOptimizer) RequiresForwardData() bool {
	return m.requireForward
}

func TestNewMultiDimUpdater(t *testing.T) {
	u := NewMultiDimUpdater()
	if u == nil {
		t.Fatal("NewMultiDimUpdater returned nil")
	}
}

func TestNewMultiDimUpdater_带域优化器(t *testing.T) {
	opt := &mockDomainOptimizer{requireForward: true}
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm": opt,
	}))
	if u == nil {
		t.Fatal("NewMultiDimUpdater returned nil")
	}
	if len(u.DomainOptimizers()) != 1 {
		t.Errorf("DomainOptimizers count = %d, want 1", len(u.DomainOptimizers()))
	}
}

func TestMultiDimUpdater_Bind默认返回零(t *testing.T) {
	u := NewMultiDimUpdater()
	n := u.Bind(nil, nil, nil)
	if n != 0 {
		t.Errorf("Bind returned %d, want 0", n)
	}
}

func TestMultiDimUpdater_RequiresForwardData_全部不需要(t *testing.T) {
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm":  &mockDomainOptimizer{requireForward: false},
		"tool": &mockDomainOptimizer{requireForward: false},
	}))

	if u.RequiresForwardData() {
		t.Error("RequiresForwardData should return false when all optimizers don't need forward")
	}
}

func TestMultiDimUpdater_RequiresForwardData_有需要前向的(t *testing.T) {
	u := NewMultiDimUpdater(WithDomainOptimizers(map[string]any{
		"llm":    &mockDomainOptimizer{requireForward: true},
		"tool":   &mockDomainOptimizer{requireForward: false},
		"memory": &mockDomainOptimizer{requireForward: false},
	}))

	if !u.RequiresForwardData() {
		t.Error("RequiresForwardData should return true when any optimizer needs forward")
	}
}

func TestMultiDimUpdater_RequiresForwardData_空优化器(t *testing.T) {
	u := NewMultiDimUpdater()

	if u.RequiresForwardData() {
		t.Error("RequiresForwardData should return false with no optimizers")
	}
}

func TestMultiDimUpdater_Process默认返回空(t *testing.T) {
	u := NewMultiDimUpdater()

	result, err := u.Process(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Process returned %d updates, want 0", len(result))
	}
}

func TestMultiDimUpdater_Update转换EvaluatedCases(t *testing.T) {
	u := NewMultiDimUpdater()

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	// MultiDimUpdater.Update 调用 from_evaluated_cases → Process
	// 当前 Process 返回空 map，但验证不报错
	_, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{ec}, nil)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
}

func TestMultiDimUpdater_GetState返回空(t *testing.T) {
	u := NewMultiDimUpdater()
	state := u.GetState()
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}
}

func TestMultiDimUpdater_LoadState无操作(t *testing.T) {
	u := NewMultiDimUpdater()
	// 不应 panic
	u.LoadState(map[string]any{"key": "value"})
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/updater/multi_dim/ -v`
Expected: FAIL — `NewMultiDimUpdater` undefined

- [ ] **Step 4: 创建 multi_dim/multi_dim.go**

```go
package multi_dim

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MultiDimUpdater 多维更新器，按 domain 分发 signals 到不同域优化器，
// 合并各域更新映射，由 Trainer 统一应用。
//
// 一致性约束：维度仅按 Operator domain 划分（llm/tool/memory/skill_experience），
// 用户只需配置 domain_optimizers 映射，每个域仅允许一个优化器。
//
// domainOptimizers 值类型当前为 any，9.72e 实现后替换为 BaseOptimizer。⤵️
//
// 当前 bind/process/get_state/load_state 为默认实现（返回零值），
// 后续具体子类实现时重写。
//
// 对应 Python: openjiuwen/agent_evolving/updater/multi_dim.py MultiDimUpdater
type MultiDimUpdater struct {
	// domainOptimizers domain → optimizer 映射。
	// ⤵️ 9.72e 时替换 any 为 BaseOptimizer 接口
	domainOptimizers map[string]any
}

// MultiDimUpdaterOption MultiDimUpdater 构造选项函数。
type MultiDimUpdaterOption func(*MultiDimUpdater)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultiDimUpdater 创建 MultiDimUpdater 实例。
//
// 对应 Python: MultiDimUpdater(domain_optimizers={...})
func NewMultiDimUpdater(opts ...MultiDimUpdaterOption) *MultiDimUpdater {
	u := &MultiDimUpdater{
		domainOptimizers: map[string]any{},
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// WithDomainOptimizers 设置域优化器映射。
func WithDomainOptimizers(optimizers map[string]any) MultiDimUpdaterOption {
	return func(u *MultiDimUpdater) {
		if optimizers != nil {
			u.domainOptimizers = optimizers
		}
	}
}

// DomainOptimizers 返回当前域优化器映射（只读副本）。
func (u *MultiDimUpdater) DomainOptimizers() map[string]any {
	result := make(map[string]any, len(u.domainOptimizers))
	for k, v := range u.domainOptimizers {
		result[k] = v
	}
	return result
}

// Bind 绑定 Operator 注册表并过滤可优化的 Operator。
// 当前默认实现返回 0，后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.bind(operators, targets, **config)
func (u *MultiDimUpdater) Bind(_ map[string]any, _ []string, _ map[string]any) int {
	return 0
}

// RequiresForwardData 检查是否有任何域优化器需要前向推理数据。
// 如果任意优化器的 requires_forward_data 返回 true，则返回 true。
//
// 对应 Python: MultiDimUpdater.requires_forward_data()
func (u *MultiDimUpdater) RequiresForwardData() bool {
	for _, opt := range u.domainOptimizers {
		type requirer interface {
			RequiresForwardData() bool
		}
		if r, ok := opt.(requirer); ok {
			if r.RequiresForwardData() {
				return true
			}
		}
	}
	return false
}

// Process 信号优先入口，按 domain 分发 signals 到对应优化器，合并更新映射。
// 当前默认实现返回空 map，后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.process(trajectories, signals, config)
func (u *MultiDimUpdater) Process(_ context.Context, _ []any, _ []*signal.EvolutionSignal, _ map[string]any) (map[schema.UpdateKey]any, error) {
	return map[schema.UpdateKey]any{}, nil
}

// Update 离线兼容入口，将 EvaluatedCase 转换为 EvolutionSignal 后调用 Process。
//
// 对应 Python: MultiDimUpdater.update(trajectories, evaluated_cases, config)
func (u *MultiDimUpdater) Update(ctx context.Context, trajectories []any, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
	var scoreThreshold *float64
	if config != nil {
		if st, ok := config["score_threshold"]; ok {
			if f, ok := st.(float64); ok {
				scoreThreshold = &f
			}
		}
	}

	signals := signal.FromEvaluatedCases(evaluatedCases, "", scoreThreshold)
	return u.Process(ctx, trajectories, signals, config)
}

// GetState 获取 Updater 可序列化状态。
// 当前默认实现返回空 map。
//
// 对应 Python: MultiDimUpdater.get_state()
func (u *MultiDimUpdater) GetState() map[string]any {
	return map[string]any{}
}

// LoadState 从检查点恢复状态，当前为无操作。
//
// 对应 Python: MultiDimUpdater.load_state(state)
func (u *MultiDimUpdater) LoadState(_ map[string]any) {
	// 默认 no-op，后续子类重写
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/updater/multi_dim/ -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/evolving/updater/multi_dim/doc.go internal/evolving/updater/multi_dim/multi_dim.go internal/evolving/updater/multi_dim/multi_dim_test.go
git commit -m "feat(evolving): 添加 MultiDimUpdater 实现 (9.70c)"
```

---

### Task 6: Trainer 回填 updater 字段类型

**Files:**
- Modify: `internal/evolving/trainer/trainer.go`
- Modify: `internal/evolving/trainer/trainer_test.go`

- [ ] **Step 1: 修改 trainer.go — 将 updater any 替换为 updater.Updater**

在 trainer.go 中：
1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/evolving/updater"`
2. 将 `updater any` 字段改为 `updater updater.Updater`
3. 将 `WithUpdater(updater any) TrainerOption` 改为 `WithUpdater(u updater.Updater) TrainerOption`
4. 更新相关方法签名中引用 `t.updater` 的地方（`_bind_updater`、`_updater_requires_forward` 等）

具体修改：

将 `updater any` 改为 `updater updater.Updater`：

```go
// updater 更新生成器。
// 对应 Python: evolving/updater/protocol.py Updater
updater updater.Updater
```

将 `WithUpdater` 改为：

```go
// WithUpdater 设置更新生成器。
func WithUpdater(u updater.Updater) TrainerOption {
	return func(t *Trainer) { t.updater = u }
}
```

将 `BindUpdater` 方法改为真实实现：

```go
// BindUpdater 将 Updater 绑定到 Agent 的 Operator 注册表。
//
// 对应 Python: Trainer._bind_updater(updater, operators)
func (t *Trainer) BindUpdater(operators map[string]operator.Operator, config map[string]any) int {
	if t.updater == nil {
		return 0
	}
	return t.updater.Bind(operators, nil, config)
}
```

将 `UpdaterRequiresForward` 方法改为真实实现：

```go
// UpdaterRequiresForward 判断 Updater 是否需要前向推理结果。
//
// 对应 Python: Trainer._updater_requires_forward(updater)
func (t *Trainer) UpdaterRequiresForward() bool {
	if t.updater == nil {
		return true
	}
	return t.updater.RequiresForwardData()
}
```

- [ ] **Step 2: 运行现有测试确认不破坏**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/trainer/ -v`
Expected: PASS（现有测试可能需要小幅调整，因为 WithUpdater 签名变了）

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/trainer/trainer.go internal/evolving/trainer/trainer_test.go
git commit -m "feat(evolving): Trainer.updater 字段类型回填为 Updater 接口 (9.70c)"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.70c 行状态为 ✅**

将：
```
| 9.70c | ☐ | Updater Protocol | Updater 协议接口（bind/requires_forward_data/update/process/get_state/load_state）+ SingleDimUpdater + MultiDimUpdater | `openjiuwen/agent_evolving/updater/` |
```

改为：
```
| 9.70c | ✅ | Updater Protocol | Updater 协议接口（bind/requires_forward_data/update/process/get_state/load_state）+ SingleDimUpdater + MultiDimUpdater | `openjiuwen/agent_evolving/updater/` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.70c Updater Protocol 实现状态为已完成"
```

---

### Task 8: 全量测试验证

- [ ] **Step 1: 运行全部 evolving 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -v`
Expected: ALL PASS

- [ ] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/evolving/...`
Expected: 各包覆盖率 ≥ 85%

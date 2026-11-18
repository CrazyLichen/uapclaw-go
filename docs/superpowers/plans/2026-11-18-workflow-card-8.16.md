# 8.16 WorkflowCard 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `openjiuwen/core/workflow/base.py`：把 WorkflowCard 从 `common/schema` 迁入 `internal/agentcore/workflow`（方案 B），补齐 base.py 全部伴生件，同改动删除旧实现（规则 8）。

**Architecture:** workflow 包新增 `base.go` 承载 base.py 全部符号（嵌入 schema.BaseCard 承载继承）；约 15 处消费方改 import 与类型引用；`schema/card.go` 删除 WorkflowCard 族（BaseCard/CardInterface/AbilityKind 留守）。四个 ⤵️8.15 回填钩子保留不实施。

**Tech Stack:** Go 1.x、标准库 testing；回归命令 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`。

**Spec:** docs/superpowers/specs/2026-11-18-workflow-card-8.16-design.md

---

### Task 1: workflow/base.go 补齐 base.py 全部符号（TDD）

**Files:**
- Modify: `internal/agentcore/workflow/base.go`（已存在：WorkflowOutput + WorkflowExecutionState）
- Modify: `internal/agentcore/workflow/base_test.go`
- Modify: `internal/agentcore/workflow/doc.go`

- [ ] **Step 1: 在 base_test.go 追加失败测试**

在现有 `base_test.go` 末尾追加（import 增加 `"testing"` 已有则略、`"github.com/google/uuid"` 不需要）：

```go
func TestWorkflowChunkType_字符串值(t *testing.T) {
	if string(workflow.WorkflowChunkTypeInteraction) != "interaction" {
		t.Fatalf("Interaction = %q", workflow.WorkflowChunkTypeInteraction)
	}
	if string(workflow.WorkflowChunkTypeOutput) != "output" {
		t.Fatalf("Output = %q", workflow.WorkflowChunkTypeOutput)
	}
	if string(workflow.WorkflowChunkTypeError) != "error" {
		t.Fatalf("Error = %q", workflow.WorkflowChunkTypeError)
	}
}

func TestGenerateWorkflowKey(t *testing.T) {
	if got := workflow.GenerateWorkflowKey("wf1", "v2"); got != "wf1_v2" {
		t.Fatalf("GenerateWorkflowKey = %q", got)
	}
	if got := workflow.GenerateWorkflowKey("wf1", ""); got != "wf1_" {
		// Python f-string 空串同样产生尾下划线，逐字对齐
		t.Fatalf("空 version = %q", got)
	}
}

func TestWorkflowCard_ToolInfo(t *testing.T) {
	// nil input_params → 空 map（对齐 Python tool_info 的 if ... else {}）
	card := workflow.NewWorkflowCard(workflow.WithName("my_wf"), workflow.WithDescription("工作流"))
	info := card.ToolInfo()
	if info == nil {
		t.Fatal("ToolInfo() = nil")
	}
	// 非 nil input_params 透传
	card2 := workflow.NewWorkflowCard()
	card2.InputParams = map[string]any{"a": 1}
	if info2 := card2.ToolInfo(); info2 == nil {
		t.Fatal("ToolInfo() = nil（非 nil params）")
	}
	if card.ID == "" || len(card.ID) != 32 {
		t.Fatalf("ID 应为 32 位 hex，got %q", card.ID)
	}
}

func TestWorkflowCard_Ability(t *testing.T) {
	card := workflow.NewWorkflowCard(workflow.WithName("wf"), workflow.WithDescription("d"))
	var _ schema.Ability = card // 编译期实现 Ability 接口
	if card.AbilityName() != "wf" || card.AbilityID() != card.ID {
		t.Fatal("AbilityName/AbilityID 不符")
	}
	if card.AbilityKind() != schema.AbilityKindWorkflow {
		t.Fatal("AbilityKind ≠ Workflow")
	}
	if card.Version != "" {
		t.Fatalf("默认 Version 应为空串（Python version: str = ''），got %q", card.Version)
	}
}

func TestWorkflowChunk_别名(t *testing.T) {
	// D-2：Go 无 Union，WorkflowChunk = any；能装下三种 stream schema 即视为别名成立
	var chunks []workflow.WorkflowChunk
	chunks = append(chunks, stream.OutputSchema{Type: "output"}, stream.TraceSchema{Type: "trace"}, stream.CustomSchema{Type: "custom"})
	if len(chunks) != 3 {
		t.Fatal("WorkflowChunk 别名无法容纳三种 schema")
	}
}
```

（同包测试直接用 `WorkflowChunkTypeInteraction` 等短名也可，保留 `workflow.` 前缀会导致同包编译错误——若 base_test.go 是 `package workflow` 则去掉前缀，若是 `package workflow_test` 则保留；写代码前先看现有 base_test.go 的 package 声明，与其保持一致。）

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/workflow/`
Expected: FAIL（WorkflowChunkTypeInteraction / GenerateWorkflowKey / NewWorkflowCard 未定义）

- [ ] **Step 3: 实现 base.go 补齐**

在 `internal/agentcore/workflow/base.go` 中追加（保持规范 2 的区块顺序：WorkflowCard 结构体加进结构体区块首位，枚举加进枚举区块，常量加进常量区块，函数加进导出函数区块）：

```go
import (
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCard 工作流元数据卡片。
// Python: openjiuwen/core/workflow/base.py WorkflowCard(BaseCard)；
// 继承以嵌入 schema.BaseCard 承载（对应 openjiuwen/core/common/schema/card.py BaseCard）。
// 偏差 D-1：Python input_params 为 dict|Type[BaseModel] Union，Go 用 any 承载。
type WorkflowCard struct {
	schema.BaseCard
	// Version 工作流版本号（Python 默认 ''）
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（map[string]any 或其他形态；Python Optional，空值语义=缺省）
	InputParams any `json:"input_params,omitempty"`
}

// NewWorkflowCard 创建工作流卡片，ID 自动生成 32 位 UUID hex（对齐 BaseCard default_factory）。
func NewWorkflowCard(opts ...schema.CardOption) *WorkflowCard {
	return &WorkflowCard{BaseCard: *schema.NewBaseCard(opts...)}
}
```

（`WithName/WithDescription` 复用 `schema.CardOption`，不新造选项——它们作用于 BaseCard。）

```go
// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowChunkType 工作流输出数据块类型。
// Python: base.py WorkflowChunkType(str, Enum)
type WorkflowChunkType string

const (
	// WorkflowChunkTypeInteraction 用户/Agent 交互流
	WorkflowChunkTypeInteraction WorkflowChunkType = "interaction"
	// WorkflowChunkTypeOutput 最终输出节点流
	WorkflowChunkTypeOutput WorkflowChunkType = "output"
	// WorkflowChunkTypeError 错误信息流
	WorkflowChunkTypeError WorkflowChunkType = "error"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// WORKFLOW_DRAWABLE 工作流可绘制标记
	WORKFLOW_DRAWABLE = "WORKFLOW_DRAWABLE"
)
```

```go
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期验证 WorkflowCard 实现 schema.Ability
var _ schema.Ability = (*WorkflowCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolInfo 返回工具信息，InputParams 为 nil 时传空 map（对齐 Python `if self.input_params else {}`）。
func (c *WorkflowCard) ToolInfo() schema.ToolInfoInterface {
	params, ok := c.InputParams.(map[string]any)
	if !ok || params == nil {
		params = map[string]any{}
	}
	return schema.NewToolInfo(c.Name, c.Description, params)
}

// AbilityName 返回能力名称。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID 返回能力唯一标识。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind 返回能力类型（工作流）。
func (c *WorkflowCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindWorkflow }

// GenerateWorkflowKey 生成工作流缓存键。
// Python: base.py generate_workflow_key → f"{workflow_id}_{workflow_version}"
func GenerateWorkflowKey(workflowID, workflowVersion string) string {
	return workflowID + "_" + workflowVersion
}

// WorkflowChunk 工作流输出数据块类型别名。
// Python: base.py WorkflowChunk = Union[OutputSchema, CustomSchema, TraceSchema]；
// 偏差 D-2：Go 无 Union，先用 any 承载，8.14 Workflow 真实消费时再定稿。
type WorkflowChunk = any
```

注意：`schema.NewToolInfo` 第三参类型为 `map[string]any`（见 common/schema/tool_info.go:61），故 ToolInfo 内做了 map 断言——这正对应 Python `tool_info` 只在 dict 形态下透传参数的行为；其他形态（Type[BaseModel]）Python 侧传给 ToolInfo 后由 Tool 的 JSON schema 解析处理，Go 侧当 map 缺失时给空 map（记入 D-1 回收条件备注）。

- [ ] **Step 4: 更新 doc.go 文件树**

`internal/agentcore/workflow/doc.go` 的 base.go 行改为：

```
//	├── base.go               # WORKFLOW_DRAWABLE/WorkflowCard/WorkflowChunkType/WorkflowExecutionState/WorkflowChunk/WorkflowOutput/GenerateWorkflowKey（8.16，一比一对应 base.py）
```

- [ ] **Step 5: 运行测试通过**

Run: `go test ./internal/agentcore/workflow/ && go build ./... && go vet ./...`
Expected: PASS（此时 schema.WorkflowCard 仍存在，双类型并存只是临时态，Task 2 收口）

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/workflow/base.go internal/agentcore/workflow/base_test.go internal/agentcore/workflow/doc.go
git commit -m "feat(workflow): 8.16 WorkflowCard 迁入 workflow 包并补齐 base.py 伴生件"
```

---

### Task 2: 消费方迁移 + schema 旧实现删除（同一改动，规则 8）

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/workflow.go:32`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go:50,92,181`
- Modify: `internal/agentcore/session/workflow.go:23,101,106`
- Modify: `internal/agentcore/session/tracer/decorator/decorator.go:329`
- Modify: `internal/agentcore/runner/resources_manager/base.go:60`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go:323,340,357,363,368-369,1665`
- Modify: `internal/agentcore/runner/resources_manager/workflow_manager.go:111`
- Modify（测试文件，机械替换同类型引用）: `internal/agentcore/session/tracer/decorator/decorator_test.go`、`internal/agentcore/session/workflow_test.go`、`internal/agentcore/runner/resources_manager/{base_test,test_helpers_test,workflow_manager_test,resource_manager_test}.go`、`internal/agentcore/runner/runner_test.go`、`internal/agentcore/single_agent/ability/ability_manager_test.go`
- Modify: `internal/common/schema/card.go`（删除）
- Modify: `internal/common/schema/doc.go`（文件树无 card.go 条目变化——card.go 保留，仅删 WorkflowCard 段；doc.go 若提及 WorkflowCard 则同步删除该句）
- Modify: `internal/common/schema/card_test.go`（删除 WorkflowCard 相关用例；BaseCard 用例保留）

- [ ] **Step 1: 逐文件替换类型引用**

非测试文件统一替换（7 个文件，逐处手改而非全局 sed——import 别名可能不同）：
- `*schema.WorkflowCard` → `*workflow.WorkflowCard`
- `schema.NewWorkflowCard(...)` → `workflow.NewWorkflowCard(...)`
- import 增加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"`；若该文件已有名为 `workflow` 的局部变量与包名冲突（先 `grep -n 'workflow :=' <file>` 排查，重点 session/workflow.go 的接收器上下文），则 import 别名 `wfcard "…/workflow"` 并使用 `wfcard.WorkflowCard`，**记录在提交信息中**。

特别注意：
- `resource_manager.go:369` 的断言 `r.Value.(*workflow.WorkflowCard)` 语义不变；
- `ability_manager.go:181` 的 type switch `case *workflow.WorkflowCard:` ——workflow 包已在 imports 中，只需改 case 表达式；
- `interfaces/workflow.go` 的 `Card()` 返回类型改动会波及所有 fake（见测试文件清单）。

- [ ] **Step 2: 删除 schema 旧实现**

`internal/common/schema/card.go` 删除以下段（其余全部保留）：
- `WorkflowCard` 结构体（L43-50）
- `var _ CardInterface = (*WorkflowCard)(nil)`（L80）
- `NewWorkflowCard`（L153-158）
- `(*WorkflowCard) ToolInfo/AbilityName/AbilityID/AbilityKind`（L160-176）
- `card_test.go` 中 WorkflowCard 专属用例同步删除（BaseCard/AbilityKind/ToolInfoInterface 用例保留）

- [ ] **Step 3: 编译验证**

Run: `go build ./... && go vet ./...`
Expected: 通过。若报 "WorkflowCard undefined" 说明仍有漏改处：`grep -rn 'schema\.WorkflowCard\|schema\.NewWorkflowCard' internal/ cmd/ tests/` 应 0 命中，逐个补改。

- [ ] **Step 4: 受影响包测试**

Run: `go test ./internal/common/schema/ ./internal/agentcore/workflow/ ./internal/agentcore/session/... ./internal/agentcore/runner/... ./internal/agentcore/single_agent/...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/common/schema/card.go internal/common/schema/card_test.go internal/common/schema/doc.go \
  internal/agentcore/single_agent/interfaces/workflow.go internal/agentcore/single_agent/ability/ability_manager.go internal/agentcore/single_agent/ability/ability_manager_test.go \
  internal/agentcore/session/workflow.go internal/agentcore/session/workflow_test.go internal/agentcore/session/tracer/decorator/decorator.go internal/agentcore/session/tracer/decorator/decorator_test.go \
  internal/agentcore/runner/resources_manager/ internal/agentcore/runner/runner_test.go
git commit -m "refactor(schema): WorkflowCard 迁入 workflow 包，删除 common/schema 旧实现（规则8 锚点清零）"
```

---

### Task 3: 计划表同步 + 全量回归收口

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（领域八 8.16 行 + 顺序注记）

- [ ] **Step 1: 锚点与回归**

Run:
```bash
grep -rn 'schema\.WorkflowCard' /home/opensource/uapclaw-gateway/internal /home/opensource/uapclaw-gateway/cmd /home/opensource/uapclaw-gateway/tests --include="*.go"   # 期望 0 命中
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...   # 期望零 FAIL
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 8.16 行**

将 8.16 行的 ☐ 改 ✅，产出列写入 Go 事实（要点：`internal/agentcore/workflow/base.go` 一比一对应 base.py 七符号；方案 B 迁移自 common/schema/card.go 并删除旧实现（锚点 0 命中）；偏差 D-1 InputParams any / D-2 WorkflowChunk=any / D-3 json tag；4 处 ⤵️8.15 回填钩子保留未做；spec 与 plan 路径）。原有 ⤴️8.15 行的依赖说明同步补一句「地基已由 8.16 兑现」。

- [ ] **Step 3: 追加顺序注记（保留全部旧注记）**

在既有顺序注记列表末尾追加：

```
> **8.x 实施顺序（2026-11-18 十一次记录）**：8.16 提前于 8.15 实施（8.16 是 8.15 WorkflowConfig.card 字段的地基且体量小、无前置阻塞；依据见 8.16 spec docs/superpowers/specs/2026-11-18-workflow-card-8.16-design.md §6）。原注记保留。
```

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 8.16 WorkflowCard 完成，追加第十一次实施顺序记录"
```

---

## 删除清单（规则 8.3）

| 对象 | 位置 | 验收 |
|---|---|---|
| `WorkflowCard` 结构体 | common/schema/card.go:43-50 | grep `schema\.WorkflowCard` 0 命中 |
| `NewWorkflowCard` | common/schema/card.go:153-158 | 同上 |
| `(*WorkflowCard)` 四方法 | common/schema/card.go:160-176 | 同上 |
| 编译期断言 `var _ CardInterface = (*WorkflowCard)(nil)` | common/schema/card.go:80 | 同上 |
| card_test.go WorkflowCard 专属用例 | common/schema/card_test.go | 测试仍全绿 |

## 回填项核对（不得丢失，全部保留）

1. session/config Get/AddWorkflowConfig `any → WorkflowConfig`（config.go:35-39、:59）→ 8.15
2. tracer getWorkflowMetadata 提取 card.version/name（tracer/workflow.go:268）→ 8.15
3. 8.12 D6 workflow_config.go WorkflowConfig 完整同步 → 8.15
4. 8.11 D-3 NodeConfig 收窄 → 8.15

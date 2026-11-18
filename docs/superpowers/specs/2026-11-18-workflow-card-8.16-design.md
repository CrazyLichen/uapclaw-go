# 8.16 WorkflowCard 设计

## 1. 背景

领域八 8.16 对齐 Python `openjiuwen/core/workflow/base.py`（69 行）。Go 侧发现 WorkflowCard 已提前在 `internal/common/schema/card.go`（L43-50）实现并被 ability_manager / resource_manager / WorkflowSession / tracer decorator / session interfaces 等约 15 处消费——这是与规则 9 文件归属的结构性偏差（Python 定义在 core/workflow/base.py，Go 放进了 common/schema）。

本设计经用户 2026-11-18 拍板采用**方案 B：严格对齐文件归属**——把 WorkflowCard 迁入 workflow 包，同改动删除 schema 侧旧实现（规则 8，旧符号 0 命中）。

## 2. Python 源盘点

`openjiuwen/core/workflow/base.py` 全部符号：

| 符号 | 性质 | 说明 |
|---|---|---|
| `WORKFLOW_DRAWABLE` | 常量 | 字符串标记 "WORKFLOW_DRAWABLE" |
| `WorkflowCard(BaseCard)` | pydantic 模型 | `version: str = ''` + `input_params: Optional[dict[str,Any]\|Type[BaseModel]] = None`；`tool_info()` 转 ToolInfo（input_params 空时传 `{}`） |
| `WorkflowChunkType(str, Enum)` | 枚举 | INTERACTION="interaction" / OUTPUT="output" / ERROR="error" |
| `WorkflowExecutionState(str, Enum)` | 枚举 | COMPLETED / INPUT_REQUIRED / ERROR |
| `WorkflowChunk` | 类型别名 | `Union[OutputSchema, CustomSchema, TraceSchema]`（来自 `openjiuwen.core.session.stream`） |
| `WorkflowOutput(BaseModel)` | 模型 | `result: Any` + `state: WorkflowExecutionState` |
| `generate_workflow_key(id, version)` | 模块级函数 | `f"{workflow_id}_{workflow_version}"` |

依赖：`openjiuwen.core.common`（BaseCard）、`foundation.tool`（ToolInfo）、`openjiuwen.core.session.stream`（OutputSchema/CustomSchema/TraceSchema）。

## 3. Go 实现方案

### 3.1 文件归属（一比一映射）

新建 `internal/agentcore/workflow/base.go`，承载 base.py 全部符号：

- `WorkflowCard`：嵌入 `schema.BaseCard`（对应 Python 继承 BaseCard）+ `Version string` + `InputParams any`（Python 是 dict 或 pydantic Type 的 Union，Go 用 any 承载，与既有 map[string]any 消费方兼容，迁移时消费方类型断言逐处核对）+ `ToolInfo()`（nil→空 map 对齐 Python `if ... else {}`）+ `AbilityName/AbilityID/AbilityKind` 三方法（实现 `schema.Ability`，schema 是低层包无环）。
- `WorkflowChunkType` / `WorkflowExecutionState`：string 枚举，值一比一（"interaction"/"output"/"error"、"COMPLETED"/"INPUT_REQUIRED"/"ERROR"），零值空串对齐 Python 枚举成员必有显式值的语义（构造函数收口或常量直用，实施时按 string 枚举惯例）。
- `WorkflowChunk`：类型别名。首选 `any`（Go 无 Union 类型）；若 stream 侧存在三类型共有的接口（如 `SchemaType() string`，见 session/stream/base.go:88-94），可收窄为该接口，实施时定并记入偏差。
- `WorkflowOutput{Result any; State WorkflowExecutionState}`。
- `GenerateWorkflowKey(workflowID, version string) string`：`workflowID + "_" + version`。
- 常量 `WORKFLOW_DRAWABLE = "WORKFLOW_DRAWABLE"`。

包声明与 doc.go 条目同步：`internal/agentcore/workflow/doc.go` 若无则新建（含文件目录树 + 对应 Python 路径），有则在文件树中追加 `base.go`。

### 3.2 同改动删除（规则 8）

从 `internal/common/schema/card.go` 删除：

- `WorkflowCard` 结构体及其 JSON tag
- `NewWorkflowCard` 构造函数
- `(*WorkflowCard) ToolInfo/AbilityName/AbilityID/AbilityKind/String`（嵌入继承自 BaseCard 的 GetID/GetName/GetDescription 除外）
- `var _ CardInterface = (*WorkflowCard)(nil)` 编译期断言

保留：`BaseCard`、`CardInterface`、`Ability`、`AbilityKind` 及其枚举值、`CardOption`、`NewBaseCard`、`ToolInfoInterface`（对应 Python `core/common/schema/card.py`，归属本来就正确）。

### 3.3 迁移波及面（消费方改 import + 引用）

- `internal/agentcore/single_agent/ability/ability_manager.go`
- `internal/agentcore/runner/resources_manager/base.go`（WorkflowProvider）、`resource_manager.go`、`workflow_manager.go`
- `internal/agentcore/session/workflow.go`（WorkflowSession workflowCard 字段）
- `internal/agentcore/session/tracer/decorator/decorator.go`（TracedWorkflow.Card()）
- `internal/agentcore/single_agent/interfaces/workflow.go`
- 其他 `grep 'schema\.WorkflowCard'` 全量命中处

验收：`grep -rn 'schema\.WorkflowCard' internal/ tests/ cmd/` 0 命中；`go build ./...` + `go vet ./...` 通过。

## 4. 已知偏差（登记）

| 编号 | Python | Go | 性质/理由 | 回收条件 |
|---|---|---|---|---|
| D-1 | `input_params: Optional[dict \| Type[BaseModel]]` | `InputParams map[string]any`（2026-11-18 审查修订：由 any 收窄） | Go 无 Union/pydantic 元类型；Type[BaseModel] 形态由外层调用方自行转 JSON Schema map 后传入；WorkflowCard.InputParams 全仓零消费方，收窄无波及 | 无（终态） |
| D-2 | `WorkflowChunk = Union[...]` | `type WorkflowChunk = stream.Schema`（2026-11-18 审查修订：由 any 收窄） | stream.Schema 接口（session/stream/base.go:15）已被三类型实现；workflow→session/stream 经 go list -deps 验证无环，any 非环所迫 | 无（终态） |
| D-3 | pydantic 模型序列化字段名（snake_case） | 导出字段 + json tag（version/input_params） | 与 BaseCard 既有 Go 惯例一致 | 无（终态） |

## 5. 回填项（保留，不提前做）

8.16 只做 base.py 本体 + 迁移。以下钩子**解锁但不实施**，仍指向 8.15：

1. session/config：`GetWorkflowConfig/AddWorkflowConfig` 收窄 `any → WorkflowConfig`（config.go:35-39、:59）
2. tracer `getWorkflowMetadata` 提取 `card.version/card.name`（session/tracer/workflow.go:268 ⤵️）
3. 8.12 D6：`workflow/workflow_config.go` 补 `WorkflowConfig` 结构体完整同步
4. 8.11 D-3：`NodeConfig() any` → `*workflow.NodeSpec` 断言收紧

理由：四处均属 8.15 WorkflowConfig 语义面，提前做会越权。8.16 落地后 `WorkflowConfig.card: WorkflowCard` 的地基已就绪。

## 6. 章节顺序（第 N 次记录）

8.16 提到 8.15 之前实施：8.16 是 8.15 WorkflowConfig.card 字段的地基，且体量小、无前置阻塞。8.13 可视化与 8.16 互不依赖维持原序。计划表追加顺序注记，旧注记全部保留。

## 7. 测试

- `base.go` 单测（`base_test.go`）：两枚举字符串值与 String()、WorkflowOutput 构造、GenerateWorkflowKey（含空 version 边界）、WorkflowCard.ToolInfo（nil input_params→空 map、非 nil 透传）、Ability 三方法。目标覆盖率 ≥ 85%。
- 迁移后全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL。
- grep 锚点验收见 §3.3。

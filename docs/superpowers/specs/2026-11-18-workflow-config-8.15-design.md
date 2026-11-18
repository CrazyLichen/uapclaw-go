# 8.15 WorkflowConfig 设计（方案 A：完整同步 + 三件回填一次收口）

- 日期：2026-11-18
- Python 参考：`openjiuwen/core/workflow/workflow_config.py`（82 行，5 类）
- Go 位置：`internal/agentcore/workflow/workflow_config.go`（8.12 前置骨架，本节完整同步）
- 前置状态：8.16 WorkflowCard ✅、8.10 ComponentAbility ✅、8.12 骨架 ✅；全部就绪，无换序需要。
- 章节依赖：8.14 Workflow 类强依赖本节（`_workflow.py:33` 直接 import 全部四个类），本节先于 8.14 实施正当。

## 一、流程位置与作用

WorkflowConfig 是工作流的**配置信封**：`Workflow.__init__ → BaseWorkflow(WorkflowConfig(card, **kwargs), PregelGraph())`（workflow.py:115 / _workflow.py:149），组件注册时打包 `NodeSpec` 写入 `WorkflowSpec.comp_configs`（_workflow.py:187-189），挂载到会话 `Config._workflow_configs: dict[str, WorkflowConfig]`（session/config/base.py:123）。执行期三条消费支线：

1. **Vertex.applyNodeConfig**：逐节点读 NodeSpec 的 timeout/max_retries/exception_config/name/io_configs（vertex.py:92-97）。
2. **session/internal/workflow.py:119**：从 NodeSpec 缓存 node_name（None 回退 comp_id）。
3. **嵌套深度门卫**：`_workflow.py:304` 用 `workflow_max_nesting_depth` 拦截子工作流递归超限。

## 二、拍板记录（讨论过程四项决策）

| # | 决策 | 内容 |
|---|------|------|
| 1 | 方案 A | 完整同步 + 三件回填一次收口（另两案 B 拆回填、C 校验延后均否） |
| 2 | 方案 α | pydantic 校验用**构造函数返回 error 复刻拒绝语义**（先例：8.3a 构造期 panic 纪律、8.33 NewMessageBus (T, error)）；非钳制、非执行期校验 |
| 3 | 方案 P | `NewWorkflowConfig(card, opts...) (*WorkflowConfig, error)` 变参选项，与 8.16 `NewWorkflowCard(opts ...schema.CardOption)` 同款惯例；spec nil 补建（default_factory）；`_workflow.py:151` 空值兜底收敛进构造函数 |
| 4 | 方案 Ⅰ | 本节补全 snake_case json tag（同包 8.16 WorkflowCard 已有先例）；`Name` 改 `*string`；ExceptionConfig.Extra 平铺序列化 |

## 三、Go 事实清单

### 3.1 WorkflowConfig 本体（workflow_config.go 完整同步）

- **`WorkflowConfig`**：`Card *WorkflowCard`（`json:"card"`，必填）+ `Spec *WorkflowSpec`（`json:"spec"`）+ `WorkflowMaxNestingDepth int`（`json:"workflow_max_nesting_depth"`）。
- **`NewWorkflowConfig(card *WorkflowCard, opts ...WorkflowConfigOption) (*WorkflowConfig, error)`**：
  - card nil → error（复刻 pydantic 缺必填 ValidationError）；
  - `WithWorkflowSpec(*WorkflowSpec)`：nil/未设置时补建 `NewWorkflowSpec()`（对齐 `default_factory=WorkflowSpec`）；
  - `WithMaxNestingDepth(int)`：越界 [0,10] → error（对齐 `ge=0, le=10`）；未设置默认 5；
  - 深度值 0 是合法值也是 Go 零值，故「未设置默认 5」由选项机制承载，不得用零值判断。
- **校验拒绝语义**：
  - `NewNodeSpec` 改 `(*NodeSpec, error)`：`MaxRetries < 0` → error（对齐 `ge=0`）；
  - `Timeout` 无约束对齐（Python `float = -1.0`，无 ge/le）；`<=0` 表示不超时的语义留在注释。
- **`NodeSpec.Name` 改 `*string`**：对齐 `Optional[str] = None`，区分未设置与空串；trace 时 nil 回退 comp_id（session/internal/workflow.py:119 语义，8.14 消费时接上）。
- **json tag 补全**（snake_case，对齐 pydantic 字段名）：
  - CompIOConfig：`inputs_schema` / `outputs_schema`；
  - ExceptionConfig：`handle_type` + Extra 平铺（见 3.2）；
  - NodeSpec：`io_configs` / `stream_io_configs` / `abilities` / `max_retries` / `timeout` / `exception_config` / `name`；
  - WorkflowSpec：`edges` / `stream_edges` / `comp_configs` / `stream_source_groups` / `start_nodes`；
  - WorkflowConfig：`card` / `spec` / `workflow_max_nesting_depth`。
- **CompIOConfig 的 InputsSchema/OutputsSchema 维持 `any`**（Python `Dict | Transformer` 无 Go 对应，map[string]any 或 session/utils MessageTransformer，沿用 8.12 骨架注释既定口径）。

### 3.2 ExceptionConfig extra=allow 的序列化

Python `model_config = {"extra": "allow"}`：未声明字段平铺进模型并可序列化。Go 承载：`Extra map[string]any` 字段 + 定制 `MarshalJSON`/`UnmarshalJSON`——`handle_type` 与 Extra 键平铺（Unmarshal 时已知键 `handle_type` 归位、其余进 Extra）。**不得**用 `json:"-"` 丢序列化面。

### 3.3 回填① — NodeConfig 管路收窄

1. `session/config/config.go`：`GetWorkflowConfig(workflowID string) any` → `*workflow.WorkflowConfig`（接口 + defaultSessionConfig 实现 + 内部 map 同步收窄）。
2. `session/internal/workflow_session.go:398 NodeConfig()`：删除 ⤵️8.15 悬挂注释，真实实现 `wfc.Spec.CompConfigs[n.nodeID]`，返回 `*workflow.NodeSpec`；nil 三分支保留（cfg nil / wfc nil / 项缺失 → nil，对齐 Python `.get(...)` 链）。对齐 Python `NodeSession.node_config() → config.get_workflow_config(workflow_id).spec.comp_configs.get(node_id)`。
3. 接口面同步：`session/node.go:296 NodeConfig() any` → `NodeConfig() *workflow.NodeSpec`；`NodeSessionFacade.GetNodeConfig`（node.go:241）与 `RouterSessionFacade.GetNodeConfig`（wrapper.go:135，桩保持 nil 返回）同步。
4. `graph/vertex.go`：`applyNodeConfig(nc any)` 断言收窄为直取 `*NodeSpec`；**偏差 D-3 内联回收**——`applyNodeConfig` 与装配面 `ApplyNodeConfig` 删除，逻辑回 `Init` 内联（对齐 Python 内联于 init，规则 9/12）；`vertex_test.go` 各处 `v.applyNodeConfig(...)` 直测改走 Init 路径或字段注入驱动。
5. 包依赖核验：workflow → session/stream 单向；session/config、session/internal → workflow 反向成立，无环。

### 3.4 回填② — tracer getWorkflowMetadata

`session/tracer` 悬挂点：从 `*WorkflowConfig` 提取 `card.version` / `card.name` 填 workflow metadata；对齐 Python workflow.py:261-264；card nil 字段沿用 Python 缺省行为。

### 3.5 回填③ + 钩子销账

- 8.12 D6（骨架完整同步）即 §3.1 本体，spec 与 IMPLEMENTATION_PLAN.md 销账。
- IMPLEMENTATION_PLAN.md：8.15 行三处 ⤴️ 改 ✅；8.16 行四处 ⤵️8.15 钩子销账；8.11 D-3 内联回收销账。
- 新实施序注记：8.15 → 8.13/8.14 → 8.17-8.26 组件（8.15 无需换序，编号序已满足依赖）。

## 四、测试与验收

1. workflow 包：构造校验（nil card error / depth 越界 error / 负 retries error / nil spec 补建 / 默认 5 与显式 0 并存）、序列化往返（Extra 平铺、Name nil vs 空串、全 tag 命名对齐）。
2. session 包：NodeConfig 真实取值 + cfg nil / wfc nil / spec nil / comp 项缺失四个 nil 分支；GetWorkflowConfig 收窄后既有测试适配。
3. graph 包：vertex 能力派生改 Init 驱动后既有用例全绿（D-3 回收零行为变化）。
4. 门槛：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全量零 FAIL + `go build ./...` + `go vet ./...`；推送即交付（规则 11，不追 CI）。

## 五、已知偏差

| 编号 | 内容 | 理由 | 回收条件 |
|------|------|------|---------|
| D-15-1 | CompIOConfig.InputsSchema/OutputsSchema 用 `any` | Python `Dict \| Transformer` Union 无 Go 对应 | 无（终态，与 8.12 骨架既定口径一致） |
| D-15-2 | `Name *string` vs Python `Optional[str]` | Go 指针承载 Optional；nil ≡ None | 无（终态） |
| D-15-3 | ExceptionConfig Extra 平铺由定制 MarshalJSON/UnmarshalJSON 承载 | pydantic extra=allow 动态字段 Go 无原生对应 | 无（终态） |
| D-15-4 | 直接构造 `ExceptionConfig{}` 零值时 HandleType 为 `""` 而非 `"interrupt"`；默认值由 `NewExceptionConfig()` 构造函数承载 | Go 无 pydantic 构造期默认值注入，零值 struct 不经构造函数 | 8.14 `add_node(exception_config=...)` 消费路径统一走 `NewExceptionConfig()`；若发现绕过构造函数的裸构造再收紧 |

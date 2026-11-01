# 2026-09-27 审查遗留项修复设计（2026-11-01）

> 来源：`docs/review/2026-09-27-48h-logic-review.md`（73 项：34S / 24M / 15T）
> 基线：main @ `4157122e`（已与 origin/main 对齐）
> Python 参考：`/home/opensource/agent-core/openjiuwen`、`/home/opensource/jiuwenswarm-develop`
> 核验方式：6 组并行代理逐项比对 Go 当前实现与 Python 语义，证据精确到 file:line

---

## 一、核验结论

73 项中 **54 项已闭环**（已修复 / 已由他处覆盖 / 不适用），**19 项待处置**（18 项来自原文档 + 1 项本轮新发现）。

### 已闭环（不再改动，仅在审查文档标注）

| 类别 | 编号 |
|---|---|
| 已修复 | S-02~S-10、S-12~S-14、S-16~S-18、S-21~S-24、S-26~S-34、M-01、M-03、M-04、M-07、M-09、M-16~M-19、M-21、M-22、M-24、M-26、T-01~T-12、T-14、T-15 |
| 已由他处覆盖 | S-05（`team_agent.go:1463,1529`）、S-06（`team_agent.go:1580,1629`）、M-09（子类实例 Name 一致）、M-22（`deep_agent.go:1818-1875` 重挂 SpanState） |
| 不适用 | S-27（`Result` 为 `map[string]any`，非 dict 分支不可达）、S-29（当前 Python 亦不重置 busy 分支）、T-01（Go 用 `inflightToolCalls` 同步跟踪） |

### 待处置 19 项与决策

| # | 编号 | 结论 | 采用方案 |
|---|---|---|---|
| 1 | S-01 残留 | 空成员列表被误判"全 SHUTDOWN"→ 误删团队 | A：补 `len(members) > 0` |
| 2 | S-11 残留 | ExternalMemoryRail 读 react 段快照，生产永不启用 | A：改 `configBase` + 测试同步 |
| 3 | S-15 | todo 工具从未注入，`todo.updated` 永不发射 | 完全 1:1：rail 内懒建 `getTodoTool()` |
| 4 | S-19 | `loadUserRails` 空壳（Python 动态执行用户 .py，Go 无等价能力） | C：归档为语言差异 |
| 5 | S-20 | 缺 `pendingTools/toolsApplied` 字段 | A′：`[]tool.Tool` + `bool` + 访问器 |
| 6 | S-25 残留（新） | `otlp_http` 用 `WithEndpoint` 传带 scheme URL，导出必失败 | A+：http 用 `WithEndpointURL`，grpc 解析剥 scheme |
| 7 | M-02 | `Setup(role, bp, inf)` vs Python `setup(*, role)` | B：KernelHost 增访问器，Setup 从 host 取 |
| 8 | M-05 | `extractAssistantOutput` 两处 warn 缺失 | 1:1 补齐（2 处，`Result==nil` 保持静默） |
| 9 | M-06 | `resolveUserTextForMemory` warn 缺 4 字段 | 1:1 补齐，`trace_id` 用 `ctx_ptr` 指针串 |
| 10 | M-08 | 列表渲染 `[a b]` vs `['a', 'b']` | A：归档（模板仅标量占位符，不可触发） |
| 11 | M-10 | `EntityTypeID()` 返回 0 无说明 | A：补注释 |
| 12 | M-11 | `auto_summarize` 多两个防御条件，注释不全 | A：补注释 |
| 13 | M-12 | `agent_tool` 错误码三码混用 | A：统一 `TOOL_TASK_TOOL_INVOKED` + `reason` 参数 |
| 14 | M-13 | 空 agentID → `agent_tool_` 无随机后缀 | A：补 uuid4 hex |
| 15 | M-15 | `EnsurePersistentCheckpointer` 硬编码 Background、持全局锁不可取消 | A：加 ctx + 30s 超时；去掉未同步快路径 |
| 16 | M-20 | entity 默认 min_score 0.02 / rank RRF，Python 为 0.3 / Weighted | A：完全对齐 |
| 17 | M-23 | `shutdownObservability` 整命名空间删除 | A：注册时攒 Off 闭包，shutdown 逐个执行 |
| 18 | M-25 | `Count` 全局分支全表扫描 | B：归档（无调用者、SDK 无 `num_entities`、无法在 CI 验证） |
| 19 | T-13 | `CodeAgentRail.Reload` 内部 `context.Background()` | A：`Reload(ctx, agent)` 传递 |

---

## 二、逐项设计

### 1. S-01：空成员列表误删团队

**根因**：`ListMembers` 用 `make([]*TeamMember, 0, n)` 返回**非 nil 空切片**（`team_backend.go:321-328`），`kernel.go:215` 的 `members != nil && allMembersShutdown(members)` 对空切片成立（空循环返回 true），于是走 `CleanTeam`（`DeleteTeam` + `DropCurSessionTables`）。Python `team.py:780-781` 返回 `[]`，`kernel.py:130` 的真值判断使其走 `recover_team`。

**改法**：`kernel.go:215` → `if len(members) > 0 && allMembersShutdown(members)`。

**测试**：`kernel_test.go` 增"team 存在 + 无非 leader 成员 → 调 RecoverTeam，CleanTeam 未被调用"（fake backend 记录调用）。

### 2. S-11：ExternalMemoryRail 配置来源错误

**根因**：`deep_adapter_rails.go:589` 与 `:654` 传 `d.configCache`（react 段快照），而 `memory.external` 是 config.yaml 顶级段（`external_memory_config.go:70-72`），`GetMemoryEngine` 永远回落 `builtin` → `IsExternalMemoryEnabled` 恒 false。同文件 `handleMemoryRailByConfig` 已改用 `d.configBase` 并留注释说明。

**改法**：
- `deep_adapter_rails.go:589` → `commmem.BuildExternalMemoryRail(d.configBase, d.workspaceDir)`
- `deep_adapter_rails.go:654` → `commmem.IsExternalMemoryEnabled(d.configBase)`
- `deep_adapter_rails_test.go` 7 处 `d.configCache = {...memory...}` → `d.configBase`

**测试**：新增"configBase 顶层分层 + engine=both + provider 齐全 → 注册成功且 `externalMemoryRailRegistered == true`"；保留"provider 为空不注册""构建失败不注册"。

### 3. S-15：todo 工具注入（完全 1:1）

**根因**：`SetTodoTool` 生产零调用；`lazyLoadTodoTool` 从 `AbilityManager.List()` 找实现 `LoadTodos` 的 ability，但该表只存 `*tool.ToolCard`（`ability_manager.go:43`），断言必失败 → `todoTool` 恒 nil → `emitTodoUpdated` 首行短路。

**Python 语义**（`stream_event_rail.py:593-624`）：`_get_todo_tool()` 在被 `_emit_todo_updated` 首次调用时才创建，数据来自 `self._deep_agent.deep_config`，缓存于 `_main_todo_tool`。**刻意不用 resource_mgr**（子 agent 会覆盖主 agent 条目，导致读错 workspace）。

**改法**（全在 rails 包内，adapter 零改动）：
- 新增窄接口 `deepConfigProvider interface{ DeepConfig() *hschema.DeepAgentConfig }`
- 新增 `getTodoTool()`：`r.deepAgent` 断言 `deepConfigProvider` → 取 `Workspace.GetNodePath(workspace.WorkspaceNodeTODO)`、`SysOperation.Fs()`、`Language`（回退 `SystemPromptBuilder().Language()`，再回退 `"cn"`）、`Card.ID` → `todo.CreateTodosTool(...)` 第二个返回值 → 加锁缓存
- 发射点 `stream_event_rail.go:374-376` 改 `loader := r.getTodoTool()`
- 删除 `lazyLoadTodoTool` 及其在 `Init` 的调用；`SetTodoTool` 保留为单测注入口
- imports 增加 `hschema`、`workspace`、`todo`（无环：`todo` 不依赖 swarm）

**测试**：抽 `newTodoToolFromDeepConfig(cfg, fallbackLang)` 纯函数直接测（临时目录 + `sys_operation.NewLocalSysOperation`，`SaveTodos` 后 `LoadTodos` 读回）；`getTodoTool` 的 nil 分支（cfg 为 nil / Workspace 为 nil / SysOperation 为 nil）。

### 4. S-19：loadUserRails 归档

**根因**：Python `rail_manager.py`（550 行）用 `importlib` 动态执行用户手写的 `extensions/<name>/rail.py`，Go 无法加载 Python 代码。`team_manager_spec.go:154,246` 引用同一机制的步骤也是空操作（10.5.7 延后）。

**改法**：
- `deep_adapter.go:1920-1926` 注释改写为语言边界说明（移除失效的 `TODO(#9.63)`），日志级别 `Debug` 保留
- `team_manager_spec.go:154,246` 注释同步指向"用户可扩展 Rail 属设计缺口"
- 审查文档标注"归档：语言边界"

**后续**：用户可扩展 Rail 作为独立设计项立项（Go 原生注册表或 plugin），不在本轮。

### 5. S-20：pendingTools / toolsApplied

**根因**：Python 侧仅定义字段与 getter，全仓无赋值/读取（`context_evolution_rail.py:60-61,83-90`），属预留 API。

**改法**：`context_evolution_rail.go` 增 `pendingTools []tool.Tool`、`toolsApplied bool` 与 `PendingTools() []tool.Tool`、`ToolsApplied() bool`（用具体类型 `tool.Tool`，不用 `any`）。

**测试**：零值与赋值后读取两条断言。

### 6. S-25 残留：OTLP 导出器 endpoint

**根因**：`DefaultObservabilityConfig().Endpoint = "http://localhost:4317"`（带 scheme），而 `otlptracehttp.WithEndpoint` 期望 `host:port`，结果为 `http://http://localhost:4317/v1/traces`。`otlptracegrpc.WithEndpoint` 同样不接受 scheme。

**改法**（`setup.go:279-296`）：
- `otlp_http` → `otlptracehttp.WithEndpointURL(config.Endpoint)`
- `otlp_grpc` → 新增 `parseOTLPEndpoint(endpoint) (hostPort string, insecure bool)`：按 `://` 剥离 scheme，`http` → insecure=true，`https`/无 scheme → 沿用配置的 `WithInsecure()` 行为
- 两分支均加日志记录解析结果

**测试**：`parseOTLPEndpoint` 表驱动（`http://h:1`、`https://h:1`、`h:1`、带路径 `/v1/traces`）；`buildExporter` 对两种 exporter 均返回非 nil 且无 error。

### 7. M-02：Setup 参数收敛

**根因**：Python `setup(*, role)` 从 `host.blueprint` / `host.infra` 取；Go 显式传参，但 `KernelHost` 当前**没有**返回 `DispatcherBlueprint` / `DispatcherInfra` 的方法（`BlueprintAccessor` 只给 `SpecAny() any`）。

**改法**：
- `types/lifecycle_accessors.go` 的 `KernelHost` 增加 `DispatcherBlueprint() DispatcherBlueprint`、`DispatcherInfra() DispatcherInfra`
- `TeamAgent` 实现（`team_agent.go` 新增两方法）：`configurator == nil || configurator.Blueprint() == nil` 时返回**真正的 nil 接口**（避免 typed-nil 绕过 Setup 的 nil 守卫）
- `Setup(role schema.TeamRole, opts ...KernelOption)`，内部从 host 取 bp/inf 并保留 nil 守卫
- 调用点 `team_agent.go:538` → `a.coordination.Setup(runtimeCtx.Role)`
- `kernel_test.go` 的 `fakeKernelHost` 增两字段 + 两方法；`e2e_test.go` 等其他构造点同步

**测试**：现有 kernel 测试全绿；新增"host 返回 nil blueprint → Setup 记录错误且 EventBus 仍为 nil"。

### 8. M-05：extractAssistantOutput 两处 warn

**改法**（`external_memory_rail.go:731-767`）：
- `inputs` 非 `*InvokeInputs` → warn `external_memory_rail_extract_output_result_missing` + `session_id`
- 遍历完无命中 → warn `external_memory_rail_extract_output_keys_missed` + `session_id` + `available_keys`（map keys **排序**后输出，保证日志可比对）
- `Result == nil` **保持静默**（与 Python `str(None) if None else ""` 一致）

### 9. M-06：resolveUserTextForMemory 字段补全

**改法**（`external_memory_rail.go:723-726`）：补 `session_id`（`InvokeInputs.ConversationID`，缺失回退 `"unknown"`）、`has_query`（inputs 为 `*InvokeInputs`）、`has_messages`（inputs 为 `*ModelCallInputs`）、`ctx_ptr`（`fmt.Sprintf("%p", cbc)`）。注释说明 Python `trace_id = id(ctx) % 10000` 是对象身份语义，Go 用指针串等价表达。

### 10. M-08：归档

模板仅 `{i}/{name}/{content}`（`cn/register.go:26`），值域为字符串，列表渲染差异不可触发。

### 11. M-10 / M-12（注释）

- `parse_llm_response.go:68-73`：注释说明 Entity 无 `entity_type_id`，返回 0 表示"不适用"，当前无调用点依赖
- `context_evolution_rail.go:387`：注释说明 `mattsMode == "none"` 对应 Python "only support matts_mode=none"，`memoryService != nil` 为防御（Python 在此会 `AttributeError`）

### 12. M-12：agent_tool 错误码统一

**改法**（`agent_tool.go`）：5 处统一 `exception.StatusToolTaskToolInvoked` + `exception.WithParam("reason", ...)`。
**附修**：现有 `:114` 传的是 `WithParam("error_msg", ...)`，而 `TOOL_TASK_TOOL_INVOKED` 模板是 `error='{reason}'`，导致消息渲染成 `<missing:reason>`、原文丢失——统一后该 bug 一并消除。
**测试**：断言 5 条错误路径的 `Status().Code == 182505`，且 `Error()` 含原因文本。

### 13. M-13：tool_id 随机后缀

**改法**（`code_agent_rail.go:370-371`）：`agentID == ""` 时 `"agent_tool_" + strings.ReplaceAll(uuid.New().String(), "-", "")`（32 位 hex，对齐 Python `uuid4().hex`）。
**测试**：空 agentID 两次构造 → id 不同且以 `agent_tool_` 开头、长度 43。

### 14. M-15 + T-13：ctx 传递

**改法**：
- `EnsurePersistentCheckpointer(ctx context.Context) error`，内部 `ctx, cancel := context.WithTimeout(ctx, 30*time.Second)`
- **去掉未同步快路径**：`persistentCheckpointerReady` 的读取全部移入锁内（消除 data race，与 `ResetPersistentCheckpointer` 一致）
- 调用链 `setCheckpoint(ctx)` → `deep_adapter.go:391`、`code_adapter.go:199`、`handle_session.go:187`、`agent_server.go:285`
- `CodeAgentRail.Reload(ctx, agent)` → `Init(ctx, agent)`，调用点 `code_adapter.go:454`

**测试**：现有测试全绿；`go test -race` 覆盖并发首次调用（可加 2 goroutine 并发 `EnsurePersistentCheckpointer(ctx)` 用例）。

### 15. M-20：entity 搜索默认配置

**根因**：Python `base.py:144` 与 `:217` 的 entity 默认是 `SearchConfig(rank_config=WeightedRankConfig())` → min_score **0.3** + **Weighted** 排序；Go 注释写的"Python 0.02"是错的，且 `register` 路径用了 0.02 + RRF。

**改法**（`graph_memory/base.go`）：
- 新增 `newEntitySearchConfig()`：`config.NewSearchConfig()` + `RankConfig = graph.NewWeightedRankConfig()`
- `:209`（`__init__` default 策略）与 `:367`（`registerSearchStrategy`）改用该函数
- 常量 `defaultSearchMinScoreEntity` 改名 `defaultSearchMinScoreRelation`（仅用于 relation=0.02）；episode 保持 0.025
- 删除错误注释

**测试**：断言 default 与 register 两条路径的 entity `MinScore == 0.3` 且 `RankConfig` 为 `*WeightedRankConfig`；relation/episode 维持 0.02/0.025。

### 16. M-23：shutdown 精确注销

**改法**（`setup.go`）：
- 包级 `registeredUnsubs []func()`；`wireCallbackHandlers` 每次 `On*` 后 `track(func(){ fw.Off*(event, fn) })`
- `ShutdownObservability` 遍历执行后置 nil，不再调用 `UnregisterNamespace`

**测试**：注册→shutdown 后 observability 的 10 个回调消失；另一 namespace 的同事件回调仍在。

### 17. M-25：归档

无调用者（Go/Python 双向确认）；`milvusclient v2.6.5` 的 `entity.Collection` 无 `NumEntities`；`count(*)` 改动无法在 CI 验证（需真实 Milvus）。仅在 `Count` 上补注释说明差异与可选优化路径。

---

## 三、影响面与风险

| 风险 | 说明 | 缓解 |
|---|---|---|
| S-11 修复后 ExternalMemoryRail 首次真正启用 | 配置了 `memory.engine: external/both` 且 provider 齐全的部署行为变化 | builder 失败已 return nil，现有"构建失败不注册"用例覆盖 |
| M-20 收紧 entity 召回（0.02→0.3）且换 Weighted 排序 | 记忆召回结果集变小、排序变化 | 按 Python 语义为准；跑记忆相关测试并按新阈值校正预期 |
| M-02 改 KernelHost 接口 | 仅 2 个实现者（`TeamAgent`、`fakeKernelHost`） | 逐个补齐；typed-nil 显式判空 |
| M-15 加 30s 超时 | 极慢磁盘首次初始化可能失败 | 失败后 ready 仍为 false，下次调用重试 |

---

## 四、验证与验收

1. `gofmt -l ./internal` 无输出；`go vet ./...` 通过
2. `go test ./internal/...` 全绿；改动包覆盖率维持 ≥85%
3. `go test -race` 覆盖 `agent_teams/agent/coordination`、`swarm/server/adapter`、`agent_teams/observability`
4. 推送后 CI 四 job（Build / Lint / Unit / Integration）全绿
5. 审查文档逐项标注 ✅/归档，全部处置后按项目惯例删除

---

## 五、待确认

审查文档收尾方式：全部处置完成后**删除** `docs/review/2026-09-27-48h-logic-review.md`（与历史轮次一致），还是保留并标注处置结果。

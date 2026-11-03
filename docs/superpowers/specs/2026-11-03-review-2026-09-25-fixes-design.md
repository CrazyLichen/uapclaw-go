# 2026-09-25 审查报告处置设计

> 来源：`docs/review/2026-09-25-48h-logic-review.md`（41 项）
> 核实基线：`main` @ `61fdc50e`（2026-11-03），Python 参考 `openjiuwen` / `jiuwenswarm-develop`
> 结论：23 项已修复或报告有误，18 项需处置

---

## 一、核实结论总览

### 已修复（16，不再改动）

- GraphMemory：S-01、S-02、S-03、M-01、M-03、T-01
- Extraction：S-07、S-08（`$defs`/`$ref` 四步处理已齐；`refDict` 恒空的结构性问题由本设计 D13 补完）
- ContextEvolver：S-16
- Swarm Rails：S-10、S-11、S-12
- ACE：S-14、S-15、M-18
- 计划项：P-01（9.82 P7 已标 ✅）

### 报告有误（7，归档不修）

| 编号 | 结论 | 依据 |
|---|---|---|
| M-02 | `SummaryTarget` 本就是 `int` | `memory/config/graph_config.go:73`；`%d` 等价 Python `str(int)` |
| M-05 | 顺序由构造保证 | `graph_memory/base.go:1506-1568` 显式按下标 zip |
| M-06 | Python 也是 `List[int]` | `trajectory_generator.py:77`、`:221` |
| M-09 | ReMe ID 与 Python 一致 | `io_schema.py:506` vs `task_memory_service.go:886` |
| M-14 | Python 的 `timezone_offset` 声明后从未使用 | `runtime_prompt_rail.py:36` 无引用 |
| S-09 | Python `ToolCall.arguments` 也是 `str` | `tool_call.py:22`、`stream_event_rail.py:479` |
| S-13 | Python 的 `_build_memory_disabled_prompt` 是死代码 | `avatar_rail.py:246` 定义后无调用 |

设计差异、予以保留：T-02（边界检查更安全）、T-03（`DedupeRelationTask` 存已解析响应）。

---

## 二、待实施决策（18 项）

### D1. ACE 手工记忆节点改用 `ACEMemory.ToVectorNode`（P-04 + M-08）

**现状**：`createACEMemory`（`context_evolver/service/task_memory_service.go:905-927`）手工拼 `VectorNode`，node ID 为 `{section}_{16hex}`，metadata 缺 `type`，缺 `created_at/updated_at`。

**失效链**：`AddMemory` → `createACEMemory` → 写库；检索 `retrieve/task/ace/run.go:69` 按 `metadataFilter{workspace_id, type:"ace_memory"}` 过滤 → 缺 `type` 的节点被跳过；`VectorNodeToMemory` 亦按 `type` 分派，无法反查。

**决策**：按 Python `ACEMemory.to_vector_node()` 复用 `schema.ACEMemory.ToVectorNode()`（`schema/memory.go:260-283`）：

- `ID = fmt.Sprintf("%s-%s", section, md5hex(content)[:8])`（8 个 hex，连字符）
- `CreatedAt/UpdatedAt = time.Now().UTC()`
- `AddMemoryResult.MemoryID` 返回 `node.ID`（Python 返回 `vector_node.id`）
- metadata 不额外保留 `algorithm`/`memory_type`（Python 无此二键）

**验收**：写后能被 `metadataFilter{type:"ace_memory"}` 检索到；`VectorNodeToMemory` 可还原；node ID 形如 `ace_{uid}_{section}-{8hex}`。

---

### D2. ReMe 手工记忆节点改用 `ReMeMemory.ToVectorNode`（P-03 + M-09）

**现状**：`createReMeMemory`（`task_memory_service.go:879-903`）手工拼节点，`VectorNode.Content = req.Content`，metadata 无 `type`、无 `created_at/updated_at`、无嵌套 `metadata`。

**失效链**：检索 `retrieve/task/reme/run.go:121-129` 过滤 `type:"reme_memory"` → 查不到；且 embedding 用的文本与 Python 相反（Python 用 `when_to_use` 做 embedding，正文在 `metadata["content"]`）。

**决策**：复用 `schema.ReMeMemory.ToVectorNode()`（`schema/memory.go:330-357`）：

- `WhenToUse` 作为 `VectorNode.Content`（embedding 文本）
- `Score = 1.0`；`Metadata{StepType:"manual", Confidence:1.0, Freq:1, Utility:1}`，`Tags`/`ToolsUsed` 用空切片
- `CreatedAt/UpdatedAt = time.Now().UTC()`
- `AddMemoryResult.MemoryID` 返回 `node.ID`
- metadata 不额外保留 `algorithm`/`memory_type`

**验收**：`type:"reme_memory"` 可检索；`node.Content == when_to_use` 且 `metadata["content"] == req.Content`；ReMe/RefCon/DivCon 三分支共用且行为一致。

---

### D3. 构造函数：service 为 nil 即创建（S-05）

**现状**：`context_evolving_react_agent.go:95` `if memoryService == nil && persistType != nil`。

**决策**：去掉 `&& persistType != nil`，与 Python `memory_service is None → TaskMemoryService(...)` 一致；`persistType == nil` 时不传 `WithPersistType`，得到纯内存 service。

**附带影响**：无 `API_KEY` 时构造返回 error（`NewOpenAILLMWrapper` 在 `service/llm_wrapper.go:62` 抛错，Python `task_memory_service.py:122` 同样抛错）。需新增该错误路径用例。

**验收**：双 nil 调用得到非 nil `GetMemoryService()`；无 API_KEY 时报错信息可读。

---

### D4. 构造签名对齐 Python + `AutoConfigure` 重写（S-06 + T-05）

**现状**：`NewContextEvolvingReActAgent(ctx, card, config, ...)` 把 config 提为必填入参；`AutoConfigure`（:234-266）重建 `TaskMemoryService`（丢 `RetrievalAlgo/SummaryAlgo/PersistType` 与已加载记忆），且完全不配置 Agent。

**关键事实**：Python 构造函数**没有 config 参数**（`context_evolving_react_agent.py:49-62`），`super().__init__(card)` 用默认配置，`_auto_configure()` 在构造末尾执行（`:116`），因此其「整体替换 config」无副作用。Go 的 `ReActAgentConfig` 字段与 Python 一一对应（`react_agent.py:147-198`），并不存在「Go 专有字段」；副作用来自「config 由调用方传入」。

**决策**：

1. 去掉 `config *config.ReActAgentConfig` 入参；内部 `saconfig.NewReActAgentConfig()` 构造默认配置（等价 `super().__init__(card)`）。
2. 构造函数末尾调用 `AutoConfigure`（解决 T-05）。
3. `AutoConfigure` 按 Python 重写：
   - `API_KEY` 为空 → 直接返回 nil（静默跳过）
   - 新建 `saconfig.NewReActAgentConfig(WithModelProvider(...), WithModelClient(provider, key, base, model), WithMaxIterations(5), WithPromptTemplate([{role:"system", content:<Python 默认系统提示词>}]))`
   - 模型名默认 `gpt-4`（Python `_auto_configure` 的默认值）
   - prompt 通过 `a.ReActAgent.AddPromptBuilderSection(identitySection, <默认系统提示词>, identitySectionPriority)` 落到 builder（Go 的 `config.PromptTemplate` 字段全项目无消费方）
   - `a.ReActAgent.Configure(ctx, cfg)` 整体替换（Python `base.py:333-348` 同样是整体替换）
   - **不再触碰 `memoryService`**
4. 调用方自定义（workspace/processors/rails）与 Python 一样在构造后通过 `Configure`/`RegisterRail` 完成。

**风险**：整体替换会清空构造期传入的配置；当前零生产调用方，且 Python 语义如此。

**验收**：`AutoConfigure` 前后 `memoryService` 指针不变、`SummaryAlgo`/`persistType` 不变；Agent config 的 model 四要素与 max_iterations 被设置；无 API_KEY 时静默返回。

---

### D5. `Execute` 轨迹改为从 ContextEngine 提取（M-11）

**现状**：`Execute`（:187-224）把 `sessionID` 塞进 `inputs["session_id"]`，而 `ReActAgent` 只读 `inputs["conversation_id"]`（`react_invoke.go:297`）→ 每个 trial 都新建 `default_session`；轨迹由 `formatTrajectoryFromResult` 拼接，USER 行是**字面量** `"USER: query"`，无 THOUGHT/ACTION/OBSERVATION。

**决策**：

1. `Execute` 内显式建 session：`session.NewSession(session.WithSessionID(sessionID))` + `sess.PreRun(ctx, inputs)`；`defer` 中 `CloseStream()` + `Commit(ctx)`（外部 session 时 `invokeImpl` 不做清理）。
2. 通过 `interfaces.WithSession(sess)` 传给 `invokeWithMemory`。
3. 结束后 `a.ReActAgent.ContextEngine().GetContext("default_context_id", sessionID)` → `GetMessages(0, true)` → 转 `service.Message` → `service.FormatTrajectory`。
4. `service.Message` 扩展为支持**多 tool call**（新增 `ToolCalls []ToolCall{Name, Args}`），`FormatTrajectory` 每条 assistant 消息输出 THOUGHT + 多条 ACTION。
5. context 为空/取消息失败时 fallback：`USER: {query}\nASSISTANT: {answer}`。

**验收**：轨迹包含 `USER/THOUGHT/ACTION/OBSERVATION` 行；USER 行是真实 query；不同 trial 的 sessionID 不同；`FormatTrajectory` 两个调用方（evolution rail、Execute）测试均通过。

---

### D6. `AddMemoryRequest.Section` 空串补 `"general"`（M-19，选 B）

**决策**：`AddMemory` 入口 `if req.Section == "" { req.Section = "general" }`；随后删除 ACE 分支中已失效的 `|| req.Section == ""` 校验与 `createACEMemory` 内不可达的 `if section == ""` 兜底。

**已知偏差**：Python 显式传 `""` 会报错，Go 会默认 `general`（宽松化，接受）。

**验收**：不传 Section 的 ACE 请求成功，`section` 为 `general`；`Content` 为空仍报错。

---

### D7. `matts_k` 单点解析（M-07）

**决策**：`Invoke`（:150-156）不再兜底，把 `inputs["matts_k"]`（缺省 0）传给 `RunTrials`，并兼容 `int`/`float64`/`json.Number`；`RunTrials`（`trajectory_generator.go:211-214`）改为 `if mattsK <= 0 { mattsK = ceconfig.GetInt("MATTS_DEFAULT_K", 3) }`。

**验收**：设 `MATTS_DEFAULT_K=5` 且未传 `matts_k` 时试验数为 5；传 `matts_k=2` 时以 2 为准；JSON 传入 `2.0` 亦生效。

---

### D8. `LoadMemories` 日志与容错（M-10）

**决策**：

- 空数据提前返回（对齐 Python `if not data: return`）
- 循环后 `logger.Info(logComponent).Int("count", loaded).Str("algo", algoName).Msg("Loaded memories into vector store")`
- `Load` 失败只记 `logger.Error(...).Str("user_id", userID)` 并返回 nil，**不再上抛**（Python 的 `load_memories` 内部吞掉全部异常）
- 构造链 `context_evolving_react_agent.go:121-125` 相应去掉「加载记忆失败」返回错误分支

**验收**：损坏 JSON 时构造成功且记忆为空；正常加载输出 count/algo 日志。

---

### D9. `auto_summarize` 保持 1:1，仅补注释（S-04）

**事实**：Go 与 Python 的 `ContextEvolvingReActAgent.auto_summarize` 字段均**只写不读**（Python `:93-94` 仅赋值 + 日志；`_invoke_with_memory` docstring 明写 `No summarization is performed here.`）。真正的自动总结在 `ContextEvolutionRail`：Go `harness/rails/evolution/context_evolution_rail.go:409`，Python `harness/rails/evolution/context_evolution_rail.py:221`。

**决策**：不实现 agent 级逻辑；字段注释改为「Python 该字段亦只写不读；自动总结由 ContextEvolutionRail 承担」，在 `invokeWithMemory` 处留一行说明。

**验收**：零行为变化，注释到位。

---

### D10. 删除 `llm_temperature` 自造通路（P-02）

**事实**：Python `extensions/context_evolver` 内 `llm_temperature` **零命中**；Python 的多样性靠直接改 `llm.temperature`（`retrieve/task/reasoning_bank/matts.py:52-90`）。Go 自造 `AgentFlowOption → inputs["llm_temperature"] → _ = llmTemp` 的空转链路。

**决策**：删除 `AgentFlowConfig.LLMTemperature`、`WithLLMTemperature`、`Execute` 的注入（:206-209）、`invokeWithMemory` 的 TODO（:297-302）、`retrieve/task/rb/matts.go:175` 的传参；在 RB MaTTS 处注释说明「Python 通过直改 `llm.temperature` 实现多样性，Go 的 agent-flow 路径暂无等价通道」。**保留** `WithTemperature`（GenerateOption，`matts.go:310` 直接调用生效）。

**验收**：全仓库无 `llm_temperature` 引用；编译通过；RB MaTTS 行为不变（此前温度本就不生效）。

---

### D11. `RuntimePromptRail` 加锁 + 入口快照（M-12）

**现状**：rail 实例挂在 adapter 上跨会话共享（`deep_adapter_rails.go:65`），8 个字段由 `Set*`（`code_adapter.go:702-709`、`deep_adapter_config.go:126-129`）写入，`BeforeModelCall` 与 7 个 `inject*` 直接读，无锁。

**决策**：加 `sync.RWMutex`；`Set*` 持写锁；`BeforeModelCall` 持读锁一次性快照 8 个字段（含 `builder`），把快照传给改为接收快照参数的 `inject*` 方法。

**验收**：`go test -race -count=5` 覆盖「并发 Set* + BeforeModelCall」用例无竞态；注入内容不变。

---

### D12. `FormatExistingRelations` 三处对齐（M-15）

**决策**：

1. `valid_since`/`valid_until` 缺键按 `0` 处理（Python `rel.get(key, 0)`）
2. 输出带单引号：`\nvalid_since='%s'`（复刻 Python debug f-string `f"{x=}"` 的 repr 引号）
3. 时间格式固定 `2006-01-02T15:04:05+00:00`（Python `isoformat()` 输出 `+00:00`，Go 原用 `Z07:00` 渲染成 `Z`）

**验收**：`valid_since=0` 输出 `valid_since='1970-01-01T00:00:00+00:00'`；与 Python 逐字符一致。

---

### D13. `ReadableSchema` 反射重写（M-16，含 S-08 残留）

**现状**：`extraction/base.go:67-105` + `formatReadableSchema:242-274` 走 JSON Schema，四处偏差：类型写成 `list[dict]` 并缩进展开、空描述仍输出 `  # `、保留尾换行、**遍历 map 导致字段顺序随机**；`ToJSONSchemaMap` 内联嵌套结构体 → `refDict` 恒空 → `FormatSchemaInfo` 的 Reference 段永不出现。

**决策**：改为按 Go 结构体反射生成（对应 Python `model_fields` 迭代）：

- 按字段声明顺序遍历，跳过未导出与 `json:"-"`
- 名称取 json tag，类型映射 `string→str`、`int/int64→int`、`float64→float`、`bool→bool`、`[]T→list[T]`、嵌套 struct→类型名（Go 结构体名与 Python 类名一致）
- 描述取 `jsonschema:"description={{[key]}}"` 占位符经语言表替换，**仅在非空时**输出 `  # desc`
- `\n` 连接，不留尾换行
- `refDict` 由遇到的嵌套结构体填充（键为类型名，值为其 properties），使 Reference 段真正生效

**验收**：`extracted_entities: list[EntityDeclaration]` 单行；字段顺序稳定且与声明一致；无描述字段无 `  # `；无尾换行；含嵌套模型时 `refDict` 非空且 `FormatSchemaInfo` 输出 Reference 段。

---

### D14. `relationDedupe` 并发化（M-04）

**决策**：按 Python `create_task` + `asyncio.wait` 语义，先收集所有提示词，再用 `errgroup` 并发调用 `InvokeLLM`，结果按**索引**写回（保持 `DedupeRelationTask` 顺序）；单条失败仍记 Warn 并跳过（对齐 Python `postprocess_graph_objects.py:170-178` 的 per-task 捕获）。

**验收**：N 条关系只消耗 1 个串行 RTT 量级（可用 mock 计数验证并发）；结果顺序与输入一致；单条失败不影响其余。

---

### D15. `extractToolInterrupt` 支持 `Unwrap() []error`（M-13）

**决策**：在 `stream_event_helpers.go:156-171` 增加 `interface{ Unwrap() []error }` 分支，逐个递归；保持先 `*ToolInterruptException` 类型断言的优先级。

**验收**：`errors.Join(其他错误, ToolInterruptException)` 能被识别为中断。

---

### D16. `removeCount` 显式 `max(0, ...)`（M-17）

**决策**：`ace/update.go:614` 改为
```go
removeCount := addCount + currentCount - o.maxBullets
if removeCount < 0 { removeCount = 0 }
```

**验收**：行为不变，语义与 Python `update.py:439` 一致。

---

### D17. `SafeJSONLoads` 第二段正则改贪婪（T-04）

**决策**：`ace/utils.go:24` 的 `reAnyJSONObject` 改为 `(?s)\{.*\}`（贪婪），与 Python `utils.py:35` 一致；第一段 markdown 提取保持非贪婪 `\{.*?\}`（Python 同）。

**已知代价**：多 JSON 对象且无代码块时，由「解析第一个对象」变为「解析失败回退」，与 Python 一致。

**验收**：单对象文本仍可解析；多对象文本按 Python 语义失败。

---

## 三、测试与验证要求

1. 每个决策至少 1 条回归用例；改动文件所属包覆盖率维持 ≥85%。
2. `graph_memory`、`rails`、`context_evolver` 相关包新增/修改用例需在 `-race` 下通过（D11、D14 必测）。
3. `gofmt -l`、`go vet`、`staticcheck` 零告警。
4. 新增文件需同步 `doc.go` 的文件目录。
5. 提交后推送并跟踪 CI：Build / Lint / Unit Test / Integration Test 四绿。

## 四、范围外

- T-02、T-03 设计差异保留
- M-02/M-05/M-06/M-09/M-14/S-09/S-13 归档为「报告有误」
- 9.82 P7 工具层接入不在本次范围（本次仅保证 `AddMemory` 语义可与 Python 互通）

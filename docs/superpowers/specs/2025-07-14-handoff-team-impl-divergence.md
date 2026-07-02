# HandoffTeam 实现差异记录

> 对比设计规格（`2025-07-14-handoff-team-design.md`）、实际代码、Python 参考实现三者，记录所有不一致之处。
> 生成时间：2026-10-27

---

## 一、严重差异（1 项）

### P1: ContainerAgent 未嵌入 CommunicableAgent，publishHandoff 为空操作

- **文件**：`internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- **位置**：`publishHandoff` 方法（第 655-681 行）
- **设计规格**：`ContainerAgent struct { teamruntime.CommunicableAgent; ... }`，嵌入 CommunicableAgent 获得 `Send/Publish/Subscribe` 方法
- **Python**：`class ContainerAgent(CommunicableAgent, BaseAgent)` 多重继承，交接时调用 `await self.publish(message=HandoffRequest(...), topic_id=f"container_{signal.target}", session_id=session_id)`
- **实际实现**：ContainerAgent **未嵌入** CommunicableAgent，`publishHandoff` 方法中 `_ = nextReq; _ = sessionID` 是占位空操作
- **影响**：交接流程的核心——将 HandoffRequest 发布到下一个 ContainerAgent——**未实际实现**，多 Agent 交接（A→B→C）无法真正工作
- **修复方向**：嵌入 CommunicableAgent 并在 `ensureInternalAgents` 时通过 `BindRuntime` 注入运行时引用；或持有 `*TeamRuntime` 引用直接调用 `runtime.Publish()`

---

## 二、中等差异（6 项）

### M1: saveAgentContext 为空操作

- **文件**：`container_agent.go` 第 516-527 行
- **设计规格**：`saveAgentContext` 持久化 Agent 上下文到 session state
- **Python**：`context_engine = getattr(target_agent, "context_engine", None)`，有 ContextEngine 时调用 `save_contexts(agent_session)`
- **实际实现**：`_ = ctx; _ = targetAgent; _ = agentSession` 空操作，注释"后续通过 ContextEngine 接口访问时再补充"。额外提供了 `saveAgentContextWithCE` 方法但未调用
- **影响**：ContextEngine 上下文未持久化，中断恢复后可能丢失上下文

### M2: ContainerAgent 缺少 resourceMgr 字段

- **设计规格**：`resourceMgr *resources_manager.ResourceMgr` 作为 ContainerAgent 的字段持有
- **Python**：通过 `Runner.resource_mgr` 全局获取
- **实际实现**：无此字段，`injectToolsOnce` 中通过全局 `runner.GetResourceMgr()` 获取
- **分析**：Go 实际实现与 Python 的获取方式一致（全局），但与设计规格的字段定义不一致

### M3: StripHandoffMessages 可见性

- **设计规格**：`stripHandoffMessages`（非导出）
- **Python**：`_strip_handoff_messages`（私有，`@staticmethod`）
- **实际实现**：`StripHandoffMessages`（导出，首字母大写）
- **分析**：导出可能是为了外部测试访问，但违反了封装性

### M4: injectToolsOnce description 为空

- **Python**：`card = self._runtime.get_agent_card(target_id) if self._runtime else None`，`description = card.description if card else ""`
- **实际实现**：`description := ""`，注释"暂不依赖 runtime，仅用空 description"
- **影响**：LLM 看到的 `transfer_to_{agent_id}` 工具描述缺少目标 Agent 的说明，可能影响交接决策质量

### M5: 消息去重键不完整

- **Python**：`_msg_key(m) = (role, str(content), str(tool_calls), tool_call_id)`（4 字段元组）
- **Go**：`msgKey(msg) = role + ":" + content`（2 字段拼接字符串）
- **影响**：可能导致本应去重的消息（相同 role+content 但不同 tool_calls）不被去重，或不应去重的消息（不同 tool_call_id 但相同 role+content）被错误去重

### M6: FlushTeamSession Commit 失败返回错误

- **设计规格**："失败仅警告"
- **Python**：`close_stream()` + `commit()` 在同一个 `try/except` 中，任何失败仅 warning，**不传播错误**
- **实际实现**：`CloseStream()` 失败仅警告（OK），但 `Commit()` 失败后 **返回 err**
- **影响**：与设计规格和 Python 行为不一致，调用方可能因 Commit 失败而中断

---

## 三、轻微差异（7 项）

### L1: ExtractInterruptSignal 路径2 Result 不含 message 键

- **Python**：路径2 构造 `result={"result_type": "interrupt", "message": message}`
- **Go**：`Result: map[string]any{"result_type": "interrupt"}`，不含 `message` 键

### L2: writeResultToStream 不支持 list 结果

- **Python**：区分 `isinstance(result, dict)` 和 `isinstance(result, list)` 两种情况，list 时逐个 dict 调用 `write_stream`
- **Go**：仅 `teamSession.WriteStream(ctx, result)`，不处理 list 情况

### L3: HandoffTeam.Invoke 未复用 StandaloneInvokeContext

- **设计规格**：使用 `standaloneInvokeContext`/`standaloneStreamContext` 管理会话
- **Python**：`async with standalone_invoke_context(...)` 管理会话
- **实际实现**：`Invoke` 中自行管理会话生命周期（创建/PreRun/Bind/PostRun/Unbind），未使用 `teams/utils.go` 中的 `StandaloneInvokeContext`
- **影响**：功能等效但代码重复

### L4: defaultContextID 定义位置

- **设计规格**：`defaultContextID` 与 `HandoffRequestKey`、`contextHistoryKey` 在 `container_agent.go` 的同一常量组
- **实际实现**：`defaultContextID` 定义在 `handoff_signal.go`，`HandoffRequestKey` 和 `contextHistoryKey` 定义在 `container_agent.go`
- **影响**：同一包内可见，不影响功能，但与设计规格的组织不一致

### L5: History 格式差异

- **Python**：`history: List[dict]`，原始 dict 列表，元素形如 `{"agent": ..., "output": ...}`
- **Go**：`History []HandoffHistoryEntry`，结构化的 `HandoffHistoryEntry{AgentID, Output}`
- **影响**：Go 提供了更强的类型安全。序列化时键名从 `"agent"` 变为 JSON tag（如有），但 `buildAgentInput` 中显式转为 `{"agent": ..., "output": ...}` 格式，功能等效

### L6: findHandoffFromSession 不支持 ast.literal_eval

- **Python**：先尝试 `json.loads(content)`，失败再尝试 `ast.literal_eval(content)`（处理 Python 字面量格式如 `{'key': 'value'}`）
- **Go**：仅 `json.Unmarshal`，无 Python literal_eval 等效逻辑
- **影响**：Go 不支持 Python 字面量格式的内容解析，语言差异，可接受

### L7: HandoffTool.Invoke 不支持 inputs 为 JSON 字符串

- **Python**：支持 `isinstance(inputs, str)` 时尝试 `json.loads(inputs)`，解析失败则 `inputs = {"reason": inputs}`
- **Go**：Tool 接口约束 inputs 为 `map[string]any`，由上层调用方负责反序列化
- **影响**：Go 类型系统约束，不影响功能

---

## 四、设计规格要求但实际未实现

| 编号 | 要求 | 实际状态 | 对应差异 |
|------|------|---------|---------|
| D1 | ContainerAgent 嵌入 CommunicableAgent | 未嵌入，publishHandoff 为空操作 | P1 |
| D2 | ContainerAgent 持有 `resourceMgr` 字段 | 用全局 `runner.GetResourceMgr()` | M2 |
| D3 | saveAgentContext 实现 | 空操作 | M1 |
| D4 | HandoffTeam.Invoke 使用 standaloneInvokeContext | 自行管理会话生命周期 | L3 |
| D5 | injectToolsOnce 从 runtime 获取 AgentCard description | 硬编码空字符串 | M4 |

---

## 五、设计规格未提及但实际额外添加

| 编号 | 额外添加 | 文件 | 评估 |
|------|---------|------|------|
| E1 | `GetRuntime() *TeamRuntime` 方法 | `handoff_team.go` | 合理，允许外部访问 runtime |
| E2 | `wrapTeamAgentProvider` 方法 | `handoff_team.go` | 必要，Go 类型转换 |
| E3 | `filterInterruptHistory` 函数 | `handoff_team.go` | 必要，与 Python isResume 过滤逻辑对应 |
| E4 | `writeResultToStream` 方法 | `container_agent.go` | 合理，从 invokeTargetWithStream 抽取 |
| E5 | `saveAgentContextWithCE` 方法 | `container_agent.go` | 预留，待 ContextEngine 接入 |
| E6 | `msgKey` 去重函数 | `container_agent.go` | 必要，但与 Python `_msg_key` 不完全一致（M5） |
| E7 | `defaultMaxHandoffs` 常量 | `handoff_orchestrator.go` | 合理，代码清晰度 |
| E8 | `logComponent` 常量 | 各文件 | 必要，日志系统要求 |
| E9 | `AbilityManager()/CallbackManager()/RegisterCallback()/RegisterRail()/UnregisterRail()` | `container_agent.go` | 必要，满足 BaseAgent 接口 |
| E10 | `handoffEndpointPrefix`/`containerTopicPrefix` 常量 | `handoff_team.go` | 合理，常量提取 |

---

## 六、修复优先级建议

| 优先级 | 差异编号 | 修复内容 | 工作量 |
|--------|---------|---------|--------|
| P0 | P1 | 嵌入 CommunicableAgent 或持有 runtime 引用，实现 publishHandoff | 中 |
| P1 | M1 | 实现 saveAgentContext（接入 ContextEngine 接口） | 小 |
| P1 | M6 | FlushTeamSession Commit 失败改为仅警告 | 极小 |
| P2 | M4 | injectToolsOnce 从 runtime 获取 AgentCard description | 小 |
| P2 | M5 | 消息去重键补齐 tool_calls 和 tool_call_id | 小 |
| P2 | M3 | StripHandoffMessages 改为非导出 | 极小 |
| P3 | M2 | ContainerAgent 添加 resourceMgr 字段（可选，与 Python 一致即可） | 小 |
| P3 | L3 | HandoffTeam.Invoke 改用 StandaloneInvokeContext | 小 |
| P3 | L2 | writeResultToStream 支持 list 结果 | 小 |

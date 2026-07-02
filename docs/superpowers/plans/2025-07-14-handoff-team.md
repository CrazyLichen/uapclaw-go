# HandoffTeam 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 HandoffTeam（8.34）——事件驱动的单活跃 Agent 交接多 Agent 团队

**Architecture:** HandoffTeam 实现 BaseTeam 接口，为每个业务 Agent 创建 ContainerAgent 包装器并注册到 TeamRuntime。交接通过 Pub/Sub 消息总线驱动，HandoffOrchestrator 协调交接状态和路由审批。Agent 的 LLM 通过调用注入的 transfer_to_{agent_id} 工具触发交接。

**Tech Stack:** Go 1.22+, 依赖已有的 TeamRuntime/MessageBus/CommunicableAgent/BaseAgent/Tool/Session 基础设施

---

## 文件结构

### 新增文件

```
internal/agentcore/multi_agent/teams/
├── doc.go
├── utils.go
├── utils_test.go
└── handoff/
    ├── doc.go
    ├── handoff_config.go
    ├── handoff_config_test.go
    ├── handoff_orchestrator.go
    ├── handoff_orchestrator_test.go
    ├── handoff_tool.go
    ├── handoff_tool_test.go
    ├── handoff_signal.go
    ├── handoff_signal_test.go
    ├── handoff_request.go
    ├── handoff_request_test.go
    ├── interrupt.go
    ├── interrupt_test.go
    ├── container_agent.go
    ├── container_agent_test.go
    ├── handoff_team.go
    └── handoff_team_test.go
```

### 修改文件（BaseAgent.Invoke 返回类型统一）

```
internal/agentcore/single_agent/interfaces/interface.go
internal/agentcore/single_agent/agents/react_invoke.go
internal/agentcore/runner/runner.go
internal/agentcore/runner/child_runner.go
internal/agentcore/runner/spawn/child.go
internal/agentcore/multi_agent/team_runtime/message_router.go
+ 对应 5 个测试文件
```

---

## Task 1: BaseAgent.Invoke 返回类型统一（前置依赖）

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`
- Modify: `internal/agentcore/runner/runner.go`
- Modify: `internal/agentcore/runner/child_runner.go`
- Modify: `internal/agentcore/runner/spawn/child.go`
- Modify: `internal/agentcore/multi_agent/team_runtime/message_router.go`
- Modify: 5 个对应测试文件

- [ ] **Step 1: 修改 BaseAgent 接口定义**

在 `single_agent/interfaces/interface.go` 中将 `Invoke` 返回类型从 `(any, error)` 改为 `(map[string]any, error)`。

- [ ] **Step 2: 修改 ReActAgent.Invoke 实现**

在 `single_agent/agents/react_invoke.go` 中：
- `Invoke` 方法签名改为 `(map[string]any, error)`
- `invokeImpl` 返回类型改为 `(map[string]any, error)`
- `reactLoop` 返回类型改为 `(map[string]any, error)`
- 删除 `innerStream` 中的 `result.(map[string]any)` 类型断言和 `[]stream.Schema` 死代码分支

- [ ] **Step 3: 修改 runner 层**

在 `runner/runner.go` 中 `RunAgent` 返回类型改为 `(map[string]any, error)`。
在 `runner/child_runner.go` 中 `ChildRunnerImpl.RunAgent` 返回类型改为 `(map[string]any, error)`。
在 `runner/spawn/child.go` 中 `ChildRunner.RunAgent` 接口返回类型改为 `(map[string]any, error)`。

- [ ] **Step 4: 修改 MessageRouter**

在 `multi_agent/team_runtime/message_router.go` 中 `RouteP2PMessage` 返回类型改为 `(map[string]any, error)`。

- [ ] **Step 5: 修改测试 stub**

更新 5 个测试文件中的 mockAgent/stubBaseAgent/fakeAgent 的 Invoke 签名。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -count=1 -timeout=120s`
Expected: 全部通过

- [ ] **Step 8: 提交**

```bash
git add -A
git commit -m "refactor: 统一 BaseAgent.Invoke 返回类型为 (map[string]any, error)"
```

---

## Task 2: HandoffConfig（配置层）

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_config.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_config_test.go`

- [ ] **Step 1: 编写 handoff_config_test.go 测试**

测试点：
- `NewHandoffConfig()` 默认值（MaxHandoffs=10, StartAgent=nil, Routes=nil）
- `HandoffRoute` 结构体字段正确
- `NewHandoffTeamConfig()` 嵌入 TeamConfig + Handoff 字段
- `HandoffTeamConfig` 可通过 `WithMaxAgents`/`ConfigureTimeout` 链式配置

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -run TestHandoff -v`
Expected: FAIL（文件不存在）

- [ ] **Step 3: 编写 handoff_config.go 实现**

对照 Python `handoff_config.py`：
- `HandoffRoute` struct（Source, Target）
- `HandoffConfig` struct（StartAgent, MaxHandoffs, Routes, TerminationCondition）+ `NewHandoffConfig()` 构造函数
- `HandoffTeamConfig` struct 嵌入 `maschema.TeamConfig` + Handoff 字段 + `NewHandoffTeamConfig()`

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 HandoffConfig/HandoffRoute/HandoffTeamConfig"
```

---

## Task 3: HandoffRequest + TeamInterruptSignal

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_request.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_request_test.go`
- Create: `internal/agentcore/multi_agent/teams/handoff/interrupt.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/interrupt_test.go`

- [ ] **Step 1: 编写 handoff_request_test.go 测试**

测试点：
- `HandoffHistoryEntry` 结构体字段
- `HandoffRequest` 结构体字段（InputMessage, History, Session）
- `SessionID()` 方法：有 session 时返回 sessionID，无 session 时返回空字符串

- [ ] **Step 2: 编写 handoff_request.go 实现**

对照 Python `handoff_request.py`：
- `HandoffHistoryEntry` struct
- `HandoffRequest` struct + `SessionID()` 方法

- [ ] **Step 3: 编写 interrupt_test.go 测试**

测试点：
- `ExtractInterruptSignal` 路径1：result 包含 `result_type="interrupt"` → 返回 TeamInterruptSignal
- `ExtractInterruptSignal` 路径2：err 为 AgentInterrupt 类型 → 返回 TeamInterruptSignal
- `ExtractInterruptSignal` 无中断 → 返回 nil
- `FlushTeamSession` session 为 nil → 无操作
- `FlushTeamSession` 正常流程 → 调用 CloseStream + Commit

- [ ] **Step 4: 编写 interrupt.go 实现**

对照 Python `interrupt.py`：
- `TeamInterruptSignal` struct
- `ExtractInterruptSignal(result, err)` 两条路径
- `FlushTeamSession(ctx, sess)` 尝试 CloseStream + Commit，失败仅警告

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 HandoffRequest/HandoffHistoryEntry/TeamInterruptSignal"
```

---

## Task 4: HandoffOrchestrator（协调器）

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_orchestrator.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_orchestrator_test.go`

- [ ] **Step 1: 编写 handoff_orchestrator_test.go 测试**

测试点：
- `NewHandoffOrchestrator` 默认值（handoffCount=0, currentAgentID=start）
- `BuildRouteGraph` 全互联：3 个 Agent → 每个 Agent 可交接给其他 2 个
- `BuildRouteGraph` 显式路由：只允许指定路由
- `RequestHandoff` 正常批准 → handoffCount++, currentAgentID 更新
- `RequestHandoff` 超过 maxHandoffs → 拒绝
- `RequestHandoff` 路由不允许 → 拒绝
- `RequestHandoff` terminationCondition 返回 true → 拒绝
- `Complete` 发送结果到 doneCh
- `Complete` 多次调用 → doneOnce 保证只发送一次
- `Error` 发送错误 dict 到 doneCh
- `SaveToSession` + `RestoreFromSession` 恢复状态
- `DoneCh` 返回只读通道
- `Close` 关闭 channel

- [ ] **Step 2: 编写 handoff_orchestrator.go 实现**

对照 Python `handoff_orchestrator.py`：
- 常量 `CoordinatorStateKey`/`HandoffHistoryKey`
- `HandoffOrchestrator` struct（maxHandoffs, terminationCondition, handoffCount, currentAgentID, routeGraph, doneCh, doneOnce）
- `NewHandoffOrchestrator()` 从 Config 提取字段
- `BuildRouteGraph()` 静态方法
- `RequestHandoff()` 审批逻辑
- `Complete()`/`Error()` doneOnce 保护
- `Close()` 关闭 channel
- `SaveToSession()`/`RestoreFromSession()` 状态持久化
- getter: `HandoffCount()`/`CurrentAgentID()`/`DoneCh()`

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 HandoffOrchestrator 协调器"
```

---

## Task 5: HandoffTool + HandoffSignal

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_tool.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_tool_test.go`
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_signal.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_signal_test.go`

- [ ] **Step 1: 编写 handoff_tool_test.go 测试**

测试点：
- `NewHandoffTool("agent_b", "代码审查")` → 工具名 `transfer_to_agent_b`，描述包含 targetDescription
- `Invoke` 正常调用 → 返回包含 `__handoff_to__`/`__handoff_message__`/`__handoff_reason__` 的 dict
- `Invoke` inputs 缺少 reason → 返回空 reason
- `Card()` 返回正确 ToolCard

- [ ] **Step 2: 编写 handoff_tool.go 实现**

对照 Python `handoff_tool.py`：
- 常量 `HandoffTargetKey`/`HandoffMessageKey`/`HandoffReasonKey`
- `HandoffTool` struct 实现 `Tool` 接口
- `NewHandoffTool()` 构造函数，工具名 `transfer_to_{targetID}`
- `Card()`/`Invoke()`/`Stream()` 实现

- [ ] **Step 3: 编写 handoff_signal_test.go 测试**

测试点：
- `ExtractHandoffSignal` 第一层：result 顶层有 `__handoff_to__` → 返回 HandoffSignal
- `ExtractHandoffSignal` 第一层：result["output"] 中有 `__handoff_to__` → 返回 HandoffSignal
- `ExtractHandoffSignal` 第一层：无信号 → 返回 nil（不传 agentSession 时）
- `ExtractHandoffSignal` 第二层：agentSession 消息历史中有 tool message 包含 `__handoff_to__` → 返回 HandoffSignal
- `ExtractHandoffSignal` 无信号 → 返回 nil
- `findHandoffPayload` 对 result/content 子路径的查找

- [ ] **Step 4: 编写 handoff_signal.go 实现**

对照 Python `handoff_signal.py`：
- `HandoffSignal` struct（Target, Message, Reason）
- `ExtractHandoffSignal(result, agentSession)` 两层提取
- `findHandoffPayload(result)` 第一层查找
- `findHandoffFromSession(agentSession)` 第二层从 session 消息历史查找

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 HandoffTool/HandoffSignal 交接工具与信号提取"
```

---

## Task 6: ContainerAgent（核心包装器）

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`

- [ ] **Step 1: 编写 container_agent_test.go 测试**

测试点（用 mock 替代真实 Agent/Session）：
- `NewContainerAgent` 构造函数
- `Card()` 返回 targetCard
- `Configure` 空操作
- `buildAgentInput` 无 history → 原样返回 InputMessage
- `buildAgentInput` 有 history → 合并 handoff_history
- `stripHandoffMessages` 过滤 role=tool 消息
- `stripHandoffMessages` 过滤含 tool_calls 的 AssistantMessage
- `Invoke` 提取 HandoffRequest 失败 → 返回空 dict
- `Invoke` coordinator 为 nil → 返回错误
- `Invoke` 目标 Agent 返回无交接信号 → coordinator.Complete 被调用
- `Invoke` 目标 Agent 返回交接信号且审批通过 → Publish 到下一个 ContainerAgent
- `Invoke` 目标 Agent 返回交接信号且审批拒绝 → coordinator.Complete 被调用
- `Invoke` 目标 Agent 返回中断信号 → handleTeamInterrupt 被调用

- [ ] **Step 2: 编写 container_agent.go 实现**

对照 Python `container_agent.py`，所有方法一一对应：
- 常量 `HandoffRequestKey`/`contextHistoryKey`/`defaultContextID`
- `ContainerAgent` struct（嵌入 CommunicableAgent + 各字段）
- `NewContainerAgent()` 构造函数
- `Card()`/`Configure()`/`Invoke()`/`Stream()` — BaseAgent 接口实现
- `getTargetAgent()` — 懒初始化
- `injectToolsOnce()` — 双层注册 ResourceMgr + AbilityManager
- `buildAgentInput()` — 合并 handoff_history
- `invokeTargetWithStream()` — 调用 Agent + 流式转发 + 信号提取
- `saveAgentContext()` — 持久化上下文
- `saveContextToTeamSession()` — 保存上下文消息（去重）
- `injectContextHistory()` — 注入历史消息
- `stripHandoffMessages()` — 过滤交接消息
- `handleTeamInterrupt()` — 中断处理

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 ContainerAgent 核心包装器"
```

---

## Task 7: HandoffTeam（顶层入口）

**Files:**
- Create: `internal/agentcore/multi_agent/teams/handoff/handoff_team.go`
- Test: `internal/agentcore/multi_agent/teams/handoff/handoff_team_test.go`

- [ ] **Step 1: 编写 handoff_team_test.go 测试**

测试点：
- `NewHandoffTeam` 构造函数
- `AddAgent` 注册 Agent
- `AddAgent` 重复注册 → 跳过
- `getStartAgentID` 配置了 startAgent → 返回其 ID
- `getStartAgentID` 未配置 → 返回第一个 Agent ID
- `lookupCoordinator` 有 → 返回 coordinator
- `lookupCoordinator` 无 → 返回 nil
- `Card()`/`Config()`/`GetAgentCount()`/`ListAgents()`
- `ensureInternalAgents` 创建 ContainerAgent + 注册 + 订阅
- `runChain` 完整流程（2 Agent 交接 A→B→完成）
- `runChain` 超时 → 返回错误
- `Invoke` 委托到 runChain
- `Stream` 流式输出

- [ ] **Step 2: 编写 handoff_team.go 实现**

对照 Python `handoff_team.py`：
- `HandoffTeam` struct 实现 BaseTeam 接口
- `NewHandoffTeam()` 构造函数
- 全部 13 个 BaseTeam 方法
- `lookupCoordinator()`/`getStartAgentID()`
- `ensureInternalAgents()` — ContainerAgent 创建 + 端点注册 + 订阅
- `makeContainerProvider()` — provider 闭包
- `runChain()` — 协调器创建 → 发布 → 等待 → 清理
- `standaloneInvokeContext()`/`standaloneStreamContext()` — 独立调用上下文

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/handoff/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加 HandoffTeam 顶层团队实现"
```

---

## Task 8: 包文档 + teams 公共工具

**Files:**
- Create: `internal/agentcore/multi_agent/teams/doc.go`
- Create: `internal/agentcore/multi_agent/teams/utils.go`
- Test: `internal/agentcore/multi_agent/teams/utils_test.go`
- Create: `internal/agentcore/multi_agent/teams/handoff/doc.go`

- [ ] **Step 1: 编写 teams/doc.go**

按项目 doc.go 规范编写：包功能概述 + 文件目录 + 对应 Python 路径。

- [ ] **Step 2: 编写 teams/utils.go + utils_test.go**

对照 Python `teams/utils.py`：
- `standaloneInvokeContext()` — 独立调用上下文
- `standaloneStreamContext()` — 独立流式上下文
- 编写测试覆盖

- [ ] **Step 3: 编写 handoff/doc.go**

按项目 doc.go 规范编写，列出 7 个核心文件。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/teams/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(handoff): 添加包文档和 teams 公共工具"
```

---

## Task 9: TeamRuntime.RegisterAgent 修复

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go`

- [ ] **Step 1: 确认当前 RegisterAgent 的 no-op placeholder**

读取 `team_runtime.go`，找到 `_ = wrappedProvider` 行。

- [ ] **Step 2: 实现真实注册**

将 `_ = wrappedProvider` 替换为通过 ResourceMgr 注册 wrappedProvider 的逻辑，对齐 Python `TeamRuntime.register_agent()` 中的 `Runner.resource_mgr.add_agent(card, wrapped_provider)`。

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentcore/multi_agent/team_runtime/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "fix(team_runtime): 修复 RegisterAgent 将 wrappedProvider 真实注册到 ResourceMgr"
```

---

## Task 10: 更新 multi_agent/doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/multi_agent/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 multi_agent/doc.go 文件目录**

在 doc.go 的文件目录树中添加 `teams/` 子目录条目。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 8.34 状态从 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add -A
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md 标记 8.34 完成"
```

---

## Task 11: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行全量单元测试**

Run: `cd /home/opensource/uap-claw-go && go test ./... -count=1 -timeout=300s`
Expected: 全部通过

- [ ] **Step 3: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/multi_agent/teams/handoff/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 最终提交**

如有遗漏修正，提交最终版本。

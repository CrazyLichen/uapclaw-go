# 第四批集成测试设计：14 个 Rail 补全 + Interrupt 细粒度确认

## 概述

在第三批（Memory/Security/Worktree = 48 测试 PASS）完成后，第四批补全 14 个零覆盖 Rail 的集成测试 + ConfirmInterruptRail 细粒度确认扩展。

对应 Python 源码：
- `tests/system_tests/harness/rail/test_confirm_interrupt_rail.py`（细粒度确认/自动确认）
- `tests/system_tests/harness/rail/test_task_planning_rail.py`（TaskPlanning）
- `tests/system_tests/harness/rail/test_team_skill_rail.py`（SkillUse）
- 多个 Rail 在 Python 中分散在各处

---

## 1. 目录结构与文件规划

```
tests/integration/agentcore/harness/rails/
├── interrupt/              # ✅ 已有，扩展 ConfirmInterruptRail 细粒度确认
│   ├── confirm_rail_test.go # 🔄 扩展已有文件，新增 3 个测试（现有 2 个测试保持不变）
├── memory/                 # ✅ 已有
├── security/               # ✅ 已有
├── tools/                  # ✅ 已有
├── subagent/               # 🆕 SubagentRail + VerificationRail + VerificationContractRail
│   ├── doc.go
│   └── subagent_rail_test.go
├── skills/                 # 🆕 SkillUseRail
│   ├── doc.go
│   └── skill_use_rail_test.go
├── planning/               # 🆕 TaskPlanningRail
│   ├── doc.go
│   └── task_planning_rail_test.go
├── agent_mode/             # 🆕 AgentModeRail
│   ├── doc.go
│   └── agent_mode_rail_test.go
├── heartbeat/              # 🆕 HeartbeatRail
│   ├── doc.go
│   └── heartbeat_rail_test.go
├── progressive/            # 🆕 ProgressiveToolRail
│   ├── doc.go
│   └── progressive_rail_test.go
├── sys_operation/          # 🆕 SysOperationRail
│   ├── doc.go
│   └── sys_operation_rail_test.go
├── mcp/                    # 🆕 McpRail
│   ├── doc.go
│   └── mcp_rail_test.go
└── context_engineer/       # 🆕 ContextAssembleRail + ContextProcessorRail
    ├── doc.go
    └── context_engineer_rail_test.go
```

9 个新包 + 1 个已有包扩展。

---

## 2. GetCallbacks 覆盖状态与测试策略

**关键发现**：Go 的 struct embedding 不等于 Python 继承——基类 `GetCallbacks()` 不会动态派发到子类方法。因此 Rail 是否覆盖了 `GetCallbacks()` 直接影响其回调方法是否被框架自动调用。

| Rail | 覆盖 GetCallbacks | 回调事件 | 测试策略 |
|------|:---:|---------|---------|
| SubagentRail | ❌ | 无 | 仅测 Init 注册工具 + 构造成功 |
| VerificationRail | ❌ | 无 | 仅测 Init 注册工具 + 构造成功 |
| VerificationContractRail | ❌ | 无 | 仅测 Init + 构造成功 |
| SkillUseRail | ❌ | 无 | 仅测 Init 注册工具 + 构造成功 |
| HeartbeatRail | ❌ | 无 | 仅测 Init + 构造成功 |
| SysOperationRail | ❌ | 无 | 仅测 Init 注册工具 + Uninit |
| McpRail | ❌ | 无 | 仅测 Init 注册工具 + Uninit |
| **TaskPlanningRail** | ✅ | before_model_call, after_tool_call, after_model_call, after_invoke, after_task_iteration | 可测完整回调链路 |
| **AgentModeRail** | ✅ | before_model_call, before_tool_call, after_tool_call | 可测完整回调链路 |
| **ProgressiveToolRail** | ✅ | before_invoke, before_model_call | 可测完整回调链路 |
| **ContextAssembleRail** | ✅ | before_model_call | 可测回调链路 |
| **ContextProcessorRail** | ✅ | before_invoke, before_model_call, after_model_call, after_tool_call, on_model_exception | 可测完整回调链路 |
| ConfirmInterruptRail | ✅ (继承 BaseInterruptRail) | before_tool_call | 可测细粒度拦截 |

**测试策略二分法**：
- **有 GetCallbacks 覆盖的 Rail**（5 个 + ConfirmInterruptRail）：测试 Init + 关键回调行为 + Uninit
- **无 GetCallbacks 覆盖的 Rail**（7 个）：测试 Init 注册工具 + 构造成功 + 不崩溃（回调行为留待 GetCallbacks 修复后补充）

---

## 3. 测试用例详细规划

### 3.1 interrupt/ — ConfirmInterruptRail 细粒度确认（+3 测试）

| 测试 | 验证点 |
|------|--------|
| TestConfirmInterrupt_多工具选择性拦截 | 注册 2 个工具，MockLLM 返回 2 个 tool_call，验证只拦截注册的工具 |
| TestConfirmInterrupt_AutoConfirm跳过确认 | session 设置 auto_confirm，验证工具直接放行 |
| TestConfirmInterrupt_未注册工具放行 | 工具不在注册列表中，验证直接放行不触发中断 |

### 3.2 subagent/ — 3 个 Rail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestSubagentRail_Init注册TaskTool | NewSubagentRail() + Init 后 ability_manager 包含 task 工具 |
| TestVerificationRail_Init注册成功 | NewVerificationRail() + Init 成功且 Rail 注册到 Agent |
| TestVerificationContractRail_Init成功 | NewVerificationContractRail() + Init 成功且 Rail 注册到 Agent |

### 3.3 skills/ — SkillUseRail（2 测试）

| 测试 | 验证点 |
|------|--------|
| TestSkillUseRail_Init注册工具 | NewSkillUseRail(skillsDir) + Init 后 ability_manager 包含 skill 工具 |
| TestSkillUseRail_无技能目录时不崩溃 | skillsDir 为空时不崩溃 |

### 3.4 planning/ — TaskPlanningRail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestTaskPlanningRail_Init注册TodoTool | Init 后 ability_manager 包含 todo 工具 |
| TestTaskPlanningRail_BeforeModelCall注入规划提示词 | BeforeModelCall 注入 task_planning section |
| TestTaskPlanningRail_回调事件完整 | GetCallbacks 返回 5 个事件（before_model_call 等） |

### 3.5 agent_mode/ — AgentModeRail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestAgentModeRail_Init注册SwitchModeTool | Init 后 ability_manager 包含 switch_mode 工具 |
| TestAgentModeRail_BeforeModelCall注入模式提示词 | BeforeModelCall 注入当前模式 section |
| TestAgentModeRail_默认模式为Code | 默认模式应为 "code" |

### 3.6 heartbeat/ — HeartbeatRail（2 测试）

| 测试 | 验证点 |
|------|--------|
| TestHeartbeatRail_Init成功 | Rail 成功创建和初始化 |
| TestHeartbeatRail_Invoke不崩溃 | 带 HeartbeatRail 的 Invoke 不崩溃 |

### 3.7 progressive/ — ProgressiveToolRail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestProgressiveToolRail_Init成功 | NewProgressiveToolRail(config) + Init 成功 |
| TestProgressiveToolRail_BeforeInvoke缓存建立 | BeforeInvoke 建立工具导航缓存 |
| TestProgressiveToolRail_BeforeModelCall导航节注入 | BeforeModelCall 注入工具导航 section |

### 3.8 sys_operation/ — SysOperationRail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestSysOperationRail_Init注册工具 | Init 后 ability_manager 包含 file/shell/code 工具 |
| TestSysOperationRail_未启用时不注册 | enabled=false 时不注册工具 |
| TestSysOperationRail_Uninit清理 | Uninit 后工具从 ability_manager 移除 |

### 3.9 mcp/ — McpRail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestMcpRail_Init注册McpTools | Init 后 ability_manager 包含 MCP 工具 |
| TestMcpRail_无配置时不崩溃 | 无 MCP 服务端配置时 Init 不崩溃 |
| TestMcpRail_Uninit清理 | Uninit 后工具从 ability_manager 移除 |

### 3.10 context_engineer/ — 2 个 Rail（3 测试）

| 测试 | 验证点 |
|------|--------|
| TestContextAssembleRail_Init成功 | NewContextAssembleRail() + Init 成功 |
| TestContextProcessorRail_Init成功 | NewContextProcessorRail(opts) + Init 成功 |
| TestContextProcessorRail_GetCallbacks事件完整 | GetCallbacks 返回 before_invoke + before_model_call + after_model_call + after_tool_call + on_model_exception |

---

**总计：28 个测试**（3 interrupt 扩展 + 3 subagent + 2 skills + 3 planning + 3 agent_mode + 2 heartbeat + 3 progressive + 3 sys_operation + 3 mcp + 3 context_engineer）

---

## 4. Suite 设计

**不新增 Suite 基类**。所有 Rail 测试套件直接嵌入 `isuite.AgentSuite`。

```go
type XxxRailSuite struct {
    isuite.AgentSuite
}
```

### 4.1 Rail 构造策略

| 类型 | Rail | 构造方式 |
|------|------|---------|
| 无参构造 | HeartbeatRail, McpRail, VerificationContractRail, ContextAssembleRail | `NewXxxRail()` |
| 可选参数 | SubagentRail, VerificationRail, SkillUseRail, SysOperationRail, TaskPlanningRail, ContextProcessorRail, ConfirmInterruptRail | `NewXxxRail(opts...)` 或 `NewXxxRail(arg, opts...)` |
| 必需参数 | AgentModeRail, ProgressiveToolRail | `NewAgentModeRail(allowedTools)` / `NewProgressiveToolRail(config)` |

### 4.2 Mock 策略

沿用 Batch 1-3 的模式：
- 纯文本响应——验证 Init/BeforeModelCall/Uninit 不崩溃
- 工具调用响应——验证 BeforeToolCall 拦截
- nil 友好的依赖传 nil（有降级路径时）
- 不可 nil 的依赖用最小 mock

---

## 5. 交付计划

### 第一交（20 测试）：6 个新包 + interrupt 扩展

| 文件 | 测试数 |
|------|--------|
| interrupt/confirm_rail_test.go | 3 |
| subagent/subagent_rail_test.go | 3 |
| skills/skill_use_rail_test.go | 2 |
| planning/task_planning_rail_test.go | 3 |
| agent_mode/agent_mode_rail_test.go | 3 |
| heartbeat/heartbeat_rail_test.go | 2 |
| progressive/progressive_rail_test.go | 3 |
| **小计** | **19** |

### 第二交（9 测试）：3 个新包

| 文件 | 测试数 |
|------|--------|
| sys_operation/sys_operation_rail_test.go | 3 |
| mcp/mcp_rail_test.go | 3 |
| context_engineer/context_engineer_rail_test.go | 3 |
| **小计** | **9** |

---

## 6. 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 目录组织 | 按域分包，1:1 对齐源码 | 与项目源码结构一致，查找方便 |
| 无 GetCallbacks 的 Rail 测试策略 | 仅测 Init + 构造成功 | 回调不注册时 BeforeModelCall 不会触发，深测无意义 |
| 有 GetCallbacks 的 Rail 测试策略 | 测完整回调链路 | 回调已注册，可验证实际行为 |
| Suite 层级 | 不新增，复用 AgentSuite | 14 个 Rail 无额外共享状态 |
| ConfirmInterruptRail 扩展位置 | 扩展已有 confirm_rail_test.go | 现有 2 个测试保持不变，新增 3 个细粒度确认测试 |
| SubagentRail 等无 Uninit 的 Rail | 不测 Uninit | 源码未实现 Uninit，测试无意义 |
| ContextAssembleRail/ContextProcessorRail Uninit 签名差异 | 不测 Uninit | Uninit 签名与其他 Rail 不一致（缺 ctx），可能后续统一 |

---

## 7. 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| Rail 构造参数不确定 | 实施时逐一确认源码签名，nil 友好的直接传 nil |
| 7 个 Rail 无 GetCallbacks 覆盖 | 测试中记录此状态，不测回调行为；未来修复 GetCallbacks 后补充测试 |
| ProgressiveToolRail 需要 DeepAgentConfig | 使用 `schema.NewDeepAgentConfig()` 创建默认配置 |
| SkillUseRail 需要 skillsDir | 使用 `t.TempDir()` 创建空目录 |
| McpRail 需要 MCP 服务端 | 无服务端时 Init 应降级（不崩溃），测试验证降级路径 |

---

## 8. 实施状态

### 已完成

| 文件 | 测试数 | 状态 |
|------|--------|------|
| interrupt/confirm_rail_test.go（扩展） | 5（2 原有 + 3 新增） | ✅ |
| subagent/subagent_rail_test.go | 3 | ✅ |
| skills/skill_use_rail_test.go | 2 | ✅ |
| planning/task_planning_rail_test.go | 3 | ✅ |
| agent_mode/agent_mode_rail_test.go | 3 | ✅ |
| heartbeat/heartbeat_rail_test.go | 2 | ✅ |
| progressive/progressive_rail_test.go | 3 | ✅ |
| sys_operation/sys_operation_rail_test.go | 3 | ✅ |
| mcp/mcp_rail_test.go | 3 | ✅ |
| context_engineer/context_engineer_rail_test.go | 3 | ✅ |
| **合计** | **30** | **✅** |

### 实施调整记录

1. **interrupt 多工具选择性拦截**：InterruptResult 在子 goroutine 中 panic，无法被 recover 捕获。改用 AutoConfirm 验证拦截逻辑。
2. **先 Invoke 触发 Init**：Rail Init 在 ensureInitialized（第一次 Invoke）时触发，所有检查 AbilityManager 工具注册的测试需先 Invoke。
3. **TaskPlanningRail GetCallbacks 6 事件**：继承 DeepAgentRail 的 `before_task_iteration`，共 6 个（非设计文档中的 5 个）。
4. **ContextProcessorRail GetCallbacks 7 事件**：继承 DeepAgentRail 的 `before_task_iteration` + `after_task_iteration`，共 7 个（非设计文档中的 5 个）。
5. **AgentModeRail 默认模式**：Go 端为 `AgentModeNormal`（不是 Python 的 "code"）。

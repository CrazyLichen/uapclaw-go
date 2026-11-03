# 模式切换 rail 注销失效 + 规划模式状态未置位 修复设计

- 日期：2026-11-03
- 前置事实：集成测试 `tests/integration/swarm/server/adapter/mode_switch_todo_test.go` 已复现全部观测
- 关联：`2026-11-03-todo-panel-empty-design.md`（已修复的 todo.updated 空列表问题，本文处理其后续发现）

## 1. 问题与实测证据

同一 DeepAdapter 实例，第 1 轮 `mode=agent.plan`、第 2 轮 `mode=agent`：

| 观测项 | 规划模式 | 性能模式 | 期望 |
|---|---|---|---|
| `HasSection("todo")` | true | **true** | 性能模式应为 false |
| AbilityManager 中 todo 工具 | 4 个 | **`[]`** | — |
| 实发系统提示词含 todo 指令 | true | **true** | 与工具状态矛盾 |
| 系统提示词长度 | 13943 | **20940（+6997）** | 不应膨胀 |
| 系统提示词一级标题 | 10 个 | **bash 使用原则等整段出现两次** | 无重复 |
| todo_create 执行 | 成功 | **仍成功**（AbilityManager 已空） | 应失败 |

## 2. 根因一（P0）：`UnregisterRail` 的回调注销静默失效

```go
// interfaces/callback.go:713
func (m *AgentCallbackManager) UnregisterRail(_ context.Context, r AgentRail) error {
    callbacks := r.GetCallbacks()        // ← 每次现场构造新闭包
    for event, fn := range callbacks {
        m.Unregister(event, fn)          // → OffPerAgent
    }
}

// runner/callback/framework.go:560
if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) { ... }  // 按函数指针匹配
```

- `TaskPlanningRail.GetCallbacks()`（`task_planning.go:437`）等所有 rail 每次调用都新建闭包；
- Go 的 func 值除 nil 外不可比较，只能 `%p` 身份比较；
- 实测同一函数字面量两次求值的 `%p` 不同（`0x4823a0` / `0x482360`）→ **匹配永不命中**。

Python 无此问题：`framework.py:844` 用 `==`，而 bound method 的 `==` 是结构比较（`__self__`+`__func__`），每次 `getattr` 的新对象也相等（已实测）。

**后果链**：`updateAgentModeRails` 调 `UnregisterRail(TaskPlanningRail)` → `Uninit` 生效（删工具、RemoveSection）→ 但 `BeforeModelCall` 回调仍在 → 每轮重新 `AddSection(SectionTodo)` → "提示词说能用、工具已没了"。同理 `ContextAssembleRail` "先注销旧再注册新"时旧钩子仍在 → **系统提示词每轮重复拼段（+7KB）**。

**影响面**：全仓 23 处 `UnregisterRail` 调用全部失效。

## 3. 根因二：规划模式状态从未置位（Python 原状，本次产品决策收紧）

- `AgentModeRail` 的 plan 分支读 `agent.LoadState(sess).PlanMode.Mode == "plan"`（`agent_mode.go:251-253`）；
- 该字段只有 `DeepAgent.SwitchMode`（`deep_agent.go:981`）会写；
- web 链路只在 `mode == "code"` 时调 `SwitchMode`（`handle_envelope.go:341`）→ `agent.plan` 下 `PlanMode.Mode` 恒为 `"normal"`；
- **Python 同样如此**（`agent_ws_server.py:1145-1150` 只对 code 调 switch_mode；`PlanModeState.mode` 默认 `"normal"`）——不是移植偏差。

本次按产品决策让 web 的规划模式真正生效（偏离 Python，需在代码注释中说明）。

## 4. 修复方案

### 4.1 方案 A：DeepAgent 级缓存回调（修根因）

`internal/agentcore/harness/deep_agent.go`：

1. 新增字段 `railCallbacks map[AgentRail]map[AgentCallbackEvent]cb.PerAgentCallbackFunc`（随 `railsMu` 保护）。
2. `registerRailSelective`：`callbacks := r.GetCallbacks()` 取一次，先存入 `railCallbacks[r]`，再用这同一批 fn 注册到内/外层（路由规则不变：bridge → ReActAgent，其余 → 外层）。
3. `UnregisterRail`：从 `railCallbacks[r]` 取缓存 fn 逐事件注销（传给内外两层管理器均可，Off 对未注册方是无害 no-op），随后删除缓存条目；`GetCallbacks()` 返回 nil 时跳过回调注销但 `Uninit` 照常执行。

**范围说明**：本方案修 DeepAgent 路径（覆盖 DeepAdapter 全部调用点）。`TeamAgent.UnregisterRail`（`team_agent.go:1283`）与 `TeamHarness.UnregisterRail`（`harness.go:618`）若存在同型问题另行处理，本次不动。

### 4.2 方案 D1：web 切模式时同步置位 plan 状态

`internal/swarm/server/adapter/deep_adapter_rails.go:984` `updateRailsForMode`：

```go
func (d *DeepAdapter) updateRailsForMode(ctx context.Context, mode string, sessionID string) {
    if mode == "agent.plan" {
        d.updatePlanModeRails(ctx)
        d.switchModeForState(ctx, sessionID, "plan")
    } else {
        d.updateAgentModeRails(ctx, mode)
        d.switchModeForState(ctx, sessionID, "normal")
    }
}

// switchModeForState 会话状态同步：sessionID 为空（CreateInstance 阶段）跳过；
// SwitchMode 自带同模式幂等（deep_agent.go:975），重复调用无副作用。
// 注意：这是对 Python 的有意偏离——Python web 链路只在 code 模式调 switch_mode
// （agent_ws_server.py:1145），plan 模式状态从不置位；本次按产品决策收紧。
func (d *DeepAdapter) switchModeForState(ctx context.Context, sessionID, planMode string) {
    if sessionID == "" {
        return
    }
    if err := d.SwitchMode(ctx, sessionID, planMode); err != nil {
        logger.Warn(logComponent).Err(err).
            Str("plan_mode", planMode).Msg("switch plan state failed, continuing")
    }
}
```

调用方（`deep_adapter_config.go:142` 等）传入 `config.SessionID`。

**生效后的完整 plan 语义**（`AgentModeRail` plan 分支，均为原设计）：
- 注入 plan 指令节；移除 todo / session_tools 节；
- todo/session 工具对模型隐藏（`hiddenInPlan`）；
- `BeforeToolCall` 白名单 + plan 文件路径校验 + git 写操作拦截。
- **规划模式下模型不再能建/改待办**（右侧任务栏在规划模式不再新增条目）——产品已确认接受。

## 5. 测试策略

| 层级 | 文件 | 内容 |
|---|---|---|
| 单测（A） | `internal/agentcore/harness/deep_agent_rail_unregister_test.go`（新增） | fake rail（GetCallbacks 返回新闭包）注册 → 事件触发计数 1 → UnregisterRail → 再触发计数不变；重复 Unregister 不 panic |
| 集成（A+D） | 改造 `mode_switch_todo_test.go`：观测 → 断言 | ① 规划模式：AbilityManager 有 4 个 todo 工具、`HasSection("todo")`=false（plan 分支移除）、实发提示词不含 todo 指令；② 性能模式：`HasSection("todo")`=false、AbilityManager 无 todo 工具、实发提示词不含 todo 指令；③ 系统提示词标题无重复（长度增长 < 1000）；④ 性能模式下 todo_create 不再成功（无 todo.updated 或工具结果为错误） |
| 门禁 | — | 改动行覆盖 ≥85%、`-race`、集成审计两层门禁 |

## 6. 验收标准

1. 单测 / 集成 / race / lint 全绿（远端 CI 四 job）。
2. 手动：规划模式下提示词含 plan 指令、todo 工具不可见；切性能模式后提示词无 todo 残留、长度不膨胀；连续切换多次提示词长度稳定。
3. 正常路径无新增 Warn。

## 7. 明确不做

- 不改 callback 框架本身（`OffPerAgent` 匹配机制、CallbackInfo 结构）；
- 不处理 TeamAgent / TeamHarness / ContainerAgent 的注销路径；
- 不审计其他 rail 的 GetCallbacks 闭包构造；
- 不动 `hiddenInPlan` 集合（规划模式完整收紧）；
- 进度提醒（每 20 次工具调用注入的 UserMessage）跨模式残留问题本轮不处理（历史上下文语义，与 Python 一致）。

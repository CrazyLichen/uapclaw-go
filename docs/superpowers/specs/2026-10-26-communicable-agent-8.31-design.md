# CommunicableAgent (8.31) 回填设计

## 背景

领域八 8.31 小节 CommunicableAgent 已标记为 ✅ 完成，但对比 Python 原版 `openjiuwen/core/multi_agent/team_runtime/communicable_agent.py`，Go 实现缺少以下功能：

| # | 差异项 | Python 行为 | Go 当前行为 |
|---|--------|------------|------------|
| 1 | BindRuntime 幂等性 | 相同 runtime+agentID 静默跳过 | 每次都覆盖赋值 |
| 2 | BindRuntime 重绑定检测 | 不同 runtime/agentID 时 `logger.warning` | 无任何检测和日志 |
| 3 | `is_bound` 属性 | `@property is_bound → bool` | 无此方法 |
| 4 | 重绑定 warning 日志 | 有 `logger.warning` | 完全缺失 |

访问器防护（Python 未绑定时抛 `AGENT_TEAM_EXECUTION_ERROR`，Go 返回 nil/空串）经讨论保持现状，不做改动。

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Runtime()/AgentID() 访问器防护 | 保持现状（返回 nil/空串） | 通信方法已有 nil 检查兜底，Go 惯例不强制 error 返回 |
| BindRuntime 幂等性 + 重绑定检测 | 完整回填 | 与 Python 完全对齐，防止误操作 |
| IsBound() 方法 | 导出 | 与 Python is_bound 对齐，外部可查询绑定状态 |
| 重绑定 warning 日志字段 | 完全对齐（class_name + agent_id） | 日志同步规则要求 f-string 变量以结构化字段等价体现 |
| 实现方案 | 最小改动（方案 A） | 通过 runtime != nil && agentID != "" 判断绑定状态，无需新增字段 |

## 修改文件清单

| 文件 | 改动类型 |
|------|---------|
| `internal/agentcore/multi_agent/team_runtime/communicable_agent.go` | 修改 |
| `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go` | 修改 |

## 详细设计

### 1. 新增 IsBound() 导出方法

在 `communicable_agent.go` 导出函数区块新增：

```go
// IsBound 判断是否已绑定运行时。
//
// 对应 Python: CommunicableAgent.is_bound 属性
func (c *CommunicableAgent) IsBound() bool {
    return c.runtime != nil && c.agentID != ""
}
```

判断逻辑：`runtime != nil && agentID != ""`，与 Python 的 `_runtime is not None and _agent_id is not None` 等价。Go 中 agentID 为空字符串对应 Python 的 None。

### 2. 修改 BindRuntime() — 幂等性 + 重绑定检测 + warning 日志

将现有 BindRuntime 替换为：

```go
// BindRuntime 绑定团队运行时，注入运行时引用和 Agent 标识。
// 实现 RuntimeBindable 接口。
//
// 幂等性：相同 runtime 和 agentID 时静默跳过。
// 重绑定：已绑定到不同 runtime 或 agentID 时记录 warning 日志。
//
// 对应 Python: CommunicableAgent.bind_runtime(runtime, agent_id)
func (c *CommunicableAgent) BindRuntime(runtime *TeamRuntime, agentID string) {
    if c.IsBound() {
        if c.runtime == runtime && c.agentID == agentID {
            // 相同 runtime 和 agentID — 幂等，静默跳过
            return
        }
        // 不同 runtime 或 agentID — 重绑定，记录 warning
        logger.Warn(logComponent).
            Str("event_type", "RUNTIME_REBIND").
            Str("class_name", "CommunicableAgent").
            Str("agent_id", c.agentID).
            Msg("Agent 已绑定到运行时，重新绑定可能导致意外行为")
    }
    c.runtime = runtime
    c.agentID = agentID
}
```

日志字段对齐说明：

| Python 字段 | Go 字段 | 对齐方式 |
|-------------|---------|---------|
| `self.__class__.__name__` | `.Str("class_name", "CommunicableAgent")` | Go 中类型固定，用字符串常量 |
| `self._agent_id` | `.Str("agent_id", c.agentID)` | 直接对应 |
| `event_type`（项目规范） | `.Str("event_type", "RUNTIME_REBIND")` | 遵循项目日志规范 |

### 3. 不改动的部分

- `Runtime()` / `AgentID()` 访问器 — 保持返回 nil/空串
- `Send/Publish/Subscribe/Unsubscribe` — 已有 `c.runtime == nil` 检查
- `schema.Communicable` 接口 — 不变
- `RuntimeBindable` 接口 — 不变
- `NewCommunicableAgent()` — 不变

## 测试用例设计

### 新增测试用例

| 测试函数 | 覆盖场景 |
|---------|---------|
| `TestCommunicableAgent_IsBound` | 初始未绑定 → false；绑定后 → true；解绑（runtime 置 nil）→ false |
| `TestCommunicableAgent_BindRuntime_幂等绑定` | 相同 runtime+agentID 再次绑定，状态不变且无 warning |
| `TestCommunicableAgent_BindRuntime_重绑定` | 不同 runtime 再次绑定，覆盖并记录 warning 日志 |

### 需更新的现有测试

`TestCommunicableAgent_BindRuntime` 中的「绑定后可访问运行时」子测试应补充 `IsBound()` 断言。

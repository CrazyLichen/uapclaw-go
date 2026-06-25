# Mode 枚举设计（10.1.3）

> 对齐 Python `jiuwenswarm/common/schema/message.py` (Mode) 枚举，在 Go `internal/swarm/schema/mode.go` 中实现。

---

## 1. 背景

Mode 枚举定义 Agent 的运行模式，在通信链路中决定适配器选择和消息路由：

```
用户输入 → Channel → Gateway → E2A编码 → [Mode 决定适配器] → AgentServer → AgentAdapter → Agent
```

6 个模式值分为三个族：
- **agent 族**：`agent.plan`（深度规划）、`agent.fast`（快速响应）
- **code 族**：`code.plan`（代码规划）、`code.normal`（代码常态）、`code.team`（代码团队）
- **team 族**：`team`（团队运行时）

---

## 2. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| ParseMode 语义 | 回退优先（完全对齐 Python from_raw） | Mode 是用户可配置的宽松值，回退比 error 更实用 |
| ParseMode 参数类型 | 仅接受 string | Go 静态类型，Mode 本身是 string，无需 any 透传 |
| ToRuntimeMode | 显式提供 | 对齐 Python API，自文档化 |
| ChannelMode / ModeSubcommand | 本次不实现，延后到各自归属包 | Python 中它们不在 schema/ 中，严格对齐结构 |
| API 风格 | 一套 API（ParseMode + IsValidMode） | 简洁直接，IsValidMode 补偿无 error 返回的信息缺失 |

---

## 3. 类型定义

```go
type Mode string
```

### 常量值

| Go 常量 | 字符串值 | Python 对应 | 模式族 |
|---------|---------|-------------|--------|
| `ModeAgentPlan` | `"agent.plan"` | `Mode.AGENT_PLAN` | agent |
| `ModeAgentFast` | `"agent.fast"` | `Mode.AGENT_FAST` | agent |
| `ModeCodePlan` | `"code.plan"` | `Mode.CODE_PLAN` | code |
| `ModeCodeNormal` | `"code.normal"` | `Mode.CODE_NORMAL` | code |
| `ModeCodeTeam` | `"code.team"` | `Mode.CODE_TEAM` | code |
| `ModeTeam` | `"team"` | `Mode.TEAM` | team |

---

## 4. 导出 API

| 函数 | 签名 | 对齐 Python | 说明 |
|------|------|-------------|------|
| `AllModes` | `() []Mode` | — | 返回全部 6 个枚举值 |
| `ParseMode` | `(s string, default Mode) Mode` | `Mode.from_raw()` | 唯一解析入口，strip+lower 后查找，非法值回退 default |
| `IsValidMode` | `(s string) bool` | — | 严格校验合法性 |
| `Mode.String` | `() string` | — | `fmt.Stringer` |
| `Mode.GoString` | `() string` | — | `fmt.GoStringer` |
| `Mode.ToRuntimeMode` | `() string` | `Mode.to_runtime_mode()` | 返回字符串值 |

---

## 5. ParseMode 行为规格

对齐 Python `Mode.from_raw(raw_mode, default)`：

| 输入 s | default | 输出 |
|--------|---------|------|
| `"agent.plan"` | 任意 | `ModeAgentPlan` |
| `"  AGENT.PLAN  "` | 任意 | `ModeAgentPlan`（strip + lower） |
| `"code.normal"` | 任意 | `ModeCodeNormal` |
| `"invalid"` | `ModeCodeNormal` | `ModeCodeNormal`（回退到 default） |
| `""` | `ModeAgentPlan` | `ModeAgentPlan`（空字符串回退） |
| `"   "` | `ModeAgentPlan` | `ModeAgentPlan`（纯空白回退） |

实现步骤：
1. `normalized := strings.TrimSpace(strings.ToLower(s))`
2. 若 `normalized` 为空 → 返回 default
3. 查找 `modeLookup[normalized]`，找到则返回
4. 未找到 → 返回 default

---

## 6. 非导出函数

- `init()`：构建 `modeLookup map[string]Mode`（与 EventType/ReqMethod 模式一致）

---

## 7. 源码声明排列

遵循项目编码规范（规则 2）：

```
枚举（type Mode + const 块）→ 全局变量（modeLookup）→ 导出函数 → 非导出函数（init）
```

---

## 8. 测试覆盖

| 测试用例 | 覆盖点 |
|---------|--------|
| `TestAllModes_数量` | 确认 6 个值 |
| `TestAllModes_无重复` | 无重复值 |
| `TestAllModes_包含全部` | 6 个常量都在列表中 |
| `TestParseMode_合法值` | 全部 6 个标准值解析正确 |
| `TestParseMode_大小写与空白` | strip + lower 后正确解析 |
| `TestParseMode_非法值回退` | 非法字符串回退到 default |
| `TestParseMode_空字符串回退` | 空/纯空白回退到 default |
| `TestParseMode_默认值参数` | 不同 default 参数生效 |
| `TestIsValidMode` | 合法/非法值判断 |
| `TestMode_String` | String() 输出 |
| `TestMode_GoString` | GoString() 输出 |
| `TestMode_ToRuntimeMode` | ToRuntimeMode() 返回字符串值 |
| `TestMode_JSON序列化往返` | JSON marshal/unmarshal 往返 |
| `TestMode_与Python对齐` | 6 个字符串值与 Python 完全一致 |

---

## 9. 关联更新

| 文件 | 更新内容 |
|------|---------|
| `internal/swarm/schema/doc.go` | 文件目录添加 `mode.go` 条目 |
| `IMPLEMENTATION_PLAN.md` | 10.1.3 状态 `☐` → `✅` |

---

## 10. 延后项

| 类型 | 归属步骤 | 说明 |
|------|---------|------|
| `ChannelMode` | Gateway 层 (11.x) | Gateway 侧镜像，值与 Mode 相同，在 `internal/swarm/gateway/channel/` 中定义 |
| `ModeSubcommand` | Slash 命令 (10.4.4) | 含 `agent/code/team` 简写，在 slash_command 包中定义 |
| `resolve_agent_request_mode` | AgentServer (10.3) | mode 分解为 `(mode, sub_mode, canonical)` 三元组 |

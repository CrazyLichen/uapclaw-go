# 10.1.1 ReqMethod 枚举设计

> 实现计划步骤 10.1.1 — ReqMethod 枚举，产出 ~142 个 RPC 方法名常量。
> Python 参考：`jiuwenswarm/common/schema/message.py` (ReqMethod)

## 1. 流程位置与作用

### 流程位置

- **层 1（Schema 层）第 1 步** / 共 8 步
- **整个 MVP 40 步计划中的第 1 步**
- 所有后续层（E2A 协议层、AgentServer、Gateway、CLI+Web）都依赖 Schema 层

### 作用

ReqMethod 枚举定义了 **Gateway ↔ AgentServer 之间 E2A 协议的 RPC 方法名**，是整个通信链路的方法路由核心：

- 每个 E2A 请求信封（E2AEnvelope）都携带 `method` 字段，值为 ReqMethod 的字符串表示
- AgentServer 的 `AgentWebSocketServer` 据此分发到对应的处理函数
- Gateway 的 `MessageHandler` 使用它构建请求路由
- Channel 的 `MessageHandler` 使用它决定本地处理还是转发

## 2. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 全量 142 个枚举值 | mvp_plan 决策"Schema 一次到位免反复"，MVP 方法集只影响路由分发，不影响 Schema 定义 |
| 类型定义 | `type ReqMethod string` | 与项目先例 AgentCallbackEvent string 一致，天然 JSON 可序列化，无需额外映射 |
| 辅助方法 | ParseReqMethod + IsValid + AllReqMethods + String + GoString | 消除 Python 中 3 处重复的 _parse_req_method() 遍历逻辑 |
| 文件组织 | 单文件 req_method.go | 与项目先例一致，142 个常量按分组注释分隔，Go 标准库常见模式 |
| 命名对齐 | `chat.interrupt` → ReqMethodChatCancel | 与 Python `CHAT_CANCEL = "chat.interrupt"` 语义对齐 |
| 查找优化 | 包级 map 查找表 | ParseReqMethod/IsValid 使用 map 实现 O(1) 查找，优于 Python 的遍历 |

## 3. 类型定义

```go
// ReqMethod E2A 协议 RPC 方法名枚举。
//
// 定义 Gateway↔AgentServer 通信链路中所有合法的 RPC 方法标识，
// 用于 E2AEnvelope.method 字段和 AgentServer 方法路由分发。
// 值为点分字符串格式（如 "chat.send"），与 Python ReqMethod 枚举值一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (ReqMethod)
type ReqMethod string
```

## 4. 常量定义

142 个枚举常量，按功能分组以注释分隔。命名规则：

- Go 常量名：`ReqMethod` + 大驼峰（如 `ReqMethodChatSend`）
- 字符串值：与 Python 完全一致（如 `"chat.send"`）

### 分组明细

| 分组 | 枚举数 | Go 常量名前缀 | 字符串值前缀 |
|------|--------|--------------|-------------|
| 初始化 / ACP | 2 | `ReqMethodInitialize`, `ReqMethodACPToolResponse` | `initialize`, `acp.*` |
| 对话核心 | 4 | `ReqMethodChat*` | `chat.*` |
| 命令 | 13 | `ReqMethodCommand*` | `command.*` |
| 配置 / 通道 | 3 | `ReqMethodConfig*`, `ReqMethodChannelGet` | `config.*`, `channel.get` |
| 会话 | 12 | `ReqMethodSession*`, `ReqMethodTeamDelete` | `session.*`, `team.delete` |
| 历史 | 2 | `ReqMethodHistory*` | `history.*` |
| 路径 / 文件 / TTS / 内存 | 5 | `ReqMethodPath*`, `ReqMethodFiles*`, `ReqMethodTTS*`, `ReqMethodMemory*` | `path.*`, `files.*`, `tts.*`, `memory.*` |
| 浏览器 | 2 | `ReqMethodBrowser*` | `browser.*` |
| Agent 管理 | 10 | `ReqMethodAgents*`, `ReqMethodAgentReloadConfig` | `agents.*`, `agent.*` |
| 技能 | 24 | `ReqMethodSkills*` | `skills.*` |
| 插件 | 6 | `ReqMethodPlugins*` | `plugins.*` |
| 扩展 | 4 | `ReqMethodExtensions*` | `extensions.*` |
| 钩子 | 1 | `ReqMethodHooksList` | `hooks.list` |
| 心跳 | 2 | `ReqMethodHeartbeat*` | `heartbeat.*` |
| 权限 | 10 | `ReqMethodPermissions*` | `permissions.*` |
| IM 通道配置 | 13 | `ReqMethodChannelFeishu*` / `ReqMethodChannelXiaoyi*` / ... | `channel.feishu.*` / `channel.xiaoyi.*` / ... |
| 更新器 | 5 | `ReqMethodUpdater*` | `updater.*` |
| 团队 | 2 | `ReqMethodTeamSnapshot`, `ReqMethodTeamHistoryGet` | `team.*` |
| Harness | 7 | `ReqMethodHarness*` | `harness.*` |
| 调度 | 9 | `ReqMethodSchedule*` | `schedule.*` |

### 特殊命名映射

Python 中部分枚举成员名与值不完全对应，Go 常量名按**语义**对齐：

| Python 枚举成员 | Python 值 | Go 常量名 | 理由 |
|----------------|----------|----------|------|
| `CHAT_CANCEL` | `"chat.interrupt"` | `ReqMethodChatCancel` | 语义是"取消/中断对话"，Cancel 更通用 |
| `CHAT_ANSWER` | `"chat.user_answer"` | `ReqMethodChatAnswer` | 语义是"用户回答"，简写对齐 Python 成员名 |

## 5. 辅助方法

### 5.1 fmt.Stringer / fmt.GoStringer

```go
// String 实现 fmt.Stringer 接口
func (m ReqMethod) String() string

// GoString 实现 fmt.GoStringer 接口
func (m ReqMethod) GoString() string
```

### 5.2 遍历

```go
// AllReqMethods 返回所有 ReqMethod 枚举值，用于遍历清理等场景
func AllReqMethods() []ReqMethod
```

### 5.3 解析与验证

```go
// ParseReqMethod 从字符串解析 ReqMethod，不合法返回错误
func ParseReqMethod(s string) (ReqMethod, error)

// IsValid 判断字符串是否为合法的 ReqMethod 值
func IsValid(s string) bool
```

`ParseReqMethod` 和 `IsValid` 内部使用包级查找表 `reqMethodLookup map[string]ReqMethod`，O(1) 查找。

### 5.4 查找表

```go
// reqMethodLookup 字符串值到 ReqMethod 枚举的查找表
var reqMethodLookup map[string]ReqMethod
```

在包初始化时从 `AllReqMethods()` 构建，消除 Python 中 3 处重复的 `_parse_req_method()` 遍历逻辑。

## 6. Python 使用模式对照

| Python 模式 | Go 对应 |
|------------|--------|
| `ReqMethod(value)` 按值构造 | `ParseReqMethod(value)` |
| `_parse_req_method()` 手动遍历 | `ParseReqMethod()` 统一方法（查找表优化） |
| `m.value` 获取字符串值 | `m.String()` 或 `string(m)` |
| `m == ReqMethod.CHAT_SEND` 等值比较 | `m == ReqMethodChatSend`（Go 直接比较） |
| `m in (ReqMethod.CHAT_SEND, ...)` 集合判断 | `m == ReqMethodChatSend \|\| m == ReqMethodChatResume` 或用 map/set |
| `m.value.startswith("skilldev.")` 前缀匹配 | `strings.HasPrefix(string(m), "skilldev.")` |
| `dict[ReqMethod, str]` 路由表 | `map[ReqMethod]string`（Go 原生支持） |
| `isinstance(req_method, ReqMethod)` 类型检查 | Go 静态类型系统天然保证 |

## 7. 文件位置

```
internal/swarm/schema/
├── doc.go            # 包文档
├── req_method.go     # ReqMethod 类型 + 142 个常量 + 辅助方法
└── req_method_test.go # 单元测试
```

## 8. 声明顺序（对齐编码规范）

```
1. 枚举类型  type ReqMethod string
2. 枚举常量  const ( ReqMethodInitialize ... )
3. 全局变量  var reqMethodLookup
4. 导出函数  AllReqMethods / ParseReqMethod / IsValid
5. 非导出函数  (如有)
```

各区块之间使用规范要求的分隔注释。

## 9. 测试要求

- `TestAllReqMethods`：验证返回数量为 142
- `TestParseReqMethod_合法值`：随机抽样验证解析成功
- `TestParseReqMethod_非法值`：验证非法字符串返回错误
- `TestIsValid`：合法/非法值验证
- `TestReqMethodString`：验证 String() 返回原始字符串值
- `TestReqMethodGoString`：验证 GoString() 格式
- `TestReqMethodJSON序列化往返`：验证 JSON marshal/unmarshal 往返一致
- `TestReqMethod常量值与Python对齐`：验证核心常量字符串值与 Python 一致

## 10. 与 MVP 计划的关系

- **Schema 层全量定义 142 个**，与 MVP 10 个方法集无关
- MVP 10 个方法集只影响 AgentServer 路由分发时实际处理的方法子集
- Schema 一次到位，后续新增 RPC 方法只需在 req_method.go 添加常量 + 更新 AllReqMethods

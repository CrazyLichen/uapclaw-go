# 11.7 SessionMap + 11.13 Gateway Hook 设计

- 日期：2026-10-22
- 状态：已确认（分节呈现逐节通过，7 项关键决策已拍板）
- 范围：11.7（SessionMap 会话映射）+ 11.13（Gateway Hook 处理器）合并交付
- Python 参考：
  - `jiuwenswarm/gateway/routing/session_map.py`（109 行）
  - `jiuwenswarm/gateway/hooks/handler.py`（127 行）
  - `jiuwenswarm/gateway/message_handler/message_handler.py` 集成点（L150-177 / L278-299 / L615-680 / L1144-1165 / L2185-2197）

## 1. 背景与目标

### 1.1 问题

普通受控通道（feishu/xiaoyi/…）的 session_id 来自 config `default_session_id` 或内存生成——Gateway 重启后会话丢失或漂移。企业 IM 场景（feishu_enterprise）要求"同一个群+机器人跨重启恢复同一 Agent 会话"。

### 1.2 解法

SessionMap 把 IM 平台稳定身份（provider/chat_id/bot_id[/user_id]）映射为轮转 agent session_id，持久化到 `{workspace}/agent/.checkpoint/session_map.json`：

- `per_chat_bot`（默认）：同群同机器人共享一个 Agent 会话
- `per_chat_bot_user`：按用户隔离会话

### 1.3 为什么带上 11.13

- `slash_cmd.go:114` 的 TODO 是 `TODO(#11.7+#11.13)`——`/new_session` 时先轮转 SessionMap 再触发 `on_session_start` hook，两者在 Python 同一函数体（L654-667），拆开做会留下半截 TODO
- GatewayHookHandler 依赖的 HooksConfig + HookExecutor（10.3.23.x）均已就绪，11.13 剩余工作量小（handler 127 行）
- forward_loop 步骤3 的 TODO(#11.13) 一并清除

## 2. 关键决策记录（讨论中拍板）

| # | 决策 | 理由 |
|---|------|------|
| D1 | 11.7 + 11.13 合并交付 | 两者在 Python 同一函数体耦合；11.13 依赖已就绪 |
| D2 | HookExecutor 从 `server/hooks` 提取到 `common/hooks` | Python 里 Gateway 直接 import server 的 executor；Go 规则 6.5 禁止 gateway→server。executor 依赖面已核实干净（仅 agentcore/* + common/*，零 server 域内依赖）。延续 transport/schema-events 破环先例 |
| D3 | SessionMapScope 直读全局 config（RegisterConfig/getGlobalConfig 通道） | Python `load_session_map_scope()` 走全局 `get_config()`（每次读盘），与 `_get_config_raw` 注入回调是两条独立通道，11.12 完成后也不改。Go 对齐采取 executor.queryLLM 同款全局注册模式 |
| D4 | `NewGatewayServer` 装配 `hookscfg.RegisterConfig(cfg)` | Go 全局 Config 原先只在 AgentServer 侧注册（agent_server.go:113）；独立 Gateway 进程（12.9）不经过它。NewGatewayServer 手里有 cfg，幂等注册，谁先启动谁注册 |
| D5 | handleChannelControl 对齐 Python 结构 | Python 把"改状态"写在入口函数体（L652-667），收尾甩异步；Go 现在是一锅端在子函数。重构为：state 获取提到 switch 前 + SessionMap 轮转/state 更新/hook 触发写在 handleChannelControl 体内 + newSessionCancelAndNotice 瘦身为收尾函数（取消旧会话+通知） |
| D6 | extractIdentityTuple 用 Go 五返回值 `(provider, chatID, botID, userID string, ok bool)` | Python tuple｜None 的 Go 惯例等价 |
| D7 | GatewayHookHandler 持有具体类型 `*hookscfg.HookExecutor` | 对齐 Python 具体持有；失败路径靠 handler 自身 recover + 真实 executor 跑非法 hook 配置测，不引入接口间接层 |

## 3. 总体架构与包布局

```
internal/common/hooks/                    ← D2：executor 并入
├── doc.go            # 更新：增 executor 条目 + 依赖说明
├── types.go          # HookType/HookEvent（不动）
├── config.go         # HooksConfig/LoadHooksConfig（不动）
├── executor.go       # ← 迁入：HookResult + HookExecutor + RegisterConfig/getGlobalConfig
└── executor_test.go / executor_llm_test.go  # ← 随迁（//go:build llm 标签保持）

internal/swarm/server/hooks/             ← 只剩 rail，包继续存在
├── doc.go            # 更新：去掉 executor 条目，import 改 common/hooks
└── user_hook_rail.go # 不动（依赖 agentcore rails/callback/interfaces，必须留 server 侧）

internal/swarm/gateway/routing/
├── session_map.go    # ← 新增 11.7
└── session_map_test.go

internal/swarm/gateway/hooks/            ← 新增 11.13
├── doc.go
├── handler.go        # GatewayHookHandler
└── handler_test.go

internal/swarm/gateway/message_handler/  ← 集成点
├── message_handler.go   # 新增字段 sessionMap + gatewayHookHandler + 构造装配 + TriggerSessionStartHook
├── channel_state.go     # 回填 ①②：GetOrCreateChannelState + ApplyChannelState
├── slash_cmd.go         # 回填 ③：/new_session 轮转 + SessionStart 触发（D5 结构）
└── forward_loop.go      # 回填 ④：步骤3 UserPromptSubmit

internal/swarm/gateway/app_gateway.go    # D4：RegisterConfig 装配
```

### 3.1 装配链

```
NewGatewayServer(cfg, agentClient)
  ├─ hookscfg.RegisterConfig(cfg)          ← 新增，幂等（重复注册只更新）
  └─ mh.NewMessageHandler(agentClient)
       ├─ sessionMap = routing.NewSessionMap(nil)      → LoadSessionMapScope() 全局读
       └─ gatewayHookHandler = hooksgw.NewGatewayHookHandler(LoadHooksConfig(cfg.Load()))
            （异常 → Warn + handler 为 nil，对齐 Python L163-170 try/except）
```

### 3.2 依赖方向（全部单向无环，已核实）

- `gateway/routing` → `common/config`、`common/logger`
- `gateway/hooks` → `common/hooks`、`common/logger`
- `gateway/message_handler` → `gateway/routing`、`gateway/hooks`
- `common/hooks`（并入 executor 后）→ `agentcore/foundation/llm` + `common/config`（agentcore 侧零反向引用 `common/hooks`；common/config→crypto→exception 链不碰 hooks——无环成立）
- `server/adapter`、`server/agent_server` → `common/hooks`（改 import，3 处）
- **gateway 域 0 个 import server**，规则 6.5 保持零违反

注：`common/hooks` 成为 common 目录第一个 import `agentcore` 的包——依赖方向（common/hooks → agentcore）合法无环，与 embedding-config 先例同理。

## 4. SessionMap 组件设计（11.7 本体）

`gateway/routing/session_map.go`，一比一对齐 Python `session_map.py`。

### 4.1 枚举与 scope 加载

```go
type SessionMapScope int
const (
    SessionMapScopePerChatBot     SessionMapScope = iota // "per_chat_bot"（默认）
    SessionMapScopePerChatBotUser                        // "per_chat_bot_user"
)

// LoadSessionMapScope 读 config gateway.session_map_scope（构造时读一次，不热重载）
// 对齐 Python load_session_map_scope L21-33：
//   raw = str((gateway or {}).get("session_map_scope") or "per_chat_bot").strip().lower()
//   未知值 → Warn + 默认；读 config 异常 → Warn + 默认
func LoadSessionMapScope() SessionMapScope
```

- 全局读通道复用 `common/hooks` 迁入后的 `getGlobalConfig()`（`cfg.Load()` 重读盘，等价 Python `get_config()` 每次读盘语义）
- 未注册（`getGlobalConfig()==nil`）/读失败 → `logger.Warn` + 默认 `PerChatBot`（Python 两个 except 分支的 Go 归并）
- 解析链：`(gateway 段).(session_map_scope)` 断言 string → 空串取默认 → `TrimSpace+ToLower` → 匹配枚举；未匹配 Warn（带原始值与默认值字段）+ 默认

### 4.2 键与 sid 生成

```go
// makeKey 对齐 Python _make_key L36-45
//   PerChatBot:     "provider::chatID::botID"
//   PerChatBotUser: "provider::chatID::botID::userID"
func makeKey(scope SessionMapScope, provider, chatID, botID, userID string) string

// makeSessionID 对齐 Python _make_session_id L48-59
//   key 结构 + "::" + hex(毫秒时间戳) + "::" + 6 位随机 hex（crypto/rand，等价 secrets.token_hex(3)）
func makeSessionID(scope SessionMapScope, provider, chatID, botID, userID string) string
```

- 时间戳 `time.Now().UnixMilli()` 格式化为小写十六进制（对齐 Python `format(int(time.time()*1000), "x")`）
- 随机 hex：routing 包内自带一份（不跨包引用 message_handler 的私有 `generateRandomHex`）；`crypto/rand` 失败降级时间戳切片（沿用 channel_state.go 模式）

### 4.3 SessionMap 主体

```go
type SessionMap struct {
    scope     SessionMapScope
    storePath string            // path.CheckpointDir()/session_map.json
    mapping   map[string]string
    mu        sync.Mutex        // Go 并发补齐
}

// NewSessionMap 对齐 Python __init__ L65-69：scope nil → LoadSessionMapScope()
func NewSessionMap(scope *SessionMapScope) *SessionMap

// GetSessionID 对齐 Python get_session_id L90-109
func (m *SessionMap) GetSessionID(provider, chatID, botID, userID string, rotate bool) string
```

GetSessionID 逻辑：

1. `key := makeKey(...)`，`existing, ok := mapping[key]`
2. `ok && !rotate` → 返回 existing
3. `sid := makeSessionID(...)`；`existing == sid` → 直接返回 sid **不写盘**（Python L105-106 同毫秒+同随机理论幂等分支，Go 保留）
4. 否则 `mapping[key] = sid` + `save()`

### 4.4 持久化（全防御式，错误只 Warn 不失败）

- `load()`（对齐 `_load` L71-80）：文件不存在静默跳过；JSON 解析失败 Warn；`data` 非 map 跳过；逐键过滤**值必须是 string 且非空**（Python `isinstance(v, str) and v`）
- `save()`（对齐 `_save` L82-88）：`os.MkdirAll(dir, 0o755)` + 写 JSON `json.MarshalIndent(m, "", "  ")`（等价 indent=2；Go 默认非 ASCII 不转义等价 ensure_ascii=False）+ 0644；失败 Warn
- 可测性：包内测试构造 `newSessionMapAt(storePath, scope)`（非导出，路径注入，不污染公共 API）

### 4.5 storePath 来源

`path.CheckpointDir()`（`internal/common/utils/path/paths.go:273`，返回 `WorkspaceDir()/agent/.checkpoint`）——与 Python `get_checkpoint_dir()`（`common/utils.py:1450`，`get_agent_root_dir()/.checkpoint`）完全一致，无迁移逻辑差异需要处理。

## 5. GatewayHookHandler 设计（11.13 本体）

`gateway/hooks/handler.go`，对齐 Python `handler.py`（127 行）。

```go
type GatewayHookHandler struct {
    config         *hookscfg.HooksConfig   // 对齐 _config
    executor       *hookscfg.HookExecutor  // 对齐 _executor（具体类型，D7）
    gatewayTimeout int                     // 对齐 _gateway_timeout = 10
    mu             sync.Mutex              // 保护 activeSessions
    activeSessions map[string]bool         // 对齐 _active_sessions: set[str]
}

func NewGatewayHookHandler(hooksConfig *hookscfg.HooksConfig) *GatewayHookHandler
```

### 5.1 四个事件方法

| 方法 | match 的 query | hook_input 字段 | 前置动作 |
|------|---------------|----------------|---------|
| `OnSessionStart(ctx, sessionID, source string)`（source 建议传 "startup"，Go 无默认参） | `source` | event/source/session_id/cwd | activeSessions 去重（命中直接 return） |
| `OnUserPromptSubmit(ctx, sessionID, prompt string)` | `""`（Python match 默认空串，hooks_config.py L131） | event/prompt/session_id | 无 |
| `OnSessionEnd(ctx, sessionID, reason string)`（建议传 "clear"） | `reason` | event/reason/session_id | activeSessions **discard**（先移除再执行） |
| `OnNotification(ctx, notificationType, message, sessionID string)`（sessionID 建议传 ""） | `notificationType` | event/notification_type/message/session_id | 无 |

每方法统一结构（以 SessionStart 为例，对齐 Python L31-57）：

```go
func (h *GatewayHookHandler) OnSessionStart(ctx context.Context, sessionID, source string) {
    h.mu.Lock()
    if h.activeSessions[sessionID] { h.mu.Unlock(); return }
    h.activeSessions[sessionID] = true
    h.mu.Unlock()

    hookConfigs := h.config.Match(hookscfg.HookEventSessionStart, source)
    if len(hookConfigs) == 0 { return }

    for i := range hookConfigs {
        if _, ok := hookConfigs[i]["timeout"]; !ok {   // setdefault 语义
            hookConfigs[i]["timeout"] = h.gatewayTimeout
        }
    }

    cwd, _ := os.Getwd()   // 失败降级空串（5.2 条款 4）
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).
                Str("event", "SessionStart").
                Any("panic", r).
                Msg("GatewayHookHandler: SessionStart hook failed")
        }
    }()
    h.executor.RunAll(ctx, hookConfigs, map[string]any{
        "event": "SessionStart", "source": source,
        "session_id": sessionID, "cwd": cwd,
    }, sessionID)
}
```

### 5.2 关键语义细节

1. **同步执行**：Python 注释明说"同步串行执行（保证 session 生命周期顺序）"——Go 方法直接同步调 RunAll（RunAll 内部 goroutine 并行跑各 hook 等价 asyncio.gather），不额外 `go`。`TriggerSessionStartHook` 的异步性在调用方（fire-and-forget goroutine 对应 `asyncio.create_task`）
2. **timeout setdefault**：Go `Match` 返回 `[]map[string]any`，map 引用语义就地改等价 Python setdefault；且这些 map 即 HooksConfig 内部存储的引用——Python 同样就地改共享 dict，行为一致，不算偏差
3. **异常吞噬边界**：`defer/recover` 包 RunAll 调用；RunAll 返回 `[]HookResult` 不检查（结果内 error 只记日志，与 Python run_all 内吞异常一致）
4. **cwd**：`os.Getwd()`；失败降级空串（Python Path.cwd() 不会失败，Go 需防御）
5. **ctx**：方法签名带 `ctx context.Context`（RunAll 需要；调用方传 forwardLoop 的 ctx 或 context.Background()）

## 6. MessageHandler 集成（4 个回填点）

### 6.1 新增字段与辅助函数（message_handler.go）

```go
// sessionMap 企业通道会话映射（对齐 Python _session_map）
sessionMap *routing.SessionMap
// gatewayHookHandler Gateway 层钩子处理器（对齐 Python _gateway_hook_handler，构造失败为 nil）
gatewayHookHandler *hooksgw.GatewayHookHandler

// extractIdentityTuple 对齐 Python _extract_identity_tuple L321-329（D6 五返回值）
// 四字段 TrimSpace 后任一为空 → ok=false
func extractIdentityTuple(msg *schema.Message) (provider, chatID, botID, userID string, ok bool)

// channelIDMatchesSessionMapTypes 对齐 Python _channel_id_matches_session_map_types L331-337
// 精确匹配 "feishu_enterprise" 或前缀 "feishu_enterprise:"
func channelIDMatchesSessionMapTypes(channelID string) bool
```

`sessionMapChannelTypes` 集合：对齐 Python `_session_map_channel_types = frozenset({"feishu_enterprise"})`，放 message_handler 包级集合（与既有 controlChannelTypes 同模式）。

品牌说明：`feishu_enterprise` 是第三方平台标识（非品牌串），不套 jiuwen→uapclaw 映射——符合品牌映射规则"第三方 env 名不改"。

### 6.2 回填点 ① GetOrCreateChannelState（channel_state.go:125 TODO 清除）

对齐 Python L293-298——state 不存在时创建默认值后，SessionMap 覆盖：

```go
state = mh.getChannelDefaultState(ch)
if provider, chatID, botID, userID, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(ch) {
    state.SessionID = mh.sessionMap.GetSessionID(provider, chatID, botID, userID, false)
}
```

已有 state 直接返回，不走 SessionMap（对齐 Python L289-291）。

### 6.3 回填点 ② ApplyChannelState（channel_state.go:92）补双分支

对齐 Python L1151-1159：

```go
if provider, chatID, botID, userID, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(cid) {
    sid := mh.sessionMap.GetSessionID(provider, chatID, botID, userID, false)
    state.SessionID = sid      // 无条件刷新 state（get_session_id 非轮转时幂等返回）
    msg.SessionID = sid
} else if state.SessionID != "" {
    msg.SessionID = state.SessionID
}
```

注意：SessionMap 分支**无条件覆盖** `state.SessionID` + `msg.SessionID`；非 SessionMap 分支才是 `elif` 语义。

### 6.4 回填点 ③ handleChannelControl（slash_cmd.go，D5 结构）

对齐 Python L652-680：

```go
// handleChannelControl 体内，switch 之前：
state := mh.GetOrCreateChannelState(msg)

case command_parser.ActionNewSessionOK:
    // 对齐 Python L654-667（全部在入口函数体内）
    oldSID := state.SessionID
    cid := msg.ChannelID
    var newSID string
    if provider, chatID, botID, userID, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(cid) {
        newSID = mh.sessionMap.GetSessionID(provider, chatID, botID, userID, true)  // rotate=true
    } else {
        newSID = GenerateChannelSessionID(string(channelType))
    }
    state.SessionID = newSID
    mh.TriggerSessionStartHook(newSID, string(channelType))   // 见 6.6
    // 收尾：取消旧会话 + 通知（newSessionCancelAndNotice 瘦身为收尾函数，oldSID 通过参数传入）
    mh.newSessionCancelAndNotice(msg, oldSID, newSID)
```

- `channelType` 取 `mh.resolveControlChannelType(msg)` 结果（对齐 Python `_resolve_control_channel_type` 返回值作 source）
- 现有 `TODO(#11.7+#11.13)` 整行删除；`newSessionCancelAndNotice` 签名调整（parsed 参数若不再使用则移除）

### 6.5 回填点 ④ forwardLoop 步骤3（forward_loop.go:97 TODO 清除）

对齐 Python L2188-2197：

```go
if mh.gatewayHookHandler != nil {
    params := 解析 msg.Params 为 map（非 map 时空 map）
    promptText := params["query"] 或 params["content"]（string 断言，对齐 Python str()）
    // 失败仅 Debug 日志不阻塞（对齐 Python except + logger.debug）
    mh.gatewayHookHandler.OnUserPromptSubmit(ctx, msg.SessionID, promptText)
}
```

### 6.6 TriggerSessionStartHook（新增导出方法）

对齐 Python L172-177（供 Channel 层调用，TUI/Web 后续接入）：

```go
func (mh *MessageHandler) TriggerSessionStartHook(sessionID, source string) {
    if mh.gatewayHookHandler == nil { return }
    // 对齐 asyncio.create_task（fire-and-forget；ctx 用 Background，对齐 Python create_task 无 ctx 概念）
    go mh.gatewayHookHandler.OnSessionStart(context.Background(), sessionID, source)
}
```

## 7. 错误处理矩阵

| 故障点 | Python 行为 | Go 对齐 |
|--------|------------|---------|
| scope 配置未知值 | Warn + per_chat_bot | Warn + 默认枚举 |
| 全局 Config 未注册 | get_config() 异常→except 降级默认 | getGlobalConfig()==nil → Warn + 默认 |
| session_map.json 不存在 | _load 静默跳过 | os.IsNotExist → 直接返回 |
| session_map.json 损坏 | Warn + 空 mapping | json 解析失败 Warn + 空 mapping |
| 值非 string/空串 | 逐项过滤 | `v, ok := v.(string); ok && v != ""` |
| save 失败（权限/磁盘） | Warn 不抛 | Warn（save 无返回值签名） |
| HooksConfig 加载失败 | Warn + handler=None | Warn + nil handler（调用点全 nil-guard） |
| executor panic | gather 捕获 | RunAll 内部 recover（既有实现） |
| SessionStart/UserPromptSubmit 失败 | except + warning | recover + Warn |
| checkpoint 目录不存在 | mkdir parents | os.MkdirAll 0755 |

## 8. Go 与 Python 的偏差记录

1. **sync.Mutex 并发保护**（SessionMap.mapping、GatewayHookHandler.activeSessions）：Python asyncio 单线程无锁，Go 必要补齐，不改变语义
2. **NewSessionMap(scope \*SessionMapScope)**：指针可选参对齐 Python `scope: SessionMapScope | None = None`；非 variadic 避免误用
3. **OnSessionStart/OnSessionEnd/OnNotification 的默认参数**：Python `source="startup"` / `reason="clear"` / `session_id=""` 默认值，Go 无默认参——签名只收必填值，调用方显式传；本设计内调用点均显式传（/new_session 传 channelType、forwardLoop 传实际值），其他调用方按 Python 默认值语义传
4. **brand 映射不适用**：`feishu_enterprise` 为第三方平台标识，不做 jiuwen→uapclaw 映射
5. **executor 归属迁移**（D2）：Python `server/hooks/executor.py` 对应物迁至 `common/hooks`，`user_hook_rail.go` 留守 `server/hooks`——包布局偏差，由 Go 禁止 gateway→server 强制（规则 6.5）
6. **RegisterConfig 全局注册**（D3/D4）：Python `get_config()` 全局函数随时可读，Go 用 RegisterConfig+getGlobalConfig 等价实现；NewGatewayServer 补注册点保证独立 Gateway 进程可用

## 9. 测试策略（覆盖率 ≥85%）

| 文件 | 重点用例 |
|------|---------|
| `session_map_test.go` | 两 scope 的 makeKey/makeSessionID 格式断言（`::` 段数、hex 时间戳）；rotate=false 幂等；rotate=true 轮转（新值≠旧值）；`existing==sid` 幂等分支（预置 mapping 构造）；load 过滤（非 string 值/空值剔除）；save/load 回环（newSessionMapAt + t.TempDir）；损坏 JSON 降级；LoadSessionMapScope 未知值/nil config 降级 |
| `handler_test.go` | 4 方法的 match-query 差异；timeout setdefault（有值不覆盖/无值补 10）；activeSessions 去重与 discard；hook_input 字段全量断言；recover 路径；DisableAllHooks 短路 |
| `channel_state_test.go` 扩展 | SessionMap 分支：identity 齐备+enterprise 通道 → sid 来自 SessionMap；identity 缺一 → config 默认；`feishu_enterprise:xxx` 前缀命中；非 enterprise 通道不触发 |
| `slash_cmd_test.go` 扩展 | /new_session 在 enterprise 通道轮转 SessionMap 且写盘；触发 SessionStart；非 enterprise 通道走 GenerateChannelSessionID |
| `forward_loop_test.go` 扩展 | 步骤3 调用 OnUserPromptSubmit（nil handler 跳过不 panic） |
| `executor_test.go` | 迁移后原测试全绿（包路径调整） |

## 10. IMPLEMENTATION_PLAN.md 回填（既有信息不丢失）

- **11.7 行**：`☐ → ✅`，产出补：SessionMap（scope 枚举+LoadSessionMapScope+makeKey/makeSessionID+checkpoint 持久化）+ MessageHandler 4 集成点回填（GetOrCreateChannelState/ApplyChannelState/handleChannelControl/forwardLoop）
- **11.13 行**：`☐ → ✅`，产出补：GatewayHookHandler（4 事件方法+activeSessions 去重+10s 超时）+ HookExecutor 迁移 common/hooks + forward_loop 步骤3 回填 + TriggerSessionStartHook
- **10.3.23.3 行**：已 ✅，补注 executor 归属迁移（server/hooks → common/hooks，user_hook_rail 留守）
- **代码内 TODO 清单**：`channel_state.go:125`（#11.7）、`slash_cmd.go:114`（#11.7+#11.13）、`forward_loop.go:97`（#11.13）全部清除
- **doc.go 同步**：`common/hooks/doc.go`（+executor）、`server/hooks/doc.go`（-executor）、`routing/doc.go`（+session_map）、`gateway/message_handler/doc.go`（更新说明）
- 注意保留其他章节既有 ⤵️/⤴️ 标记（如 11.12 IM Pipeline 的 TODO(#11.12) 不动、11.15 TUI 的 ⤴️ 不动）

## 11. 不做的事（范围外）

- ACP 会话别名相关 `_is_session_map_style_session_id` / `_is_known_jiuwenswarm_session_id`（唯一调用方是 ACP 流程，属 11.25 ACP 协议桥接范围）
- 11.12 IM Pipeline（TODO(#11.12) 保留）
- Python `trigger_session_start_hook` 的 TUI/Web Channel 层调用接线（Go 侧 TriggerSessionStartHook 导出方法就绪，等通道实现时接入）
- Cron/心跳等 11.10/11.11

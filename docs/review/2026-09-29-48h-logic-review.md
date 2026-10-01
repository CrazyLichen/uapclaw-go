# 48h 逻辑审查报告（2026-09-29）

> 审查范围：48 小时内（2026-09-27 ~ 2026-09-29）提交的代码，覆盖实现计划章节 7.14 / 7.15 / 7.25 / 9.68 / 9.69 / 9.28 回填

## 审查范围

| 章节 | 模块 | Python 参考 | Go 实现 |
|------|------|-------------|---------|
| 7.14 | Mem0Provider | `openjiuwen/core/memory/external/mem0_provider.py` | `internal/agentcore/memory/external/mem0_provider.go` + `mem0_client.go` |
| 7.15 | OpenVikingProvider | `openjiuwen/core/memory/external/openviking_memory_provider.py` | `internal/agentcore/memory/external/viking_provider.go` + `viking_client.go` |
| 7.25 | DistributedLock | `openjiuwen/core/memory/common/distributed_lock.py` | `internal/agentcore/memory/common/distributed_lock.go` |
| 9.68 | Team Rails | `jiuwenswarm/agent_teams/rails/` | `internal/agent_teams/rails/` |
| 9.69 | Team Prompts | `jiuwenswarm/agent_teams/prompts/` | `internal/agent_teams/prompts/` |
| 9.28 | PlanAgent 回填 | `openjiuwen/harness/subagents/plan_agent.py` + `openjiuwen/agent_teams/rails/team_plan_mode_rail.py` | `internal/agentcore/harness/subagents/plan_agent.go` + `internal/agent_teams/rails/team_plan_mode_rail.go` |

---

## 问题统计

| 级别 | 7.14 | 7.15 | 7.25 | 9.68 | 9.69 | 9.28 | 合计 |
|------|------|------|------|------|------|------|------|
| S（严重） | 3 | 2 | 2 | 6 | 6 | 6 | **25** |
| M（一般） | 4 | 3 | 1 | 3 | 5 | 2 | **18** |
| T（提示） | 6 | 1 | 1 | 2 | 4 | 2 | **16** |
| **小计** | 13 | 6 | 4 | 11 | 15 | 10 | **59** |

---

## 一、7.14 Mem0Provider

### S-01: Initialize 未处理 rerank kwargs 覆盖

**问题**: Python `initialize()` 允许通过 `kwargs["rerank"]` 覆盖构造时的 `rerank` 设置，Go 的 `Initialize()` 完全忽略了这个参数。如果调用方在初始化时传入 `rerank=true`，Go 侧不会生效。

**Python**:
```python
async def initialize(self, **kwargs: Any) -> None:
    self._api_key = kwargs.get("api_key") or self._api_key
    self._user_id = kwargs.get("user_id") or self._user_id
    self._agent_id = kwargs.get("agent_id") or self._agent_id
    if "rerank" in kwargs:
        self._rerank = bool(kwargs["rerank"])
```

**Go** (`mem0_provider.go`):
```go
func (p *Mem0Provider) Initialize(_ context.Context, opts ...ProviderOption) error {
    po := applyOptions(opts...)
    if po.UserID != "" {
        p.userID = po.UserID
    }
    if po.ScopeID != "" {
        p.agentID = po.ScopeID
    }
    // ❌ 没有处理 rerank 的覆盖
```

**修复方案**: 在 `ProviderOptions` 中添加 `Rerank *bool` 字段（指针区分未设置和 false），在 `Initialize` 中处理：
```go
if po.Rerank != nil {
    p.rerank = *po.Rerank
}
```

---

### S-02: Prefetch 和 QueuePrefetch 完全忽略 opts 中的 top_k 和 rerank 参数

**问题**: Python `prefetch()` 和 `queue_prefetch()` 接受 `**kwargs`，从中读取 `top_k`（默认5，上限50）和 `rerank`（默认用 `self._rerank`）。Go 的 `Prefetch()` 和 `QueuePrefetch()` 虽然签名接受 `opts ...ProviderOption`，但完全未使用 opts 参数，硬编码 `topK=5` 和 `rerank=p.rerank`。调用方传入的 `WithTopK(20)` 等选项会被静默丢弃。

**Python**:
```python
async def prefetch(self, query: str, **kwargs: Any) -> str:
    top_k = min(int(kwargs.get("top_k", 5)), 50)
    rerank = bool(kwargs.get("rerank", self._rerank))
```

**Go** (`mem0_provider.go`):
```go
func (p *Mem0Provider) Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error) {
    topK := 5           // 硬编码，未从 opts 读取
    rerank := p.rerank  // 硬编码，未从 opts 读取
```

**修复方案**: 在 `ProviderOptions` 中添加 `TopK int` 和 `Rerank *bool` 字段，在 Prefetch/QueuePrefetch 中从 opts 读取并应用上限 50：
```go
po := applyOptions(opts...)
topK := 5
if po.TopK > 0 {
    topK = min(po.TopK, 50)
}
rerank := p.rerank
if po.Rerank != nil {
    rerank = *po.Rerank
}
```

---

### S-03: SyncTurn 错误时行为不一致 — Python 吞错返回 None，Go 传播 error

**问题**: Python `sync_turn()` 在异常时只调用 `_record_failure()` + `logger.warning()` 然后静默返回 `None`（不抛异常）。Go `SyncTurn()` 在 `client.add()` 失败时**返回 error**。Python 的设计意图是"同步失败不应阻塞对话"，Go 的实现让同步失败变成可阻断的错误。

**Python**:
```python
async def sync_turn(self, user_msg: str, assistant_msg: str, **kwargs: Any) -> None:
    if self._is_breaker_open() or not user_msg or not assistant_msg:
        return
    try:
        ...
        await self._client_call("add", messages, **self._write_filters())
        self._record_success()
    except Exception as exc:
        self._record_failure()
        logger.warning("Mem0 sync failed: %s", exc)
        # 不 re-raise，静默返回 None
```

**Go** (`mem0_provider.go`):
```go
func (p *Mem0Provider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, _ ...ProviderOption) error {
    ...
    err := client.add(ctx, messages, p.writeFilters(), nil)
    if err != nil {
        p.recordFailure()
        logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn failed")
        return err  // ❌ Python 不传播错误，Go 传播了
    }
```

**修复方案**: 对齐 Python，SyncTurn 失败时应 `return nil`：
```go
if err != nil {
    p.recordFailure()
    logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn failed")
    return nil  // 吞掉错误，对齐 Python 韧性边界
}
```

---

### M-01: Initialize 未处理 api_key kwargs 覆盖

**问题**: Python `initialize()` 允许通过 `kwargs["api_key"]` 覆盖构造时的 api_key，Go 没有 `WithAPIKey` 选项。

**Python**:
```python
self._api_key = kwargs.get("api_key") or self._api_key
```

**修复方案**: 在 `ProviderOptions` 添加 `APIKey string` 字段和 `WithAPIKey` 选项。

---

### M-02: unwrapResults 处理 dict 格式空结果时有边界问题

**问题**: Go `unwrapResults` 先尝试 dict 格式，如果 `len(dictResp.Results) > 0` 才返回。当 API 返回 `{"results": []}` 时，dict 格式反序列化成功但 Results 长度为 0，会继续尝试 list 格式反序列化，list 解析可能失败（因为顶层不是 array），最终返回 `nil, nil`。

**Python**:
```python
@staticmethod
def _unwrap_results(response: Any) -> list[dict[str, Any]]:
    if isinstance(response, dict):
        return response.get("results", [])
    if isinstance(response, list):
        return response
    return []
```

**Go**:
```go
func unwrapResults(data []byte) ([]mem0MemoryItem, error) {
    var dictResp mem0SearchResponse
    if err := json.Unmarshal(data, &dictResp); err == nil && len(dictResp.Results) > 0 {
        return dictResp.Results, nil  // ❌ 空列表时不会走这个分支
    }
    var listResp []mem0MemoryItem
    if err := json.Unmarshal(data, &listResp); err == nil {
        return listResp, nil
    }
    return nil, nil  // ❌ 两种都不匹配时返回 nil 而非空列表
}
```

**修复方案**:
```go
var dictResp mem0SearchResponse
if err := json.Unmarshal(data, &dictResp); err == nil {
    return dictResp.Results, nil  // dict 格式成功就返回，无论空不空
}
var listResp []mem0MemoryItem
if err := json.Unmarshal(data, &listResp); err == nil {
    return listResp, nil
}
return []mem0MemoryItem{}, nil  // 返回空切片而非 nil
```

---

### M-03: 熔断器字段无并发保护

**问题**: `consecutiveFailures` 和 `breakerOpenUntil` 字段在多个 goroutine 中读写（QueuePrefetch 启动的后台 goroutine 会调用 `recordSuccess`/`recordFailure`/`isBreakerOpen`，主 goroutine 也会调用），但没有用 mutex 或 atomic 保护。Python 中由于 GIL 和 asyncio 单线程事件循环，不存在此问题。

**Go**:
```go
type Mem0Provider struct {
    mu sync.Mutex  // 只保护 client 延迟初始化
    consecutiveFailures int    // ❌ 无保护
    breakerOpenUntil time.Time // ❌ 无保护
}
```

**修复方案**: 使用 `atomic.Int64` 保护 `consecutiveFailures`，使用 `atomic.Int64` 存储 `breakerOpenUntil` 的 unix timestamp：
```go
consecutiveFailures atomic.Int64
breakerOpenUntilTs  atomic.Int64  // unix timestamp
```

---

### M-04: QueuePrefetch 硬编码 topK=5，忽略 opts

**问题**: 与 S-02 同源，QueuePrefetch 函数体完全忽略 opts 参数。

**修复方案**: 同 S-02 修复方案。

---

### T-01: handleSearch 中 top_k 解析不处理 string 和 json.Number 类型

**Python**:
```python
int(args.get("top_k", 10))  # 对任何类型都尝试转 int
```

**Go**: 只处理 `float64` 和 `int` 类型，字符串 `"20"` 会被忽略使用默认值 10。

**修复方案**: 添加 `case string` 分支尝试 `strconv.Atoi`。

---

### T-02: handleSearch 中 rerank 解析只处理 bool 类型

**Python**:
```python
bool(args.get("rerank", False))  # 字符串 "true" 会转为 True
```

**Go**: 只处理 `bool` 类型。

**修复方案**: 添加 `case string` 分支尝试 `strconv.ParseBool`。

---

### T-03: recordFailure 日志使用中文而非英文

**Go**: `Msg("[Mem0Provider] 熔断器开启")`，根据项目日志规范（见 `log-language-english.md`），logger.Msg 应保留英文。

**修复方案**: 改为 `Msg("[Mem0Provider] Circuit breaker opened")`。

---

### T-04: ProviderOptions 缺少 TopK 和 Rerank 字段

**修复方案**: 扩展 `ProviderOptions`：
```go
type ProviderOptions struct {
    UserID    string
    ScopeID   string
    SessionID string
    TopK      int     // 默认 0 表示未设置
    Rerank    *bool   // nil 表示未设置
    APIKey    string  // 用于 Initialize 覆盖
}
```

---

### T-05: unwrapResults 返回 nil 而非空切片

**问题**: 两种格式都不匹配时返回 `nil, nil`，下游如果用 `== nil` 检查会出问题。

**修复方案**: 返回 `[]mem0MemoryItem{}, nil`。

---

### T-06: joinLines 和 joinParams 可以用 strings.Join 替代

**修复方案**: 使用 `strings.Join(lines, "\n")` 和 `strings.Join(params, "&")` 替代手写循环。

---

## 二、7.15 OpenVikingProvider

### S-01: truncStr 按字节截断而非按字符截断，CJK 字符串可能在多字节中间截断

**问题**: Python 的 `user_msg[:4000]` 按字符截断，Go 的 `truncStr` 用 `len(s)` 和 `s[:maxLen]` 按字节截断。对 CJK 字符串，4000 字节大约只能容纳 1333 个中文字符（Python 4000 字符可容纳 4000 个）。更严重的是，`s[:4000]` 可能正好在一个 UTF-8 多字节序列中间截断，产生无效 UTF-8。

**Python**:
```python
"user_msg[:4000]"  # 按字符截断，不会产生非法编码
```

**Go** (`viking_provider.go`):
```go
func truncStr(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen]  // 按字节截断，CJK 可能在字节中间截断
}
```

**修复方案**:
```go
func truncStr(s string, maxLen int) string {
    if utf8.RuneCountInString(s) <= maxLen {
        return s
    }
    runes := []rune(s)
    return string(runes[:maxLen])
}
```
同样的问题也存在于 `handleVikingRead` 中的 `contentStr[:8000]` 截断。

---

### S-02: handleVikingSearch 中 score 判断 rawScore > 0 无法区分缺失和零值

**问题**: Python 中 `raw_score is not None` 用来区分"score 字段缺失/为 None"和"score 字段为 0.0"。Go 的 `floatVal` 对缺失字段返回 0.0，无法区分。Go 代码用 `if rawScore > 0` 来判断是否 round，如果 score 是负数（虽不太可能），Go 会输出 `0.0` 而非 round 后的负数。

**Python**:
```python
raw_score = item.get("score")
sort_score = raw_score if raw_score is not None else 0.0
entry["score"] = round(raw_score, 3) if raw_score is not None else 0.0
```

**Go**:
```go
rawScore := floatVal(itemMap["score"])
sortScore := rawScore
if rawScore > 0 {  // 应该检查 score key 是否存在，而非 > 0
    entry["score"] = roundTo3(rawScore)
} else {
    entry["score"] = 0.0
}
```

**修复方案**:
```go
var rawScore float64
scoreVal, hasScore := itemMap["score"]
if hasScore && scoreVal != nil {
    rawScore = floatVal(scoreVal)
    entry["score"] = roundTo3(rawScore)
} else {
    rawScore = 0.0
    entry["score"] = 0.0
}
sortScore := rawScore
```

---

### M-01: handleVikingSearch 中 args["limit"] 只处理数值类型

**问题**: Python `args.get("limit")` 直接赋值给 `payload["top_k"]` 保持原始类型，Go 只处理 `float64`。如果 LLM 工具调用传字符串 limit，Go 的 `floatVal` 会返回 0 导致 limit 被忽略。

**Python**:
```python
if args.get("limit"):
    payload["top_k"] = args["limit"]  # 直接赋值
```

**Go**:
```go
if limit := floatVal(args["limit"]); limit > 0 {
    payload["top_k"] = int(limit)  // 只处理数值类型
}
```

**修复方案**: 检查 `args["limit"]` 是否为非零值并做类型断言回退。

---

### M-02: HandleToolCall 中 json.Marshal 转义非 ASCII 字符

**问题**: Python 的 `json.dumps(result, ensure_ascii=False)` 保留中文原文，Go 的 `json.Marshal` 默认转义非 ASCII 字符为 `\uXXXX`。LLM 看到的原始文本不同，可能影响模型理解质量。

**Python**:
```python
return json.dumps(result, ensure_ascii=False)  # 中文原文保留
```

**Go**:
```go
b, _ := json.Marshal(result)  // 默认转义非 ASCII 为 \uXXXX
return string(b), nil
```

**修复方案**:
```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.Encode(result)
return buf.String(), nil
```

---

### M-03: Initialize 缺少通用异常捕获

**问题**: Python 的 `initialize` 有三层异常处理：`ImportError`/`Exception`/health check 失败。Go 的 `Initialize` 只处理了 health check 失败，缺少通用异常捕获。

**Python**:
```python
except ImportError:
    logger.warning("httpx not installed — OpenViking disabled")
    self._client = None
except Exception as e:
    logger.warning("OpenViking init failed: %s", e)
    self._client = None
```

**修复方案**: 在 Initialize 中用 recover 捕获 panic：
```go
func (p *OpenVikingProvider) Initialize(ctx context.Context, opts ...ProviderOption) (retErr error) {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(vikingLogComponent).Msgf("OpenViking init failed: %v", r)
            p.client = nil
            p.initialized = false
            retErr = nil
        }
    }()
    // ...existing code...
}
```

---

### T-01: roundTo3 对负数四舍五入行为与 Python round 不一致

**Go**: `int(f*1000+0.5)` 对负数不正确。例如 `roundTo3(-0.1236)` = `int(-123.1)` = `-123` → `-0.123`，但 Python `round(-0.1236, 3)` = `-0.124`。

**修复方案**:
```go
func roundTo3(f float64) float64 {
    return math.Round(f*1000) / 1000
}
```

---

## 三、7.25 DistributedLock

### S-01: Release 非原子操作 — Get+Delete 竞态窗口（与 Python 行为一致，但为已知局限）

**问题**: Go 的 `Release` 先 `Get` 读取 lockValue，比较后再 `Delete`，两步不是原子操作。TTL 过期后可能误删他人持有的锁。Python 原版也有同样问题。

**Python**:
```python
async def release(self):
    try:
        lock_key = await self.store.get(self.lock_key)
        if lock_key == self.lock_value:
            await self.store.delete(self.lock_key)
    except Exception as e:
        ...
```

**Go** (`distributed_lock.go:88-103`):
```go
func (l *DistributedLock) Release(ctx context.Context) error {
    val, err := l.store.Get(ctx, l.lockKey)
    if err != nil { ... return nil }
    if string(val) == l.lockValue {
        if delErr := l.store.Delete(ctx, l.lockKey); delErr != nil { ... }
    }
    return nil
}
```

**修复方案**: 1:1 复刻 Python 行为，暂不需修复。标记为**已知局限**。未来 Redis 后端可用 Lua 脚本实现原子释放。

---

### S-02: logComponent 使用 ComponentAgentCore 而非设计文档规定的 ComponentCommon

**问题**: 设计文档明确指定 `logComponent` 应使用 `logger.ComponentCommon`（common 属于基础设施层），但实现使用了 `logger.ComponentAgentCore`。

**设计文档** (`docs/superpowers/specs/2028-01-20-distributed-lock-7.25-design.md`):
```
- 组件常量使用 `logger.ComponentCommon`（common 属于基础设施层）
```

**Go** (`distributed_lock.go:40`):
```go
var (
    logComponent = logger.ComponentAgentCore  // ❌ 应为 ComponentCommon
)
```

**修复方案**:
```go
var (
    logComponent = logger.ComponentCommon
)
```

---

### M-01: event_type 使用硬编码字符串 "MEMORY_STORE" 与 Python 枚举值大小写不一致

**Python**: `LogEventType.MEMORY_STORE` 枚举值 = `"memory_store"`（小写）
**Go**: `Str("event_type", "MEMORY_STORE")`（大写）

**修复方案**: 定义常量 `const logEventMemoryStore = "memory_store"`，与 Python 枚举值对齐。

---

### T-01: WithLock 的 defer 实现比设计文档更好

**Go 实际**: `defer func() { _ = l.Release(ctx) }()` — 更明确地表示有意忽略返回值。
**设计文档**: `defer l.Release(ctx)` — 更简洁但不明确。

**修复方案**: 不需要修改代码，建议同步更新设计文档。

---

## 四、9.68 Team Rails

### S-01: TeamToolRail 缺少 Runner.resource_mgr.add_tool 注册

**问题**: Python TeamToolRail.init() 在 ability_manager.add 之前先调用 `Runner.resource_mgr.add_tool(tools, refresh=True)`，Go 侧 Init 完全缺少这一步。没有 resource_mgr 注册，工具的 invoke 调度将无法解析到运行时实例。

**Python**:
```python
try:
    Runner.resource_mgr.add_tool(tools, refresh=True)
except Exception:
    team_logger.debug("Runner.resource_mgr not available, skipping tool registration")
```

**Go** (`team_tool_rail.go:123-132`):
```go
am := agent.AbilityManager()
if am != nil {
    for _, tl := range toolList {
        card := tl.Card()
        if card != nil {
            am.Add(card)
        }
    }
}
// ❌ 缺少 Runner.resource_mgr.add_tool 等价逻辑
```

**修复方案**: 在 am.Add 之前，添加 Runner.resource_mgr 等价的工具实例注册。若 Go 侧 resource_mgr 未实现则标注 TODO。

---

### S-02: TeamToolRail 缺少 priority=90 设置

**问题**: Python TeamToolRail 声明 `priority = 90`，Go 侧嵌入 DeepAgentRail 但从未调用 `WithPriority(90)`，默认使用 BaseRail 的 priority=50。Priority 90 意味着在大多数其他 rail 之后执行，Go 侧默认 50 会导致执行顺序错乱。

**Python**:
```python
class TeamToolRail(DeepAgentRail):
    priority = 90
```

**Go**: 无任何 priority 设置，默认 50。

**修复方案**:
```go
func NewTeamToolRail(opts ...TeamToolRailOption) *TeamToolRail {
    r := &TeamToolRail{
        DeepAgentRail: *harnessrails.NewDeepAgentRail(),
    }
    r.WithPriority(90)
    // ...
}
```

---

### S-03: TeamPolicyRail 缺少 priority=12 设置

**问题**: Python TeamPolicyRail 声明 `priority = 12`，Go 侧未设置，默认 50。Priority 12 意味着在大多数 rail 之前执行 BeforeModelCall（注入系统提示词），顺序错误会导致其他 rail 在提示词未注入时就执行。

**Python**:
```python
class TeamPolicyRail(DeepAgentRail):
    priority = 12
```

**修复方案**: 在 NewTeamPolicyRail 中设置 `r.WithPriority(12)`。

---

### S-04: TeamToolApprovalRail 需确认 priority=90 是否正确设置

**问题**: Python TeamToolApprovalRail 继承 ConfirmInterruptRail（priority=90），Go 侧通过 `interrupt.NewBaseInterruptRail(toolNames...)` 创建。需确认 `NewBaseInterruptRail` 是否正确设置了 priority=90。

**修复方案**: 验证 `interrupt_base.go` 中 `baseInterruptRailPriority = 90` 常量是否被构造函数使用。如未使用则手动设置。

---

### S-05: TeamPlanModeRail BeforeModelCall 缺少 plan_mode 状态检查

（与 9.69 S-02 / 9.28 S-02 重复，此处合并）

**问题**: Python `before_model_call` 检查 `state.plan_mode.mode == "plan"` 才注入，否则移除 section。Go 侧完全跳过此检查，始终注入 team.plan 指令。

**Python**:
```python
state = self._agent.load_state(ctx.session)
if getattr(state.plan_mode, "mode", None) != "plan":
    self.system_prompt_builder.remove_section(SectionName.MODE_INSTRUCTIONS)
    return
```

**Go**:
```go
// TODO(#9.runtime): 集成 agent plan_mode 状态检查
// 缺少 plan_mode 检查，总是注入
```

**修复方案**: 实现 plan_mode 状态检查，或在 BaseAgent 接口添加 LoadState/PlanMode 访问方法。**当前状态会导致非 plan 模式下也注入 team.plan 指令，严重影响 agent 行为**。

**流程示例**:

```
正常流程（Python）:
  1. BeforeModelCall 被调用
  2. 检查 state.plan_mode.mode
  3. 如果 != "plan" → 移除 MODE_INSTRUCTIONS section → return
  4. 如果 == "plan" → specializePlanAgent() → 注入 team.plan 指令

Go 当前流程（有 bug）:
  1. BeforeModelCall 被调用
  2. 跳过状态检查
  3. 始终注入 team.plan 指令  ← BUG：build_mode 等非 plan 模式也被注入
```

---

### S-06: TeamPlanModeRail specializePlanAgent 是空壳

（与 9.69 S-03 / 9.28 S-03 重复，此处合并）

**问题**: Python `_specialize_plan_agent()` 调用 `apply_team_plan_agent_prompt()` 替换默认 plan_agent 的 system prompt。Go 侧完全空实现。

**Python**:
```python
def _specialize_plan_agent(self) -> None:
    if self._agent is None:
        return
    deep_config = getattr(self._agent, "deep_config", None)
    applied = apply_team_plan_agent_prompt(
        getattr(deep_config, "subagents", None),
        language=self._resolve_language(),
    )
    if applied:
        team_logger.info("[team.plan] specialized built-in plan_agent prompt")
```

**Go**:
```go
func (r *TeamPlanModeRail) specializePlanAgent() {
    if r.agent == nil {
        return
    }
    // TODO(#9.runtime): 集成 apply_team_plan_agent_prompt 逻辑
}
```

**修复方案**: 实现 `prompts.ApplyTeamPlanAgentPrompt` 函数并在 `specializePlanAgent()` 中调用。

---

### M-07: TeamPolicyRail.Init 缺少 super().init(agent) 调用

**问题**: Python TeamPolicyRail.init 调用 `super().init(agent)`，Go 侧未调用基类 Init。

**Python**:
```python
def init(self, agent):
    super().init(agent)
    self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
```

**Go**:
```go
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
    r.systemPromptBuilder = agent.SystemPromptBuilder()
    return nil  // ❌ 缺少 r.DeepAgentRail.Init(ctx, agent)
}
```

**修复方案**: 添加 `if err := r.DeepAgentRail.Init(ctx, agent); err != nil { return err }`。

---

### M-08: TeamPlanModeRail.Init 缺少 super().init(agent) 调用

**修复方案**: 同 M-07。

---

### M-09: TeamPolicyRail.BeforeModelCall 使用 context.Background() 丢弃上层 context

**Go**:
```go
func (r *TeamPolicyRail) BeforeModelCall(_ context.Context, ...) error {
    ...
    ctx := context.Background()
    infoSection := r.infoCache.Refresh(ctx)
```

**修复方案**: 使用传入的 ctx 参数。

---

### M-10: TeamToolApprovalRail.resolveInterrupt 使用 context.Background()

**修复方案**: 将 ctx 传播到 SendMessage 调用。

---

### M-11: TeamToolApprovalRail.isAutoConfirmedSimple 仅支持 bool，应复用 interrupt.isAutoConfirmed

**问题**: Go 侧 `isAutoConfirmedSimple` 仅支持 `bool` 类型断言，而 interrupt 包的 `isAutoConfirmed` 支持 bool/int/float64/string。

**修复方案**: 删除 `isAutoConfirmedSimple`，改用 `interrupt.isAutoConfirmed`。

---

### M-12: CreateTeamTools 缺少 _wrap_invoke_with_logging 等价功能

**Python**:
```python
for tool in tools:
    _wrap_invoke_with_logging(tool)
```

**修复方案**: 在 CreateTeamTools 返回前添加日志包装逻辑。

---

### M-13: 所有 14 个 TeamTool 的 Invoke 方法均未实现

**Go**:
```go
func (t *BuildTeamTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
    return nil, fmt.Errorf("BuildTeamTool.Invoke 未实现")
}
// ... 同样模式重复 14 次
```

**修复方案**: 逐个实现工具的 Invoke 方法，对齐 Python 中对应工具的逻辑。标记为后续回填项。

---

### T-14: TeamToolRail 的 workspaceManager/worktreeManager 使用 any 类型占位

**Go**:
```go
workspaceManager any // TODO(#9.66): TeamWorkspaceManager 类型
worktreeManager any // TODO(#9.66a): WorktreeManager 类型
```

**Python**:
```python
if self._workspace_manager is not None:
    tools.append(WorkspaceMetaTool(self._workspace_manager, ws_t))
if self._worktree_manager is not None:
    tools.append(EnterWorktreeTool(self._worktree_manager, language=self._language))
```

**修复方案**: 当类型可用后替换 any 并实现工具追加逻辑。

---

### T-15: TeamPolicyRail doc.go "8个PromptSection" 描述不够精确

**修复方案**: 改为 "6 个静态 + 2 个动态 PromptSection" 以更准确。

---

## 五、9.69 Team Prompts

### S-01: MtimeSectionCache.Invalidate() 不清除 cached 和 cachedMtime

**问题**: Python `invalidate()` 清除三个字段，Go 只清除 `initialized`。

**Python**:
```python
def invalidate(self) -> None:
    self._cached_section = None
    self._cached_mtime = 0
    self._initialized = False
```

**Go**:
```go
func (c *MtimeSectionCache) Invalidate() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.initialized = false
    // ❌ 缺少 c.cached = nil 和 c.cachedMtime = 0
}
```

**修复方案**:
```go
func (c *MtimeSectionCache) Invalidate() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cached = nil
    c.cachedMtime = 0
    c.initialized = false
}
```

---

### S-02: TeamPlanModeRail.BeforeModelCall 未检查 plan_mode 状态

（与 9.68 S-05 / 9.28 S-02 合并，详见上文）

---

### S-03: TeamPlanModeRail.specializePlanAgent() 空实现

（与 9.68 S-06 / 9.28 S-03 合并，详见上文）

---

### S-04: ApplyTeamPlanAgentPrompt 函数未实现

**问题**: Python `__init__.py` 导出 `apply_team_plan_agent_prompt`，Go 的 `team_plan_agent.go` 只实现了 `BuildTeamPlanAgentCard` 和常量，没有 `ApplyTeamPlanAgentPrompt` 函数。这是 plan subagent 特化的核心函数。

**Python**:
```python
def apply_team_plan_agent_prompt(subagents, *, language=None) -> bool:
    if not subagents:
        return False
    resolved_language = resolve_language(language)
    builtin_prompts = set(DEFAULT_PLAN_AGENT_SYSTEM_PROMPT.values())
    for spec in subagents:
        if not isinstance(spec, SubAgentConfig):
            continue
        if spec.agent_card.name != "plan_agent":
            continue
        if spec.system_prompt not in builtin_prompts:
            return False
        spec.system_prompt = _team_plan_agent_prompt(resolved_language)
        spec.agent_card = spec.agent_card.model_copy(
            update={"description": _team_plan_agent_description(resolved_language)}
        )
        return True
    return False
```

**修复方案**: 在 `team_plan_agent.go` 中实现 `ApplyTeamPlanAgentPrompt`，逻辑：
1. 遍历 subagents 找到 `name == "plan_agent"` 的 SubAgentConfig
2. 检查其 system_prompt 是否匹配默认值集合
3. 替换为 team_plan_agent prompt 和 description
4. 返回是否成功替换

---

### S-05: BuildTeamPlanModeSection 函数未实现

**问题**: Python 的 `build_team_plan_mode_section` 根据 agent/session 状态动态生成 `enter_plan_mode_status` 和 `plan_file_info`。Go 版本直接传空字符串给 `BuildTeamPlanModePrompt`，导致模板中关键占位符被替换为空。

**Python**:
```python
def build_team_plan_mode_section(
    *, language, agent, session
) -> PromptSection:
    resolved_language = resolve_language(language)
    content = build_team_plan_mode_prompt(
        resolved_language,
        enter_plan_mode_status=_build_enter_plan_mode_status(agent, session, resolved_language),
        plan_file_info=_build_plan_file_info(agent, session, resolved_language),
    )
    return PromptSection(
        name=SectionName.MODE_INSTRUCTIONS,
        content={resolved_language: content},
        priority=85,
    )
```

**Go**:
```go
content := prompts.BuildTeamPlanModePrompt(language, "", "")  // 空串！
```

**流程示例**:
```
Python 正常流程:
  1. before_model_call 被调用
  2. 检查 plan_mode == "plan"
  3. 调用 _build_enter_plan_mode_status(agent, session, language)
     → 如果未调用过 enter_plan_mode: "你尚未调用 enter_plan_mode。请立即调用它。"
     → 如果已调用: "你已处于 plan 模式。"
  4. 调用 _build_plan_file_info(agent, session, language)
     → 如果 plan 文件存在: "Plan 文件路径: /path/to/plan.md"
     → 如果不存在: "Plan 文件尚不存在。"
  5. 将这些信息注入模板

Go 当前流程（有 bug）:
  1. before_model_call 被调用
  2. 跳过状态检查
  3. 传空串给模板 → 占位符被替换为空 → 用户看不到关键提示
```

**修复方案**: 实现 `BuildTeamPlanModeSection` 函数，或在 rail 中正确计算 `enterPlanModeStatus` 和 `planFileInfo`。如当前无法获取 agent/session 状态，至少使用合理的兜底文本。

---

### S-06: Section priority 84 应为 85

**问题**: Python `build_team_plan_mode_section` 返回 `PromptSection(priority=85)`，Go 使用 `84`。注释误将 rail priority(84) 当作 section priority。

**Python**:
```python
return PromptSection(
    name=SectionName.MODE_INSTRUCTIONS,
    content={resolved_language: content},
    priority=85,
)
```

**Go**:
```go
section := saprompt.NewPromptSection(
    modeInstructionsSectionName,
    map[string]string{language: content},
    84, // ❌ 应为 85
)
```

**修复方案**: 将 `84` 改为 `85`。

---

### M-01: RolePolicy 使用 string 而非 TeamRole

**Python**:
```python
def role_policy(role: TeamRole, language: str = "cn") -> str:
```

**Go**:
```go
func RolePolicy(role string, language string) string {
```

**修复方案**: 将 `role` 参数改为 `atschema.TeamRole`。

---

### M-02: BuildSystemPrompt 使用 string 而非 TeamRole

**修复方案**: 同 M-01。

---

### M-03: BuildTeamPlanModePrompt 硬编码替换而非使用 PromptTemplate.Render

**Python**:
```python
return get_team_plan_mode_prompt(language).format(
    enter_plan_mode_status=enter_plan_mode_status,
    plan_file_info=plan_file_info,
)
```

**Go**:
```go
return strings.ReplaceAll(strings.ReplaceAll(tpl,
    "{enter_plan_mode_status}", enterPlanModeStatus),
    "{plan_file_info}", planFileInfo)
```

**修复方案**: 改用 `prompts.PromptTemplate` 的 `Render` 方法。

---

### M-04: policy.go 和 sections.go 重复定义 workflowTemplates

**Go**:
```go
// policy.go
var workflowTemplates = map[string]string{...}
// sections.go
var workflowTemplateName = map[string]string{...}  // 内容完全相同
```

**修复方案**: 保留一份，另一处引用之。

---

### M-05: TeamPolicyRail.BeforeModelCall 使用 context.Background()

（与 9.68 M-09 合并）

---

### T-01: doc.go 提及未实现的函数

**修复方案**: 实现函数后更新 doc.go，或在 doc.go 中标注实际内容。

---

### T-02: 缺少 resolve_language 对齐

**修复方案**: 在 prompts 包中添加 `ResolveLanguage(language string) string` 工具函数。

---

### T-03: BuildTeamPlanModePrompt 关键参数传空字符串

**修复方案**: 至少用合理的默认值替换空字符串。

---

### T-04: policy.go 和 sections.go labels 部分重复

**修复方案**: 低优先级，可考虑统一为一套 labels。

---

## 六、9.28 PlanAgent 回填

### S-01: TeamPlanModeRail 未初始化嵌入的 DeepAgentRail

**问题**: `NewTeamPlanModeRail()` 创建 `&TeamPlanModeRail{}` 但未初始化嵌入的 `harnessrails.DeepAgentRail`。Python 的 `__init__` 调用 `super().__init__()` 完成基类初始化。Go 中零值初始化导致 `BaseRail.priority=0`（应为 84）。

**Python**:
```python
class TeamPlanModeRail(DeepAgentRail):
    priority = 84
    def __init__(self, *, language=None):
        super().__init__()  # 初始化 DeepAgentRail
```

**Go**:
```go
func NewTeamPlanModeRail(opts ...TeamPlanModeRailOption) *TeamPlanModeRail {
    r := &TeamPlanModeRail{}  // DeepAgentRail 零值初始化！priority=0
```

**修复方案**:
```go
func NewTeamPlanModeRail(opts ...TeamPlanModeRailOption) *TeamPlanModeRail {
    r := &TeamPlanModeRail{
        DeepAgentRail: *harnessrails.NewDeepAgentRail(),
    }
    r.WithPriority(84)
    for _, opt := range opts {
        opt(r)
    }
    return r
}
```

---

### S-02: BeforeModelCall 跳过 plan_mode 状态检查

（与 9.68 S-05 / 9.69 S-02 合并，详见上文）

---

### S-03: specializePlanAgent 空壳 + apply_team_plan_agent_prompt 未实现

（与 9.68 S-06 / 9.69 S-03/S-04 合并，详见上文）

---

### S-04: BuildPlanAgentConfig 缺少默认 [SysOperationRail()]

**问题**: Python 的 `build_plan_agent_config` 设置 `rails=rails if rails is not None else [SysOperationRail()]`，Go 侧直接赋值 `cfg.Rails = params.Rails`，当 params.Rails 为 nil 时 PlanAgent 无任何 Rail。

**Python**:
```python
rails=rails if rails is not None else [SysOperationRail()],
```

**Go** (`plan_agent.go:122`):
```go
cfg.Rails = params.Rails  // ❌ 缺少默认 [SysOperationRail()]
```

**修复方案**:
```go
if params.Rails != nil {
    cfg.Rails = params.Rails
} else {
    cfg.Rails = []agentinterfaces.AgentRail{rails.NewSysOperationRail()}
}
```

---

### S-05: PromptSection priority 不一致 — Go 用 84，Python 用 85

（与 9.69 S-06 合并，详见上文）

---

### S-06: BuildTeamPlanModePrompt 传空串，模板占位符未渲染

（与 9.69 S-05 合并，详见上文）

---

### M-01: 缺少 CreatePlanAgent 函数

**Python**: 同时提供 `build_plan_agent_config()` 和 `create_plan_agent()`。

**修复方案**: 标记为后续回填项。

---

### M-02: resolveLanguage 未调用 hprompts.ResolveLanguage 做语言规范化

**Go**:
```go
func (r *TeamPlanModeRail) resolveLanguage() string {
    if r.languageOverride != "" {
        return r.languageOverride
    }
    if r.systemPromptBuilder != nil {
        lang := r.systemPromptBuilder.Language()
        if lang != "" {
            return lang  // ❌ 未规范化
        }
    }
    return "cn"
}
```

**修复方案**: 调用 `hprompts.ResolveLanguage(lang)` 做规范化。

---

### T-01: 缺少 specialize 成功日志

**修复方案**: 实现 specializePlanAgent 后同步添加 Info 日志。

---

### T-02: BuildTeamPlanModePrompt 使用 strings.ReplaceAll 而非 Python format 风格

（与 9.69 M-03 合并）

---

## 去重后的问题总表

以下为去重合并后的完整问题列表（跨章节重复的已合并）：

### 严重问题（S）— 15 个

| 编号 | 模块 | 问题 | 首现章节 |
|------|------|------|----------|
| S-01 | Mem0Provider | Initialize 未处理 rerank kwargs 覆盖 | 7.14 |
| S-02 | Mem0Provider | Prefetch/QueuePrefetch 完全忽略 opts 中的 top_k/rerank | 7.14 |
| S-03 | Mem0Provider | SyncTurn 失败时传播 error，Python 静默吞错 | 7.14 |
| S-04 | VikingProvider | truncStr 按字节截断，CJK 可能在多字节中间截断 | 7.15 |
| S-05 | VikingProvider | handleVikingSearch score 判断 rawScore>0 无法区分缺失和零值 | 7.15 |
| S-06 | DistributedLock | logComponent 应为 ComponentCommon 而非 ComponentAgentCore | 7.25 |
| S-07 | TeamToolRail | 缺少 Runner.resource_mgr.add_tool 注册 | 9.68 |
| S-08 | TeamToolRail | 缺少 priority=90 设置（默认 50 导致执行顺序错乱） | 9.68 |
| S-09 | TeamPolicyRail | 缺少 priority=12 设置（默认 50 导致执行顺序错乱） | 9.68 |
| S-10 | TeamPlanModeRail | 未初始化嵌入的 DeepAgentRail，priority=0 | 9.28 |
| S-11 | TeamPlanModeRail | BeforeModelCall 跳过 plan_mode 状态检查，非 plan 模式也注入指令 | 9.68/9.69/9.28 |
| S-12 | TeamPlanModeRail | specializePlanAgent 空壳 + ApplyTeamPlanAgentPrompt 未实现 | 9.68/9.69/9.28 |
| S-13 | TeamPrompts | BuildTeamPlanModeSection 未实现，plan_mode 状态/路径传空字符串 | 9.69 |
| S-14 | TeamPrompts | Section priority 84 应为 85（混淆 rail priority 和 section priority） | 9.69 |
| S-15 | PlanAgent | BuildPlanAgentConfig 缺少默认 [SysOperationRail()] | 9.28 |

### 一般问题（M）— 14 个

| 编号 | 模块 | 问题 | 首现章节 |
|------|------|------|----------|
| M-01 | Mem0Provider | Initialize 未处理 api_key kwargs 覆盖 | 7.14 |
| M-02 | Mem0Provider | unwrapResults dict 格式空结果处理有边界问题 | 7.14 |
| M-03 | Mem0Provider | 熔断器字段无并发保护 | 7.14 |
| M-04 | VikingProvider | handleVikingSearch args["limit"] 只处理数值类型 | 7.15 |
| M-05 | VikingProvider | json.Marshal 转义非 ASCII 字符，与 ensure_ascii=False 不一致 | 7.15 |
| M-06 | VikingProvider | Initialize 缺少通用异常捕获 | 7.15 |
| M-07 | DistributedLock | event_type 硬编码 "MEMORY_STORE" 与 Python 枚举值 "memory_store" 大小写不一致 | 7.25 |
| M-08 | TeamPolicyRail | Init 缺少 super().init 调用 | 9.68 |
| M-09 | TeamPlanModeRail | Init 缺少 super().init 调用 | 9.68 |
| M-10 | TeamPolicyRail/TeamToolApprovalRail | BeforeModelCall/resolveInterrupt 使用 context.Background() 丢弃上层 context | 9.68 |
| M-11 | TeamToolApprovalRail | isAutoConfirmedSimple 仅支持 bool，应复用 interrupt.isAutoConfirmed | 9.68 |
| M-12 | TeamPrompts | RolePolicy/BuildSystemPrompt 使用 string 而非 TeamRole | 9.69 |
| M-13 | TeamPrompts | BuildTeamPlanModePrompt 硬编码替换而非使用 PromptTemplate.Render | 9.69 |
| M-14 | TeamPrompts | policy.go 和 sections.go 重复定义 workflowTemplates | 9.69 |

### 提示问题（T）— 12 个

| 编号 | 模块 | 问题 | 首现章节 |
|------|------|------|----------|
| T-01 | Mem0Provider | handleSearch top_k 解析不处理 string/json.Number | 7.14 |
| T-02 | Mem0Provider | handleSearch rerank 解析只处理 bool | 7.14 |
| T-03 | Mem0Provider | recordFailure 日志使用中文而非英文 | 7.14 |
| T-04 | Mem0Provider | ProviderOptions 缺少 TopK/Rerank/APIKey 字段 | 7.14 |
| T-05 | Mem0Provider | unwrapResults 返回 nil 而非空切片 | 7.14 |
| T-06 | VikingProvider | roundTo3 对负数四舍五入行为与 Python round 不一致 | 7.15 |
| T-07 | DistributedLock | WithLock defer 实现比设计文档更好，建议同步文档 | 7.25 |
| T-08 | TeamTools | 14 个 TeamTool.Invoke 均未实现 | 9.68 |
| T-09 | TeamToolRail | workspaceManager/worktreeManager 为 any 占位 | 9.68 |
| T-10 | TeamPrompts | doc.go 提及未实现的函数 | 9.69 |
| T-11 | TeamPrompts | 缺少 resolve_language 对齐 | 9.69 |
| T-12 | TeamPrompts | BuildTeamPlanModePrompt 关键参数传空字符串 | 9.69 |

---

## 正面发现

### 7.14 Mem0Provider
- 所有 Python 方法在 Go 中都有对应实现，无缺失方法
- 双重熔断器逻辑（consecutiveFailures + breakerOpenUntil）完整对齐 Python
- QueuePrefetch goroutine + context cancel 对齐 Python asyncio.create_task + task.cancel
- mem0_client HTTP 客户端行为对齐 Python httpx

### 7.15 OpenVikingProvider
- 12 个 Python 方法全部有 Go 对应，无缺失
- Prefetch（memories + resources 各取前3）格式化正确
- SyncTurn 两次 POST + sessionID fallback 逻辑正确
- 5 个工具（search/read/browse/remember/add_resource）完整分发
- 环境变量 fallback 全部正确
- Shutdown close + nil + recover 对齐 Python

### 7.25 DistributedLock
- 结构体字段、构造函数参数、Acquire/Release/WithLock 方法逻辑与 Python 行为一致
- 测试覆盖率完整

### 9.68 Team Rails
- FirstIterationGate channel 替代 asyncio.Event 实现正确
- 14 ToolCard 全部定义
- CreateTeamTools + QualifyTeamToolIDs 逻辑对齐 Python

### 9.69 Team Prompts
- 8 个 section builder 全部实现且逻辑正确对齐 Python
- 所有 18 个 markdown 模板文件内容与 Python 完全一致
- HITT section 6 种变体文本一比一复刻
- 双语标签 `_LABELS` 完整对齐
- Section 优先级（11/12/13/14/15/16/65/66）对齐 Python
- loader 使用 go:embed + sync.Map 缓存对齐 Python @cache

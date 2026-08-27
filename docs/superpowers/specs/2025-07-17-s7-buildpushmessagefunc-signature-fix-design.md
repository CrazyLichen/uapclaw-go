# S7 修复设计：BuildPushMessageFunc 参数顺序对齐 BuildServerPushMessage

> 审查编号：S7（来自 2026-07-16-logic-review.md）
> 影响章节：10.3.9 EvolutionHelpers

## 1. 问题

`BuildPushMessageFunc` 的参数顺序与 `BuildServerPushMessage` 不兼容，导致消费者无法直接将 `BuildServerPushMessage` 作为回调传入，必须手写闭包适配（且闭包中参数语义靠人肉对齐，编译器无法检查）。

### 1.1 当前签名对比

```go
// evolution/helpers.go — 函数类型定义
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any
//                                   1         2              3                4

// session/session_metadata.go — 实际实现
func BuildServerPushMessage(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any
//                                1         2          3                    4
```

参数 3 和 4 的位置对调了。由于参数数量和类型均不同，Go 编译器直接拒绝赋值。

### 1.2 Python 设计

Python 使用关键字参数，不存在位置问题：

```python
build_push_message(
    session_id=push_context.session_id,
    request_id=status_update.request_id,
    fallback_channel_id=push_context.channel_id,
    payload=payload,
)
```

## 2. 修复方案

将 `BuildPushMessageFunc` 参数顺序改为与 `BuildServerPushMessage` 一致，使消费者可直接传函数引用。

### 2.1 新签名

```go
type BuildPushMessageFunc func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any
```

变更点：
- `fallbackChannelID` 从第 3 个固定 `string` 参数 → 第 4 个 `...string` variadic 参数
- `payload` 从第 4 个参数 → 第 3 个参数
- `fallbackChannelID` 改为 variadic 以保留"可选"语义，对齐 Python `fallback_channel_id: str | None = None`

### 2.2 受影响的调用点（3 处）

**PushEvolutionStatus（helpers.go:569）**

```go
// 旧
msg := buildMsgFn(pushCtx.SessionID, update.RequestID, pushCtx.ChannelID, payload)
// 新
msg := buildMsgFn(pushCtx.SessionID, update.RequestID, payload, pushCtx.ChannelID)
```

**PushEvolutionEvent（helpers.go:592）**

```go
// 旧
msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, evt)
// 新
msg := buildMsgFn(pushCtx.SessionID, requestID, evt, pushCtx.ChannelID)
```

**PushEvolutionProgress（helpers.go:644）**

```go
// 旧
msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, parsed)
// 新
msg := buildMsgFn(pushCtx.SessionID, requestID, parsed, pushCtx.ChannelID)
```

### 2.3 受影响的测试（5 处）

`helpers_test.go` 中所有闭包需同步修改签名：

```go
// 旧
buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {

// 新
buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

## 3. 修复后的效果

消费者可以直接写：

```go
evolution.PushEvolutionStatus(ctx, pushCtx, update, session.BuildServerPushMessage)
```

编译器自动检查类型兼容性，无需手写闭包适配。

## 4. 不修复项

| 编号 | 问题 | 决策 | 理由 |
|------|------|------|------|
| G5 | `EvolutionPushContext.ChannelID` 为 `string` 而非 `*string` | 不修复 | 当前空字符串被 `BuildServerPushMessage` 正确过滤，功能正确；Go 中空字符串表示"无值"不罕见 |
| T13 | `EvolutionOutcomeFromEvent` 的 nil payload 检查永远不触发 | 不修复 | 保留冗余防御性代码，对齐 Python 端同样存在的冗余 `isinstance` 检查 |

## 5. 改动清单

| 文件 | 改动 |
|------|------|
| `internal/swarm/server/adapter/evolution/helpers.go` | 1) `BuildPushMessageFunc` 类型定义改签名；2) `PushEvolutionStatus` 内 buildMsgFn 调用改参数顺序；3) `PushEvolutionEvent` 内 buildMsgFn 调用改参数顺序；4) `PushEvolutionProgress` 内 buildMsgFn 调用改参数顺序 |
| `internal/swarm/server/adapter/evolution/helpers_test.go` | 5 处闭包签名同步修改 |

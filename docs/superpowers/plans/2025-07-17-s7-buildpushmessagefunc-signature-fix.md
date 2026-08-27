# S7 修复：BuildPushMessageFunc 参数顺序对齐

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 `BuildPushMessageFunc` 参数顺序，使其与 `BuildServerPushMessage` 一致，消费者可直接传函数引用而无需闭包适配

**Architecture:** 将 `BuildPushMessageFunc` 的 `fallbackChannelID` 从第 3 个固定 string 参数改为第 4 个 variadic `...string` 参数，`payload` 从第 4 位移到第 3 位。同步修改 3 处调用点和 5 处测试闭包。

**Tech Stack:** Go 1.24+

**Design Spec:** `docs/superpowers/specs/2025-07-17-s7-buildpushmessagefunc-signature-fix-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/swarm/server/adapter/evolution/helpers.go` | 类型定义 + 3 处调用 |
| Modify | `internal/swarm/server/adapter/evolution/helpers_test.go` | 5 处闭包签名 |

---

### Task 1: 修改 BuildPushMessageFunc 类型定义

**Files:**
- Modify: `internal/swarm/server/adapter/evolution/helpers.go:61-63`

- [ ] **Step 1: 修改类型定义**

将第 61-63 行：

```go
// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// 对齐 Python: build_server_push_message 回调参数
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any
```

改为：

```go
// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// 对齐 Python: build_server_push_message 回调参数（关键字参数，位置无关）。
// 参数顺序与 session.BuildServerPushMessage 一致，可直接赋值。
type BuildPushMessageFunc func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/evolution/...`
Expected: 编译失败（3 处调用点参数顺序不匹配）——这是预期的，下一步修复

---

### Task 2: 修改 PushEvolutionStatus 内调用

**Files:**
- Modify: `internal/swarm/server/adapter/evolution/helpers.go:569`

- [ ] **Step 1: 修改 buildMsgFn 调用**

将第 569 行：

```go
	msg := buildMsgFn(pushCtx.SessionID, update.RequestID, pushCtx.ChannelID, payload)
```

改为：

```go
	msg := buildMsgFn(pushCtx.SessionID, update.RequestID, payload, pushCtx.ChannelID)
```

---

### Task 3: 修改 PushEvolutionEvent 内调用

**Files:**
- Modify: `internal/swarm/server/adapter/evolution/helpers.go:592`

- [ ] **Step 1: 修改 buildMsgFn 调用**

将第 592 行：

```go
	msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, evt)
```

改为：

```go
	msg := buildMsgFn(pushCtx.SessionID, requestID, evt, pushCtx.ChannelID)
```

---

### Task 4: 修改 PushEvolutionProgress 内调用

**Files:**
- Modify: `internal/swarm/server/adapter/evolution/helpers.go:644`

- [ ] **Step 1: 修改 buildMsgFn 调用**

将第 644 行：

```go
		msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, parsed)
```

改为：

```go
		msg := buildMsgFn(pushCtx.SessionID, requestID, parsed, pushCtx.ChannelID)
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/evolution/...`
Expected: 编译失败（测试文件中 5 处闭包签名不匹配）——这是预期的，下一步修复

---

### Task 5: 修改测试文件中 5 处闭包签名

**Files:**
- Modify: `internal/swarm/server/adapter/evolution/helpers_test.go`

共 5 处闭包，签名均从旧格式改为新格式。

- [ ] **Step 1: 修改 TestPushEvolutionStatus 闭包（第 664 行）**

将：

```go
	buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {
```

改为：

```go
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

- [ ] **Step 2: 修改 TestPushEvolutionStatus_noRequestID 闭包（第 698 行）**

将：

```go
	buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {
```

改为：

```go
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

- [ ] **Step 3: 修改 TestPushEvolutionEvent 闭包（第 723 行）**

将：

```go
	buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {
```

改为：

```go
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

- [ ] **Step 4: 修改 TestPushEvolutionProgress 闭包（第 781 行）**

将：

```go
	buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {
```

改为：

```go
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

- [ ] **Step 5: 修改 TestPushEvolutionProgress_nilChunk 闭包（第 807 行）**

将：

```go
	buildMsgFn := func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any {
```

改为：

```go
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
```

---

### Task 6: 编译 + 测试验证 + 提交

- [ ] **Step 1: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/evolution/...`
Expected: 编译成功

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/evolution/... -v -cover`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 提交**

```
fix(evolution): align BuildPushMessageFunc signature with BuildServerPushMessage
```

---

## Self-Review

### Spec Coverage Check

| Spec Section | Task |
|-------------|------|
| §2.1 新签名 | Task 1 |
| §2.2 PushEvolutionStatus 调用 | Task 2 |
| §2.2 PushEvolutionEvent 调用 | Task 3 |
| §2.2 PushEvolutionProgress 调用 | Task 4 |
| §2.3 测试闭包 | Task 5 |

### Placeholder Scan

无 TBD/TODO/实现后补 — 所有步骤含具体代码。

### Type Consistency

- `BuildPushMessageFunc` 新签名 `func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string)` ✅
- `BuildServerPushMessage` 签名 `func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string)` ✅
- Task 2-4 中 `buildMsgFn` 调用参数顺序 `(sessionID, requestID, payload, fallbackChannelID)` ✅
- Task 5 中闭包签名与新类型定义一致 ✅

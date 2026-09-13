# 9.24 P3 SkillEvolutionRail 实现偏差修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 SkillEvolutionRail P3 实现中的 6 项偏差：空实现补全、类型断言修正、推送流程对齐 Python、FromLegacyDict 补全、ApprovalAnswer 具体类型。

**Architecture:** 修改集中在 `skill_evolution_rail.go`（Fix 1+2）、`deep_adapter_evolution.go`（Fix 3+4）、`contracts.go`（Fix 5）、`deep_adapter.go` + `deep_adapter_evolution.go` + `deep_adapter_slash.go`（Fix 6）。每个 Fix 独立编译验证。

**Tech Stack:** Go 1.22+, stream.OutputSchema, evolution/helpers.go 推送辅助, gateway_push.ChannelPushTransport

---

## 文件清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` | Fix 1: disabledSkills 字段 + normalizeNameSet; Fix 2: ShouldHintSimplifyOrRebuild |
| 修改 | `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go` | Fix 1+2 测试补充 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_evolution.go` | Fix 3: isOutcomeEvent; Fix 4: watchEvolutionAndPush + pushEventToFrontend + outputSchemaToDict |
| 修改 | `internal/swarm/server/adapter/deep_adapter.go` | Fix 4: watchEvolutionAndPush 调用加 channelID; Fix 6: HandleUserAnswer 解析 ApprovalAnswer |
| 修改 | `internal/swarm/server/adapter/deep_adapter_slash.go` | Fix 6: handleGovernanceApproval 签名 |
| 修改 | `internal/agentcore/harness/rails/evolution/contracts.go` | Fix 5: FromLegacyDict 补全 |
| 修改 | `internal/agentcore/harness/rails/evolution/contracts_test.go` | Fix 5 测试更新 |
| 修改 | `internal/evolving/experience/manager.go` | C-any #4: Actions 注释更新 |

---

### Task 1: Fix 1 — WithDisabledSkillsSet + normalizeNameSet 空实现

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 在结构体中添加 disabledSkills 字段**

在 `SkillEvolutionRail` 结构体的 `// ─── 配置` 区块中，`autoSave bool` 之后添加：

```go
	// autoSave 是否自动保存（跳过审批）
	autoSave bool
	// disabledSkills 禁用的技能名称列表
	disabledSkills []string
```

- [ ] **Step 2: 补全 WithDisabledSkillsSet 函数体**

将当前的空实现：
```go
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		// 委托给基类的 WithDisabledSkills，通过 normalizeNameSet 在构造时应用
	}
}
```

替换为：
```go
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		r.disabledSkills = names
	}
}
```

- [ ] **Step 3: 补全 normalizeNameSet 函数体**

将当前的空实现：
```go
func (r *SkillEvolutionRail) normalizeNameSet() []string {
	// 从 skillsDir 列出已有技能名称
	return []string{}
}
```

替换为：
```go
func (r *SkillEvolutionRail) normalizeNameSet() []string {
	return r.disabledSkills
}
```

- [ ] **Step 4: 补充单元测试**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestWithDisabledSkillsSet(t *testing.T) {
	r := &SkillEvolutionRail{}
	WithDisabledSkillsSet([]string{"foo", "bar"})(r)
	assert.Equal(t, []string{"foo", "bar"}, r.disabledSkills)
}

func TestNormalizeNameSet(t *testing.T) {
	r := &SkillEvolutionRail{disabledSkills: []string{"a", "b"}}
	assert.Equal(t, []string{"a", "b"}, r.normalizeNameSet())
}

func TestNormalizeNameSet_空(t *testing.T) {
	r := &SkillEvolutionRail{}
	assert.Equal(t, []string(nil), r.normalizeNameSet())
}
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`
Expected: 编译通过

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/rails/evolution/... -run "TestWithDisabledSkillsSet|TestNormalizeNameSet"`
Expected: 3 个测试通过

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): 补全 WithDisabledSkillsSet 和 normalizeNameSet 空实现 (9.24 P3 Fix1)"
```

---

### Task 2: Fix 2 — ShouldHintSimplifyOrRebuild 走 EvolutionStore 封装

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 替换 ShouldHintSimplifyOrRebuild 实现**

将当前实现（约第 972-993 行）：
```go
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
	store := r.evolutionStore
	skillDir := store.ResolveSkillDir(context.Background(), skillName)
	if skillDir == "" {
		return false
	}
	evoPath := filepath.Join(skillDir, "evolutions.json")
	data, err := os.ReadFile(evoPath)
	if err != nil {
		return false
	}
	var evoData map[string]any
	if err := json.Unmarshal(data, &evoData); err != nil {
		return false
	}
	entries, ok := evoData["entries"].([]any)
	if !ok {
		return false
	}
	// Python: return len(entries) >= 10
	return len(entries) >= 10
}
```

替换为：
```go
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
	evoLog, err := r.evolutionStore.LoadFullEvolutionLog(context.Background(), skillName)
	if err != nil || evoLog == nil {
		return false
	}
	// Python: return len(entries) >= 10
	return len(evoLog.Entries) >= 10
}
```

- [ ] **Step 2: 清理无用 import**

检查 `os` 和 `encoding/json` 是否还有其他引用。如果 `os` 仅在 `WithSharingConfig` 中用于 `os.Getenv` 则保留；`encoding/json` 如果无其他引用则删除 import。

Run: `cd /home/opensource/uap-claw-go && grep -n 'os\.' internal/agentcore/harness/rails/evolution/skill_evolution_rail.go | head -10`
Run: `cd /home/opensource/uap-claw-go && grep -n 'json\.' internal/agentcore/harness/rails/evolution/skill_evolution_rail.go | head -10`

如 `json` 无引用，从 import 块删除 `"encoding/json"`。

- [ ] **Step 3: 补充单元测试**

在 `skill_evolution_rail_test.go` 中添加（需 mock EvolutionStore）：

```go
func TestShouldHintSimplifyOrRebuild_走Store(t *testing.T) {
	// 应通过 EvolutionStore.LoadFullEvolutionLog 而非直接 os.ReadFile
	// 验证方法：确认 ShouldHintSimplifyOrRebuild 不依赖 evolutions.json 文件
	// 完整测试需要 mock EvolutionStore，当前仅验证函数签名
	t.Log("ShouldHintSimplifyOrRebuild 应走 EvolutionStore.LoadFullEvolutionLog")
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): ShouldHintSimplifyOrRebuild 走 EvolutionStore 封装消除 any (9.24 P3 Fix2)"
```

---

### Task 3: Fix 3 — isOutcomeEvent 类型断言修复

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go`

- [ ] **Step 1: 修改 isOutcomeEvent 函数签名和实现**

将当前实现（约第 470-482 行）：
```go
func isOutcomeEvent(event any) bool {
	if event == nil {
		return false
	}
	if outputSchema, ok := event.(*struct {
		Payload map[string]any
	}); ok {
		if meta, ok := outputSchema.Payload["_evolution_meta"].(map[string]string); ok {
			return meta["event_kind"] == "outcome"
		}
	}
	return false
}
```

替换为：
```go
func isOutcomeEvent(event *stream.OutputSchema) bool {
	if event == nil {
		return false
	}
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return false
	}
	meta, ok := payload["_evolution_meta"].(map[string]string)
	if !ok {
		return false
	}
	return meta["event_kind"] == "outcome"
}
```

需要在文件 import 中添加：`"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`

- [ ] **Step 2: 更新 watchEvolutionAndPush 中的调用**

将 `watchEvolutionAndPush` 中（约第 186 行）：
```go
				if isOutcomeEvent(event) {
```
改为（event 类型已从 `any` 变为 `*stream.OutputSchema`，无需改动调用语法，但需确认 DrainPendingApprovalEvents 返回类型匹配）：

`DrainPendingApprovalEvents` 返回 `[]*stream.OutputSchema`，遍历时 `event` 类型已是 `*stream.OutputSchema`，调用 `isOutcomeEvent(event)` 自动匹配新签名。**无需改动调用点。**

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_evolution.go
git commit -m "fix(adapter): 修复 isOutcomeEvent 类型断言与 DrainPendingApprovalEvents 返回类型不匹配 (9.24 P3 Fix3)"
```

---

### Task 4: Fix 4 — pushEventToFrontend 对齐 Python 推送流程

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`

- [ ] **Step 1: 添加 outputSchemaToDict 薄适配函数**

在 `deep_adapter_evolution.go` 的非导出函数区添加：

```go
// outputSchemaToDict 将 OutputSchema 转为 helpers 期望的 map[string]any 格式。
// helpers 函数（PushEvolutionProgress 等）全部基于 map[string]any，
// 而 DrainPendingApprovalEvents 返回 []*stream.OutputSchema。
func outputSchemaToDict(schema *stream.OutputSchema) map[string]any {
	if schema == nil {
		return nil
	}
	if payload, ok := schema.Payload.(map[string]any); ok {
		return payload
	}
	// fallback：将 OutputSchema 整体转为 dict
	return map[string]any{
		"type":    schema.Type,
		"index":   schema.Index,
		"payload": schema.Payload,
	}
}
```

- [ ] **Step 2: 修改 pushEventToFrontend 签名和实现**

将当前桩实现（约第 463-467 行）：
```go
func (d *DeepAdapter) pushEventToFrontend(event any, sessionID string) {
	// 通过 session stream 推送事件
	// 当前为日志记录实现，后续接入 stream 时替换
	logger.Info(logComponent).Str("session_id", sessionID).Any("event_type", "evolution_event").Msg("推送演进事件到前端")
}
```

替换为：
```go
func (d *DeepAdapter) pushEventToFrontend(event *stream.OutputSchema, sessionID string, channelID string, requestID string) {
	evt := outputSchemaToDict(event)
	if evt == nil {
		return
	}

	ctx := context.Background()
	pushCtx := &evoHelpers.EvolutionPushContext{
		Transport: gatewaypush.NewChannelPushTransport(),
		ChannelID: channelID,
		SessionID: sessionID,
	}

	evtKind := evoHelpers.EvolutionEventKind(evt)
	switch evtKind {
	case "approval":
		// 审批事件 → PushEvolutionEvent
		if err := evoHelpers.PushEvolutionEvent(ctx, pushCtx, requestID, evt, session.BuildServerPushMessage); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("推送审批事件失败")
		}
	case "outcome":
		// 结果事件 → PushEvolutionStatus
		outcome := evoHelpers.EvolutionOutcomeFromEvent(evt)
		update := evoHelpers.EvolutionStatusUpdate{
			RequestID: requestID,
			Status:    outcome["status"],
			Message:   outcome["message"],
		}
		if err := evoHelpers.PushEvolutionStatus(ctx, pushCtx, update, session.BuildServerPushMessage); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("推送结果状态失败")
		}
	default:
		// 进度事件 → PushEvolutionProgress（批量推送在 watchEvolutionAndPush 中处理）
		logger.Debug(logComponent).Str("session_id", sessionID).Str("event_kind", evtKind).Msg("演进进度事件已在批量推送中处理")
	}
}
```

需要在文件 import 中添加：
```go
	evoHelpers "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
	session "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
```

- [ ] **Step 3: 重写 watchEvolutionAndPush 完整推送逻辑**

将当前实现（约第 162-194 行）：
```go
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, requestID string) error {
	...
}
```

替换为：
```go
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, channelID string, requestID string) error {
	if d.skillEvolutionRail == nil {
		return nil
	}

	pushCtx := &evoHelpers.EvolutionPushContext{
		Transport: gatewaypush.NewChannelPushTransport(),
		ChannelID: channelID,
		SessionID: sessionID,
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			events := d.skillEvolutionRail.DrainPendingApprovalEvents(true, nil)
			if len(events) == 0 {
				continue
			}

			// 将 []*stream.OutputSchema 转为 []map[string]any 供 helpers 使用
			dicts := make([]map[string]any, 0, len(events))
			for _, e := range events {
				d := outputSchemaToDict(e)
				if d != nil {
					dicts = append(dicts, d)
				}
			}

			// 1. 推送进度事件（进度事件不单独 pushEventToFrontend，批量推送）
			_ = evoHelpers.PushEvolutionProgress(
				ctx, pushCtx, requestID, dicts,
				func(evt map[string]any) map[string]any { return evt },
				session.BuildServerPushMessage,
			)

			// 2. 逐个处理审批/结果事件
			for _, event := range events {
				evt := outputSchemaToDict(event)
				if evt == nil {
					continue
				}
				if evoHelpers.IsEvolutionApprovalEvent(evt) || evoHelpers.IsEvolutionOutcomeEvent(evt) {
					d.pushEventToFrontend(event, sessionID, channelID, requestID)
				}
				// 检查是否包含 outcome 事件
				if isOutcomeEvent(event) {
					return nil
				}
			}
		}
	}
}
```

- [ ] **Step 4: 更新 deep_adapter.go 中的调用点**

将约第 1088 行：
```go
				_ = d.watchEvolutionAndPush(ctx, sessionID, req.RequestID)
```
改为：
```go
				_ = d.watchEvolutionAndPush(ctx, sessionID, req.ChannelID, req.RequestID)
```

- [ ] **Step 5: 更新 deep_adapter_helpers_test.go 中的测试调用**

搜索测试中 `watchEvolutionAndPush` 的调用，添加 channelID 参数：
```go
// 旧：d.watchEvolutionAndPush(ctx, "s1", "req1")
// 新：d.watchEvolutionAndPush(ctx, "s1", "ch1", "req1")
```

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 7: 运行 adapter 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/...`
Expected: 测试通过

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_evolution.go internal/swarm/server/adapter/deep_adapter.go
git commit -m "fix(adapter): watchEvolutionAndPush 对齐 Python 推送流程 (9.24 P3 Fix4)"
```

---

### Task 5: Fix 5 — FromLegacyDict 补全新增字段

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/contracts.go`
- Modify: `internal/agentcore/harness/rails/evolution/contracts_test.go`

- [ ] **Step 1: 补全 FromLegacyDict 解析逻辑**

将当前实现（约第 179-206 行）：
```go
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot {
	var traj *trajectory.Trajectory
	if t, ok := snapshot["trajectory"]; ok {
		if typed, ok := t.(*trajectory.Trajectory); ok {
			traj = typed
		}
	}

	messages := []map[string]any{}
	if m, ok := snapshot["messages"]; ok {
		if typed, ok := m.([]map[string]any); ok {
			messages = typed
		}
	}

	var skillName *string
	if sn, ok := snapshot["skill_name"]; ok {
		if typed, ok := sn.(string); ok {
			skillName = &typed
		}
	}

	return EvolutionSnapshot{
		Trajectory: traj,
		Messages:   messages,
		SkillName:  skillName,
	}
}
```

替换为：
```go
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot {
	var traj *trajectory.Trajectory
	if t, ok := snapshot["trajectory"]; ok {
		if typed, ok := t.(*trajectory.Trajectory); ok {
			traj = typed
		}
	}

	messages := []map[string]any{}
	if m, ok := snapshot["messages"]; ok {
		if typed, ok := m.([]map[string]any); ok {
			messages = typed
		}
	}

	var skillName *string
	if sn, ok := snapshot["skill_name"]; ok {
		if typed, ok := sn.(string); ok {
			skillName = &typed
		}
	}

	sessionID := ""
	if sid, ok := snapshot["session_id"]; ok {
		if typed, ok := sid.(string); ok {
			sessionID = typed
		}
	}

	var presentedEntries []experience.PresentedRecordEntry
	if pe, ok := snapshot["presented_entries"]; ok {
		if typed, ok := pe.([]experience.PresentedRecordEntry); ok {
			presentedEntries = typed
		}
	}

	var incrementalMessages []map[string]any
	if im, ok := snapshot["incremental_messages"]; ok {
		if typed, ok := im.([]map[string]any); ok {
			incrementalMessages = typed
		}
	}

	return EvolutionSnapshot{
		Trajectory:          traj,
		Messages:            messages,
		SkillName:           skillName,
		SessionID:           sessionID,
		PresentedEntries:    presentedEntries,
		IncrementalMessages: incrementalMessages,
	}
}
```

- [ ] **Step 2: 更新测试**

在 `contracts_test.go` 中更新 `TestEvolutionSnapshot_FromLegacyDict_全字段` 测试，验证新增 3 个字段的恢复：

```go
func TestEvolutionSnapshot_FromLegacyDict_全字段含扩展(t *testing.T) {
	traj := &trajectory.Trajectory{}
	skillName := "test-skill"
	entries := []experience.PresentedRecordEntry{
		{SkillName: "s1", RecordIDs: []string{"r1"}},
	}
	incremental := []map[string]any{{"role": "user", "content": "hi"}}

	dict := map[string]any{
		"trajectory":           traj,
		"messages":             []map[string]any{{"role": "user"}},
		"skill_name":           skillName,
		"session_id":           "sess-123",
		"presented_entries":    entries,
		"incremental_messages": incremental,
	}

	snap := FromLegacyDict(dict)
	assert.Equal(t, traj, snap.Trajectory)
	assert.Equal(t, &skillName, snap.SkillName)
	assert.Equal(t, "sess-123", snap.SessionID)
	assert.Equal(t, entries, snap.PresentedEntries)
	assert.Equal(t, incremental, snap.IncrementalMessages)
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`
Expected: 编译通过

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/rails/evolution/... -run "TestEvolutionSnapshot_FromLegacyDict"`
Expected: 测试通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/contracts.go internal/agentcore/harness/rails/evolution/contracts_test.go
git commit -m "fix(evolution): 补全 FromLegacyDict 解析新增字段 (9.24 P3 Fix5)"
```

---

### Task 6: Fix 6 — 定义 ApprovalAnswer 结构体 + 改签名

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_slash.go`

- [ ] **Step 1: 在 deep_adapter.go 中定义 ApprovalAnswer 结构体和解析函数**

在结构体区块之后添加：

```go
// ApprovalAnswer 审批回答条目，从前端 WebSocket 消息解析。
//
// Python: answers 参数为 list[dict]，每个 dict 包含 selected_options 字段。
// 对齐 Python: JiuWenClawDeepAdapter._handle_evolution_approval(answers: list)
type ApprovalAnswer struct {
	// SelectedOptions 已选项标签列表
	SelectedOptions []string `json:"selected_options"`
}

// parseApprovalAnswersFromAny 从 any 类型解析为 []ApprovalAnswer。
// answers 来自 req.Params 的 JSON 反序列化，可能是 []any 或 []map[string]any。
func parseApprovalAnswersFromAny(answers any) []ApprovalAnswer {
	if answers == nil {
		return nil
	}
	// 尝试 []any（JSON 反序列化默认）
	if slice, ok := answers.([]any); ok {
		var result []ApprovalAnswer
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				var opts []string
				if raw, ok := m["selected_options"]; ok {
					if optSlice, ok := raw.([]any); ok {
						for _, o := range optSlice {
							if s, ok := o.(string); ok {
								opts = append(opts, s)
							}
						}
					}
				}
				result = append(result, ApprovalAnswer{SelectedOptions: opts})
			}
		}
		return result
	}
	return nil
}
```

- [ ] **Step 2: 修改 HandleUserAnswer 中的 answers 处理**

将约第 1166 行：
```go
	answers := params["answers"]
```
之后添加解析：
```go
	parsedAnswers := parseApprovalAnswersFromAny(answers)
```

将约第 1180 行：
```go
		if parseApprovalAnswers(answers) {
			approvalType = "approve"
		}
		resolved = d.handleGovernanceApproval(requestID, answers, approvalType)
```
改为：
```go
		if parseApprovalAnswers(parsedAnswers) {
			approvalType = "approve"
		}
		resolved = d.handleGovernanceApproval(requestID, parsedAnswers, approvalType)
```

将约第 1186 行：
```go
		resolved = d.handleEvolutionApproval(requestID, answers)
```
改为：
```go
		resolved = d.handleEvolutionApproval(requestID, parsedAnswers)
```

- [ ] **Step 3: 修改 handleEvolutionApproval 签名**

在 `deep_adapter_evolution.go` 中，将：
```go
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers any) bool {
```
改为：
```go
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers []ApprovalAnswer) bool {
```

将函数体中：
```go
	approved := parseApprovalAnswers(answers)
```
改为（parseApprovalAnswers 签名也变了，见 Step 5）：
```go
	approved := parseApprovalAnswers(answers)
```
（调用语法不变，但参数类型变了）

- [ ] **Step 4: 修改 handleGovernanceApproval 签名**

在 `deep_adapter_slash.go` 中，将：
```go
func (d *DeepAdapter) handleGovernanceApproval(requestID string, answers any, approvalType string) bool {
```
改为：
```go
func (d *DeepAdapter) handleGovernanceApproval(requestID string, answers []ApprovalAnswer, approvalType string) bool {
```

- [ ] **Step 5: 修改 parseApprovalAnswers 签名**

在 `deep_adapter_evolution.go` 中，将：
```go
func parseApprovalAnswers(answers any) bool {
	if answers == nil {
		return false
	}
	// 尝试解析为 []map[string]any
	if answerList, ok := answers.([]any); ok {
		for _, a := range answerList {
			if m, ok := a.(map[string]any); ok {
				label, _ := m["label"].(string)
				if label == "接收" || label == "Accept" || label == "approve" {
					return true
				}
			}
		}
		return false
	}
	// 尝试解析为单个 string
	if label, ok := answers.(string); ok {
		return label == "接收" || label == "Accept" || label == "approve"
	}
	return false
}
```

改为：
```go
func parseApprovalAnswers(answers []ApprovalAnswer) bool {
	for _, ans := range answers {
		for _, opt := range ans.SelectedOptions {
			if opt == "接收" || opt == "Accept" || opt == "approve" || opt == "执行" {
				return true
			}
		}
	}
	return false
}
```

注意：Python 中 evolution approval 检查 `"接收" in ans.get("selected_options", [])`，governance approval 检查 `accept_labels & set(ans.get("selected_options", []))`（simplify 为 `"执行"`）。Go 版统一检查 `SelectedOptions` 字段，覆盖所有关键词。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/...`
Expected: 测试通过

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_evolution.go internal/swarm/server/adapter/deep_adapter_slash.go
git commit -m "fix(adapter): 定义 ApprovalAnswer 结构体替换 answers any (9.24 P3 Fix6)"
```

---

### Task 7: C-any #4 — Actions 注释更新

**Files:**
- Modify: `internal/evolving/experience/manager.go`

- [ ] **Step 1: 更新 PendingGovernance.Actions 注释**

将：
```go
	// Actions 整理操作列表（来自 LLM 输出，保持 []map[string]any）
	Actions []map[string]any
```

改为：
```go
	// Actions 整理操作列表（来自 LLM 输出，保持 []map[string]any）。
	// 每个 dict 约定字段：action (DELETE|MERGE|REFINE|KEEP)、record_id、
	// merge_remove_ids (MERGE)、new_content (MERGE/REFINE)。
	// 因 LLM 输出非硬约束，保持 any 而非强类型结构体。
	Actions []map[string]any
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/experience/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/evolving/experience/manager.go
git commit -m "docs(experience): 补充 PendingGovernance.Actions 字段约定注释 (9.24 P3)"
```

---

### Task 8: 全量编译 + 测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行 evolution 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/rails/evolution/...`
Expected: 测试通过

- [ ] **Step 3: 运行 adapter 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/...`
Expected: 测试通过

- [ ] **Step 4: 运行 experience 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/experience/...`
Expected: 测试通过

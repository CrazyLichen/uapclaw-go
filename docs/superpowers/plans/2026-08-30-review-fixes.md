# 审查问题修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 修复 2026-08-30 审查文档中验证确认的 23 个代码问题 + 2 个注释修改

**Architecture:** 按模块分批修复：7.x Memory → 9.x Teams → 10.x AgentServer → 9.72 Evolving。每个 Task 聚焦一个文件或紧密关联的修改，确保编译通过后再进入下一个。

**Tech Stack:** Go 1.22+, 标准库 crypto/rand, math/rand, crypto/md5

---

## 文件修改清单

| 文件 | 修改类型 | 涉及问题 |
|------|---------|---------|
| `internal/evolving/experience/scorer.go` | 修改 | #9 |
| `internal/agentcore/memory/lite/embeddings.go` | 修改 | #11, #12, #13, #29 |
| `internal/agentcore/memory/manage/index/fragment_manager.go` | 修改 | #28 |
| `internal/agent_teams/tools/team_backend.go` | 修改 | #2, #3, #5, #15, #17, #18, #19, #32 |
| `internal/agent_teams/schema/i18n.go` | 修改 | #21 |
| `internal/agent_teams/i18n.go` | 修改 | #21 |
| `internal/agentcore/multi_agent/teams/handoff/container_agent.go` | 修改 | #4(注释), #20 |
| `internal/agentcore/multi_agent/teams/hierarchical_msgbus/supervisor_agent.go` | 修改 | #34 |
| `internal/swarm/server/runtime/skill/skill_manager.go` | 修改 | #6+7 |
| `internal/swarm/server/runtime/skill/plugin_yaml.go` | 修改 | #8, #23 |
| `internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go` | 修改 | #24 |
| `internal/swarm/server/runtime/agent_manager.go` | 修改 | #10(注释) |

---

### Task 1: 修复 parseTimestamp 不支持无时区 ISO 时间戳（问题9）

**Files:**
- Modify: `internal/evolving/experience/scorer.go:L660-L674`
- Test: `internal/evolving/experience/scorer_test.go`

- [x] **Step 1: 写失败测试**

在 `scorer_test.go` 中添加测试：

```go
func TestParseTimestamp_无时区ISO时间戳(t *testing.T) {
	tests := []struct {
		input    string
		wantYear int
	}{
		{"2025-01-15T10:30:00", 2025},                        // 无时区
		{"2025-01-15T10:30:00.123456789", 2025},              // 无时区+纳秒
		{"2025-01-15T10:30:00Z", 2025},                       // UTC Z
		{"2025-01-15T10:30:00+08:00", 2025},                  // 带偏移
		{"2025-01-15T10:30:00.123456789+00:00", 2025},        // 纳秒+偏移
	}
	for _, tt := range tests {
		got, err := parseTimestamp(tt.input)
		if err != nil {
			t.Errorf("parseTimestamp(%q) error: %v", tt.input, err)
			continue
		}
		if got.Year() != tt.wantYear {
			t.Errorf("parseTimestamp(%q) year = %d, want %d", tt.input, got.Year(), tt.wantYear)
		}
		if got.Location() != time.UTC {
			t.Errorf("parseTimestamp(%q) location = %v, want UTC", tt.input, got.Location())
		}
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/experience/... -run TestParseTimestamp_无时区 -v`
Expected: 无时区格式 FAIL

- [x] **Step 3: 修改 parseTimestamp 函数**

将 `scorer.go:L660-L674` 替换为：

```go
func parseTimestamp(ts string) (time.Time, error) {
	s := strings.ReplaceAll(ts, "Z", "+00:00")
	// 先尝试标准格式（含时区）
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	// 尝试无时区 ISO 格式，补 UTC（对齐 Python: fromisoformat + tzinfo=None → UTC）
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp: %s", ts)
}
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/experience/... -run TestParseTimestamp -v`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/evolving/experience/scorer.go internal/evolving/experience/scorer_test.go
git commit -m "fix: parseTimestamp 增加无时区 ISO 时间戳回退解析，对齐 Python fromisoformat"
```

---

### Task 2: 修复 MockEmbeddingProvider 返回空向量（问题11）

**Files:**
- Modify: `internal/agentcore/memory/lite/embeddings.go:L30-L37,L55-L67`
- Test: `internal/agentcore/memory/lite/embeddings_test.go`

- [x] **Step 1: 写失败测试**

```go
func TestMockEmbeddingProvider_EmbedQuery_返回128维向量(t *testing.T) {
	m := NewMockEmbeddingProvider()
	vec, err := m.EmbedQuery(context.Background(), "hello")
	assert.NoError(t, err)
	assert.Len(t, vec, 128)
	// 确定性：相同输入返回相同向量
	vec2, _ := m.EmbedQuery(context.Background(), "hello")
	assert.Equal(t, vec, vec2)
	// 值在 [-1, 1] 范围内
	for _, v := range vec {
		assert.GreaterOrEqual(t, v, -1.0)
		assert.LessOrEqual(t, v, 1.0)
	}
}

func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	m := NewMockEmbeddingProvider()
	vecs, err := m.EmbedDocuments(context.Background(), []string{"a", "b"})
	assert.NoError(t, err)
	assert.Len(t, vecs, 2)
	assert.Len(t, vecs[0], 128)
	assert.Len(t, vecs[1], 128)
	// 不同输入返回不同向量
	assert.NotEqual(t, vecs[0], vecs[1])
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/lite/... -run TestMockEmbeddingProvider -v`
Expected: FAIL（当前返回空切片，长度不为 128）

- [x] **Step 3: 修改 MockEmbeddingProvider 实现**

将 `embeddings.go:L60-L67` 的 EmbedQuery 和 EmbedDocuments 替换为：

```go
// EmbedQuery 返回基于文本 hash 的 128 维确定性随机向量。
//
// 对齐 Python: MockEmbeddingProvider.embed_query — random.seed(md5(text).hexdigest()), [random.uniform(-1,1) for _ in range(128)]
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	h := md5.Sum([]byte(text))
	seed := int64(binary.BigEndian.Uint64(h[:8]))
	r := rand.New(rand.NewSource(seed))
	vec := make([]float64, 128)
	for i := range vec {
		vec[i] = r.Float64()*2 - 1 // uniform(-1, 1)
	}
	return vec, nil
}

// EmbedDocuments 批量嵌入文档。
//
// 对齐 Python: MockEmbeddingProvider.embed_documents
func (m *MockEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := m.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}
```

同时在 import 中添加 `"crypto/md5"`, `"encoding/binary"`, `"math/rand"`

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/lite/... -run TestMockEmbeddingProvider -v`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/embeddings.go internal/agentcore/memory/lite/embeddings_test.go
git commit -m "fix: MockEmbeddingProvider 返回 128 维确定性随机向量，对齐 Python"
```

---

### Task 3: 修复 ResolveEmbeddingConfigFromEnv model_name 默认值 + dims 默认值 + 降级 mock Warn 日志（问题12+13+29）

**Files:**
- Modify: `internal/agentcore/memory/lite/embeddings.go:L98-L153`
- Test: `internal/agentcore/memory/lite/embeddings_test.go`

这三个问题在同一文件中，合并修复。

- [x] **Step 1: 写失败测试**

```go
func TestResolveEmbeddingConfigFromEnv_modelName为空时返回nil(t *testing.T) {
	t.Setenv("EMBEDDING_MODEL_NAME", "")
	os.Unsetenv("EMBEDDING_MODEL_NAME")
	os.Unsetenv("EMBED_MODEL")
	cfg := ResolveEmbeddingConfigFromEnv("", "https://api.test.com", "test-key")
	assert.Nil(t, cfg, "model_name 全部为空时应返回 nil，对齐 Python")
}

func TestCreateEmbeddingProvider_dims默认1024(t *testing.T) {
	// 需要 mock 场景
	provider, err := CreateEmbeddingProvider("", "", "mock")
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	// 检查 Dims() 返回 1024
	assert.Equal(t, 1024, provider.Dims(), "dims 默认值应为 1024，对齐 Python")
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/lite/... -run "TestResolveEmbeddingConfigFromEnv_modelName|TestCreateEmbeddingProvider_dims" -v`

- [x] **Step 3: 修改 embeddings.go**

1. 删除 `L101` 的 `EMBED_MODEL` 读取行
2. 删除 `L121` 的 `envModelName = "default"` 行
3. 在 `L144` 的返回行添加 dims 字段：
   ```go
   return &baseEmbeddingAdapter{base: base, prov: provider, model: model, dims: 1024}, nil
   ```
4. 在 `L149` 的 `return NewMockEmbeddingProvider(), nil` 前添加：
   ```go
   logger.Warn(logComponent).Msg("Embedding API key not found, using mock provider")
   ```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/lite/... -v`

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/embeddings.go internal/agentcore/memory/lite/embeddings_test.go
git commit -m "fix: 删除 model_name 默认值、EMBED_MODEL 变量，dims 默认 1024，降级 mock 添加 Warn 日志"
```

---

### Task 4: 修复类型断言跳过无 Warn 日志（问题28）

**Files:**
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go:L86-L89`

- [x] **Step 1: 修改 fragment_manager.go**

将 `L86-L89`：
```go
frag, ok := unit.(*mem_model.FragmentMemoryUnit)
if !ok {
    continue
}
```

替换为：
```go
frag, ok := unit.(*mem_model.FragmentMemoryUnit)
if !ok {
    logger.Warn(logComponent).Str("memory_type", memType).
        Str("user_id", userID).Str("scope_id", scopeID).
        Msg("mem_unit is not a FragmentMemoryUnit")
    continue
}
```

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/index/...`

- [x] **Step 3: 提交**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go
git commit -m "fix: 类型断言失败时添加 Warn 日志，对齐 Python isinstance warning"
```

---

### Task 5: 修复 ShutdownMember 缺少 FSM 状态转换校验（问题2）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go:L521-L526`

- [x] **Step 1: 在 ShutdownMember 中添加 FSM 校验**

在 `L521`（`ok := tb.db.Member().TryTransitionMemberStatus`）之前插入：

```go
// 对齐 Python: is_valid_transition(current_status, SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS)
if !fsm.IsValidMemberTransition(member.Status, string(atschema.MemberStatusShutdownRequested)) {
    return atschema.NewMemberOpResultFail(
        fmt.Sprintf("Member %s cannot shut down from status '%s'", memberName, member.Status))
}
```

确认 `fsm` 包已导入（`internal/agent_teams/fsm`）。

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`

- [x] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix: ShutdownMember CAS 前添加 FSM 状态转换校验，返回精确错误消息"
```

---

### Task 6: 修复 ApprovePlan 缺少前置校验（问题3）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go:L797-L820`

- [x] **Step 1: 在 ApprovePlan 中添加三层校验**

在 `L803`（`err := tb.taskManager.ApprovePlan`）之前插入：

```go
// 对齐 Python: 三层前置校验
// 校验 1: planID 非空
if planID == "" {
    logger.Error(tbLogComponent).Msg("ApprovePlan: plan_id is required")
    return atschema.NewMemberOpResultFail("approve_plan requires plan_id")
}
// 校验 2: plan record 存在
task, err := tb.taskManager.Get(ctx, planID)
if err != nil || task == nil {
    logger.Error(tbLogComponent).Str("plan_id", planID).Msg("ApprovePlan: plan not found")
    return atschema.NewMemberOpResultFail("plan not found: " + planID)
}
// 校验 3: member 存在
memberName := task.Assignee
if memberName == "" {
    logger.Error(tbLogComponent).Str("plan_id", planID).Msg("ApprovePlan: plan has no member_name")
    return atschema.NewMemberOpResultFail("plan has no member_name: " + planID)
}
member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
if err != nil || member == nil {
    logger.Error(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
        Msg("ApprovePlan: member not found in team")
    return atschema.NewMemberOpResultFail(fmt.Sprintf("member %s not found in team %s", memberName, tb.teamName))
}
```

同时删除原 `L807-L810` 的 `task, _ := tb.taskManager.Get(ctx, planID)` 和 `memberName := ""` 相关代码（已在上面的校验中完成）。

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`

- [x] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix: ApprovePlan 添加三层前置校验（planID/plan_record/member），对齐 Python"
```

---

### Task 7: 修复 build_team 预定义成员未传 allocation（问题5）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go:L631-L639`

- [x] **Step 1: 修改 BuildTeam 预定义成员循环**

将 `L631-L639`：
```go
for _, pm := range tb.predefinedMembers {
    if pm.RoleType == atschema.TeamRoleHumanAgent {
        continue
    }
    memberCardID := tb.teamName + "_" + pm.MemberName
    agentCard := memberCardID
    _ = agentCard
    tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCardID, string(pm.RoleType),
        pm.Persona, pm.PromptHint, pm.ModelName)
}
```

替换为：
```go
for _, pm := range tb.predefinedMembers {
    if pm.RoleType == atschema.TeamRoleHumanAgent {
        continue
    }
    memberCardID := tb.teamName + "_" + pm.MemberName
    // 对齐 Python: allocation = self._allocate_model_config(member_spec.model_name) if self._allocate_model_config else None
    var allocOpt SpawnMemberOption
    if tb.modelConfigAllocator != nil {
        allocOpt = WithAllocation(tb.modelConfigAllocator(pm.ModelName))
    }
    tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCardID, string(pm.RoleType),
        pm.Persona, pm.PromptHint, pm.ModelName, allocOpt)
}
```

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`

- [x] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix: BuildTeam 预定义成员显式传 WithAllocation，对齐 Python"
```

---

### Task 8: 修复 CancelMember 失败返回 success + 缺少汇总日志（问题15+32）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go:L562-L578`

- [x] **Step 1: 修改 CancelMember 中的 reset 循环和 SendMessage**

将 `L562-L578`：
```go
tasks, _ := tb.taskManager.GetTasksByAssignee(ctx, memberName, string(atschema.TaskStatusClaimed))
for _, t := range tasks {
    if err := tb.taskManager.Reset(ctx, t.TaskID); err != nil {
        logger.Warn(tbLogComponent).Str("task_id", t.TaskID).Err(err).
            Msg("CancelMember: reset task failed")
    }
}
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
```

替换为：
```go
tasks, _ := tb.taskManager.GetTasksByAssignee(ctx, memberName, string(atschema.TaskStatusClaimed))
// 对齐 Python: reset_count 统计 + 汇总日志
resetCount := 0
for _, t := range tasks {
    if err := tb.taskManager.Reset(ctx, t.TaskID); err != nil {
        logger.Warn(tbLogComponent).Str("task_id", t.TaskID).Err(err).
            Msg("CancelMember: reset task failed")
    } else {
        resetCount++
    }
}
if resetCount > 0 {
    logger.Info(tbLogComponent).Str("member_name", memberName).
        Int("reset_count", resetCount).Msg("CancelMember: reset tasks from member")
}
// 对齐 Python: 检查消息发送结果，失败返回 fail
msgResult, msgErr := tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
if msgErr != nil || (msgResult != nil && !msgResult.OK) {
    logger.Error(tbLogComponent).Str("member_name", memberName).Err(msgErr).
        Msg("CancelMember: 发送取消消息失败")
    return atschema.NewMemberOpResultFail("cancel message failed for: " + memberName)
}
```

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`

- [x] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix: CancelMember 检查消息发送结果+添加 reset 计数汇总日志，对齐 Python"
```

---

### Task 9: 修复 CleanTeam 移除 ERROR 豁免 + ForceCleanTeam 综合返回（问题17+18）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go:L690-L691,L738-L742`

- [x] **Step 1: 修改 CleanTeam**

将 `L690-L691`：
```go
if m.Status != string(atschema.MemberStatusShutdown) &&
    m.Status != string(atschema.MemberStatusError) {
```

替换为：
```go
// 对齐 Python: 只允许 SHUTDOWN 状态
if m.Status != string(atschema.MemberStatusShutdown) {
```

- [x] **Step 2: 修改 ForceCleanTeam**

将 `L738-L742`：
```go
tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
tb.RemoveCleanupPaths(ctx)
logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: 团队已强制清理")
return true, nil
```

替换为：
```go
// 对齐 Python: success = force_delete_team_session(...)
success := tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
// 对齐 Python: 清理路径失败设 success=false
if err := tb.RemoveCleanupPaths(ctx); err != nil {
    logger.Error(tbLogComponent).Err(err).Str("team_name", tb.teamName).
        Msg("ForceCleanTeam: 清理路径失败")
    success = false
}
if success {
    logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: 团队已强制清理")
}
return success, nil
```

注意：需要先修改 `RemoveCleanupPaths` 签名增加 error 返回值。

- [x] **Step 3: 修改 RemoveCleanupPaths 签名**

搜索 `RemoveCleanupPaths` 定义，将签名从 `func (tb *TeamBackend) RemoveCleanupPaths(ctx context.Context)` 改为 `func (tb *TeamBackend) RemoveCleanupPaths(ctx context.Context) error`，在实现中收集错误并返回。

- [x] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`

- [x] **Step 5: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "fix: CleanTeam 移除 ERROR 豁免，ForceCleanTeam 综合返回结果，对齐 Python"
```

---

### Task 10: 重构 SpawnMember 签名使用 AgentCard 结构体 + 修复 SpawnHumanAgent（问题19）

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go` — SpawnMember 签名及所有调用方
- Modify: `internal/agent_teams/tools/team_backend_test.go` — 测试调用方
- Modify: `internal/agent_teams/memory/human_agent_inbox_test.go` — 测试调用方

这是影响面最大的修改，需要：
1. 将 `agentCard string` 参数改为 `agentCard *agentschema.AgentCard`
2. SpawnMember 内部从 agentCard 提取 ID/Name/Description
3. BuildTeam 和 SpawnHumanAgent 中构造完整 AgentCard 传入
4. 所有测试调用方适配新签名

- [x] **Step 1: 修改 SpawnMember 签名**

将 `L377`：
```go
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string, opts ...SpawnMemberOption) atschema.MemberOpResult
```

改为：
```go
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName string, agentCard *agentschema.AgentCard, role, desc, prompt, modelName string, opts ...SpawnMemberOption) atschema.MemberOpResult
```

- [x] **Step 2: 修改 SpawnMember 内部对 agentCard 的使用**

在 SpawnMember 内部，将原来使用 `agentCard`（string）的地方改为 `agentCard.CardID` 或从 `agentCard` 提取字段。

- [x] **Step 3: 修改 BuildTeam 中的 SpawnMember 调用（L619, L638）**

构造 AgentCard 对象传入：
```go
card := &agentschema.AgentCard{
    BaseCard: agentschema.BaseCard{
        CardID:      memberCardID,
        Name:        displayName,
        Description: persona,
    },
}
tb.SpawnMember(ctx, ..., card, ...)
```

- [x] **Step 4: 修改 SpawnHumanAgent 中的调用（L857）**

```go
memberCardID := tb.teamName + "_" + memberName
card := &agentschema.AgentCard{
    BaseCard: agentschema.BaseCard{
        CardID:      memberCardID,
        Name:        displayName,
        Description: desc,
    },
}
result := tb.SpawnMember(ctx, memberName, displayName, card, ...)
```

- [x] **Step 5: 修改所有测试调用方**

搜索所有 `SpawnMember(` 调用，将 agentCard 字符串参数改为 AgentCard 结构体。

- [x] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`

- [x] **Step 7: 提交**

```bash
git add internal/agent_teams/
git commit -m "refactor: SpawnMember 签名改用 AgentCard 结构体，SpawnHumanAgent 构造完整 AgentCard"
```

---

### Task 11: 修复 interrupt_signal 重复提取（问题20）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go:L258`

- [x] **Step 1: 移除 L258 的重复提取**

删除 `L258`：`interruptSignal = ExtractInterruptSignal(result, nil)`

保留 L268 作为 err==nil 时的统一提取点，L228/L246 作为异常路径提取点。

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/handoff/...`

- [x] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go
git commit -m "fix: 移除 interrupt_signal 重复提取，统一为单次提取点"
```

---

### Task 12: 修改 i18n T() 为返回 (string, error)（问题21）

**Files:**
- Modify: `internal/agent_teams/schema/i18n.go:L157-L185`
- Modify: `internal/agent_teams/i18n.go:L38`
- Modify: `internal/agent_teams/tools/team_backend.go` — 所有 T() 调用方（4处）

- [x] **Step 1: 修改 T() 签名**

将 `i18n.go:L157` 的 `func T(key string, kwargs ...map[string]any) string` 改为 `func T(key string, kwargs ...map[string]any) (string, error)`

将 panic 替换为 `return "", fmt.Errorf("缺失 i18n key '%s'，语言 '%s' 和默认语言均无此键", key, lang)`

正常返回改为 `return translated, nil`

- [x] **Step 2: 修改 i18n 包装函数**

将 `internal/agent_teams/i18n.go:L38` 的 `func T(key string, kwargs ...map[string]any) string { return schema.T(key, kwargs...) }` 改为 `func T(key string, kwargs ...map[string]any) (string, error) { return schema.T(key, kwargs...) }`

- [x] **Step 3: 修改 team_backend.go 中 4 处 T() 调用**

所有 `atschema.T(...)` 调用改为处理 error：
```go
msg, err := atschema.T("team.shutdown_request_content")
if err != nil {
    logger.Warn(tbLogComponent).Err(err).Msg("i18n key missing, using fallback")
    msg = "team.shutdown_request_content" // 降级使用 key 本身
}
```

- [x] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`

- [x] **Step 5: 提交**

```bash
git add internal/agent_teams/schema/i18n.go internal/agent_teams/i18n.go internal/agent_teams/tools/team_backend.go
git commit -m "fix: i18n T() 改为返回 (string, error)，对齐 Python KeyError 可恢复行为"
```

---

### Task 13: 修复 AddInstalledPlugin 缺少 normalizePlugin + saveState（问题6+7）

**Files:**
- Modify: `internal/swarm/server/runtime/skill/skill_manager.go:L1905-L1918,L597,L606,L646,L655,L1275,L1283,L1714,L1722`

- [x] **Step 1: 修改 AddInstalledPlugin**

在 `L1906`（`plugins := sm.GetInstalledPlugins()`）前添加：
```go
plugin = sm.normalizePlugin(plugin)
```

在 `L1912`（`return`）前添加 `sm.saveState()`

在 `L1917`（`plugins = append` 后）的 `sm.state[...]` 后添加 `sm.saveState()`

- [x] **Step 2: 移除外层冗余 saveState 调用**

删除以下 4 处冗余 saveState：
- `L606`（HandleSkillsInstall 中）
- `L655`（HandleSkillsInstallBuiltin 中）
- `L1283`（HandleSkillsClawhubDownload 中）
- `L1722`（HandleSkillsTeamSkillsHubInstall 中）

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/runtime/skill/...`

- [x] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go
git commit -m "fix: AddInstalledPlugin 内补齐 normalizePlugin + saveState，移除外层冗余 saveState"
```

---

### Task 14: 修复 buildTeamskillsPublishZip README.md 路径 + defer f.Close() 累积（问题8+23）

**Files:**
- Modify: `internal/swarm/server/runtime/skill/plugin_yaml.go:L67,L77,L150,L172-L178`

- [x] **Step 1: 保存原始 skillDir**

在 `L69`（`skillMdPath := filepath.Join(skillDir, "SKILL.md")`）前添加：
```go
originalDir := skillDir // 保存原始路径，README.md 在原始目录查找（对齐 Python: root / "README.md"）
```

- [x] **Step 2: 修改 README.md 路径**

将 `L150`：
```go
readmePath := filepath.Join(filepath.Dir(skillDir), "README.md")
```
改为：
```go
readmePath := filepath.Join(originalDir, "README.md") // 对齐 Python: root / "README.md"
```

- [x] **Step 3: 修改 WalkDir 内 defer f.Close() 为闭包**

将 `L172-L178`：
```go
f, err := os.Open(path)
if err != nil {
    return nil
}
defer func() { _ = f.Close() }()
_, _ = io.Copy(w, f)
return nil
```

改为：
```go
func() {
    f, err := os.Open(path)
    if err != nil {
        return
    }
    defer func() { _ = f.Close() }()
    _, _ = io.Copy(w, f)
}()
return nil
```

- [x] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/runtime/skill/...`

- [x] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/skill/plugin_yaml.go
git commit -m "fix: README.md 在原始目录查找，WalkDir 内文件句柄用闭包立即关闭"
```

---

### Task 15: 修复 generateToolID 碰撞概率（问题24）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go:L204-L207`

- [x] **Step 1: 替换 generateToolID 实现**

将 `L204-L207`：
```go
func generateToolID() string {
    n, _ := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
    return fmt.Sprintf("%08x", n)
}
```

替换为：
```go
// generateToolID 生成唯一工具 ID。
//
// 对齐 Python: uuid.uuid4().hex（128 位随机，32 字符 hex）
func generateToolID() string {
    b := make([]byte, 16)
    _, _ = crypto_rand.Read(b)
    return hex.EncodeToString(b)
}
```

同时修改 import：删除 `"math/big"` 和 `"math/rand"`（如果无其他使用），添加 `"crypto/rand"` as `crypto_rand` 和 `"encoding/hex"`。

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/common/rails/...`

- [x] **Step 3: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go
git commit -m "fix: generateToolID 使用 crypto/rand 16 字节对齐 Python uuid4"
```

---

### Task 16: 重命名 SupervisorAgent.Create（问题34）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/supervisor_agent.go:L87`
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/supervisor_agent_test.go` — 5处调用

- [x] **Step 1: 重命名函数**

将 `L87` 的 `func Create(` 改为 `func NewSupervisorAgentCard(`

- [x] **Step 2: 修改测试文件中所有调用**

将 `supervisor_agent_test.go` 中 5 处 `Create(` 改为 `NewSupervisorAgentCard(`

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/...`

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/teams/hierarchical_msgbus/
git commit -m "refactor: SupervisorAgent.Create 重命名为 NewSupervisorAgentCard，对齐 Go 惯例"
```

---

### Task 17: 添加注释修改（问题4+10）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go` — 问题4
- Modify: `internal/swarm/server/runtime/agent_manager.go` — 问题10

- [x] **Step 1: 在 container_agent.go 中 HandoffRequest.InputMessage 字段添加注释**

在 `handoff_request.go` 的 `InputMessage map[string]any` 字段上方添加：
```go
// InputMessage 输入消息。
// 调用方必须将纯字符串包装为 {"query": msg} 传入，
// Python 的 isinstance(msg, dict) 安全网在 Go 中由类型系统保证。
```

- [x] **Step 2: 在 agent_manager.go 中 latestEnvOverrides 字段添加注释**

在 `latestEnvOverrides map[string]string` 字段上方添加：
```go
// latestEnvOverrides 最近一次 env 覆盖。
// 对齐 Python: self._latest_env_overrides。
// 注意：Go 收窄为 map[string]string，空字符串 "" 表示删除环境变量（Python 中 None = 删除，"" = 设置空串）。
// 当前约定：空串=删除，设置空串的需求极少，如有需要需改为 map[string]*string。
```

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_request.go internal/swarm/server/runtime/agent_manager.go
git commit -m "docs: 添加 InputMessage 调用方约定注释和 env override 空串语义注释"
```

---

### Task 18: 全量编译 + 测试验证

- [x] **Step 1: 检查残留编译进程**

Run: `pgrep -f 'go (build|test)'`

- [x] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

- [x] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/evolving/... ./internal/agentcore/memory/... ./internal/agent_teams/... ./internal/swarm/... -timeout 120s`

- [x] **Step 4: 提交最终状态**

确认所有测试通过后，更新 `IMPLEMENTATION_PLAN.md` 中的相关状态。

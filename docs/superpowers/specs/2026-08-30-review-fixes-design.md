# 审查问题修复设计文档

> 基于 `docs/review/2026-08-30-48h-logic-review.md` 的 36 个审查问题，
> 逐项对照 Python 源码和 Go 代码验证后，确认 25 个需修复、7 个排除、4 个暂不修复。

---

## 一、验证结论总表

### 排除项（4个误报 + 3个已知占位）

| # | 问题 | 排除原因 |
|---|------|---------|
| 1 | WithFsAppend(appendMode) | 误报：Go 语义等价，Python 硬编码 append=True 是因为 write_file 语义不同 |
| 16 | ShutdownMember 消息失败处理 | 与 Python 行为一致：失败都只记日志不提前返回 |
| 22 | CommunicableAgent agentID 保护 | Go 在 BindRuntime 有更严格的 eager 保护 |
| 35 | FromEvaluatedCase 未用 MakeEvolutionSignal | 误报：实际已使用 |
| 31 | MemUpdateChecker stub | 已标记 ⤵️ 待回填 |
| 33 | extractor.go 占位 | 已标记 ⤵️ 待回填 |
| 36 | readWorkspaceFile 未调用 | 已标记 ⤵️ 待回填 |

### 暂不修复项（5个）

| # | 问题 | 暂不修复原因 |
|---|------|-------------|
| 4 | buildAgentInput 非dict | Go 类型系统已排除纯字符串，仅加注释 |
| 10 | env override 空串语义 | 仅注释约定，实际场景极少需设置空串 |
| 14 | ListFragmentMemories nil vs [] | nil 和空切片 len() 行为一致，不影响功能 |
| 25 | GetTeamTrajectoryIssues 类型断言 | 当前无 JSON 反序列化场景，Python 也没有 |
| 26 | Evaluate 返回 nil vs [] | nil 和空切片功能等价，不影响调用方 |
| 27 | BaseOptimizerMixin.Bind config | 当前子类已自行覆盖 Bind 使用 config |
| 30 | delete 日志 memory_id 格式 | 仅日志格式差异，不影响功能 |

### 需修复项（23个，含2个仅注释修改）

详见下方各模块设计。

---

## 二、7.x Memory 模块修复

### 问题 9（🔴严重）：parseTimestamp 不支持无时区 ISO 时间戳

**文件**：`internal/evolving/experience/scorer.go` — `parseTimestamp` 函数

**方案**：增加无时区格式回退解析

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
    return time.Time{}, fmt.Errorf("cannot parse timestamp: %s", s)
}
```

### 问题 11（🟡一般）：MockEmbeddingProvider 返回空向量

**文件**：`internal/agentcore/memory/lite/embeddings.go` — `MockEmbeddingProvider`

**方案**：实现 128 维确定性随机向量，基于 md5(text) 种子

```go
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
    h := md5.Sum([]byte(text))
    seed := binary.BigEndian.Uint64(h[:8])
    r := rand.New(rand.NewSource(int64(seed)))
    vec := make([]float64, 128)
    for i := range vec {
        vec[i] = r.Float64()*2 - 1 // uniform(-1, 1)
    }
    return vec, nil
}

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

### 问题 12（🟡一般）：ResolveEmbeddingConfigFromEnv model_name 默认 "default"

**文件**：`internal/agentcore/memory/lite/embeddings.go` — `ResolveEmbeddingConfigFromEnv`

**方案**：删除 `envModelName = "default"` 硬编码和 `EMBED_MODEL` 环境变量读取。envModelName 为空时直接返回 nil。

### 问题 13（🟡一般）：baseEmbeddingAdapter.dims 默认值 0

**文件**：`internal/agentcore/memory/lite/embeddings.go` — `CreateEmbeddingProvider`

**方案**：返回 baseEmbeddingAdapter 时设置 `dims: 1024`

### 问题 28（🟢提示）：类型断言跳过无 Warn 日志

**文件**：`internal/agentcore/memory/manage/index/fragment_manager.go`

**方案**：`frag, ok := unit.(*mem_model.FragmentMemoryUnit)` 失败时，添加 logger.Warn 日志，包含 memory_type、user_id、scope_id 字段

### 问题 29（🟢提示）：降级到 mock 无 Warn 日志

**文件**：`internal/agentcore/memory/lite/embeddings.go` — `CreateEmbeddingProvider`

**方案**：`return NewMockEmbeddingProvider(), nil` 前添加 `logger.Warn(logComponent).Msg("Embedding API key not found, using mock provider")`

---

## 三、9.x Teams 模块修复

### 问题 2（🔴严重）：ShutdownMember 缺少 FSM 状态转换校验

**文件**：`internal/agent_teams/tools/team_backend.go` — `ShutdownMember`

**方案**：CAS 前调用 `fsm.IsValidMemberTransition` 校验，不合法时返回精确消息

```go
if !fsm.IsValidMemberTransition(member.Status, string(atschema.MemberStatusShutdownRequested)) {
    return atschema.NewMemberOpResultFail(
        fmt.Sprintf("Member %s cannot shut down from status '%s'", memberName, member.Status))
}
// CAS 转换（保留作为并发安全保障）
ok := tb.db.Member().TryTransitionMemberStatus(...)
```

### 问题 3（🔴严重）：ApprovePlan 缺少前置校验

**文件**：`internal/agent_teams/tools/team_backend.go` — `ApprovePlan`

**方案**：完整对齐 Python 三层校验

1. planID 空值检查 → 返回 fail
2. taskManager.Get(ctx, planID) 查询 plan record → 不存在返回 fail
3. db.Member().GetMember 查询 member → 不存在返回 fail

每步校验失败记录 Error 日志。

### 问题 5（🔴严重）：build_team 预定义成员未传 allocation

**文件**：`internal/agent_teams/tools/team_backend.go` — `BuildTeam`

**方案**：显式传 WithAllocation

```go
for _, pm := range tb.predefinedMembers {
    if pm.RoleType == atschema.TeamRoleHumanAgent {
        continue
    }
    memberCardID := tb.teamName + "_" + pm.MemberName
    var allocOpt SpawnMemberOption
    if tb.modelConfigAllocator != nil {
        allocOpt = WithAllocation(tb.modelConfigAllocator(pm.ModelName))
    }
    tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCardID,
        string(pm.RoleType), pm.Persona, pm.PromptHint, pm.ModelName, allocOpt)
}
```

### 问题 15（🟡一般）：CancelMember 失败返回 success

**文件**：`internal/agent_teams/tools/team_backend.go` — `CancelMember`

**方案**：检查 SendMessage 返回值，失败返回 fail

```go
result, err := tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
if err != nil || result == nil || !result.OK {
    logger.Error(tbLogComponent).Str("member_name", memberName).Err(err).
        Msg("CancelMember: 发送取消消息失败")
    return atschema.NewMemberOpResultFail("cancel message failed for: " + memberName)
}
```

### 问题 17（🟡一般）：CleanTeam 额外允许 ERROR 状态

**文件**：`internal/agent_teams/tools/team_backend.go` — `CleanTeam`

**方案**：移除 ERROR 豁免，只允许 SHUTDOWN

```go
if m.Status != string(atschema.MemberStatusShutdown) {
```

### 问题 18（🟡一般）：ForceCleanTeam 始终返回 true

**文件**：`internal/agent_teams/tools/team_backend.go` — `ForceCleanTeam`

**方案**：捕获 ForceDeleteTeamSession 返回值和 RemoveCleanupPaths 错误，综合返回

需要修改 `RemoveCleanupPaths` 签名增加 error 返回值。

### 问题 19（🟡一般）：SpawnHumanAgent 缺少 AgentCard

**文件**：`internal/agent_teams/tools/team_backend.go` — SpawnMember 签名及所有调用方

**方案**：重构 SpawnMember 签名，将 agentCard string 参数改为 AgentCard 结构体（含 ID/Name/Description）。SpawnHumanAgent 中构造完整 AgentCard 传入。

影响面：SpawnMember 所有调用方需同步修改（BuildTeam、SpawnHumanAgent 等）。

### 问题 20（🟡一般）：interrupt_signal 重复提取

**文件**：`internal/agentcore/multi_agent/teams/handoff/container_agent.go` — `Invoke`

**方案**：移除 L258 的重复提取，保留 L268（err==nil 正常路径）和 L228（异常路径）作为单次提取点。

### 问题 21（🟡一般）：i18n T() panic

**文件**：`internal/agent_teams/schema/i18n.go`（或 i18n 包路径）— `T()` 函数

**方案**：签名改为 `func T(key string) (string, error)`，key 缺失时返回 `("", fmt.Errorf(...))` 对齐 Python KeyError。所有调用方需处理 error。

### 问题 32（🟢提示）：CancelMember 缺少 reset 汇总日志

**文件**：`internal/agent_teams/tools/team_backend.go` — `CancelMember`

**方案**：添加 resetCount 变量，循环后记录 Info 汇总日志

```go
resetCount := 0
for _, t := range tasks {
    if err := tb.taskManager.Reset(ctx, t.TaskID); err != nil {
        logger.Warn(...)
    } else {
        resetCount++
    }
}
if resetCount > 0 {
    logger.Info(tbLogComponent).Str("member_name", memberName).
        Int("reset_count", resetCount).Msg("CancelMember: reset tasks")
}
```

### 问题 34（🟢提示）：SupervisorAgent.Create 命名误导

**文件**：`internal/agentcore/multi_agent/teams/hierarchical_msgbus/` — `Create` 函数

**方案**：重命名为 `NewSupervisorAgentCard`，所有调用方同步修改。

---

## 四、9.72 Evolving 模块修复

（无额外修复项，问题9归入7.x模块，问题25/26/27暂不修复）

---

## 五、10.x AgentServer 模块修复

### 问题 6+7（🔴严重）：AddInstalledPlugin 缺少 normalizePlugin + saveState

**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`

**方案**：
1. AddInstalledPlugin 入口处添加 `plugin = sm.normalizePlugin(plugin)`
2. 替换和追加两条路径末尾各加 `sm.saveState()`
3. 移除外层调用者的冗余 saveState 调用（HandleSkillsInstall、HandleSkillsInstallBuiltin、HandleSkillsClawhubDownload、HandleSkillsTeamSkillsHubInstall）

### 问题 8（🔴严重）：buildTeamskillsPublishZip README.md 路径错误

**文件**：`internal/swarm/server/runtime/skill/plugin_yaml.go` — `buildTeamskillsPublishZip`

**方案**：函数入口保存 `originalDir := skillDir`，README.md 查找改为 `filepath.Join(originalDir, "README.md")`

### 问题 23（🟡一般）：defer f.Close() 在 WalkDir 循环内累积

**文件**：`internal/swarm/server/runtime/skill/plugin_yaml.go` — `buildTeamskillsPublishZip`

**方案**：使用 lambda 闭包，defer 在闭包内生效

```go
func() {
    f, err := os.Open(path)
    if err != nil { return }
    defer func() { _ = f.Close() }()
    _, _ = io.Copy(w, f)
}()
```

### 问题 24（🟡一般）：generateToolID 碰撞概率高

**文件**：`internal/swarm/agents/harness/common/rails/structured_ask_user_tool.go`

**方案**：使用 crypto/rand 生成 16 字节，格式化为 32 字符 hex

```go
func generateToolID() string {
    b := make([]byte, 16)
    _, _ = crypto_rand.Read(b)
    return hex.EncodeToString(b)
}
```

---

## 六、影响面评估

| 影响等级 | 问题 | 说明 |
|---------|------|------|
| 🔴 大影响面 | #21 T()签名变更 | 所有 T() 调用方需处理 error |
| 🔴 大影响面 | #19 SpawnMember签名变更 | 所有 SpawnMember 调用方需适配 AgentCard 结构体 |
| 🟡 中影响面 | #6+7 移除外层saveState | 4个 HandleSkills* 方法 |
| 🟡 中影响面 | #18 RemoveCleanupPaths签名 | 需增加 error 返回值 |
| 🟡 中影响面 | #34 Create重命名 | 所有调用方 |
| 🟢 小影响面 | 其余问题 | 单文件或少量调用方修改 |

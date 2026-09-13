# 9.24 P3 SkillEvolutionRail 实现偏差修复设计

## 背景

9.24 P3 SkillEvolutionRail 实现完成后审查发现 9 个偏差和 4 类 any 使用问题。经逐个讨论，确定 6 项需立即修复，其余为文档更新。

## 修复项

### Fix 1: WithDisabledSkillsSet + normalizeNameSet 空实现

**问题**: `WithDisabledSkillsSet` 函数体为空，`normalizeNameSet()` 返回 `[]string{}`，导致 disabled_skills 传入后无效果。

**修复**:
- `SkillEvolutionRail` 结构体添加 `disabledSkills []string` 字段
- `WithDisabledSkillsSet` 写入 `r.disabledSkills = names`
- `normalizeNameSet()` 返回 `r.disabledSkills`
- 构造基类时 `WithDisabledSkills(r.normalizeNameSet())` 传入正确值

**涉及文件**: `skill_evolution_rail.go`

### Fix 2: ShouldHintSimplifyOrRebuild 走 EvolutionStore 封装

**问题**: 直接 `os.ReadFile(evoPath)` + `json.Unmarshal` 到 `map[string]any`，绕过 EvolutionStore 封装。

**修复**:
```go
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
    evoLog, err := r.evolutionStore.LoadFullEvolutionLog(ctx, skillName)
    if err != nil || evoLog == nil { return false }
    return len(evoLog.Entries) >= 10
}
```

**涉及文件**: `skill_evolution_rail.go`

### Fix 3: isOutcomeEvent 类型断言修复

**问题**: `isOutcomeEvent` 使用匿名结构体 `*struct{ Payload map[string]any }` 断言，但 `DrainPendingApprovalEvents` 返回 `[]*stream.OutputSchema`（Payload 类型是 `any`），断言永远失败，`watchEvolutionAndPush` 永远不退出。

**修复**: 改为接受 `*stream.OutputSchema`，正确提取：
```go
func isOutcomeEvent(event *stream.OutputSchema) bool {
    payload, ok := event.Payload.(map[string]any)
    if !ok { return false }
    meta, ok := payload["_evolution_meta"].(map[string]string)
    if !ok { return false }
    return meta["event_kind"] == "outcome"
}
```

**涉及文件**: `deep_adapter_evolution.go`

### Fix 4: pushEventToFrontend 对齐 Python 推送流程

**问题**: `pushEventToFrontend` 仅日志占位，未接入已有推送基础设施（`evolution/helpers.go` 中的 `PushEvolutionStatus`/`PushEvolutionEvent`/`PushEvolutionProgress`）。

**修复**:
1. 添加薄适配函数 `outputSchemaToDict(schema *stream.OutputSchema) map[string]any`：从 OutputSchema.Payload 提取 dict
2. `watchEvolutionAndPush` 增加 `channelID string` 参数
3. 构建 `EvolutionPushContext{Transport: ChannelPushTransport{}, ChannelID: channelID, SessionID: sessionID}`
4. 轮询 events 后，对每个事件按类型分流：
   - 进度事件 → `PushEvolutionProgress`
   - 审批事件 → `PushEvolutionEvent`
   - 结果事件 → `PushEvolutionStatus`
5. `pushEventToFrontend` 改为接受 `*stream.OutputSchema`，内部用 `outputSchemaToDict` 转换
6. 调用点同步更新

**涉及文件**: `deep_adapter_evolution.go`

### Fix 5: FromLegacyDict 补全新增字段

**问题**: `FromLegacyDict` 只解析 trajectory/messages/skill_name，新增的 PresentedEntries/SessionID/IncrementalMessages 未解析。

**修复**: 补全三个字段的解析逻辑，与 ToLegacyDict 对称。

**涉及文件**: `contracts.go`

### Fix 6: 定义 ApprovalAnswer 结构体 + 改签名

**问题**: `handleEvolutionApproval`/`handleGovernanceApproval`/`parseApprovalAnswers` 的 answers 参数为 `any`，运行时做类型断言。

**修复**:
1. 定义 `ApprovalAnswer` 结构体：
```go
type ApprovalAnswer struct {
    SelectedOptions []string `json:"selected_options"`
}
```
2. `HandleUserAnswer` 入口处从 `any` 解析为 `[]ApprovalAnswer`
3. `handleEvolutionApproval(requestID string, answers []ApprovalAnswer) bool`
4. `handleGovernanceApproval(requestID string, answers []ApprovalAnswer, approvalType string) bool`
5. `parseApprovalAnswers(answers []ApprovalAnswer) bool` — 检查 SelectedOptions 中是否含"接收"/"Accept"/"approve"/"执行"

**涉及文件**: `deep_adapter.go`, `deep_adapter_evolution.go`, `deep_adapter_slash.go`

## 不修复项（仅更新计划描述）

| # | 偏差 | 原因 |
|---|------|------|
| #1 | skillsDir []string vs string | 与 EvolutionStore API 对齐，合理 |
| #2 | evolver 包路径 import 别名 | 非功能偏差，代码正确 |
| #5 | utils 包不存在，本地重实现 | 实现合理，功能对齐 |
| #7 | evolveSkillWithSharing 合并 | 与 Python 对齐 |
| C-any #4 | Actions 保持 []map[string]any | LLM 输出非硬约束，加注释说明字段约定 |

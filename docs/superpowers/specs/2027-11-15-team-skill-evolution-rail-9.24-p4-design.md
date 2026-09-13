# 9.24 P4 — TeamSkillEvolutionRail 设计文档

## 概述

实现 TeamSkillEvolutionRail，团队技能演进护栏，是 SkillEvolutionRail（P3）的团队场景对应物。
当团队所有成员任务完成时，自动分析团队协作轨迹，发现协作中的问题（角色空缺、流程低效、约束违反）或用户意图，
生成团队技能经验记录，经用户审批后追加到 SKILL.md 的 evolution 区域。

对齐 Python: `openjiuwen/harness/rails/evolution/team_skill_evolution_rail.py`

## 在 Agent 会话中的流程位置

```
团队会话 ReAct 循环
  ├── before_invoke  → 重置 passiveEvolutionPending 标记
  ├── 每轮 tool_call → after_tool_call
  │   ├── 记录经验详情读取（experience detail reads）
  │   └── 拦截 view_task → 检测所有任务是否完成
  │       └── 若全部完成 → 标记 passiveEvolutionPending = true
  ├── after_invoke   → 检查 passiveEvolutionPending
  │   └── 若为 true → 触发 run_evolution（异步后台执行）
  └── run_evolution（后台）
      ├── 聚合团队轨迹（aggregateTeamTrajectory）
      ├── 检测使用的团队技能（detectUsedTeamSkill）
      ├── 运行 TeamSignalDetector（trajectory signals + user intent）
      ├── 暂存演化记录 → 审批/自动保存
      └── 评估已展示的经验（evaluatePresentedEntries）
```

## 与 SkillEvolutionRail 的核心区别

| 维度 | SkillEvolutionRail (P3) | TeamSkillEvolutionRail (P4) |
|------|--------------------------|------------------------------|
| 继承 | SkillEvolutionSharingMixin + EvolutionRail | EvolutionRail（无 SharingMixin） |
| Priority | 80 | 80 |
| 默认 member role | N/A | "leader" |
| 触发条件 | 每次 after_invoke | 仅在团队任务完成时 |
| 信号检测器 | ConversationSignalDetector | TeamSignalDetector |
| 技能范围 | 排除 team-skill/swarm-skill | 仅限 team-skill/swarm-skill |
| 轨迹来源 | 单 Agent | 聚合所有成员轨迹 |
| 优化器 | SkillExperienceOptimizer | TeamSkillExperienceOptimizer |
| 经验共享 | 有（ShareStager/ExperienceSharer） | 无 |
| auto_save 默认 | true | false（需审批） |
| 超时 | 600s | 720s |
| orchestrator prefix | skill_evolve | team_skill_evolve |

## 实现策略

**纯独立结构体（方案 A）**：与 Python 一比一对应，TeamSkillEvolutionRail 独立定义结构体，嵌入 `*EvolutionRail` 基类，独立实现全部 10 个 EvolutionExtension 方法。两个 Rail 为兄弟关系，零共享，5 个相同的静态方法各自实现一份。

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `evolution/team_skill_evolution_rail.go` | 结构体 + 构造器 + EvolutionExtension 10 方法 + 公开 API + 私有辅助 + 模块级函数 |
| `evolution/team_skill_evolution_rail_test.go` | 单元测试 |

### 修改已有文件

| 文件 | 变更 |
|------|------|
| `evolution/doc.go` | 添加 `team_skill_evolution_rail.go` 条目 + 更新包概述 |
| `adapter/deep_adapter_team.go` | 回填 3 个 stub |
| `adapter/deep_adapter.go` | 回填 `team_skill_evolve_` switch 分支 + 新增 `teamSkillEvolutionRail` 字段 |
| `IMPLEMENTATION_PLAN.md` | 9.24 P4 状态 ☐→✅ |

## 结构体设计

```go
type TeamSkillEvolutionRail struct {
    *EvolutionRail  // 嵌入基类

    // ─── 核心组件 ───
    evolutionStore     *checkpointing.EvolutionStore
    generator          *skillopt.TeamSkillExperienceOptimizer
    scorer             *experience.ExperienceScorer
    manager            *experience.ExperienceManager
    approvalRuntime    *EvolutionApprovalRuntime
    onlineUpdater      *single_dim.SingleDimUpdater
    onlineOrchestrator *experience.OnlineEvolutionOrchestrator
    teamSignalDetector *signal.TeamSignalDetector
    experienceTracker  *experience.ExperienceTracker

    // ─── 配置 ───
    autoScan                        bool
    autoSave                        bool
    userRequestLLMPolicy            llm_resilience.LLMInvokePolicy
    trajectoryIssueLLMPolicy        llm_resilience.LLMInvokePolicy
    evolutionTotalTimeoutSecs       float64
    evalInterval                    int
    language                        string
    teamID                          string
    trajectorySource                trajectory.TrajectorySource
    trajectoriesDir                 string

    // ─── 运行时状态 ───
    pendingApprovalSnapshots        map[string]*experience.PendingChange
    pendingGovernance               map[string]map[string]any
    experienceSkillOps              map[string]*skill_call.SkillExperienceOperator
    passiveEvolutionPending         bool
    hostCompletionPendingSessionID  *string
    processedSignalKeys             map[[4]string]bool
}
```

## 构造器

```go
func NewTeamSkillEvolutionRail(
    skillsDir []string,
    llmModel *llm.Model,
    modelName string,
    opts ...TeamSkillEvolutionRailOption,
) *TeamSkillEvolutionRail
```

Option 函数模式与 SkillEvolutionRail 一致，包括：
- `WithAutoScan` / `WithAutoSave` / `WithEvalInterval`
- `WithEvolutionTimeout` / `WithUserRequestLLMPolicy` / `WithTrajectoryIssueLLMPolicy`
- `WithRecordLLMPolicy` / `WithEvaluateLLMPolicy` / `WithSimplifyLLMPolicy`
- `WithTeamID` / `WithTrajectorySource` / `WithTrajectorySink` / `WithMemberRole`
- `WithAsyncEvolution` / `WithMaxConcurrentEvolution` / `WithDisabledSkills`

关键初始化差异（对比 SkillEvolutionRail）：
- `generator` = `TeamSkillExperienceOptimizer`（非 `SkillExperienceOptimizer`）
- `manager` 的 `kind` = `"team-skill"`（非 `"skill"`）
- `onlineOrchestrator` 的 `requestIDPrefix` = `"team_skill_evolve"`（非 `"skill_evolve"`）
- `onlineOrchestrator` 的 `stageSource` = `"team_skill_experience_updater"`
- `autoSave` 默认 `false`（非 `true`）
- `evolutionTotalTimeoutSecs` 默认 `720.0`（非 `600.0`）
- `defaultMemberRole` = `"leader"`（非空）
- 无 `shareStager` / `experienceSharer` / `keywordExtractor`

## EvolutionExtension 10 个方法

| 方法 | 团队特化行为 |
|------|-------------|
| `OnBeforeInvoke` | 重置 `passiveEvolutionPending = false` |
| `OnAfterModelCall` | 空实现（团队演化不在 model_call 后触发） |
| `OnAfterToolCall` | (1) 调用 `recordPresentedExperienceDetail` (2) 若 autoScan 且 tool_name="view_task"，调用 `isCompletedTeamTaskView` 判断完成，标记 `passiveEvolutionPending = true` |
| `OnAfterInvoke` | 空实现（触发由基类 AllowEvolutionTrigger 控制） |
| `OnAfterTaskIteration` | 空实现 |
| `OnAfterEvolutionTriggered` | 消费 `hostCompletionPendingSessionID`（置 nil） |
| `AllowEvolutionTrigger` | `autoScan && (passiveEvolutionPending \|\| hostCompletionPendingSessionID == currentBuilderSessionID)` |
| `SnapshotForEvolution` | 捕获轨迹 + 消息 + `skill_name="team-skill"` + `presentedEntries`（从 ExperienceTracker consume） |
| `RunEvolution` | 核心流程：聚合团队轨迹 → 检测 team skill → TeamSignalDetector → handleEvolutionFromSignals → evaluatePresentedEntries |
| `GetEvolutionTotalTimeoutSecs` | 返回 720.0 |

## 公开 API

| 方法 | Python 对应 |
|------|------------|
| `NotifyTeamCompleted(ctx) (bool, error)` | `notify_team_completed()` |
| `RequestUserEvolution(ctx, skillName, userIntent, autoApprove) (*EvolutionRequestResult, error)` | `request_user_evolution()` |
| `ApproveRecord(ctx, requestID) error` | `approve_record()` |
| `RejectRecord(ctx, requestID) error` | `reject_record()` |
| `RequestSimplify(ctx, skillName, userIntent) (*SimplifyRequestResult, error)` | `request_simplify()` |
| `OnApproveSimplify(ctx, requestID) (map[string]int, error)` | `on_approve_simplify()` |
| `OnRejectSimplify(requestID)` | `on_reject_simplify()` |
| `RequestRebuild(ctx, skillName, userIntent, minScore) (string, error)` | `request_rebuild()` |
| `RecordPresentedExperiences(ctx, skillName, snippet, sessionID, recordIDs)` | `record_presented_experiences()` |

属性访问器：`EvolutionStore()` / `Scorer()` / `Generator()` / `TeamSignalDetector()` / `ApprovalRuntime()` / `AutoScan()` / `AutoSave()` / `EvolutionConfig()` 等。

## 私有辅助方法

### 团队特有方法

| 方法 | 说明 |
|------|------|
| `isTeamSkill(ctx, name) bool` | 检查 SKILL.md frontmatter `kind ∈ {"team-skill", "swarm-skill"}` |
| `detectUsedTeamSkill(ctx, trajectory) string` | 从轨迹推断使用的 team skill |
| `teamSkillForExperienceDetailFile(ctx, filePath) string` | 从文件路径反查 team skill |
| `aggregateTeamTrajectory(trajectory) *Trajectory` | 通过 TrajectorySource 聚合团队轨迹 |
| `detectUserRequest(ctx, messages, skillContent) (*UserIntent, error)` | 桥接 TeamSignalDetector.detect_user_intent |
| `handleEvolutionFromSignals(...)` | 共享下游处理器（审批事件用 BuildTeamSkillApprovalEventFromRecords） |
| `stageEvolutionFromSignals(...)` | 通过 OnlineEvolutionOrchestrator 暂存 |

### 与 SkillEvolutionRail 同名但独立实现的方法

| 方法 | 说明 |
|------|------|
| `detectExperienceDetailRead(inputs) string` | 检测经验详情读取（过滤 team-skill 而非 regular） |
| `extractToolArgs(toolArgs) map[string]any` | 静态方法，提取工具参数 |
| `extractToolContent(inputs) string` | 静态方法，提取工具内容 |
| `extractPresentedRecordIDs(content) []string` | 静态方法，正则提取 record ID |
| `isExperienceDetailRelativePath(path) bool` | 静态方法，判断相对路径 |
| `appendUniqueSignal(signals, signal)` | 去重追加信号 |
| `emitProgress(stage, message)` | TUI 进度（rail_kind="team"，前缀="[Team Skill Evolution]"） |
| `buildRecordApprovalEvent(...) *OutputSchema` | 用 BuildTeamSkillApprovalEventFromRecords |
| `emitRecordApprovalEvent(...)` | 缓存审批事件 + 进度日志 |

### ExperienceTracker 代理方法

| 方法 | 说明 |
|------|------|
| `recordPresentedExperienceDetail(ctx, inputs)` | 在 after_tool_call 中记录 |
| `consumePresentedEntries(session) []PresentedRecordEntry` | 快照捕获时消费 |
| `evaluatePresentedEntries(entries)` | RunEvolution 末尾评估 |

## 模块级辅助函数

| 函数 | Python 对应 | 说明 |
|------|------------|------|
| `isCompletedTeamTaskView(result any) bool` | `is_completed_team_task_view()` | 检查 view_task 结果：有 completed 且无 pending/claimed/in_progress/blocked |
| `inferTeamSkillFromTrajectory(traj, knownSkills, store) string` | `infer_team_skill_from_trajectory()` | 从轨迹推断 team skill（调用 checkpointing.InferSkillFromTexts） |

## 常量

```go
const (
    teamSkillDefaultEvolutionTimeoutSecs = 720.0
    teamSkillKinds                       = map[string]bool{"team-skill": true, "swarm-skill": true}
    teamTaskNonTerminalStates            = []string{"pending", "claimed", "in_progress", "blocked"}
)
```

LLM Policy 常量（对齐 Python）：
- `teamUserRequestLLMPolicy`: attempt=60s, total=300s, max=2
- `teamTrajectoryIssueLLMPolicy`: attempt=60s, total=300s, max=2
- `teamRecordLLMPolicy`: attempt=150s, total=300s, max=2

正则常量：
- `skillMDRe`: `[/\\]([^/\\]+)[/\\]SKILL\.md`
- `experienceRecordHeadingRe`: `#+\s*\[([A-Za-z0-9_-]+)\]`

## DeepAdapter 回填

### 新增字段

在 DeepAdapter 结构体中新增：
```go
teamSkillEvolutionRail *evolution.TeamSkillEvolutionRail
```

### findTeamSkillRail

在 `instance.rails` 中遍历查找 `*TeamSkillEvolutionRail` 类型，赋值到 `d.teamSkillEvolutionRail`。
对齐 Python: `_find_team_skill_rail()` (line 3651-3670)

### handleTeamSkillEvolveApproval

参照 `handleEvolutionApproval()` 模式：
1. 检查 `d.teamSkillEvolutionRail != nil`
2. 解析 answers 为 approve/reject
3. 调用 `d.teamSkillEvolutionRail.ApproveRecord(ctx, requestID)` 或 `RejectRecord(ctx, requestID)`
4. 启动 `watchTeamSkillEvolutionAndPush` 观察器（复用现有 `watchEvolutionAndPush` 模式，从 TeamSkillEvolutionRail 排空事件）

对齐 Python: `handle_team_skill_evolve_approval()` (line 3651-3767)

### pushTeamSkillEvolveResolutionStatus

1. 检查 `d.teamSkillEvolutionRail != nil`
2. 调用 `d.teamSkillEvolutionRail.DrainPendingHostEvents(true, nil)` 获取结果
3. 通过 globalSendPushFunc 推送

对齐 Python: `_push_team_skill_evolve_resolution_status()` (line 3768-3790)

### deep_adapter.go switch 分支

将 `team_skill_evolve_` 前缀的 `resolved = false` 替换为：
```go
resolved = d.handleTeamSkillEvolveApproval(ctx, requestID, parsedAnswers, sessionID, channelID)
```

## 不做的事情

- ❌ 不在 DeepAdapter 中实现 TeamSkillEvolutionRail 的实例化/注入（留到 10.6.3-10）
- ❌ 不实现独立的 `watchTeamSkillEvolutionAndPush`（复用现有观察器模式，在 handleTeamSkillEvolveApproval 内启动）
- ❌ 不实现 Sharing（TeamSkillEvolutionRail 不需要共享，Python 也没有 SkillEvolutionSharingMixin）
- ❌ 不提取公共工具函数（纯独立结构体策略）
- ❌ 不实现 9.68-69 的 team.plan 特化

## 依赖关系

全部基础设施已就绪：
- ✅ TeamSkillExperienceOptimizer（9.72d）
- ✅ TeamSignalDetector（9.73）
- ✅ EvolutionStore（9.78）
- ✅ ExperienceManager + OnlineEvolutionOrchestrator（9.79）
- ✅ BuildTeamSkillApprovalEventFromRecords（审批事件）
- ✅ EvolutionApprovalRuntime（审批运行时）
- ✅ EvolutionRail 基类 + EvolutionExtension 接口（P2）
- ✅ SkillEvolutionRail（P3，作为参考/对照）
- ✅ ParseTopLevelFrontmatter（用于 isTeamSkill）
- ✅ InferSkillFromTexts（用于 inferTeamSkillFromTrajectory）

## 测试计划

### 单元测试文件：team_skill_evolution_rail_test.go

1. **构造器测试**：NewTeamSkillEvolutionRail 基本构造 / Option 覆盖 / 默认值验证
2. **EvolutionExtension 方法测试**：
   - OnBeforeInvoke 重置 passiveEvolutionPending
   - OnAfterToolCall view_task 拦截 + 任务完成检测
   - AllowEvolutionTrigger 条件组合
   - SnapshotForEvolution 包含 presentedEntries
   - GetEvolutionTotalTimeoutSecs 返回 720.0
3. **公开 API 测试**：
   - NotifyTeamCompleted 标记 hostCompletionPendingSessionID
   - ApproveRecord / RejectRecord 路由到 approvalRuntime
   - RequestSimplify / OnApproveSimplify / OnRejectSimplify
4. **辅助方法测试**：
   - isTeamSkill 判断
   - isCompletedTeamTaskView 判断
   - inferTeamSkillFromTrajectory 推断
   - extractToolArgs / extractToolContent / extractPresentedRecordIDs / isExperienceDetailRelativePath
5. **审批事件测试**：buildRecordApprovalEvent 使用 BuildTeamSkillApprovalEventFromRecords

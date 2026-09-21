# 48h 逻辑审查 — 2026-09-21 (Evolution Sharing 专项)

> 审查范围：最近 48h 内提交涉及 9.24 EvolutionRail P5 跨用户共享差距修复（G1-G8），
> 以及 b04da762 审查文档修复的 12 项逻辑问题。
> 重点对照 Python 参考项目 `openjiuwen/harness/rails/evolution/` 和 `openjiuwen/agent_evolving/sharing/`。

**审查方法**：逐方法对照 Python 参考实现，检查方法签名、执行步骤、错误处理、日志对齐；验证 ⤵️/TODO 标记的真实状态

---

## 问题汇总

| 级别 | 数量 |
|------|------|
| 严重 | 10 |
| 一般 | 14 |
| 提示 | 12 |

---

## 严重问题 (S)

### S-01: syncSkillPackage 中 SkillPackageMeta.UploadedAt 为空字符串

**Python 样例** (experience_sharer.py L177-183):
```python
# SkillPackageMeta dataclass 的 uploaded_at 字段有 default_factory
@dataclass
class SkillPackageMeta:
    skill_id: str
    skill_name: str
    description: str
    uploaded_at: str = field(default_factory=_now_iso)  # 自动填充 UTC 时间

# _sync_skill_package 中创建 meta 时不需要显式传 uploaded_at
meta = SkillPackageMeta(
    skill_id=resolved_id,
    skill_name=meta.skill_name,
    description=meta.description,
)
# meta.uploaded_at 自动为 "2026-09-21T12:34:56.789012+00:00"
```

**Go 问题** (experience_sharer.go L514-518):
```go
meta := SkillPackageMeta{
    SkillID:     skillID,
    SkillName:   firstNonEmpty(resolvedName, skillName),
    Description: description,
    // UploadedAt 未设置！Go 零值为空字符串 ""
}
```

`SkillPackageMeta.UploadedAt` 为空字符串，导致 `UploadSkillPackage` 写入 `meta.json` 时 `uploaded_at` 字段为空。下游 `ensureGlobalIndexEntry` 中全局索引也使用 `meta.UploadedAt`，会写入空字符串。

**影响**：skill package 元数据的上传时间丢失，影响 hub 搜索排序和审计。

**修复方案**：在构造 `SkillPackageMeta` 时显式设置 `UploadedAt`:
```go
meta := SkillPackageMeta{
    SkillID:     skillID,
    SkillName:   firstNonEmpty(resolvedName, skillName),
    Description: description,
    UploadedAt:  time.Now().UTC().Format(time.RFC3339Nano),
}
```

---

### S-02: emitSharedRecordsApproval 中 StageRecords 使用 context.Background() 丢弃调用方 ctx

**Python 样例** (skill_evolution_sharing.py L250-267):
```python
async def _emit_shared_records_approval(self, *, ctx, skill_name, records, messages):
    request = self._manager.stage_records(
        skill_name, records,
        source="experience_sharing", messages=messages, is_shared_records=True,
    )
    await self._emit_generated_records(ctx, skill_name, request)
```
Python 的 `stage_records` 在异步上下文中执行，自然继承调用方的 async context。

**Go 问题** (skill_evolution_rail.go L1384):
```go
request, _ := r.manager.StageRecords(
    context.Background(), skillName, records,  // ← context.Background() 丢弃了调用方 ctx
    true, "experience_sharing", "", nil, nil, ...
)
```

`emitSharedRecordsApproval` 接收 `ctx context.Context` 参数，但调用 `StageRecords` 时传了 `context.Background()`，导致：
- 超时控制失效：如果调用方设置了 deadline，StageRecords 不会被取消
- 追踪链断裂：分布式追踪无法从上层传播到 StageRecords

**影响**：审批流程中的 StageRecords 无法被上层超时控制，可能导致演化操作在系统关闭后仍然执行。

**修复方案**：将 `context.Background()` 改为 `ctx`:
```go
request, _ := r.manager.StageRecords(
    ctx, skillName, records,
    true, "experience_sharing", "", nil, nil, ...
)
```

---

### S-03: 同步模式 RunEvolution 丢失 AgentCallbackContext

**Python 样例** (evolution_rail.py L478):
```python
# 同步模式 — 直接调用 run_evolution(trajectory, ctx)
await self.run_evolution(trajectory, ctx)
```

**Go 问题** (evolution_rail.go L734):
```go
// 同步模式 — 直接调用 RunEvolution
return r.ext.RunEvolution(context.Background(), traj, nil)
```

Python 同步模式传入活跃的 `ctx`（`AgentCallbackContext`），Go 同步模式用 `context.Background()` 代替且 `snapshot` 为 `nil`。这意味着 Go 同步模式下 `RunEvolution` 无法访问原始的 `AgentCallbackContext`（包含 session、agent card 等关键信息）。

**影响**：同步模式下（`asyncEvolution=false`）的演化无法获取 session 信息、无法读取 agent card、无法构建正确的快照。`SkillEvolutionRail.RunEvolution` 在 `snapshot == nil` 时从 `traj` 重建 messages，但丢失了 `presentedEntries`、`sessionID`、`incrementalMessages` 等关键数据。

**修复方案**：在同步模式下，先调用 `SnapshotForEvolution` 获取快照，再调用 `RunEvolution`:
```go
} else {
    // 同步模式 — 先捕获快照，再执行演化
    snapshot := r.ext.SnapshotForEvolution(context.Background(), traj, cbc)
    return r.ext.RunEvolution(context.Background(), traj, snapshot)
}
```

---

### S-04: evolveSkillWithSharing 不传递 cbc 给 handleEvolutionFromSignals

**Python 样例** (skill_evolution_sharing.py L204-248):
```python
async def _evolve_skill_with_sharing(self, *, skill_name, skill_signals, messages, ctx, shared_records):
    if shared_records:
        shared_records = await self._filter_duplicate_shared_records(skill_name, shared_records)
    if not shared_records:
        request = await self._handle_evolution_from_signals(
            skill_name=skill_name, signals=skill_signals, messages=messages,
            ctx=ctx,  # ← 传递了原始回调上下文
            requires_approval=not self._auto_save)
        return request is not None
    if self._auto_save:
        await self._handle_evolution_from_signals(
            skill_name=skill_name, signals=skill_signals, messages=messages,
            ctx=ctx,  # ← 传递了原始回调上下文
            requires_approval=False)
        return True
```

**Go 问题** (skill_evolution_rail.go L1341, L1362):
```go
// 无共享记录分支
request, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages,
    nil,  // ← cbc 为 nil！
    "", !r.autoSave, true)

// autoSave 分支
if _, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages,
    nil,  // ← cbc 为 nil！
    "", false, true); err != nil {
```

`handleEvolutionFromSignals` 的 `cbc` 参数在所有调用处都传了 `nil`。当 `requiresApproval=true` 时，`handleEvolutionFromSignals` 调用 `emitGeneratedRecords(cbc, ...)` → `BuildSkillApprovalEvent` → 构建审批事件。`cbc=nil` 导致审批事件中缺少 agent 上下文信息。

**影响**：审批事件无法关联到具体的 Agent 会话，影响审批 UI 的显示和审批结果的路由。

**修复方案**：在 `RunEvolution` 中保存 `cbc`（通过 snapshot 或新增字段），在 `evolveSkillWithSharing` 调用时传递：
```go
// 方案1: 在 EvolutionSnapshot 中增加 Cbc 字段
type EvolutionSnapshot struct {
    Trajectory           *trajectory.Trajectory
    Messages             []map[string]any
    Cbc                  *agentinterfaces.AgentCallbackContext  // 新增
    ...
}
```

---

### S-05: HasSkillPackage 无异常防御，syncSkillPackage 可能误判

**Python 样例** (experience_sharer.py L124-131):
```python
try:
    exists = await self._backend.has_skill_package(skill_id)
except Exception:
    logger.warning("[ExperienceSharer] has_skill_package failed; skipping sync")
    return  # 异常时中止整个 sync 流程
```

**Go 问题** (experience_sharer.go L500):
```go
exists := es.backend.HasSkillPackage(ctx, skillID)
// 直接使用 exists，无异常处理
```

Go 中 `HasSkillPackage` 返回 `(bool, error)`，但 `syncSkillPackage` 只取了 `bool` 忽略了 `error`。如果 `HasSkillPackage` 因为 IO 错误返回 `(false, err)`，Go 会误判为 skill package 不存在，触发重复上传。Python 在异常时中止整个 sync 流程（不继续上传 bundle）。

**影响**：当 hub 存储出现 IO 错误时，Go 会重复上传 skill package，可能覆盖已有数据或产生不一致。

**修复方案**：检查 `HasSkillPackage` 的 error 返回值：
```go
exists, err := es.backend.HasSkillPackage(ctx, skillID)
if err != nil {
    logger.Warn(logComponent).Err(err).Str("skill_id", skillID).
        Msg("[ExperienceSharer] has_skill_package 失败; 跳过 sync")
    return
}
```

---

### S-06: downloadSharedExperiences 中 sharedExp.Record.Context 无空值保护

**Python 样例** (skill_evolution_sharing.py L398):
```python
shared_exp.record.context = (shared_exp.record.context or "") + marker
# or "" 确保即使 context 为 None 也不会 TypeError
```

**Go 问题** (skill_evolution_rail.go L1310):
```go
sharedExp.Record.Context = (sharedExp.Record.Context) + marker
// 无空值保护；如果 Context 为空字符串可正常拼接
// 但 EvolutionRecord.Context 类型需确认是否为 string（非 *string）
```

如果 `EvolutionRecord.Context` 是 `string` 类型（非指针），Go 零值为 `""`，拼接无问题。但如果是 `*string` 类型，nil 解引用会 panic。需确认类型定义。

**影响**：如果 Context 字段为 nil 指针，运行时 panic 导致演化流程崩溃。

**修复方案**：添加空值保护（防御性编程）：
```go
ctx := sharedExp.Record.Context
if ctx == "" { ctx = " " }  // 或按实际类型处理
sharedExp.Record.Context = ctx + marker
```

---

### S-07: TeamSkillEvolutionRail.RunEvolution 同步模式不获取 presentedEntries

**Python 样例** (team_skill_evolution_rail.py L580-583):
```python
elif ctx is not None:
    messages = self._collect_messages_from_trajectory(trajectory)
    session = ctx.session
    presented_entries = self._consume_presented_entries(session)
```

**Go 问题** (team_skill_evolution_rail.go L612-617):
```go
if snapshot != nil {
    messages = snapshot.Messages
    presentedEntries = snapshot.PresentedEntries
} else {
    messages = collectMessagesFromTrajectory(traj)
    // presentedEntries 未获取！保持 nil
}
```

Go 同步模式（`snapshot == nil`）下只收集了 `messages`，但没有获取 `presentedEntries`（经验展示条目）。Python 通过 `ctx.session` 获取并传入 `_consume_presented_entries`。

**影响**：同步模式下 `evaluatePresented` 不会评估任何已展示的经验，可能导致经验评分遗漏。

**修复方案**：在同步模式分支中，从轨迹中恢复 sessionID 并获取 presentedEntries：
```go
} else {
    messages = collectMessagesFromTrajectory(traj)
    if traj.SessionID != "" {
        presentedEntries = r.consumePresentedEntries(traj.SessionID)
    }
}
```

---

### S-08: RunEvolution 中 recover() 只捕获 panic，error 返回值逃逸

**Python 样例** (team_skill_evolution_rail.py L569-670):
```python
try:
    signals = await self.team_signal_detector.detect_trajectory_signals(...)
    ...
except Exception as exc:
    logger.warning("[TeamSkillEvolutionRail] run_evolution failed: %s", exc)
```

**Go 问题** (team_skill_evolution_rail.go L590-597):
```go
defer func() {
    if rec := recover(); rec != nil {
        logger.Warn(logComponent).Any("panic", rec).Msg("[TeamSkillEvolutionRail] RunEvolution panic")
    }
}()
```

Go 的 `recover()` 只捕获 panic（nil 指针解引用等），不捕获 `error` 返回值。如果子方法返回 error 且没有被局部处理（例如信号检测返回 error），error 会从 `RunEvolution` 返回到 `safeRunEvolution`，但 `safeRunEvolution` 只记录 Warn 后返回 err，不会发出 outcome 事件（因为 err 不是 context.DeadlineExceeded）。

**影响**：演化失败时无法通过 outcome 事件通知宿主系统，宿主无法知道演化已失败。

**修复方案**：在 `RunEvolution` 中对关键步骤的错误做统一捕获处理：
```go
defer func() {
    if rec := recover(); rec != nil {
        logger.Warn(logComponent).Any("panic", rec).Msg("[TeamSkillEvolutionRail] RunEvolution panic")
        // 设置错误标记供 safeRunEvolution 发出 outcome 事件
    }
}()
// 或者在 safeRunEvolution 中统一处理所有非 DeadlineExceeded 的 error：
if err != nil {
    outcome := map[string]string{"status": "failed", ...}
    r.emitBackgroundOutcomeEvent(outcome)
    return err
}
```

---

### S-09: _emit_background_outcome_event 的 rail_kind 默认值缺失

**Python 样例** (evolution_rail.py L718-719):
```python
rail_kind=outcome.get("rail_kind", "base")  # 默认 "base"
```

**Go 问题** (evolution_rail.go L800):
```go
RailKind: stringPtr(outcome["rail_kind"]),  // stringPtr("") 返回 nil
```

Go 中 `stringPtr` 对空字符串返回 `nil`，而 Python 默认为 `"base"`。当 outcome 中没有 `rail_kind` 时，Go 的 `EvolutionHostEventMeta.RailKind` 为 `nil`，`ToPayload()` 中 nil 指针被跳过，导致事件元数据中缺少 `rail_kind` 字段。

**影响**：后台演化结果事件缺少 `rail_kind` 字段，宿主系统无法区分事件来源。

**修复方案**：添加默认值逻辑：
```go
railKind := outcome["rail_kind"]
if railKind == "" {
    railKind = "base"
}
meta := EvolutionHostEventMeta{
    RailKind: &railKind,
    ...
}
```

---

### S-10: emitGeneratedRecords 缺少 request_id 的进度事件

**Python 样例** (skill_evolution_rail.py L744-748):
```python
self._emit_progress(
    "approval_required",
    f"experience records for '{skill_name}' ready, awaiting approval",
    skill_name=skill_name,
    request_id=approval_request.request_id,  # ← 包含 request_id
)
```

**Go 问题** (skill_evolution_rail.go L1785):
```go
r.emitProgress("approval_required", fmt.Sprintf("'%s' 的经验记录已就绪，等待审批", skillName),
    WithSkillName(skillName))
    // ← 缺少 WithRequestID(approvalRequest.RequestID)
```

`emitProgress` 调用时缺少 `WithRequestID` 选项，导致 `approval_required` 进度事件中不包含 `request_id` 字段。

**影响**：宿主系统收到 `approval_required` 事件后无法关联到具体的审批请求，影响审批流程的用户体验。

**修复方案**：添加 `WithRequestID`:
```go
r.emitProgress("approval_required",
    fmt.Sprintf("'%s' 的经验记录已就绪，等待审批", skillName),
    WithSkillName(skillName),
    WithRequestID(approvalRequest.RequestID))
```

---

## 一般问题 (M)

### M-01: KeywordExtractor 多传了 GenerateRecordsLLMPolicy 参数

**Python 样例** (skill_evolution_sharing.py L65):
```python
self._keyword_extractor = KeywordExtractor(llm=llm, model=model, language=language)
# 只有 3 个参数，使用默认的 QUERY_KEYWORDS_LLM_POLICY
```

**Go 问题** (skill_evolution_rail.go L1086):
```go
r.keywordExtractor = sharing.NewKeywordExtractor(llmModel, model, language, skillopt.GenerateRecordsLLMPolicy)
// 传了 4 个参数，第 4 个是生成策略而非查询策略
```

Go 传了 `GenerateRecordsLLMPolicy`（生成经验记录的策略，超时/budget 通常更大），Python 使用默认的 `QUERY_KEYWORDS_LLM_POLICY`（`attempt_timeout_secs=1500, total_budget_secs=4000, max_attempts=5`）。两者的超时/重试参数可能不同，导致关键词提取的行为与 Python 不一致。

**修复方案**：使用正确的查询策略 `QUERY_KEYWORDS_LLM_POLICY` 或对应的 Go 常量：
```go
r.keywordExtractor = sharing.NewKeywordExtractor(llmModel, model, language, skillopt.QueryKeywordsLLMPolicy)
```

---

### M-02: makeSharingContextProvider 中 ReadPristineSkillContent 失败静默降级

**Python 样例** (skill_evolution_sharing.py L73-82):
```python
async def _provider(skill_name: str):
    skill_id = await evolution_store.ensure_skill_id(skill_name)
    package_bytes = await evolution_store.pack_skill_for_sharing(skill_name)
    content = await evolution_store.read_pristine_skill_content(skill_name)
    description = evolution_store.extract_description_from_skill_md(content)
    return skill_id, package_bytes, skill_name, description
```

**Go 问题** (skill_evolution_rail.go L1182):
```go
content, err := r.evolutionStore.ReadPristineSkillContent(ctx, skillName)
if err != nil { return skillID, packageBytes, skillName, "", nil }
// err 不返回！description 为空但 error 为 nil
```

`ReadPristineSkillContent` 失败时，Go 返回 `error=nil` 和空的 description，调用方不知道发生了降级。Python 中异常会向上传播。

**影响**：skill package 上传到 hub 时可能缺少 description，但调用方不知道降级发生。

**修复方案**：至少记录一条 Warn 日志：
```go
content, err := r.evolutionStore.ReadPristineSkillContent(ctx, skillName)
if err != nil {
    logger.Warn(logComponent).Err(err).Str("skill", skillName).
        Msg("[SkillEvolutionRail] ReadPristineSkillContent 失败; description 为空")
    return skillID, packageBytes, skillName, "", nil
}
```

---

### M-03: sharingAfterAutoApproved 中 records 直接引用未复制

**Python 样例** (skill_evolution_sharing.py L333-349):
```python
records = list(pending.payload) if pending is not None else []
# list() 显式复制，避免后续修改影响原始数据
```

**Go 问题** (skill_evolution_rail.go L1769):
```go
records := stagedRequest.PendingChange.Payload
// 直接引用，无复制
```

Go 中 `records` 是对 `PendingChange.Payload` 的直接引用（slice 底层数组共享）。如果 `uploadApprovedRecordsForSharing` 中修改了 records（例如去重过滤），会影响 `PendingChange.Payload` 的内容。Python 用 `list()` 显式复制避免此问题。

**影响**：如果 `uploadApprovedRecordsForSharing` 的内部逻辑修改了 records slice，可能导致 `PendingChange.Payload` 被意外修改。

**修复方案**：复制 slice：
```go
records := make([]checkpointing.EvolutionRecord, len(stagedRequest.PendingChange.Payload))
copy(records, stagedRequest.PendingChange.Payload)
```

---

### M-04: ShareStager._wrap 中 record 未完全深拷贝

**Python 样例** (share_stager.py L80):
```python
record=copy.deepcopy(record)  # 深拷贝，完全隔离
```

**Go 问题** (share_stager.go L190):
```go
Record: record  // 值拷贝；但如果 EvolutionRecord 含 slice/map 字段则为浅拷贝
```

Go 中 struct 赋值是值拷贝，但如果 `EvolutionRecord` 包含 `[]string`、`map[string]any` 等引用类型字段，这些字段仍然共享底层内存。

**影响**：如果 `EvolutionRecord` 的引用类型字段在入队后被修改，可能影响已暂存的共享记录的数据一致性。

**修复方案**：检查 `EvolutionRecord` 的字段类型，如果包含引用类型则使用深拷贝：
```go
// 如果 EvolutionRecord 含引用类型
import "github.com/mohae/deepcopy"
recordCopy := deepcopy.Copy(record).(checkpointing.EvolutionRecord)
```

---

### M-05: initSharing 中 enabled 检查对 sharingConfig==nil 的处理不一致

**Python 样例** (skill_evolution_sharing.py L104-110):
```python
@classmethod
def _build_experience_sharer(cls, sharing_config):
    config = sharing_config or {}
    enabled = cls._resolve_bool(config.get("enabled", False))  # 默认 False
    ...
```

**Go 问题** (skill_evolution_rail.go L1069):
```go
if !enabled || sharingConfig == nil {
    // sharingConfig == nil 时直接禁用，即使 enabled 解析为 true
    r.experienceSharer = nil
    ...
    return
}
```

Go 中 `sharingConfig == nil` 是短路条件，即使 `enabled` 解析为 true（例如通过环境变量 `EVOLUTION_SHARING_ENABLED=true`），只要 `sharingConfig` 为 nil 就会禁用共享。Python 中 `_build_experience_sharer` 接收 `sharing_config` 时先做 `config = sharing_config or {}`，nil 等价于空 dict，不会阻止环境变量启用。

**影响**：当 `sharingConfig` 为 nil 但环境变量 `EVOLUTION_SHARING_ENABLED=true` 时，Go 会禁用共享而 Python 会启用。

**修复方案**：移除 `sharingConfig == nil` 的短路判断，或调整为 `if !enabled`：
```go
if !enabled {
    r.experienceSharer = nil; r.keywordExtractor = nil; r.shareStager = nil
    r.sharingEnabled = false
    return
}
if sharingConfig == nil {
    sharingConfig = map[string]any{}  // nil 等价于空 map
}
```

---

### M-06: TeamSkillEvolutionRail 信号统计方式与 Python 不一致

**Python 样例** (team_skill_evolution_rail.py L636-643):
```python
for sig in trajectory_issue_signals:
    issues = get_team_trajectory_issues(sig)  # 获取子问题列表
    issue_count = len(issues)  # 统计子问题数（一个信号可能包含多个 issue）
    ...
```

**Go 问题** (team_skill_evolution_rail.go L670-677):
```go
for _, sig := range signals {
    if sig.SignalType == string(signal.TeamSignalTypeTrajectoryIssue) {
        issueCount++  // 统计信号级别数量，不调用 getTeamTrajectoryIssues 获取子问题数
    }
}
```

Python 统计的是 trajectory issue 的子问题数（一个信号可能包含多个子问题），Go 统计的是信号级别的数量。两者的语义不同。

**影响**：日志中报告的 issue 数量与 Python 不一致，可能误导开发者判断问题的严重程度。

**修复方案**：调用 `getTeamTrajectoryIssues` 获取子问题列表并统计：
```go
for _, sig := range signals {
    if sig.SignalType == string(signal.TeamSignalTypeTrajectoryIssue) {
        issues := getTeamTrajectoryIssues(sig)
        issueCount += len(issues)
    }
}
```

---

### M-07: TeamSkillEvolutionRail.approval_runtime 无运行时验证

**Python 样例** (team_skill_evolution_rail.py L372-385):
```python
@property
def approval_runtime(self):
    # 每次访问时检查 _manager 和 _pending_approval_snapshots 是否与当前实例一致
    if (self._manager is not self._experience_manager
        or self._pending_approval_snapshots is not self._pending_approval_snapshots_store):
        self._approval_runtime = EvolutionApprovalRuntime(...)
    return self._approval_runtime
```

**Go 问题** (team_skill_evolution_rail.go L271-274):
```go
// 构造时初始化一次，无运行时验证
r.approvalRuntime = NewEvolutionApprovalRuntime(r.manager, r.pendingApprovalSnapshots)
```

Go 在构造时初始化 `approvalRuntime`，假设 `manager` 和 `pendingApprovalSnapshots` 不会在运行时被替换。Python 有防御性检查，每次访问时验证一致性。

**影响**：如果 `manager` 或 `pendingApprovalSnapshots` 在运行时被替换（虽然目前不太可能），审批流程会使用过期的 runtime。

**修复方案**：当前风险较低，可暂不修复。如果未来需要支持运行时替换，需改为 property 模式。

---

### M-08: TeamSkillEvolutionRail.team_signal_detector 无惰性初始化

**Python 样例** (team_skill_evolution_rail.py L296-317):
```python
@property
def team_signal_detector(self):
    if self._team_signal_detector is None:
        self._team_signal_detector = TeamSignalDetector(
            llm=self._generator.llm,
            model=self._generator.model,
            ...
        )
    return self._team_signal_detector
```

**Go 问题** (team_skill_evolution_rail.go L298-302):
```go
// 构造时直接初始化
r.teamSignalDetector = signal.NewTeamSignalDetector(llmModel, modelName, ...)
```

Python 使用 `@property` 惰性创建 `team_signal_detector`，Go 在构造时直接创建。如果 `TeamSignalDetector` 构造涉及重量级操作（如 LLM 客户端初始化），Go 会推迟启动时间。

**影响**：启动性能略有影响，不影响功能。

**修复方案**：当前风险较低，保持现状即可。如果需要优化启动性能，可改为惰性初始化。

---

### M-09: evolution_config 缺少 max_concurrent_evolution 字段

**Python 样例** (team_skill_evolution_rail.py L340-351):
```python
@property
def evolution_config(self):
    return {
        "record_llm_policy": self._generator.record_llm_policy,
        "max_concurrent_evolution": self._max_concurrent_evolution,
    }
```

**Go 问题** (team_skill_evolution_rail.go L1005-1015):
```go
func (r *TeamSkillEvolutionRail) EvolutionConfig() map[string]any {
    return map[string]any{
        "record_llm_policy": r.recordLLMPolicy,
        // 缺少 "max_concurrent_evolution"
    }
}
```

**影响**：下游代码如果依赖 `evolution_config["max_concurrent_evolution"]` 会获取不到值。

**修复方案**：添加缺失字段：
```go
return map[string]any{
    "record_llm_policy":       r.recordLLMPolicy,
    "max_concurrent_evolution": r.maxConcurrentEvolution,
}
```

---

### M-10: FlushPendingUploads 中 lock 粒度与 Python 不同

**Python 样例** (experience_sharer.py L108-141):
```python
async def flush_pending_uploads(self, skill_name):
    async with self._lock:  # 整个方法期间持有锁（包括上传重试）
        pending = self._pending_uploads.pop(skill_name, [])
        ...
        # 重试循环也在锁内
```

**Go 问题** (experience_sharer.go L215-280):
```go
func (es *ExperienceSharer) FlushPendingUploads(ctx context.Context, skillName string) (*UploadResult, error) {
    es.mu.Lock()
    pending := es.pendingUploads[skillName]
    delete(es.pendingUploads, skillName)
    es.mu.Unlock()  // ← 只在取队列的瞬间加锁
    // 重试循环在锁外执行
```

Go 仅在取走队列时加锁，上传重试循环在锁外执行。Python 在整个方法期间持锁。Go 版本并发度更高，但上传期间其他 goroutine 可以继续入队，可能导致同一个 skill 的上传和入队并发。

**影响**：并发环境下可能出现同一个 skill 的多个 flush 操作同时执行（虽然 Python 也不会，因为有 asyncio 单线程保证）。Go 的行为是设计选择差异，但可能导致上传顺序与 Python 不一致。

**修复方案**：保持当前行为（Go 的并发模型不同，锁粒度调整是合理的），但在注释中说明设计差异。

---

### M-11: KeywordExtractor 异常分类丢失

**Python 样例** (keyword_extractor.py L75-80):
```python
except BaseError as exc:
    logger.warning("[KeywordExtractor] LLM call failed (%s)", exc)
except Exception as exc:
    logger.warning("[KeywordExtractor] unexpected LLM error (%s)", exc)
```

**Go 问题** (keyword_extractor.go):
```go
if err != nil {
    logger.Warn(...).Msg("[KeywordExtractor] LLM 调用失败")
}
// 只有一处 err != nil，统一处理，不区分 BaseError 和意外异常
```

**影响**：丢失了 Python 中区分"预期 LLM 错误"和"意外异常"的能力，影响问题排查。

**修复方案**：使用 Go 的错误类型断言区分：
```go
if err != nil {
    var baseErr *exception.BaseError
    if errors.As(err, &baseErr) {
        logger.Warn(...).Str("error_type", "base_error").Msg("[KeywordExtractor] LLM 调用失败")
    } else {
        logger.Warn(...).Str("error_type", "unexpected").Msg("[KeywordExtractor] 意外 LLM 错误")
    }
}
```

---

### M-12: ApprovalRuntime.FinalizeStagedEvolutionRequest 返回值差异

**Python 样例** (approval_runtime.py L93-115):
```python
async def finalize_staged_evolution_request(self, request, requires_approval, emit_approval_request, on_auto_approved):
    ...
    return request  # 始终返回 request
```

**Go 问题** (approval_runtime.go L100-121):
```go
func (r *EvolutionApprovalRuntime) FinalizeStagedEvolutionRequest(
    request *experience.ExperienceApprovalRequest, ...) error {
    ...
    return nil  // 返回 error，不返回 request
}
```

Go 返回 `error`，Python 返回 `request`。Go 的 `handleEvolutionFromSignals` 使用 `request` 变量（来自 stage 结果），不依赖 Finalize 返回值。Python 的 `_handle_evolution_from_signals` 返回 `await self.approval_runtime.finalize_staged_evolution_request(...)` 即 Finalize 的返回值。

**影响**：当前 Go 调用方已适配，不影响功能。但语义差异可能导致后续维护混淆。

**修复方案**：保持当前行为，在注释中说明差异。

---

### M-13: EvolutionSnapshot 不可变性丢失

**Python 样例** (contracts.py L28-35):
```python
@dataclass(frozen=True)
class EvolutionSnapshot:
    trajectory: Trajectory
    messages: list[dict]
    skill_name: Optional[str] = None
```

**Go 问题** (contracts.go):
```go
type EvolutionSnapshot struct {
    Trajectory           *trajectory.Trajectory
    Messages             []map[string]any
    ...
    // 普通 struct，可变
}
```

Python 使用 `frozen=True` dataclass 保证不可变，Go 的 struct 是可变的。同样 `EvolutionHostEventMeta` 和 `EvolutionRequestResult` 在 Python 中也是 frozen dataclass，Go 中可变。

**影响**：并发场景下可能出现数据竞争，例如一个 goroutine 修改 snapshot 的 Messages 而另一个 goroutine 正在读取。

**修复方案**：在使用时注意不要修改 snapshot，或在关键路径使用深拷贝。

---

### M-14: LocalFileBackend.UploadBundle 中 indexFile.Close 失败不触发 outbox

**Python 样例** (local_file.py):
```python
try:
    # mkdir + write bundle + write index + upsert global index
except OSError:
    self._spool_to_outbox(bundle, reason)  # 任何 OSError 都触发 outbox
```

**Go 问题** (local_file.go):
```go
// indexFile.Close() 失败只记录 warning，不 spoolToOutbox
if err := indexFile.Close(); err != nil {
    logger.Warn(logComponent).Err(err).Msg("[LocalFileBackend] 关闭索引文件失败")
}
```

Python 中 `indexFile.close()` 如果抛出 `OSError` 会被外层 `except` 捕获并触发 outbox。Go 中 `Close()` 失败只记录 warning。

**影响**：如果索引文件写入不完整（Close 失败意味着数据可能未 flush），bundle 数据丢失且不会进入 outbox 备份。

**修复方案**：在 `Close()` 失败时也触发 spoolToOutbox：
```go
if err := indexFile.Close(); err != nil {
    logger.Warn(logComponent).Err(err).Msg("[LocalFileBackend] 关闭索引文件失败")
    b.spoolToOutbox(bundle, "index_close_failed")
    return UploadResult{OK: false, Reason: "index_close_failed", Retryable: true}
}
```

---

## 提示问题 (T)

### T-01: emitGeneratedRecords 日志缺少 record_count 中的 request_id

**Python 样例** (skill_evolution_rail.py L750-755):
```python
logger.info(
    "[SkillEvolutionRail] buffered approval request (%s) with %d record(s) for skill=%s",
    approval_request.request_id,
    approval_request.proposal.record_count,
    skill_name,
)
```

**Go 问题** (skill_evolution_rail.go L1787-1791):
```go
logger.Info(logComponent).
    Str("request_id", approvalRequest.RequestID).
    Int("record_count", approvalRequest.Proposal.RecordCount()).
    Str("skill", skillName).
    Msg("[SkillEvolutionRail] 审批请求已缓存")
```

Go 日志格式与 Python 基本对齐，但 `record_count` 的取值路径需确认 `RecordCount()` 方法是否正确返回 `proposal` 中的记录数。

**修复方案**：确认 `Proposal.RecordCount()` 的实现正确即可。

---

### T-02: ShareStager QC 丢弃日志格式中文化

**Python 样例** (share_stager.py):
```python
logger.info("[ShareStager] share-QC dropped record=%s reason=%s skill=%s", ...)
```

**Go 问题** (share_stager.go):
```go
logger.Info(logComponent).Msg("[ShareStager] 共享 QC 丢弃记录")
```

日志消息中文化不影响功能，但与 Python 的英文日志不一致。

**修复方案**：保持中文即可，对齐项目编码规范。

---

### T-03: 时间格式差异（+00:00 vs Z）

**Python**: `datetime.now(tz=timezone.utc).isoformat()` → `2026-09-21T12:34:56.789012+00:00`
**Go**: `time.Now().UTC().Format(time.RFC3339Nano)` → `2026-09-21T12:34:56.789012Z`

**影响**：字符串精确比对或哈希时可能不一致，但不影响功能。

**修复方案**：无需修复，两者都是合法的 ISO 8601 / RFC 3339 格式。

---

### T-04: DedupJaccardThreshold 负数处理差异

**Python**: 构造函数默认 `dedup_jaccard_threshold=0.85`，传入负数也保留负数
**Go**: `if dedupJaccardThreshold <= 0 { dedupJaccardThreshold = 0.85 }` 传入负数回退到 0.85

**影响**：Go 更防御性，不影响正常使用。

**修复方案**：无需修复，Go 的行为更安全。

---

### T-05: SharedSkillBundle.make 去重使用 List vs Map

**Python**: `seen: List[str]`，O(n) 线性查找
**Go**: `seen := make(map[string]bool)`，O(1) 哈希查找

**影响**：Go 更高效，行为等价。

**修复方案**：无需修复。

---

### T-06: KeywordExtractor 构造函数零值 policy 检测

**Python**: 总是接受传入的 `query_llm_policy`
**Go**: `if p.MaxAttempts == 0 { p = QUERY_KEYWORDS_LLM_POLICY }` 零值回退

**影响**：Go 更防御性，不影响正常使用。

**修复方案**：无需修复。

---

### T-07: DownloadRelevant 中 mirror 失败不影响返回

**Python**: `_mirror_bundle(bundle, kind="downloaded")` 无返回值，失败不传播
**Go**: `es.mirrorBundle(&bundles[i], "downloaded")` 返回 error 但调用方只记 warning

**影响**：行为等价。

**修复方案**：无需修复。

---

### T-08: EvolutionSnapshot 字段扩展差异

**Python**: 3 字段 + dict 动态注入
**Go**: 6 字段强类型（增加了 `PresentedEntries`、`SessionID`、`IncrementalMessages`）

**影响**：Go 的设计更清晰，类型安全。这是设计选择差异，不是 bug。

**修复方案**：无需修复。

---

### T-09: eval_interval < 1 处理差异

**Python**: `raise ValueError("eval_interval must be >= 1")`
**Go**: `if r.evalInterval < 1 { r.evalInterval = 1 }` 静默修正

**影响**：Go 更宽容，不影响功能。

**修复方案**：无需修复。

---

### T-10: ShareStager 构造中 qc_score_threshold 硬编码

**Python**: `ShareStager(keyword_extractor=..., sharer=...)` — 2 参数，`qc_score_threshold` 使用默认值 0.6
**Go**: `sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0.6, nil)` — 4 参数，显式传 0.6

**影响**：行为一致，Go 显式传值更清晰。

**修复方案**：无需修复。

---

### T-11: buildExperienceSharer 异常处理方式差异

**Python**: `try/except Exception` 捕获构造异常
**Go**: `defer recover()` 捕获 panic

**影响**：Go 的 recover 只捕获 panic，不捕获 error 返回值。但在 Go 中构造函数通常不返回 error，panic 是唯一可捕获的异常路径。行为等价。

**修复方案**：无需修复。

---

### T-12: FlushPendingUploads 重试退避支持 ctx 取消

**Python**: `await asyncio.sleep(...)` 无法被 ctx 取消中断
**Go**: 使用 `select` 监听 `ctx.Done()`，退避等待中若 ctx 被取消则立即返回

**影响**：Go 增强了超时控制能力，是正向差异。

**修复方案**：无需修复。

---

## ⤵️ 占位标记验证

在 evolution 包和 sharing 包的 .go 源码中，**未发现任何 ⤵️、⤴️、TODO、FIXME 标记**。所有代码均已实现，无占位符。

---

## 修复优先级建议

| 优先级 | 问题 | 说明 |
|--------|------|------|
| P0 | S-01 | UploadedAt 为空导致 hub 元数据不完整 |
| P0 | S-02 | context.Background() 丢弃 ctx 影响超时控制 |
| P0 | S-05 | HasSkillPackage error 忽略可能导致重复上传 |
| P1 | S-03 | 同步模式丢失 ctx（影响低，目前都用异步模式） |
| P1 | S-04 | cbc=nil 影响审批事件上下文 |
| P1 | S-06 | Context 空值保护（取决于类型定义） |
| P1 | M-01 | KeywordExtractor LLM 策略传错 |
| P1 | M-05 | sharingConfig==nil 短路与 env 变量矛盾 |
| P2 | S-07 | 同步模式不获取 presentedEntries |
| P2 | S-08 | RunEvolution error 逃逸 |
| P2 | S-09 | rail_kind 默认值缺失 |
| P2 | S-10 | 进度事件缺少 request_id |
| P2 | M-02 | ReadPristineSkillContent 静默降级 |
| P2 | M-03 | records 未复制 |
| P2 | M-04 | record 浅拷贝 |
| P3 | 其余 | 影响较小或为设计选择差异 |

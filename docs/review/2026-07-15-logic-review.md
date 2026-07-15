# 代码逻辑审查报告 — 2026-07-15

> 审查范围：48小时内提交的代码（`1cc3f2a..c266d81`，共 79 个 commit，274 个 Go 文件变更）
>
> 审查章节：9.55 TeamAgent、9.57 AgentConfigurator、9.70 Trainer/Dataset/Constant、9.71 Evaluator、9.20 Interrupt、10.3 Adapter 辅助（EvolutionHelpers/RecapPrompts/SysOpBuilder/Session/GatewayPush）
>
> 审查方法：逐文件对照 Python 参考项目方法签名和逻辑步骤，识别移植遗漏和不一致

---

## 问题汇总

| 等级 | 数量 |
|------|------|
| 🔴 严重（功能逻辑缺失或错误） | 18 |
| 🟡 一般（实现差异但不影响核心流程） | 16 |
| 🔵 提示（风格/日志等不影响功能） | 12 |

---

## 🔴 严重问题

### S01. CaseLoader.Split 缺少 shuffle，与 Python 行为不一致

**章节**：9.70b Dataset

**Python 原始代码** (`case_loader.py:79-97`):
```python
def split(self, ratio: float, seed: int = 0) -> Tuple["CaseLoader", "CaseLoader"]:
    if not 0.0 <= ratio <= 1.0:
        raise ValueError(f"ratio must be in [0.0, 1.0], got {ratio}")
    shuffled = shuffle_cases(self._cases, seed)
    cut = int(len(shuffled) * ratio)
    return CaseLoader(shuffled[:cut]), CaseLoader(shuffled[cut:])
```

**Go 问题代码** (`case_loader.go:43-63`):
```go
func (cl *CaseLoader) Split(ratio float64) (*CaseLoader, *CaseLoader) {
    // 无 shuffle，无 ratio 校验
    splitIdx := int(float64(len(cl.cases)) * ratio)
    copy(trainCases, cl.cases[:splitIdx])
    copy(valCases, cl.cases[splitIdx:])
    return NewCaseLoader(trainCases), NewCaseLoader(valCases)
}
```

**问题**：Python 的 `split()` 先用 `shuffle_cases(self._cases, seed)` 打乱顺序再分割，Go 直接按原始顺序分割，且缺少 ratio 范围校验。训练集/验证集数据分布不同，影响评估结果可复现性。

**修复方案**：
```go
func (cl *CaseLoader) Split(ratio float64, seed int64) (*CaseLoader, *CaseLoader, error) {
    if ratio < 0.0 || ratio > 1.0 {
        return nil, nil, fmt.Errorf("ratio must be in [0.0, 1.0], got %f", ratio)
    }
    shuffled := ShuffledCopy(cl.cases, seed) // 先打乱
    cut := int(float64(len(shuffled)) * ratio)
    return NewCaseLoader(shuffled[:cut]), NewCaseLoader(shuffled[cut:]), nil
}
```

---

### S02. MetricEvaluator.Evaluate 的 MetricResult 类型丢失 Python 的 float 分支

**章节**：9.71 Evaluator

**Python 原始代码** (`evaluator.py:233-246`):
```python
for metric in self._metrics:
    out = metric.compute(predict, case.label, question=case.inputs, case=case)
    if isinstance(out, dict):          # dict 分支
        for k, v in out.items():
            vf = self._safe_convert(v)
            per_metric[k] = vf
            scores.append(vf)
    else:                              # float 分支 — 自动用 metric.name 作为 key
        score = self._safe_convert(out)
        per_metric[metric.name] = score
        scores.append(score)
```

**Go 问题代码** (`evaluator.go:208-222`):
```go
for _, metric := range m.metrics {
    result, err := metric.Compute(predict, case_.Label, ...)
    for k, v := range result {         // 只有 dict 分支，没有 float→metric.Name() 映射
        vf := safeConvert(v)
        perMetric[k] = vf
        scores = append(scores, vf)
    }
}
```

**问题**：Python 的 `Metric.compute()` 返回 `Union[float, Dict[str, float]]`，当返回 float 时自动用 `metric.name` 作为 key。Go 的 `MetricResult = map[string]float64` 丢失了 "float → 自动用 metric.Name() 映射" 保障，如果某个 Metric 忘了用 metric.Name() 作为 key，映射关系就丢失了。

**修复方案**：在 MetricEvaluator.Evaluate 中添加 fallback — 当 result 只有一个 key 且不等于 metric.Name() 时补充映射：
```go
if len(result) == 0 {
    // float 分支 fallback：Metric 未返回 key 时自动用 metric.Name()
    perMetric[metric.Name()] = 0.0
} else if _, hasKey := result[metric.Name()]; !hasKey && len(result) == 1 {
    // 单 key 但不是 metric.Name()，补充映射
    for _, v := range result {
        perMetric[metric.Name()] = safeConvert(v)
        break
    }
}
```

---

### S03. GroupEvolutionApprovals 返回值第二项语义错误

**章节**：10.3 EvolutionHelpers

**Python 原始代码** (`evolution_helpers.py:357-373`):
```python
def group_evolution_approvals(session_id, events, *, warn_missing_request_id=None):
    grouped = {}
    for evt in events:
        if not is_evolution_approval_event(evt): continue
        request_id = extract_evolution_request_id(evt)
        if request_id is None:
            if warn_missing_request_id is not None:
                warn_missing_request_id(session_id)
            continue  # ← 跳过，不加入任何列表
        grouped.setdefault(request_id, []).append(evt)
    return grouped, []  # ← 第二项始终为空列表
```

**Go 问题代码** (`helpers.go:575-595`):
```go
func GroupEvolutionApprovals(sessionID string, events []any, warnMissing ...WarnMissingRequestIDFunc) (map[string][]any, []string) {
    var missing []string
    for _, evt := range events {
        // ...
        if requestID == nil {
            // ...
            missing = append(missing, "")  // ← 错误：往 missing 追加空串
            continue
        }
        // ...
    }
    return grouped, missing  // ← 第二项不为空
}
```

**问题**：Python 第二项返回值始终为空列表 `[]`（仅调用 warn 回调但不收集缺失的 request_id）。Go 版本往 `missing` 追加空字符串，导致返回值第二项与 Python 语义不一致。如果调用方依赖第二项为空来判定无缺失，会产生误判。

**修复方案**：移除 `missing = append(missing, "")`，直接返回 `grouped, nil`。

---

### S04. SendPush 缺少"无活跃连接"保护检查和异常处理

**章节**：10.3 AgentServer

**Python 原始代码** (`agent_ws_server.py:4342-4365`):
```python
async def send_push(self, msg) -> None:
    if self._current_ws is None or self._current_send_lock is None:
        logger.warning("[AgentWebSocketServer] send_push 失败: 无活跃 Gateway 连接")
        return
    try:
        wire = build_server_push_wire(msg)
        async with self._current_send_lock:
            await self._current_ws.send(json.dumps(wire, ensure_ascii=False))
    except Exception as e:
        logger.warning("[AgentWebSocketServer] send_push 失败: %s", e)
```

**Go 问题代码** (`agent_server.go:181-201`):
```go
func (s *AgentServer) SendPush(ctx context.Context, msg map[string]any) error {
    wire := transport.BuildServerPushWire(msg)
    data, err := json.Marshal(wire)
    // ...
    s.sendToGateway(data)  // ← 无活跃连接检查，无异常捕获，错误被静默丢弃
    return nil             // ← 总是返回 nil
}
```

**问题**：
1. 没有检查 Gateway 连接是否活跃，可能导致 panic 或写入已关闭的通道
2. `sendToGateway` 内部丢弃错误，`SendPush` 始终返回 nil，调用方无法得知推送是否成功

**修复方案**：
```go
func (s *AgentServer) SendPush(ctx context.Context, msg map[string]any) error {
    if s.transport == nil {
        logger.Warn(logComponent).Msg("SendPush 失败: 无活跃 Gateway 连接")
        return nil  // 对齐 Python：warn + 静默返回
    }
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("error", r).Msg("SendPush 失败")
        }
    }()
    wire := transport.BuildServerPushWire(msg)
    data, err := json.Marshal(wire)
    if err != nil {
        logger.Warn(logComponent).Err(err).Msg("SendPush: wire 编码失败")
        return nil  // 对齐 Python：warn + 静默返回
    }
    s.sendToGateway(data)
    return nil
}
```

---

### S05. sessionMetadata 缺少 round_id 字段（多处）

**章节**：10.3 Session

**Python 原始代码** (`session_metadata.py:138-150`):
```python
metadata = {
    "session_id": session_id,
    "channel_id": channel_id,
    "user_id": user_id,
    "created_at": _current_timestamp(),
    "last_message_at": _current_timestamp(),
    "title": title,
    "message_count": 0,
    "mode": mode,
    "team_name": team_name,
    "round_id": 0,
}
```

**Go 问题代码** (`handle_session.go:372-380`):
```go
meta := map[string]any{
    "session_id":      sessionID,
    "channel_id":      "",
    "created_at":      ts,
    "last_message_at": ts,
    "title":           "",
    "message_count":   0,
    "mode":            "unknown",
    // ← 缺少 "round_id": 0
    // ← 缺少 "team_name": ""
}
```

**问题**：Go 创建 sessionMetadata 时缺少 `round_id` 和 `team_name` 字段。同样的问题也存在于 `SetSessionDeliveryContext` 的 fallback metadata 创建中。如果其他模块读取 `metadata.json` 时期望这两个字段存在，会导致 nil/空值问题。

**修复方案**：在所有创建 metadata 的地方补充 `"round_id": 0` 和 `"team_name": ""`。

---

### S06. sessionMetadata 缺少 channel_metadata 字段

**章节**：10.3 Session

**Python 原始代码** (`session_metadata.py:196-197`):
```python
if channel_metadata:
    metadata["channel_metadata"] = channel_metadata
```

**Go 问题代码**：`handle_session.go` 中没有 `channel_metadata` 相关逻辑。session 的渠道元数据丢失，影响前端展示和路由。

**修复方案**：在 `sessionMetadata` 中添加 `ChannelMetadata map[string]any \`json:"channel_metadata,omitempty"\``，在 session 创建/更新时对齐 Python 逻辑。

---

### S07. handleSessionCreate 缺少 channel_id 从 request 提取

**章节**：10.3 Session

**Python 原始代码**：`_handle_session_create` 调用 `init_session_metadata` 时传入 `channel_id=channel_id`。

**Go 问题代码** (`handle_session.go:372`):
```go
"channel_id": "",  // ← 总是空字符串，未从 request 获取
```

**问题**：Go 的 `handleSessionCreate` 没有从 request 中提取 `channel_id` 写入 metadata。

**修复方案**：从 request 中提取 channel_id 并写入 metadata。

---

### S08. ApproveResult.NewArgs 空字符串语义不一致

**章节**：9.20 Interrupt

**Python 原始代码** (`interrupt_base.py:142-144`):
```python
if isinstance(decision, ApproveResult):
    if decision.new_args is not None:  # ← is not None：空字符串也会替换
        ctx.inputs.tool_args = decision.new_args
```

**Go 问题代码** (`interrupt_base.go:202`):
```go
if d.NewArgs != "" {  // ← != ""：空字符串不会替换
    toolInputs.ToolArgs = d.NewArgs
}
```

**问题**：Python 用 `is not None` 判断，`new_args=""` (空字符串) 也会替换参数。Go 用 `!= ""` 判断，空字符串不会替换参数。语义不一致，影响 Interrupt Rail 的审批行为。

**修复方案**：将 `NewArgs` 类型改为 `*string`，nil 表示不替换，`&""` 表示替换为空字符串：
```go
type ApproveResult struct {
    Approved bool
    NewArgs  *string  // nil = 不替换, &"" = 替换为空字符串
}
// 判断时：
if d.NewArgs != nil {
    toolInputs.ToolArgs = *d.NewArgs
}
```

---

### S09. TeamAgent.Lifecycle() 默认值不一致

**章节**：9.55 TeamAgent

**Python 原始代码**：
```python
@property
def lifecycle(self) -> str:
    if self.spec is None:
        return "temporary"
    return self.spec.lifecycle
```

**Go 问题代码**：
```go
func (a *TeamAgent) Lifecycle() string {
    if a.configurator != nil {
        return a.configurator.Lifecycle()
    }
    return ""  // ← 错误：应返回 "temporary"
}
```

**问题**：当 `configurator` 为 nil 时返回空字符串 `""`，而 Python 明确返回 `"temporary"`。

**修复方案**：将 `return ""` 改为 `return "temporary"`。

---

### S10. TeamAgent.Configure() 的 SetupInfra 回调全传 nil

**章节**：9.55 TeamAgent

**Python 原始代码**：
```python
def _setup_infra(self, spec: TeamAgentSpec, ctx: TeamRuntimeContext) -> None:
    self._configurator.setup_infra(
        spec, ctx,
        on_teammate_created=self._on_teammate_created,  # ← 实际方法引用
        on_team_cleaned=self._mark_team_cleaned,
        on_team_built=self._mark_team_built,
    )
```

**Go 问题代码**：
```go
a.configurator.SetupInfra(spec, runtimeCtx,
    WithOnTeammateCreated(nil),  // ← 传 nil！
    WithOnTeamCleaned(nil),
    WithOnTeamBuilt(nil),
)
```

**问题**：Python 中三个回调是 TeamAgent 的方法，在 configure 时传递给 configurator。Go 版本传了 nil，意味着：队友创建后无法自动 spawn、clean_team 完成后无法锁存标志、build_team 完成后无法持久化 DB 状态。

**修复方案**：待 `SpawnManager`/`SessionManager` 等依赖就绪后回填。当前应在代码注释中明确标注：
```go
// ⤵️ 回填: 9.58 SpawnManager 就绪后传入 _on_teammate_created
// ⤵️ 回填: 9.59 SessionManager 就绪后传入 _mark_team_cleaned/_mark_team_built
```

---

### S11. TeamHarness.RunAgentCustomizer 缺少 panic recover

**章节**：9.57 AgentConfigurator

**Python 原始代码**：
```python
def run_agent_customizer(self, customizer: AgentCustomizer) -> None:
    try:
        customizer(self._deep_agent, self._member_name, self._role.value)
    except Exception as exc:
        team_logger.warning("[{}] agent_customizer failed: {}", self._member_name or "?", exc)
```

**Go 问题代码**：
```go
func (h *TeamHarness) RunAgentCustomizer(customizer AgentCustomizer) {
    if customizer == nil { return }
    // ← 无 recover/panic 捕获
    customizer(h.deepAgent, h.memberName, h.role)
}
```

**问题**：Python 用 try/except 吞掉异常保证团队启动不被破坏。Go 版本没有 `recover()` 或错误处理，如果 customizer panic 会导致整个 goroutine 崩溃。

**修复方案**：
```go
func (h *TeamHarness) RunAgentCustomizer(customizer AgentCustomizer) {
    if customizer == nil { return }
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Str("member_name", h.memberName).
                Any("error", r).Msg("agent_customizer 失败")
        }
    }()
    customizer(h.deepAgent, h.memberName, h.role)
}
```

---

### S12. Trainer 骨架缺少 6 个核心方法桩

**章节**：9.70 Trainer

**Python 原始方法**：
1. `_select_best_candidate_on_val()` — 多候选评估选择
2. `_snapshot_operators_state()` / `_restore_operators_state()` — Operator 状态快照/回滚
3. `_get_operator_registry()` — 从 Agent 获取 Operator 注册表
4. `_bind_updater()` / `_updater_requires_forward()` — 绑定 Updater、判断是否需要前向推理
5. `_resume_if_needed()` / `_save_checkpoint_if_needed()` — 断点续训恢复/保存

**Go 问题代码**：`trainer.go` 中完全无这些方法。

**问题**：Python Trainer 的 8 个内部方法在 Go 骨架中完全没有预留桩，后续填充时容易遗漏逻辑分支。

**修复方案**：添加桩方法，参数使用 `any` 占位：
```go
// SelectBestCandidateOnVal 多候选评估与选择（方案 A）。
// 依赖 9.70a Operator + 9.70b Dataset 填充后实现。
func (t *Trainer) SelectBestCandidateOnVal(_ context.Context, _, _, _, _ any) (float64, any, error) {
    return 0, nil, errors.New("not implemented: Trainer.SelectBestCandidateOnVal")
}

// SnapshotOperatorsState 快照当前 Operator 状态。
// 依赖 9.70a Operator 填充后实现。
func (t *Trainer) SnapshotOperatorsState(_ any) any { return nil }

// RestoreOperatorsState 恢复 Operator 状态。
func (t *Trainer) RestoreOperatorsState(_, _ any) {}
```

---

### S13. Trainer.Callbacks 字段使用 any 而非函数类型，丧失类型安全

**章节**：9.70 Trainer

**Python 原始代码**：
```python
class Callbacks:
    def on_train_begin(self, agent, progress, eval_info): pass
    def on_train_end(self, agent, progress, eval_info): pass
    def on_train_epoch_begin(self, agent, progress): pass
    def on_train_epoch_end(self, agent, progress, eval_info): pass
```

**Go 问题代码**：
```go
type Callbacks struct {
    OnTrainBegin any    // ← 丧失类型信息
    OnTrainEnd any
    OnTrainEpochBegin any
    OnTrainEpochEnd any
}
```

**问题**：`any` 回调导致调用方需要做类型断言，增加出错风险。

**修复方案**：
```go
type Callbacks struct {
    OnTrainBegin func(agent any, progress *Progress, evalInfo any)
    OnTrainEnd func(agent any, progress *Progress, evalInfo any)
    OnTrainEpochBegin func(agent any, progress *Progress)
    OnTrainEpochEnd func(agent any, progress *Progress, evalInfo any)
}
```

---

### S14. SubAgentSpec 缺少 13 个字段

**章节**：9.55 Blueprint

**Python 原始代码** (`deep_agent_spec.py:338-355`):
```python
class SubAgentSpec(BaseModel):
    agent_card: AgentCard
    system_prompt: str
    tools: list[ToolCard | BuiltinToolSpec] = []
    mcps: list[McpServerConfig] = []
    model: Optional[TeamModelConfig] = None
    rails: Optional[list[RailSpec]] = None
    skills: Optional[list[str]] = None
    workspace: Optional[WorkspaceSpec] = None
    sys_operation: Optional[SysOperationSpec] = None
    language: Optional[str] = None
    prompt_mode: Optional[str] = None
    enable_task_loop: bool = False
    max_iterations: Optional[int] = None
    factory_name: Optional[str] = None
    factory_kwargs: dict[str, Any] = {}
```

**Go 问题代码** (`schema/blueprint.go:88-91`):
```go
type SubAgentSpec struct {
    AgentCard    any    `json:"agent_card"`
    SystemPrompt string `json:"system_prompt"`
}
```

**问题**：缺失 `tools`, `mcps`, `model`, `rails`, `skills`, `workspace`, `sys_operation`, `language`, `prompt_mode`, `enable_task_loop`, `max_iterations`, `factory_name`, `factory_kwargs` 共 13 个字段。`AgentCard` 应为 `*agentschema.AgentCard` 而非 `any`。

**修复方案**：补全所有字段，`AgentCard` 改为具体类型。

---

### S15. SysOperationSpec 缺少 work_config 和 gateway_config 字段

**章节**：9.55 Blueprint

**Python 原始代码** (`deep_agent_spec.py:239-246`):
```python
class SysOperationSpec(BaseModel):
    id: str
    mode: OperationMode = OperationMode.LOCAL
    work_config: Optional[LocalWorkConfig] = None
    gateway_config: Optional[SandboxGatewayConfig] = None
```

**Go 问题代码** (`schema/blueprint.go:69-73`):
```go
type SysOperationSpec struct {
    ID   string `json:"id"`
    Mode string `json:"mode"`
}
```

**问题**：缺少 `work_config` 和 `gateway_config`，直接影响沙箱卡片的 `build_card()` 能力。

**修复方案**：添加 `WorkConfig` 和 `GatewayConfig` 字段。

---

### S16. TeamAgentSpec.Validate() 未调用 validateReservedNames 和 validateHittConsistency

**章节**：9.55 Blueprint

**Python 原始代码** (`schema/blueprint.py:374-375`):
```python
self._validate_reserved_names()
self._validate_hitt_consistency()
```

**Go 问题代码** (`schema/blueprint.go:260-266`):
```go
func (s *TeamAgentSpec) Validate() error {
    if err := s.validatePoolRouterExclusive(); err != nil {
        return err
    }
    s.defaultTransportForSpawnMode()
    return nil  // ← 未调用 validateReservedNames 和 validateHittConsistency
}
```

**问题**：虽然 Go 中已实现了这两个方法，但 `Validate()` 未调用它们，实际校验路径跳过了保留名和 HITT 一致性检查。

**修复方案**：
```go
func (s *TeamAgentSpec) Validate() error {
    if err := s.validatePoolRouterExclusive(); err != nil { return err }
    if err := s.validateReservedNames(); err != nil { return err }
    if err := s.validateHittConsistency(); err != nil { return err }
    s.defaultTransportForSpawnMode()
    return nil
}
```

---

### S17. schema/task.py 在 Go 中完全缺失

**章节**：9.55 agent_teams

**Python** (`schema/task.py`)：定义 `TaskOpResult`, `TaskCreateResult`, `TaskSummary`, `TaskDetail`, `TaskListResult`, `NewTaskSpec`, `GraphMutationResult` — 任务操作和视图的核心数据结构。

**Go**：`internal/agent_teams/schema/` 下无 `task.go`。

**问题**：这些类型被 `tools/task_manager.py` 大量使用，缺失会导致后续工具层无法对齐。

**修复方案**：新增 `schema/task.go`，定义对应结构体。

---

### S18. resolveProjectDir 和 resolveDisplayPath 未解析符号链接

**章节**：10.3 SysOpBuilder

**Python 原始代码** (`sysop_builder.py:277,827`):
```python
resolved = cand.expanduser().resolve()   # resolve() 解析符号链接和 ~
return str(Path(text).expanduser().resolve())
```

**Go 问题代码** (`policy.go:316, display.go:243`):
```go
resolved, err := filepath.Abs(cand)   // Abs 不解析符号链接
resolved, err := filepath.Abs(text)    // 同上
```

**问题**：`filepath.Abs` 不解析符号链接。如果项目目录是 symlink，Go 不会展开到真实路径，可能导致沙箱 bind mount 路径不一致、`FindAutoManagedMatch` 的路径比较误判。

**修复方案**：添加 `os.UserHomeDir()` 展开 `~`，再 `filepath.EvalSymlinks`：
```go
func resolvePath(path string) string {
    if strings.HasPrefix(path, "~/") {
        home, _ := os.UserHomeDir()
        path = filepath.Join(home, path[2:])
    }
    abs, err := filepath.Abs(path)
    if err != nil { return path }
    resolved, err := filepath.EvalSymlinks(abs)
    if err != nil { return abs }
    return resolved
}
```

---

## 🟡 一般问题

### G01. shuffle_cases 不可复现 — CaseLoader.ShuffleCases 无 seed 参数

**章节**：9.70b Dataset

**Python** (`case_loader.py:13-26`): `shuffle_cases(cases, seed=0)` 返回新列表，保证可复现。
**Go** (`case_loader.go:68-72`): `ShuffleCases()` 无 seed，用全局随机源，直接修改原列表。

**修复方案**：增加 `ShuffleCasesWithSeed(seed int64)` 和 `ShuffledCopy(seed int64) []Case`。

---

### G02. BatchEvaluate 缺少 numParallel 范围校验和 min 限制

**章节**：9.71 Evaluator

**Python** (`evaluator.py:73-76`):
```python
TuneUtils.validate_digital_parameter(num_parallel, "num_parallel", 1, 20)
num_workers = min(num_parallel, len(cases))
```

**Go**：`validateBatchArgs` 不校验 numParallel 范围，也不做 `min(numParallel, len(cases))` 限制。

**修复方案**：在 `validateBatchArgs` 中增加范围校验，并添加 `numParallel = min(numParallel, len(cases))`。

---

### G03. DefaultEvaluator 重试解析时不校验 result+reason 双字段

**章节**：9.71 Evaluator

**Python** (`evaluator.py:162-175`): 首次和重试都要求 `result` + `reason` 两个字段。
**Go** (`evaluator.go:264-317`): 首次校验双字段，重试只检查是否为 map。

**修复方案**：重试解析也校验双字段。

---

### G04. Trainer.ApplyUpdates 应为包级函数（对应 Python @staticmethod）

**Python** (`trainer.py`): `apply_updates` 是 `@staticmethod`。
**Go**: `(t *Trainer) ApplyUpdates` 是实例方法。

**修复方案**：改为包级导出函数 `func ApplyUpdates(_, _ any) { ... }`。

---

### G05. Trainer.WithCheckpointDir 仅设置字符串，未初始化 checkpointStore/Manager

**Python** 中 `checkpoint_dir` 非空时立即创建 `FileCheckpointStore` 和 `DefaultCheckpointManager`。Go 仅保存目录路径。

**修复方案**：在 `WithCheckpointDir` 中添加注释说明延迟初始化策略。

---

### G06. CreateSandboxSysOpCard 日志严重缺失

**章节**：10.3 SysOpBuilder

**Python** 日志包含 12 个字段（idle_check_interval, preserve_file_sharing_mode, filesystem_policy 各子项数量+内容, preserve_files_upload）。
**Go** 日志仅包含 6 个字段。

**修复方案**：补全所有日志字段，对齐 Python 的完整 policy 日志。

---

### G07. CreateLocalSysOpCard 日志缺少 mode=LOCAL 字段

**章节**：10.3 SysOpBuilder

**Python**: `logger.info("[sysop_builder] local SysOperationCard created (mode=LOCAL)")`
**Go**: `logger.Info(logComponent).Msg("本地 SysOperationCard 已创建")` — 缺少 `mode` 字段。

**修复方案**：添加 `.Str("mode", "LOCAL")` 字段。

---

### G08. collectIntrinsicTargets 中 intrinsic rw 文件路径解析失败被静默吞掉

**章节**：10.3 SysOpBuilder

**Python**: `logger.debug("[sysop_builder] intrinsic rw file path func failed: %s", exc)`
**Go**: `raw := fn(); if raw == "" { continue }` — 无日志。

**修复方案**：在 `raw == ""` 分支添加 Debug 日志。

---

### G09. BuildFilesystemPolicy 的 os.Stat 被调用两次

**章节**：10.3 SysOpBuilder

**Go** (`policy.go:134,139`): 两次 `os.Stat(path)` 可合并为一次。

**修复方案**：只 stat 一次，复用 `info`。

---

### G10. BuildServerPushMessage fallback_channel_id 逻辑差异

**章节**：10.3 Session

**Python** (`session_metadata.py:347-349`):
```python
channel_id = str(delivery_context.get("channel_id") or fallback_channel_id or "default").strip() or "default"
```
Python 的 fallback 在 `delivery_context.channel_id` 为空时生效。

**Go**: 只有 `channelID == "default"` 时才用 fallback。如果 delivery_context 的 channel_id 是空字符串，Python 会走到 fallback，Go 不会。

**修复方案**：将条件改为检查 deliveryCtx channel_id 是否为空：
```go
if cid, ok := deliveryCtx["channel_id"].(string); ok && strings.TrimSpace(cid) != "" {
    channelID = strings.TrimSpace(cid)
} else if len(fallbackChannelID) > 0 && fallbackChannelID[0] != "" {
    channelID = fallbackChannelID[0]
}
```

---

### G11. PushEvolutionProgress 错误处理范围不足

**章节**：10.3 EvolutionHelpers

**Python** 的 try-except 包裹了 `parse_stream_chunk` 和 `send_push` 两个调用。Go 只在 `SendPush` 返回错误时 warn，如果 `parseChunk` panic 无法捕获。

**修复方案**：在 `parseChunk` 调用外增加 defer/recover 保护。

---

### G12. EvolutionPushContext.ChannelID 类型差异

**Python**: `channel_id: str | None`（可为 None）
**Go**: `ChannelID string`（空字符串代替 None）

**修复方案**：将 `ChannelID` 改为 `*string`，对齐 Python 的 `str | None`。

---

### G13. DeepAdapter.CreateInstance 缺少 auto_create_workspace 和 completion_timeout 参数

**章节**：10.3 Adapter

**Python** (`interface_deep.py:2596,2604`):
```python
common_kwargs = dict(..., auto_create_workspace=False)
self._instance = create_deep_agent(**common_kwargs, completion_timeout=config.get("completion_timeout", 3600.0))
```

**Go**：`CreateDeepAgentParams` 中无 `AutoCreateWorkspace` 和 `CompletionTimeout` 字段。

**修复方案**：在 `CreateDeepAgentParams` 中添加这两个字段。

---

### G14. TeamAgent.MemberName() 返回 string 而非 *string

**章节**：9.55 TeamAgent

**Python**: `Optional[str]`（None 或字符串）。Go: `string`（空字符串），无法区分"未设置"和"空名字"。

**修复方案**：如果需要区分，改为返回 `*string`。否则保持现状。

---

### G15. TeamHarness 缺少 IsPendingInterruptResumeValid 方法

**章节**：9.57 TeamHarness

**Python** (`team_harness.py`): `is_pending_interrupt_resume_valid` 验证中断恢复输入的合法性。
**Go**: 完全缺失。

**修复方案**：在 9.60 StreamController 就绪后回填。

---

### G16. DBConfig 类型不支持 MemoryDatabaseConfig 联合类型

**章节**：9.55 Blueprint

**Python**: `db_config: DatabaseConfig | MemoryDatabaseConfig`
**Go**: `DBConfig database.DatabaseConfig` — 只支持 `DatabaseConfig`。

**修复方案**：改用 `any` 或定义接口/联合类型。

---

## 🔵 提示问题

### T01. IsPassResult 的字符串比较不完整

**章节**：9.71 Evaluator

**Python**: `result.strip().lower() == "true"` — 处理各种大小写和空白变体。
**Go**: `s == "true" || s == "True" || s == "TRUE"` — 遗漏带空格的变体。

**修复方案**：`strings.EqualFold(strings.TrimSpace(s), "true")`

---

### T02. CaseID 生成方式不一致 — UUID 格式差异

**Python**: `uuid.uuid4().hex` → 32 位无连字符。
**Go**: `uuid.New().String()` → 36 位带连字符。

**修复方案**：使用 `strings.ReplaceAll(uuid.New().String(), "-", "")` 或 `uuid.New().Hex()`。

---

### T03. LLMAsJudgeMetric.Compute 使用 context.Background() 而非传入 ctx

**章节**：9.71 Evaluator

`Compute` 方法签名中没有 `ctx context.Context` 参数，导致无法传递取消信号。

**修复方案**：考虑在 Metric 接口的 Compute 方法中增加 ctx 参数。

---

### T04. Trainer 缺少包级日志组件常量和日志调用

**章节**：9.70 Trainer

Python `trainer.py` 有 3 处 logger 调用（resume/checkpoint/no operator），Go 骨架无任何日志。

**修复方案**：在 `trainer.go` 中添加 `const logComponent = logger.ComponentAgentCore`。

---

### T05. SysOpBuilder 环境变量名从 JIUSWARM 改为 UAPCLAW

**Python**: `JIUSWARM_SANDBOX_PROJECT_DIR`
**Go**: `UAPCLAW_SANDBOX_PROJECT_DIR`

可能是有意为之（项目重命名），需确认是否需要向后兼容。

---

### T06. normalizeFSEntry 对 `<nil>` 的特殊处理

**Go** (`policy.go:415`): `if path == "" || path == "<nil>" {`

Python 无此逻辑。这是 Go `fmt.Sprintf("%v", nil)` 产生 `<nil>` 的 workaround，应在注释中说明原因。

---

### T07. collectIntrinsicTargets ro 文件日志级别偏高

**Python**: 路径解析失败用 `logger.debug`。
**Go**: 用 `logger.Warn`。

**修复方案**：将 ro 文件路径解析失败的日志级别从 Warn 降为 Debug。

---

### T08. TeamAgentBlueprint 是可变结构体而非不可变

**Python**: `@dataclass(frozen=True, slots=True)`。
**Go**: 普通 struct。

这是 Go 的语言限制，设计意图是蓝图构造后不应修改，但无法强制。

---

### T09. TeamAgent 缺少 _persist_team_db_state/_mark_team_cleaned/_mark_team_built 方法

**Python** 中这三个方法在 TeamAgent 上定义，负责锁存标志 + 持久化 DB 状态。
**Go**: 完全缺失，待 Manager 就绪后回填。

---

### T10. TeamAgent 缺少 _request_completion_poll 和 _wake_mailbox_if_interrupt_cleared 回调

**Python**: 作为 StreamController 回调传入。
**Go**: 完全缺失，待 9.60 StreamController 就绪后回填。

---

### T11. schema/stream.py 在 Go 中缺失

**Python** (`schema/stream.py`): `TeamOutputSchema(OutputSchema)` 扩展流式输出。
**Go**: 缺失。

**修复方案**：新增 `schema/stream.go`。

---

### T12. SubAgentSpec.AgentCard 和 DeepAgentSpec 部分字段使用 any 无回填标注

| 位置 | 占位类型 | 回填标注 |
|------|---------|---------|
| `SubAgentSpec.AgentCard` | `any` | ❌ 无标注，应为 `*agentschema.AgentCard` |
| `DeepAgentSpec.Tools/Mcps/Subagents/Rails` | `[]any` | ❌ 无标注 |
| `TeamInfra.TaskManager` | `any` | ❌ 无回填标注 |
| `TeamInfra.MessageManager` | `any` | ❌ 无回填标注 |

**修复方案**：补充回填标注或替换为具体类型。

---

## 优先修复建议

### 可立即修复（无需等依赖）

| 编号 | 问题 | 修复量 |
|------|------|--------|
| S09 | TeamAgent.Lifecycle() 默认值 `"temporary"` | 1 行 |
| S11 | RunAgentCustomizer 添加 panic recover | 5 行 |
| S03 | GroupEvolutionApprovals 移除 `missing = append(missing, "")` | 1 行 |
| S05 | sessionMetadata 补充 `round_id`/`team_name` 字段 | 4 行 |
| S16 | Validate() 补充调用两个校验方法 | 3 行 |
| S13 | Callbacks 字段改为函数类型 | 4 行 |
| T01 | IsPassResult 改用 `strings.EqualFold+TrimSpace` | 1 行 |

### 需等依赖就绪后修复

| 编号 | 问题 | 依赖 |
|------|------|------|
| S01 | CaseLoader.Split 添加 shuffle+ratio 校验 | 无 |
| S08 | ApproveResult.NewArgs 改为 `*string` | 无 |
| S04 | SendPush 添加连接检查+异常处理 | 无 |
| S10 | TeamAgent.Configure() 回调 | 9.58/9.59 |
| S12 | Trainer 添加 8 个方法桩 | 9.70a/9.78 |
| S14-S15 | SubAgentSpec/SysOperationSpec 补字段 | 各对应包 |
| S17 | schema/task.go | 无 |
| S18 | resolveProjectDir 解析 symlink | 无 |

---

*审查完成时间：2026-07-15*
*审查工具：6 个并行代理逐文件对照 Python 源码*

# 48h 逻辑审查报告（2026-08-28）

> 审查范围：2026-08-27 ~ 2026-08-28 期间提交
> 涉及章节：9.70a ToolCallOperator descriptions 类型变更、9.64 TeamMemory any→具体类型回填、10.3.21-22 BuildPushMessageFunc 签名对齐、9.70-9.80 Evolution 系统覆盖率补充、通用注释/日志中文化
> 对比标准：Python 源码方法签名、步骤流程、提示词一比一复刻

---

## 一、审查范围

| 提交 | 日期 | 涉及章节 | 说明 |
|------|------|---------|------|
| `6a3b38d8` | 08-27 | 9.64 | deepAgent/extractionModel/sharedDB any→具体类型回填 |
| `ff4a39d4` | 08-27 | 10.3.21 | S7 fix design — BuildPushMessageFunc 签名对齐设计文档 |
| `4d868db6` | 08-27 | 10.3.21 | S7 fix implementation plan |
| `b85eeed0` | 08-27 | 10.3.21 | BuildPushMessageFunc 签名对齐（fallbackChannelID 改 variadic） |
| `05d0534f` | 08-27 | 9.70a/9.72b | ToolCallOperator descriptions map[string]any→map[string]string |
| `e9a2684c` | 08-27 | 通用 | 40+ 文件补全中文注释与日志中文化 |
| `4486e00e` | 08-28 | 9.70/9.72 | 修复 gofmt 格式问题，覆盖率提升至 75%+ |

---

## 二、问题汇总

| # | 分类 | 模块 | 问题摘要 |
|---|------|------|---------|
| 1 | 🔴 严重 | 9.70a | `toDescriptions` 中 map[string]any 非字符串值被静默丢弃为空字符串 |
| 2 | 🔴 严重 | 9.70 | Trainer.Train 中 `len(updated)==0` 时不评估验证分数，improved 永远 false |
| 3 | 🔴 严重 | 9.70 | Trainer.Train 中 Forward 失败后继续执行 Update（零值 trajectories/evaluated） |
| 4 | 🔴 严重 | 9.77 | TracerTrajectoryExtractor.buildToolDetail 中 toolName 始终为空字符串 |
| 5 | 🔴 严重 | 9.64 | BuildMemoryManager 缺少 6 个关键字段（workspace/sysOperation/db/taskManager 等） |
| 6 | 🔴 严重 | 9.64 | Go paths.go 缺少 `defaultTeamMemoryDir` 函数，SharedMemoryManager 永远不创建 |
| 7 | 🟡 一般 | 9.72b | `isMostlyEnglish` 使用 `len(string)` 按 byte 计数而非 rune 计数 |
| 8 | 🟡 一般 | 9.70a | SetParameter/LoadState 回调传 map[string]string，消费者可能期望 map[string]any |
| 9 | 🟡 一般 | 9.77 | buildMeta 缺少 agent_id 字段序列化 |
| 10 | 🟡 一般 | 9.70 | Trainer.Train `updateErr!=nil` 时 continue 与 Python 行为不同 |
| 11 | 🟡 一般 | 9.64 | TeamMemoryManager workspace 覆盖逻辑缺失（readOnlySource 场景） |
| 12 | 🟡 一般 | 9.64 | SharedMemoryManager 创建缺少 EnsureDir() 调用 |
| 13 | 🟡 一般 | 9.64 | extractor.go 中 ExtractTeamMemories 的 model 参数仍为 any |
| 14 | 🟡 一般 | 10.3.21 | BuildServerPushWire Provenance 缺少 converter/converted_at/details |
| 15 | 🟡 一般 | 10.3.21 | buildServerPushWireChunk 未传递 is_complete 字段 |
| 16 | 🟡 一般 | 10.3.21 | PushEvolutionEvent 修改原始 evt 而非创建 payload 副本 |
| 17 | 🟢 提示 | 9.73 | FromEvaluatedCase 未通过 MakeEvolutionSignal 创建信号，路径不一致 |
| 18 | 🟢 提示 | 9.70 | trainer_test.go 缺少关键路径覆盖 |
| 19 | 🟢 提示 | 9.78 | EvolutionPatch.ToDict 缺少 keywords/summary 字段（Python 同样遗漏） |
| 20 | 🟢 提示 | 10.3.21 | BuildServerPushWire Channel/SessionID 空串 vs null 差异 |
| 21 | 🟢 提示 | 9.64 | harness.go DeepConfig/Workspace/SysOperation/Model 返回 any（可回填为具体类型） |
| 22 | 🟢 提示 | 9.64 | shared_resources.go GetSharedDB config 参数仍为 any |

---

## 三、问题详述

### 🔴 问题 1：`toDescriptions` 中 map[string]any 非字符串值被静默丢弃 — 严重

**Python 参考代码** (`core/operator/tool_call/base.py:74-88`):

```python
def set_parameter(self, target: str, value: Any) -> None:
    if target != "tool_description":
        return
    if not isinstance(value, dict):
        return
    self._descriptions = value.copy()  # ← 原样保留所有值，Python 是动态类型
```

Python 的 `_descriptions` 声明为 `Dict[str, str]`，但 `value.copy()` 会原样保留所有值（动态类型，`{"tool": 42}` 不会报错）。

**Go 问题代码** (`tool_call_operator.go:165-190`):

```go
func toDescriptions(value any) map[string]string {
    case map[string]any:
        result := make(map[string]string, len(v))
        for k, val := range v {
            result[k] = toString(val)  // ← 非字符串值变为空字符串 ""
        }
        return result
}

func toString(v any) string {
    switch val := v.(type) {
    case string:
        return val
    default:
        return ""  // ← 非字符串直接丢弃！
    }
}
```

**影响**：
- 当 `map[string]any` 中的 value 不是 string（例如 JSON 反序列化后 `float64`、`int`、嵌套 `map`），Go 将其转为空字符串 `""`
- 例如 `{"count": 42}` → Go 存 `{"count": ""}`，Python 存 `{"count": 42}`
- 测试 `tool_call_operator_test.go:119-127` 甚至认为这是正确行为，但与 Python 不一致

**修复方案**：

```go
func toString(v any) string {
    switch val := v.(type) {
    case string:
        return val
    case fmt.Stringer:
        return val.String()
    default:
        return fmt.Sprintf("%v", val)  // 保留值的字符串表示
    }
}
```

---

### 🔴 问题 2：Trainer.Train 中 `len(updated)==0` 时不评估验证分数 — 严重

**Python 参考代码** (`trainer/trainer.py:199-211`):

```python
updated = asyncio.run(self._updater.update(trajectories, evaluated, config=kwargs))
if isinstance(updated, list):
    val_score, val_evaluated = self._select_best_candidate_on_val(...)
else:
    updates: Updates = updated or {}  # ← 空 dict 也走此分支
    self.apply_updates(operators, updates)  # 空更新，跳过
    val_score, val_evaluated = self.evaluate(agent, val_cases)  # ← 仍然评估！
```

Python 中 `isinstance(updated, list)` 为 False 时（即 `updated` 是 dict，包括空 dict `{}`），走 else 分支并**执行 evaluate**。

**Go 问题代码** (`trainer/trainer.go:201-219`):

```go
if updateErr == nil {
    if len(updated) > 1 {
        // 多候选集
    } else if len(updated) == 1 {
        // 单映射
    }
    // len(updated) == 0: 无分支！valScore=0, valEvaluated=nil
}
improved := valScore > progress.BestScore  // 永远 false（valScore=0），不会保存检查点
```

**影响**：当 `updater.Update` 返回空切片时，验证分数不被计算，`improved` 永远为 false，检查点不会保存。这在某些 Updater 返回空更新的场景下（如 ToolOptimizer 的黑盒模式），会导致训练循环永远不保存最优检查点。

**修复方案**：

```go
if updateErr == nil {
    if len(updated) > 1 {
        // 多候选集 ...
    } else if len(updated) == 1 {
        // 单映射 ...
    } else {
        // len(updated) == 0: 空更新，仍需评估验证分数（对齐 Python updated or {} 分支）
        valScore, valEvaluated, _ = t.Evaluate(ctx, agent, valCases)
    }
}
```

---

### 🔴 问题 3：Trainer.Train 中 Forward 失败后继续执行 Update — 严重

**Python 参考代码** (`trainer/trainer.py:192-199`):

```python
score, evaluated, trajectories, _sessions = self.forward(agent, train_cases)
# forward 内部对每个 case 的 invoke 失败仅记录 error dict，不中断
updated = asyncio.run(self._updater.update(trajectories, evaluated, config=kwargs))
```

Python 的 `forward` 不返回 error——它内部对每个 case 的 invoke 失败仅记录 `dict(error=...)`，但正常返回已收集的数据。

**Go 问题代码** (`trainer/trainer.go:172-186`):

```go
if t.UpdaterRequiresForward() {
    forwardScore, forwardEvaluated, forwardTrajectories, _, err := t.Forward(ctx, agent, trainCases)
    if err != nil {
        logger.Warn(logComponent).Msg("Train Forward 失败")
        // ← 没有 continue！继续执行后续 Update
    }
    progress.CurrentEpochScore = forwardScore
    evaluated = forwardEvaluated      // ← 可能为 nil！
    trajectories = forwardTrajectories // ← 可能为 nil！
}
```

**影响**：Forward 失败后 `evaluated` 和 `trajectories` 为零值（nil），传给 `updater.Update` 可能产生非预期结果。

**修复方案**：Forward 失败时 `continue` 跳过当前 epoch：

```go
if err != nil {
    logger.Warn(logComponent).Msg("Train Forward 失败，跳过当前 epoch")
    continue
}
```

---

### 🔴 问题 4：TracerTrajectoryExtractor.buildToolDetail 中 toolName 始终为空 — 严重

**Python 参考代码** (`trajectory/extractor.py:147`):

```python
tool_name = getattr(span, "name", "") or ""
```

Python 从同一个 `span` 对象读取 `name` 属性（`TraceAgentSpan.name`）。

**Go 问题代码** (`trajectory/extractor.go:257-261`):

```go
func (e *TracerTrajectoryExtractor) buildToolDetail(span *tracer.Span) *ToolCallDetail {
    toolName := ""  // ← 始终为空！
```

**调用链分析**：

```
buildStep(span *tracer.TraceAgentSpan)
  └─ buildDetail(&span.Span, kind)   // ← 只传了内嵌的 Span，丢失了 TraceAgentSpan.Name
       └─ buildToolDetail(span *tracer.Span)  // ← 无法访问 Name
```

`buildDetail` 接收 `*tracer.Span`（内嵌结构体），而 `tool_name` 在 `TraceAgentSpan.Name` 中。由于类型收缩，`buildToolDetail` 无法获取工具名。

**影响**：所有 Tool 类型的轨迹步骤中 `ToolName` 字段为空，导致：
- `buildMeta` 中无法查找工具描述和 Schema
- 轨迹分析无法按工具名归因
- 信号检测中无法按工具名过滤

**修复方案**：修改 `buildDetail` 签名，将 `TraceAgentSpan`（或至少其 `Name` 字段）传入 `buildToolDetail`：

```go
func (e *TracerTrajectoryExtractor) buildDetail(span *tracer.TraceAgentSpan, kind StepKind) StepDetail {
    switch kind {
    case StepKindLLM:
        return e.buildLLMDetail(&span.Span)
    case StepKindTool:
        return e.buildToolDetail(&span.Span, span.Name)  // 传入 Name
    ...
```

---

### 🔴 问题 5：BuildMemoryManager 缺少 6 个关键字段 — 严重

**Python 参考代码** (`agent_configurator.py:482-500`):

```python
params = TeamMemoryManagerParams(
    member_name=member_name,
    team_name=self.team_name,
    role=spec.role,
    lifecycle=spec.lifecycle,
    ...
    workspace=agent_workspace,            # ← Go 缺失
    sys_operation=sys_operation,          # ← Go 缺失
    team_memory_dir=team_memory_dir,      # ← Go 缺失
    enable_auto_extract=(spec.memory.auto_extract and spec.lifecycle == "persistent"),  # ← Go 逻辑不同
    read_only_source_workspace=read_only_source,  # ← Go 缺失
    db=self.team_backend.db if self.team_backend else None,   # ← Go 缺失
    task_manager=self.task_manager,       # ← Go 缺失
)
```

**Go 问题代码** (`agent_configurator.go:386-404`):

```go
params := memory.TeamMemoryManagerParams{
    MemberName:          memberName,
    TeamName:            c.TeamName(),
    Role:                memory.TeamRole(c.Role()),
    // ... 仅 10 个字段，缺失 6 个
    // Workspace, SysOperation, TeamMemoryDir, ReadOnlySourceWorkspace, DB, TaskManager 均未传入
    EnableAutoExtract:   memCfg.AutoExtract,  // ← 缺少 && lifecycle=="persistent" 条件
}
```

**缺失字段影响**：

| 缺失字段 | 影响 |
|---------|------|
| `Workspace` | 记忆工具无法访问工作空间，读写文件会失败 |
| `SysOperation` | 共享记忆管理器无法执行文件操作 |
| `TeamMemoryDir` | SharedMemoryManager 永远不创建（`TeamMemoryDir` 为 nil） |
| `ReadOnlySourceWorkspace` | temporary lifecycle 下工作空间指向错误 |
| `DB` | 记忆工具无法访问团队数据库 |
| `TaskManager` | 记忆工具无法访问任务管理器 |
| `EnableAutoExtract` 逻辑 | 未检查 lifecycle=="persistent"，临时团队也会自动提取 |

**修复方案**：在 `BuildMemoryManager` 中补全字段（部分依赖 harness 等上游模块实现）：

```go
params := memory.TeamMemoryManagerParams{
    // ... 现有字段
    Workspace:               c.harness.Workspace(),
    SysOperation:            c.harness.SysOperation(),
    TeamMemoryDir:           teamMemoryDir,     // 需实现 defaultTeamMemoryDir
    ReadOnlySourceWorkspace: readOnlySource,     // 当 lifecycle=="temporary" 时设值
    DB:                      c.TeamBackend().DB(),
    TaskManager:             c.TaskManager(),
    EnableAutoExtract:       memCfg.AutoExtract && string(spec.Lifecycle) == "persistent",
}
```

---

### 🔴 问题 6：Go paths.go 缺少 `defaultTeamMemoryDir` 函数 — 严重

**Python 参考代码** (`paths.py`):

```python
def team_memory_dir(team_name: str) -> Path:
    return team_home(team_name) / "team-workspace" / "team-memory"
```

**Go 代码**：`paths.go` 中只有 `TeamHome(teamName)`，没有 `DefaultTeamMemoryDir` 函数。

**影响**：`BuildMemoryManager` 无法计算 `team_memory_dir` 默认值，导致 `SharedMemoryManager` 永远不会被创建（因为 `TeamMemoryDir` 始终为 nil）。共享记忆功能完全不可用。

**修复方案**：

```go
// DefaultTeamMemoryDir 返回团队共享记忆目录默认路径。
// 对齐 Python: team_memory_dir(team_name)
// 布局：{TeamHome(teamName)}/team-workspace/team-memory/
func DefaultTeamMemoryDir(teamName string) string {
    return filepath.Join(TeamHome(teamName), "team-workspace", "team-memory")
}
```

---

### 🟡 问题 7：`isMostlyEnglish` 使用 `len(string)` 按 byte 计数 — 一般

**Python 参考代码** (`customized_reviewer.py:116-127`):

```python
text_no_space = re.sub(r'\s+', '', text)
english_chars = len(re.findall(r'[a-zA-Z]', text_no_space))
english_ratio = english_chars / len(text_no_space)  # ← Python len() 返回 Unicode 字符数
```

**Go 问题代码** (`reviewer.go:364`):

```go
englishRatio := float64(englishChars) / float64(len(textNoSpace))  // ← len() 返回字节数！
```

**影响示例**：

```python
# Python: "Hello世界" → len=7, english_chars=5, ratio=5/7≈0.71 → True
```

```go
// Go: "Hello世界" → len=11, englishChars=5, ratio=5/11≈0.45 → False（误判）
```

**修复方案**：

```go
import "unicode/utf8"
englishRatio := float64(englishChars) / float64(utf8.RuneCountInString(textNoSpace))
```

---

### 🟡 问题 8：SetParameter/LoadState 回调传 map[string]string，消费者可能类型断言失败 — 一般

**Python 参考代码** (`core/operator/tool_call/base.py:91-92`):

```python
if self._on_parameter_updated is not None:
    self._on_parameter_updated(target, self._descriptions.copy())
```

Python 的回调收到 `Dict[str, str]` 的副本，但 Python 的动态类型使得消费者无需类型断言。

**Go 问题代码** (`tool_call_operator.go:106,131`):

```go
op.onParameterUpdated(target, maps.Clone(op.descriptions))  // 传 map[string]string
```

`ParameterUpdatedCallback` 签名为 `func(target string, value any)`。消费者如果做 `value.(map[string]any)` 类型断言会 panic。

**修复方案**：在回调时转为 `map[string]any`：

```go
if op.onParameterUpdated != nil {
    anyMap := make(map[string]any, len(op.descriptions))
    for k, v := range op.descriptions {
        anyMap[k] = v
    }
    op.onParameterUpdated(target, anyMap)
}
```

---

### 🟡 问题 9：buildMeta 缺少 agent_id 字段序列化 — 一般

**Python 参考代码** (`trajectory/extractor.py:189-191`):

```python
agent_id = getattr(span, "agent_id", None) or base_meta.get("agent_id")
if agent_id:
    meta["agent_id"] = agent_id
```

**Go 问题代码** (`trajectory/extractor.go:288-318`)：缺少 `agent_id` 相关代码。

**修复方案**：

```go
if span.AgentID != "" {
    meta["agent_id"] = span.AgentID
} else if v, ok := baseMeta["agent_id"]; ok && v != nil {
    meta["agent_id"] = v
}
```

---

### 🟡 问题 10：Trainer.Train `updateErr!=nil` 时 continue 与 Python 行为不同 — 一般

**Python 参考代码**：Python `Trainer.train` 中 `asyncio.run(self._updater.update(...))` 无 try-except，异常直接传播到上层。

**Go 问题代码** (`trainer/trainer.go:189-196`):

```go
updated, updateErr := t.updater.Update(ctx, trajectories, evaluated, config)
if updateErr != nil {
    logger.Warn(logComponent).Msg("Train Updater.Update 失败")
    continue  // ← Python 中无此行为，异常直接传播
}
```

**分析**：Go 版本选择 `continue` 更健壮（训练不会因单个 Updater 失败而中断），但与 Python 行为不一致。此外第 201 行 `if updateErr == nil` 是冗余检查（continue 后不会到达）。

**修复方案**：保留 `continue` 行为，移除冗余 `if updateErr == nil`，添加注释说明行为差异。

---

### 🟡 问题 11：TeamMemoryManager workspace 覆盖逻辑缺失 — 一般

**Python 参考代码** (`manager.py:74-77`):

```python
if self._read_only_source:
    self._workspace = Workspace(root_path=self._read_only_source)  # ← 用 readOnlySource 覆盖
else:
    self._workspace = params.workspace
```

**Go 问题代码** (`manager.go:109-110`):

```go
workspace: params.Workspace,  // ← 直接赋值，无覆盖逻辑
```

**影响**：temporary lifecycle + readOnlySource 场景下，Go 的 workspace 指向 harness 的 workspace 而非 parent 的 workspace，记忆工具会指向错误的工作空间。

**修复方案**：

```go
if params.ReadOnlySourceWorkspace != nil && *params.ReadOnlySourceWorkspace != "" {
    mgr.workspace = workspace.NewWithPath(*params.ReadOnlySourceWorkspace)
} else {
    mgr.workspace = params.Workspace
}
```

---

### 🟡 问题 12：SharedMemoryManager 创建缺少 EnsureDir() 调用 — 一般

**Python 参考代码** (`manager.py:115-122`):

```python
# 在 init_toolkit() 中创建
if self._team_memory_dir:
    self._shared_manager = SharedMemoryManager(
        team_memory_dir=self._team_memory_dir,
        sys_operation=self._sys_operation,
    )
    await self._shared_manager.ensure_dir()  # ← 确保目录存在
```

**Go 问题代码** (`manager.go:115-117`):

```go
if params.TeamMemoryDir != nil && params.SharedMemory {
    mgr.sharedManager = NewSharedMemoryManager(*params.TeamMemoryDir, params.SysOperation)
    // ← 缺少 EnsureDir() 调用！
}
```

**影响**：目录可能不存在时后续读写会失败。

**修复方案**：在 `InitToolkit` 中调用 `mgr.sharedManager.EnsureDir()`，同时移除冗余的 `params.SharedMemory` 检查。

---

### 🟡 问题 13：extractor.go 中 ExtractTeamMemories 的 model 参数仍为 any — 一般

**Python 参考代码** (`extractor.py:194-203`):

```python
async def extract_team_memories(
    ...
    model: Optional["Model"] = None,  # ← 明确类型
```

**Go 问题代码** (`extractor.go:81`):

```go
func ExtractTeamMemories(ctx context.Context, ..., model any, ...) error {  // ← 仍是 any
```

**修复方案**：`model any` → `model *llm.Model`，同时 `tasks []any` → 对应 Go 类型，`messages []any` → 对应 Go 类型。

---

### 🟡 问题 14：BuildServerPushWire Provenance 缺少 converter/converted_at/details — 一般

**Python 参考代码** (`wire.py:33-38`):

```python
provenance=E2AProvenance(
    source_protocol="e2a",
    converter=_CONVERTER,        # "jiuwenswarm.server.gateway_push.wire:build_server_push_wire"
    converted_at=utc_now_iso(),  # 时间戳
    details={"kind": "server_push"},
)
```

**Go 问题代码** (`wire.go:79-86`):

```go
e2aResp := e2a.NewE2AResponse()  // Provenance 只设了 source_protocol
// 未设置 Provenance.Converter / ConvertedAt / Details
```

**修复方案**：

```go
e2aResp.Provenance = e2a.E2AProvenance{
    SourceProtocol: e2a.E2ASourceProtocolE2A,
    Converter:      "uapclaw-go/internal/swarm/transport/wire:BuildServerPushWire",
    ConvertedAt:    e2a.UTCNowISO(),
    Details:        map[string]any{"kind": "server_push"},
}
```

---

### 🟡 问题 15：buildServerPushWireChunk 未传递 is_complete 字段 — 一般

**Python 参考代码** (`wire.py:51-56`):

```python
chunk = AgentResponseChunk(
    ...
    is_complete=bool(msg.get("is_complete", False)),
)
```

**Go 问题代码** (`wire.go:127-128`):

```go
chunk := schema.NewAgentResponseChunk(requestID, channelID, payload)
// 未读取 msg["is_complete"]，默认 false
```

**影响**：如果调用方传了 `is_complete=True`，Go 编码出的 wire 会丢失此信息。

**修复方案**：读取 `msg["is_complete"]` 并传递给 chunk 构造。

---

### 🟡 问题 16：PushEvolutionEvent 修改原始 evt 而非创建 payload 副本 — 一般

**Python 参考代码** (`evolution_helpers.py:411-422`):

```python
payload = event_payload_dict(evt)  # ← 创建副本
evt_type = event_type(evt)
if evt_type and "event_type" not in payload:
    payload["event_type"] = evt_type  # 修改副本
payload.setdefault("request_id", request_id)
await push_context.transport.send_push(
    build_push_message(..., payload=payload)  # 传副本
)
```

**Go 问题代码** (`helpers.go:583-594`):

```go
evtType := EventType(evt)
if evtType != "" {
    if _, ok := evt["event_type"]; !ok {
        evt["event_type"] = evtType  // ← 直接修改原始 evt！
    }
}
if _, ok := evt["request_id"]; !ok {
    evt["request_id"] = requestID  // ← 直接修改原始 evt！
}
msg := buildMsgFn(pushCtx.SessionID, requestID, evt, pushCtx.ChannelID)  // 传原始 evt
```

**影响**：(1) 调用者的 evt 被副作用修改；(2) `BuildServerPushMessage` 中 `message["payload"]` 和调用者的 evt 指向同一个 map，下游修改会互相影响。

**修复方案**：创建 payload 副本后操作：

```go
payload := EventPayloadDict(evt)  // 浅拷贝
evtType := EventType(evt)
if evtType != "" {
    if _, ok := payload["event_type"]; !ok {
        payload["event_type"] = evtType
    }
}
if _, ok := payload["request_id"]; !ok {
    payload["request_id"] = requestID
}
msg := buildMsgFn(pushCtx.SessionID, requestID, payload, pushCtx.ChannelID)
```

---

### 🟢 问题 17：FromEvaluatedCase 未通过 MakeEvolutionSignal 创建信号 — 提示

**Python 参考代码** (`signal/from_eval.py:37-49`):

```python
return make_evolution_signal(
    signal_type=signal_type,
    section="Troubleshooting",
    excerpt=f"score={case.score:.2f}",
    skill_name=operator_id or None,
    source="offline_evaluation",  # ← 通过 source 参数传递
    context={...},
)
```

**Go 问题代码** (`signal/from_eval.go:40-55`): 直接构造 `&EvolutionSignal{}`，`source` 放在 context 而非通过 `WithSource` 传递。

**影响**：功能上无 bug，但创建路径与其他信号检测器不一致，增加维护成本。

---

### 🟢 问题 18：trainer_test.go 缺少关键路径覆盖 — 提示

**缺失的关键路径**：

1. `Train` 完整训练循环测试
2. `SelectBestCandidateOnVal` 测试
3. Forward/Updater 返回 error 时的行为
4. Early stop 触发测试
5. Updater.RequiresForwardData=false 的黑盒优化器路径

---

### 🟢 问题 19：EvolutionPatch.ToDict 缺少 keywords/summary 字段 — 提示

Python 的 `_OPTIONAL_FIELDS` 同样不包含 `keywords` 和 `summary`，所以 Go 对齐了 Python。这是上游 Python 的序列化遗漏。

---

### 🟢 问题 20：BuildServerPushWire Channel/SessionID 空串 vs null 差异 — 提示

Python `channel=str(msg.get("channel_id", "")) or None`，空串时设为 `None`（序列化后 `"channel": null`）。Go 空串时保留零值 `""`（序列化后 `"channel": ""`）。如果下游不依赖 null vs "" 区分，可暂不处理。

---

### 🟢 问题 21：harness.go DeepConfig/Workspace/SysOperation/Model 返回 any — 提示

`harness.go` 中 `deepAgent` 已回填为 `DeepAgentInterface`，但 `DeepConfig()` 等方法仍返回 `any`。`DeepAgentInterface` 已有 `DeepConfig() *hschema.DeepAgentConfig` 方法，可以立即回填：

```go
func (h *TeamHarness) DeepConfig() *hschema.DeepAgentConfig {
    return h.deepAgent.DeepConfig()
}
```

---

### 🟢 问题 22：shared_resources.go GetSharedDB config 参数仍为 any — 提示

Go `GetSharedDB(config any) any` 应改为具体类型。当前 `SetupTeamBackend` 中需要类型断言 `sharedDB.(database.TeamDatabase)`，是运行时风险。

---

## 四、待回填占位验证

### 正确的 ⤵️ 标记（功能确实未实现）

| 模块 | 标记 | 状态 |
|------|------|------|
| `memory/manager.go` | ⤵️ 回填: 7.2+9.65a（5个方法空实现） | ✅ 正确 |
| `spawn/shared_resources.go` | ⤵️ 预留 9.64/9.85 | ✅ 正确：GetSharedDB 返回 nil |
| `agent_teams/runtime/manager.go` | ⤵️ 待 9.55/9.62 | ✅ 正确：CoordinationKernel 未实现 |
| `swarm/extensions/` | ⤵️ 10.5.7-10.5.10 | ✅ 正确：Loader/Manager/Crypto 均为 stub |
| `evolving/evaluator/evaluator_pipeline/` | ⤵️ 等待后续章节回填 | ✅ 正确 |
| `agent_teams/agent/team_agent.go` | ⤵️ 待 9.55/9.62 | ✅ 正确：约 25 处未实现 |
| `swarm/server/adapter/deep_adapter.go` | ⤵️ 10.6.3-10/11.10 | ✅ 正确：约 40 处未实现 |

### 过时标记（功能已实现但标记未清理）

| 文件路径 | 标记内容 | 修复建议 |
|---------|---------|---------|
| `memory/manager.go:52` | `⤴️ 9.64 具体类型回填` | 删除标记（6a3b38d8 已回填） |
| `memory/shared_memory.go` | `⤴️ 9.64 具体类型回填` | 删除标记 |
| `context_engine/engine.go:85` | `⤵️ 5.31 回填 SessionModelContext 构造` | 删除标记（已实现） |
| `context_engine/processor/offloader/message_offloader.go:317-323` | `⤵️ 5.31 回填：mc.WorkspaceDir()` | 删除标记和误导注释（接口已有该方法） |
| `context_engine/processor/offloader/message_summary_offloader.go:516-521` | 同上 | 删除标记 |
| `context_engine/processor/offloader/tool_result_budget_processor.go:559-565` | 同上 | 删除标记 |
| `context_engine/processor/compressor/full_compact_processor.go:523` | `⤵️ 5.31 回填：Session Memory 路径返回 nil` | 删除标记（`_buildSessionMemoryMessages` 已实现） |

---

## 五、48h 变更整体评价

### 正面

1. **ToolCallOperator 类型精确化**：`map[string]any` → `map[string]string` 对齐了 Python 的 `Dict[str, str]` 类型声明，方向正确
2. **BuildPushMessageFunc 签名对齐**：fallbackChannelID 改为 variadic 参数，使 `session.BuildServerPushMessage` 可直接赋值给回调，消除了闭包适配
3. **any→具体类型回填**：harness.go、memory/manager.go、shared_resources.go 中的 `any` 逐步替换为 `DeepAgentInterface`/`*llm.Model` 等具体类型，提高类型安全
4. **覆盖率提升**：trainer_test.go、shared_memory_test.go 等补充了关键 getter 测试

### 风险

1. **toDescriptions 转换逻辑**：类型变更引入了 `toString` 的静默丢弃行为，与 Python 不一致，可能导致演化更新数据丢失
2. **Trainer 训练循环**：`len(updated)==0` 和 `Forward 失败` 两个边界条件处理缺失，可能导致训练检查点永远不保存
3. **Trajectory 提取**：`buildToolDetail` 中 toolName 始终为空，影响轨迹分析和信号检测
4. **BuildMemoryManager**：缺少 6 个关键字段 + `defaultTeamMemoryDir` 函数缺失，导致共享记忆功能完全不可用
5. **PushEvolutionEvent**：直接修改原始 evt map，产生副作用和共享引用风险

---

## 六、修复优先级建议

| 优先级 | 问题编号 | 说明 |
|--------|---------|------|
| P0 立即 | 1, 2, 3 | Evolution 训练循环逻辑 bug，可能导致训练异常或数据丢失 |
| P0 立即 | 5, 6 | TeamMemory 共享记忆功能完全不可用 |
| P1 本周 | 4, 16 | 轨迹提取 toolName 为空 + PushEvent 副作用 |
| P2 下周 | 7, 8, 11, 12, 13, 14, 15 | 类型精度、rune 计数、缺少字段等 |
| P3 排期 | 9, 10, 17, 18, 19, 20, 21, 22 | 非功能性问题、上游遗漏、提示级差异 |

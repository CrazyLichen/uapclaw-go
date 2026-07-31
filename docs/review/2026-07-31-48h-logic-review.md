# 代码逻辑审查报告（2026-07-31）

> 审查范围：近10天提交记录（7月25日~7月27日）涉及的章节
> 审查方法：逐方法对比 Python 参考项目与 Go 实现，重点检查方法签名、执行步骤、逻辑分支、待回填代码
> 涉及章节：10.3.14 TenantAgentPool、9.72b ToolOptimizer、10.3.7 CodeAgentRail、工具类修复（shell/browser/todo）、agent_teams 修复、common/utils、adapter/agent_manager

---

## 严重问题（共 13 个）

### S01. Bash/PowerShell Tool 缺少 rm 目标记录逻辑和 _build_history_path

**严重程度**：严重 — 文件删除追踪功能完全缺失

**Python 样例**：
```python
# bash/_tool.py L192-199: 执行前记录 rm 目标
parsed_targets = self._parse_rm_targets(command)
self._record_rm_targets_before_deletion(parsed_targets, session_id, agent_id)

# bash/_tool.py L230-232: 执行后检测并记录删除
self._detect_and_record_deletions(command, cwd, session_id, agent_id)

# bash/_tool.py L146-157: 构建 history 路径
def _build_history_path(self, session_id=None):
    path = Path(f".agent_history/file_ops_{self.agent_id}_{session_id}.json")
```

**Go 问题代码**：
```go
// bash.go 和 powershell.go 中完全缺少上述逻辑
// rm_tracker.go 中定义了 ParseRmTargets/ParsePSRemoveTargets 但未被调用
```

**修复方案**：在 bash.go 和 powershell.go 的 invoke 函数中，执行命令前后分别调用 `ParseRmTargets`/`RecordRmTargetsBeforeDeletion` 和 `DetectAndRecordDeletions`。需要先实现 `_build_history_path` 的 Go 等价。

---

### S02. Permission CheckPermission 使用 MatchString（全文匹配）而非 search（子串匹配）

**严重程度**：严重 — deny/allow 规则匹配语义完全不同，可能导致安全规则失效

**Python 样例**：
```python
# bash/_permission.py + powershell/_permission.py
# deny 层：pattern.search(segment) — 子串匹配
for pattern, reason in self._deny_patterns:
    if pattern.search(segment):
        return PermissionResult(allowed=False, reason=reason)

# allow 层：pattern.search(command) — 子串匹配
for pattern in self._allow_patterns:
    if pattern.search(command):
        return PermissionResult(allowed=True)
```

**Go 问题代码**：
```go
// permission.go L134 (deny 层)
if pattern.MatchString(segment) {  // MatchString = 全文匹配，要求整个 segment 完全匹配 pattern

// permission.go L144 (allow 层)
if pattern.MatchString(command) {  // 同样全文匹配
```

**差异分析**：
- Python `pattern.search(segment)` 是子串匹配：deny pattern `rm` 会拒绝 `rm -rf /`（因为 segment 中包含 "rm"）
- Go `pattern.MatchString(segment)` 是全文匹配：deny pattern `rm` 不会拒绝 `rm -rf /`（因为整个 segment 不是 "rm"）
- **这是一个安全漏洞**：本应被 deny 规则拦截的命令可能被放行

**修复方案**：
```go
// 改用 regexp.FindString 或 MatchReader 进行子串匹配
// 方案 1：用 FindString
if pattern.FindString(segment) != "" {
    return false, reason
}
// 方案 2：用 MatchString 但确保 pattern 自身包含正确的锚点
// 注意：当前 deny/allow patterns 由 CompilePatterns 编译，需要确认是否添加了 ^$ 锚点
```

---

### S03. SpawnManager.on_teammate_unhealthy 缺少 cleanup 和状态标记步骤

**严重程度**：严重 — unhealthy 重启流程缺少关键步骤

**Python 样例**：
```python
# spawn_manager.py
async def on_teammate_unhealthy(self, member_name):
    await self.cleanup_teammate(member_name)          # 步骤 1: 清理旧句柄
    team_backend = self._configurator.team_backend
    if team_backend and team_name:
        await team_backend.db.member.update_member_status(  # 步骤 2: 标记 RESTARTING
            member_name, team_name, MemberStatus.RESTARTING.value)
    await self.restart_teammate(member_name)           # 步骤 3: 重启
```

**Go 问题代码**：
```go
// spawn_manager.go
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
    // 缺少步骤 1: CleanupTeammate
    // 缺少步骤 2: 标记 RESTARTING
    go func() {
        if err := m.RestartTeammate(recoverCtx, memberName, defaultMaxRetries); err != nil { ... }
    }()
}
```

**修复方案**：
```go
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
    // 步骤 1: 清理旧句柄
    m.CleanupTeammate(memberName)
    // 步骤 2: ⤵️ 待 DB 实现，标记 RESTARTING 状态
    // 步骤 3: 重启
    go func() { ... }()
}
```

---

### S04. ToolOptimizer description_method 中 examples 参数传递链不一致

**严重程度**：严重 — neg_examples 在整个调用链中丢失

**Python 样例**：
```python
# description_method.py L38-42: step() 构建 examplesObtained dict
neg_examples = self.get_negative_examples(function_name)
examples_obtained = {"neg_examples": neg_examples, "examples": examples}
output = self.generate(tool, examples_obtained, prev_outputs, it)

# description_method.py L394-396: generate_description_from_documentation 使用 dict
pos = examples["examples"]
neg = examples["neg_examples"]
```

**Go 问题代码**：
```go
// description_method.go L80: 直接传 []ExampleTuple，不构建 dict
outputMap = m.Generate(ctx, tool, examples, prevOutputs, it)

// description_method.go L440: 没有 neg 变量
pos := examples  // 只有正例
```

**修复方案**：在 `Step()` 的 `it > 0` 分支中构建 `examplesObtained` struct 或 map，包含 `NegExamples` 和 `Examples` 字段；将 `Generate` 和 `GenerateDescriptionFromDocumentation` 的 `examples` 参数改为接收该 struct。

---

### S05. ToolOptimizer OptimizeTool 循环中更新描述取索引与 Python 不一致

**严重程度**：严重 — 影响迭代优化过程中的描述更新

**Python 样例**：
```python
# base_method.py L57: 循环中取第一个 step 的描述
latest_description = result_descs[-1][-1][0]["description"]
```

**Go 问题代码**：
```go
// base.go L146-157: 循环中取最后一个 step 的描述
if len(lastNode) > 0 {
    lastStep := lastNode[len(lastNode)-1]  // [-1][-1][-1]
    if desc, ok := lastStep["description"].(string); ok { ... }
}
```

**修复方案**：将循环中的描述更新改为取 `resultDescs[-1][-1][0]`（第一个 step），与 Python 对齐：
```go
lastStep := lastNode[0]  // 取第一个 step而非最后一个
```

---

### S06. ToolOptimizer CritiqueAllDescriptions 缺少 prevOutputs 非空检查

**严重程度**：严重 — 当 examples 不为空但 prevOutputs 为空时行为不一致

**Python 样例**：
```python
# description_method.py L328: 同时检查 examples 和 prev_outputs
if len(examples) > 0 and prev_outputs is not None and len(prev_outputs) > 0:
    # 正负例分析分支
```

**Go 问题代码**：
```go
// description_method.go L293: 只检查 examples
if len(examples) == 0 {
    // 直接发送基础 prompt
    ...
}
// 当 examples 不为空但 prevOutputs 为空时，Go 会走正负例分析分支
// Python 会走基础 prompt 分支
```

**修复方案**：在 `CritiqueAllDescriptions` 中增加 `prevOutputs != nil && len(prevOutputs) > 0` 的检查条件。

---

### S07. StreamController.runOneRound 缺少 BaseException→ERROR 状态更新

**严重程度**：严重 — panic 后状态不一致

**Python 样例**：
```python
# stream_controller.py
except BaseException as e:
    team_logger.error("Failed to execute deep agent, {}", e, exc_info=True)
    await self._update_status(MemberStatus.ERROR)
```

**Go 问题代码**：
```go
// stream_controller.go — runOneRound 的 defer 中没有 panic 恢复后的 ERROR 状态更新
```

**修复方案**：在 `runOneRound` 的 defer 中增加 panic 恢复后的 ERROR 状态更新：
```go
defer func() {
    if r := recover(); r != nil {
        _ = sc.updateStatus(ctx, atschema.MemberStatusError)
    }
}()
```

---

### S08. AgentTool.createSubAgent workspace 构建逻辑与 Python 不对齐

**严重程度**：严重 — workspace 路径构建差异影响子代理工作环境

**Python 样例**：
```python
# code_agent_rail.py: workspace 有两个分支
if parent_config.workspace and isinstance(parent_config.workspace, Workspace):
    workspace = Workspace(root_path=str(parent_config.workspace.root_path), language=parent_config.language)
else:
    workspace_path = f"{parent_config.workspace}/{sub_session_id}" if parent_config.workspace else f"./{sub_session_id}"
    workspace = Workspace(root_path=workspace_path, language=parent_config.language)
```

**Go 问题代码**：
```go
// agent_tool.go: 简化为单一逻辑
if ws == nil {
    ws = hworkspace.NewWorkspace(subSessionID, "en")  // 硬编码 "en"，Python 用 parent_config.language
}
// fallback 时 Python 用 "./{sub_session_id}"，Go 只用 subSessionID（无 "./" 前缀）
```

**修复方案**：对齐 Python 的两分支逻辑：如果父 workspace 存在，复用 root_path+language；否则用 `./{subSessionID}` 作为 root_path，从 DeepConfig 获取 language。

---

### S09. AgentTool.createSubAgent max_iterations 硬编码 15 vs Python 使用父配置 fallback

**严重程度**：严重 — 默认迭代次数不一致

**Python 样例**：
```python
"max_iterations": spec.max_iterations if spec.max_iterations is not None else parent_config.max_iterations
```

**Go 问题代码**：
```go
maxIter := 15; if spec.MaxIterations > 0 { maxIter = spec.MaxIterations }  // 硬编码 15
```

**修复方案**：从 DeepConfig 获取 `parentConfig.MaxIterations` 作为 fallback，不硬编码 15。

---

### S10. SessionsSpawnTool task_id 格式不一致

**严重程度**：严重 — 跨系统追踪可能失败

**Python 样例**：
```python
task_id = uuid.uuid4().hex  # 32位纯十六进制
```

**Go 问题代码**：
```go
taskID := uuid.New().String()  // 带连字符 UUID
```

**修复方案**：
```go
taskID := hex.EncodeToString(uuid.New().Bytes())  // 对齐 Python uuid.uuid4().hex
```

---

### S11. ActionController.run_action 没有 panic 恢复 — handler panic 导致程序崩溃

**严重程度**：严重 — handler 异常会导致整个程序崩溃

**Python 样例**：
```python
# controllers/action.py L216-245
try:
    async with self._lock:
        raw = await _maybe_await(handler(...))
except Exception as exc:
    return {"ok": False, "error": str(exc)}
```

**Go 问题代码**：
```go
// controllers.go L336: 没有 recover，handler panic 会崩溃
result := handler(ctx, sid, rid, kwargs)
```

**修复方案**：在 RunAction 中包裹 handler 调用，使用 defer recover 捕获 panic：
```go
func (c *ActionController) RunAction(ctx context.Context, ...) map[string]any {
    // ...
    var result map[string]any
    func() {
        defer func() {
            if r := recover(); r != nil {
                result = map[string]any{"ok": false, "error": fmt.Sprintf("handler panic: %v", r)}
            }
        }()
        result = handler(ctx, sid, rid, kwargs)
    }()
    // ...
}
```

---

### S12. ActionController.run_action handler 执行不加锁 — 并发安全问题

**严重程度**：严重 — 同一时刻可能有多个 action 并发执行

**Python 样例**：
```python
# controllers/action.py L217: handler 执行受 asyncio.Lock 保护
async with self._lock:
    raw = await _maybe_await(handler(...))
```

**Go 问题代码**：
```go
// controllers.go L316-318: 只在获取 handler 时加锁，handler 执行不加锁
c.mu.Lock()
handler := c.handlers[actionName]
c.mu.Unlock()
// handler 执行时无锁保护
result := handler(ctx, sid, rid, kwargs)
```

**修复方案**：在 handler 执行期间加锁，确保同一时刻只有一个 action 执行：
```go
c.actionMu.Lock()  // 专用 action 互斥锁
defer c.actionMu.Unlock()
result := handler(ctx, sid, rid, kwargs)
```

---

### S13. browser_move buildDragPayload 缺少 element_source_offset 和 element_target_offset

**严重程度**：严重 — selector 模式坐标偏移功能完全缺失，影响拖拽精度

**Python 样例**：
```python
# controllers/action.py L554-555
payload["element_source_offset"] = _normalize_offset(kwargs.get("element_source_offset"))
payload["element_target_offset"] = _normalize_offset(kwargs.get("element_target_offset"))

# _normalize_offset (L283-294): 将 offset 值转为 {x: int, y: int}
def _normalize_offset(value):
    if isinstance(value, dict):
        return {"x": int(value.get("x", 0)), "y": int(value.get("y", 0))}
    ...
```

**Go 问题代码**：
```go
// controllers.go L1016-1026: payload 不包含 offset 字段
// _normalize_offset 函数完全缺失
// JS 代码中引用 element_source_offset/element_target_offset 但 payload 不提供
```

**修复方案**：
1. 实现 `normalizeOffset` 函数，将 offset 值转为 `{x: int, y: int}` 格式
2. 在 `buildDragPayload` 中添加 `element_source_offset` 和 `element_target_offset` 字段解析和传递

## 一般问题（共 15 个）

### G01. TenantAgentPool ResetInstance 缺少 "Resetting" 日志

**Python 样例**：
```python
@classmethod
def reset_instance(cls) -> None:
    if cls._instance is not None:
        logger.info("[TenantAgentPool] Resetting singleton instance")
```

**Go 问题代码**：
```go
func ResetInstance() {
    tenantAgentPoolSingleton.Reset()  // 无 "Resetting" 日志
}
```

**修复方案**：在 ResetInstance() 中补充日志。

---

### G02. SessionsSpawnTool task_id 生成在 session 检查之前

**修复方案**：将 `taskID := uuid.New().String()` 移到 session 检查之后。

---

### G03. AgentModeRail isTaskToolRegistered 查找路径与 Python 不同

**修复方案**：确认两个注册表在注册时同步更新，或改用 `runner.GetResourceMgr()` 查找。

---

### G04. AgentModeRail rejectTool 在 inputs 类型不匹配时不完整

**修复方案**：增加警告日志说明无法完整拒绝工具。

---

### G05. TodoTool._update_todos 中 removingFromInProgress 预计算逻辑与 Python 后验不一致

**修复方案**：当前实现语义等价且更安全，建议在注释中明确说明意图。

---

### G06. SpawnManager.on_unhealthy 回调注册时序 — 应先注册再存储

**Python 样例**：
```python
handle.on_unhealthy = _trigger_unhealthy_recovery  # 存储前设置
self.spawned_handles[member_name] = handle
```

**Go 问题代码**：
```go
m.spawnedHandles[memberName] = handle  // 先存储
h.SetOnUnhealthy(...)                  // 后注册回调
```

**修复方案**：将 SetOnUnhealthy 注册移到存储之前。

---

### G07. SpawnManager.RestartTeammate 返回值语义不同 + 缺少 DB ERROR 状态更新

**修复方案**：在最终失败路径添加 DB 状态更新或 TODO 注释标注。

---

### G08. SpawnManager.RestartTeammate 缺少 team_backend nil 检查

**修复方案**：在 BuildContextFromDB 后添加 team_backend nil 检查，提前返回错误。

---

### G09. SpawnManager.spawn_teammate session 参数传递为 nil

**Python 样例**：
```python
handle = await Runner.spawn_agent(..., session=session)
```

**Go 问题代码**：
```go
handle, err := runner.SpawnAgent(ctx, agentConfig, inputs, nil, nil, spawnOpts...)  // session=nil
```

**修复方案**：传入 session 或从 context 获取。

---

### G10. StreamController.executeRound 缺少 TIMED_OUT 状态分支

**修复方案**：检查 Go 的 ExecutionStatus 是否有 ExecutionStatusTimedOut 枚举值，添加超时检测分支。

---

### G11. SecurityCheckResult 缺少 warning 字段

**Python 样例**：
```python
class SecurityCheck:
    blocked: bool
    reason: str | None = None
    warning: str | None = None
```

**Go 问题代码**：
```go
type SecurityCheckResult struct {
    Blocked bool
    Reason string
    // 缺少 Warning string
}
```

**修复方案**：添加 `Warning string` 字段到 SecurityCheckResult。

---

### G12. PersistLargeOutput 编码方式差异 — Python UTF-8 replace vs Go []byte

**Python 样例**：
```python
content_bytes = combined.encode("utf-8", errors="replace")
```

**Go 问题代码**：
```go
contentBytes := []byte(combined)  // 不处理非 UTF-8 字符
```

**修复方案**：对纯 UTF-8 字符串两者结果一致，但如需完全对齐可使用 `strings.ToValidUTF8(combined, "\ufffd")` 替换无效字符。

---

### G13. ToolOptimizer OptimizeTool 错误处理行为不一致 — Go 不中断 vs Python 中断

**Python 样例**：`customized_pipeline` 失败时直接抛异常中断循环

**Go 问题代码**：`OptimizeTool` 在 example/description 阶段失败时不中断，继续执行

**修复方案**：如严格对齐 Python，example/description 阶段失败时应返回 error。如有意改进（更健壮），应在注释中明确标注这是偏离 Python 的改进。

---

### G14. ToolOptimizer tool_name 硬编码 "tool" vs Python 从 kwargs 获取

**修复方案**：添加 `WithToolName(name string)` 选项函数。

---

### G15. AgentCard 结构体缺失 — Python 有 AgentCard(BaseCard)

**Python 样例**：
```python
class AgentCard(BaseCard):
    input_params: Optional[dict] = None
    output_params: Optional[dict] = None
    interface_url: Optional[str] = None
```

**Go 问题代码**：card.go 中只有 BaseCard 和 WorkflowCard，缺少 AgentCard

**修复方案**：在 card.go 或单独文件中添加 AgentCard 结构体。

---

## 提示问题（共 15 个）

### T01-T06（TenantAgentPool/CodeAgentRail）

见原始报告 T01-T06，全部可接受/无需修改。

---

### T07. TaskManager.Cancel CancelledBy 设置为自身而非调用者

**Python 样例**：通过 `_current_task_id` 上下文变量获取调用者 ID

**Go 问题代码**：`task.CancelledBy = taskID`（被取消任务自身 ID）

**修复方案**：引入 context 传播机制或在 Cancel 方法签名中增加 cancelledBy 参数。

---

### T08. TaskManager 使用普通 map vs Python WeakValueDictionary

**修复方案**：Go 没有 weak reference，当前通过 RemoveCompleted() 手动清理是合理折衷。

---

### T09. TaskStatus.String() 返回大写 vs Python 小写

**修复方案**：如需跨语言通信，应返回小写值。仅内部使用则大写可接受。

---

### T10. ModelRequestConfig.top_p 默认值差异 — Python 0.1 vs Go nil

**修复方案**：需确认意图。Python 总传 top_p=0.1，Go 不传。0.1 会显著影响输出多样性。

---

### T11. ModelClientConfig Validate 需手动调用 vs Python 自动触发

**修复方案**：建议在构造函数中自动调用 Validate。

---

### T12. Bash/PowerShell Tool 缺少 stream 方法

**修复方案**：Python 有 invoke 和 stream 两个方法，Go 只有 invoke。可能有意延后。

---

### T13. tool_discovery LoadToolsTool 日志不含 result 内容

**Python 样例**：日志包含 `result=%s`（完整结果内容）

**Go 问题代码**：日志只有 tool_names 和 replace，不含 result

**修复方案**：可接受，Go 日志简化了信息量。如需对齐可补充。

---

### T14. tool_discovery SearchToolsTool 日志不含 matched 名称列表

**Python 样例**：日志包含 `matched=%s`（匹配工具名称列表）

**Go 问题代码**：日志只有 `match_count`，不含名称列表

**修复方案**：可接受。Go 有 formatMatches 函数但未在日志中使用。

---

### T15. Singleton ResetWithCleanup 测试未真正验证 resettable 接口

**修复方案**：创建一个实现 Cleanup() error 方法的类型来测试。

---

## ⤵️ 待回填代码状态检查

| 模块 | ⤵️ 标记位置 | 指向 | 确认状态 |
|------|-----------|------|---------|
| deep_adapter_a2x.go | A2X 4个方法 | 11.10 A2A | ✅ 正确指向 |
| allocator.go | 3个分配器 | 9.64 | ✅ 正确指向 |
| agent_manager.go | team evolution | 10.3.2 | ✅ 正确指向 |
| team_agent.go | coordination/destroy | 9.62/9.58 | ✅ 正确指向 |
| inprocess_spawn.go | Runner.RunAgentTeam | 9.85 | ✅ 正确指向 |
| stream_controller.go | session 传递 | 9.62 | ✅ 正确指向 |
| browser_move service.go | 5个核心方法 (runTask/EnsureRuntime/EnsureStarted/restart/NormalizeScreenshot) | 9.38-49 | ✅ 正确指向 |

所有 ⤵️ 标记的占位代码确实尚未实现，且指向了正确的未来章节。**无遗漏**。

---

## 审查总结

| 类别 | 数量 | 需立即修复 | 标注⤵️待回填 |
|------|------|-----------|-------------|
| 严重 | 13 | S01,S02,S03,S04,S05,S06,S07,S08,S09,S10,S11,S12,S13 | — |
| 一般 | 15 | G01-G15 | G07(⤵️DB) |
| 提示 | 15 | 无需修改 | — |
| ⤵️ 待回填 | 0 | — | 全部指向正确 |

### 优先立即修复（不依赖未实现模块）

| 优先级 | 问题编号 | 问题描述 | 修复复杂度 |
|--------|---------|---------|-----------|
| P0 | **S02** | Permission CheckPermission MatchString→子串匹配 | 中（需检查所有 pattern 是否含锚点） |
| P0 | **S01** | Bash/PowerShell 缺 rm 目标记录 | 高（需实现 history path + 调用链） |
| P1 | **S03** | SpawnManager on_unhealthy 缺 cleanup | 低（加一行 CleanupTeammate 调用） |
| P1 | **S11** | ActionController RunAction 无 panic 恢复 | 低（加 defer recover） |
| P1 | **S12** | ActionController RunAction handler 不加锁 | 低（加专用 action 互斥锁） |
| P1 | **S07** | StreamController runOneRound 缺 ERROR 状态 | 低（加 defer recover+状态更新） |
| P1 | **S10** | SessionsSpawnTool task_id 格式不一致 | 低（改为 hex.EncodeToString） |
| P1 | **S13** | buildDragPayload 缺 offset 字段 | 中（需实现 normalizeOffset） |
| P2 | **G06** | SpawnManager 回调注册时序 | 低（移动一行代码） |
| P2 | **G01** | TenantAgentPool ResetInstance 缺日志 | 低（加一行日志） |
| P2 | **G04** | AgentModeRail rejectTool 缺警告日志 | 低（加一行日志） |

### 需确认的设计差异

- **S01/S08/S09**: 需确认是否属于 ⤵️ 待回填（rm tracker、workspace、max_iterations）
- **S04/S05/S06**: ToolOptimizer 调用链中 neg_examples 传递，需设计重构
- **S03**: on_unhealthy 缺 DB 状态标记是 ⤵️ 9.64，但缺 CleanupTeammate 不是 ⤵️，应立即修复

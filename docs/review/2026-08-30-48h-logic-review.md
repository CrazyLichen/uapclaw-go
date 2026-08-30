# 48h 逻辑审查报告（2026-08-30）

> 审查范围：2026-08-29 ~ 2026-08-30 期间提交（26 个 commit，182 文件，+8682/-1700 行）
> 涉及章节：7.x Memory、9.x Multi-Agent Teams、9.70b/9.72b/9.72d Evolving、10.x AgentServer
> 对比标准：Python 源码方法签名、步骤流程、提示词一比一复刻

---

## 一、审查范围

| 提交 | 日期 | 涉及章节 | 说明 |
|------|------|---------|------|
| `ab02d362` | 08-29 | 7.1+7.2 | AddMemories 改用 MemoryUnit 接口 + 添加 llmModel 参数 |
| `181b4103` | 08-29 | 7.1 | Search 添加 score 降序排序和 topK 截断 |
| `fbf96473` | 08-29 | 7.2 | ListFragmentMemories 添加 memType 校验 |
| `2fbb73cf` | 08-29 | 7.3 | 补充 processConflictInfo ⤵️ 回填标记说明 |
| `d611e8bb` | 08-29 | 9.2-9.12 | TeamBackend 7 项修复 |
| `991fb294` | 08-29 | 9.70b | NewCase 添加 inputs/label 非空验证 |
| `cb56ae69` | 08-29 | 9.72b | ParseJSON 追加 Python 字面量替换 |
| `a7d4787c` | 08-29 | 9.72b | Format 补全 4 个 prompt 变体 |
| `ad015b73` | 08-29 | 9.72b | apiWrapper nil 时返回 error |
| `acbf4131` | 08-29 | 9.72d | EvolutionStoreReader 接口迁移 + LoadFullEvolutionLog 签名对齐 |
| `56d3b82d` | 08-29 | 10.3.10 | RecapPrompts 三处逻辑差异修复 |
| `2cc8efd2` | 08-29 | 10.3.12 | AgentManager 4 项逻辑修复 |
| `0e4f3e1b` | 08-29 | 10.1+10.2 | StructuredAskUserTool 自建 schema + 目录迁移 |
| `e00ef1d8` | 08-29 | 10.3.7 | AddInstalledPlugin 补充 version/commit 字段 |
| `1b383eb2` | 08-29 | 10.3.5 | TeamSkillsHub Publish 规范化 plugin.yaml + ZIP + SHA256 |
| `e2214fc2` | 08-29 | 10.6.1-10.6.2 | Prompt Builder 3 项修复 |
| `ccdfc892` | 08-29 | 10.3.19-20 | 技能管理 13 项修复 |
| `8d4a7517` | 08-30 | 通用 | gofmt 格式化 + 补充测试提升覆盖率 |
| `caffcc78` | 08-30 | lint | SA1012 和 errcheck 修复 |
| `64f5cf20` | 08-30 | lint | 剩余 SA1012 nil Context 和 errcheck 修复 |

---

## 二、问题汇总

| # | 分类 | 模块 | 问题摘要 |
|---|------|------|---------|
| 1 | 🔴 严重 | 7.x | WriteMemoryWithContext 中 WithFsAppend(appendMode) 应为 WithFsAppend(true) 对齐 Python |
| 2 | 🔴 严重 | 9.x | ShutdownMember 缺少 FSM 状态转换校验，错误信息不精确 |
| 3 | 🔴 严重 | 9.x | ApprovePlan 缺少前置校验（plan_id 空值、plan_record 查询、member 存在校验） |
| 4 | 🔴 严重 | 9.x | ContainerAgent.buildAgentInput 不处理非 dict 输入，纯字符串 query 会 panic |
| 5 | 🔴 严重 | 9.x | build_team 预定义成员未传 allocation，模型分配丢失 |
| 6 | 🔴 严重 | 10.x | AddInstalledPlugin 缺少 normalizePlugin 调用，enabled 字段缺失 |
| 7 | 🔴 严重 | 10.x | AddInstalledPlugin 未调用 saveState，安装记录不落盘 |
| 8 | 🔴 严重 | 10.x | buildTeamskillsPublishZip README.md 路径可能错误 |
| 9 | 🔴 严重 | 9.72 | experience/scorer.go parseTimestamp 不支持无时区 ISO 时间戳，freshness 计算错误 |
| 10 | 🟡 一般 | 10.x | ReloadAgentsConfig env override 空字符串语义与 Python 不一致（Go 删除 vs Python 设置空串） |
| 11 | 🟡 一般 | 7.x | MockEmbeddingProvider 返回空向量而非 128 维随机向量 |
| 12 | 🟡 一般 | 7.x | ResolveEmbeddingConfigFromEnv 的 model_name 默认 "default" 与 Python 不一致 |
| 13 | 🟡 一般 | 7.x | baseEmbeddingAdapter.dims 未设置默认值 |
| 14 | 🟡 一般 | 7.x | ListFragmentMemories 非法 memType 返回 nil 切片（JSON null vs []） |
| 15 | 🟡 一般 | 9.x | CancelMember/SendMessage 失败时返回 success 而非 fail |
| 16 | 🟡 一般 | 9.x | ShutdownMember 缺少 shutdown 消息发送失败处理 |
| 17 | 🟡 一般 | 9.x | CleanTeam Python 只检查 SHUTDOWN，Go 额外允许 ERROR |
| 18 | 🟡 一般 | 9.x | ForceCleanTeam 缺少 success 返回值 |
| 19 | 🟡 一般 | 9.x | spawn_human_agent 缺少 AgentCard 创建 |
| 20 | 🟡 一般 | 9.x | ContainerAgent.Invoke 中 interrupt_signal 重复提取 |
| 21 | 🟡 一般 | 9.x | i18n T() 函数 panic 与 Python KeyError 行为差异 |
| 22 | 🟡 一般 | 9.x | CommunicableAgent 缺少 agentID 空值保护 |
| 23 | 🟡 一般 | 10.x | defer f.Close() 在 WalkDir 循环内累积，可能超出文件描述符限制 |
| 24 | 🟡 一般 | 10.x | generateToolID 碰撞概率高于 Python uuid4 |
| 25 | 🟡 一般 | 9.72 | GetTeamTrajectoryIssues 类型断言在反序列化场景可能失败 |
| 26 | 🟡 一般 | 9.72 | Evaluate LLM 失败时返回 nil 而非空 slice |
| 27 | 🟡 一般 | 9.72 | BaseOptimizerMixin.Bind 中 config 参数无法传递给子类 |
| 28 | 🟢 提示 | 7.x | 类型断言跳过时无 Warn 日志（对齐 Python isinstance warning） |
| 29 | 🟢 提示 | 7.x | CreateEmbeddingProvider 缺少降级到 mock 时的 Warn 日志 |
| 30 | 🟢 提示 | 7.x | delete 日志 memory_id 格式差异（列表 vs 字符串） |
| 31 | 🟢 提示 | 7.x | MemUpdateChecker stub 未实现 duplicate_ids 检查 |
| 32 | 🟢 提示 | 9.x | CancelMember 缺少 reset 任务计数汇总日志 |
| 33 | 🟢 提示 | 9.x | extractor.go 全部为 ⤵️ 占位确认 |
| 34 | 🟢 提示 | 9.x | SupervisorAgent.Create 命名可能误导 |
| 35 | 🟢 提示 | 9.72 | FromEvaluatedCase 未使用 MakeEvolutionSignal 构建信号 |
| 36 | 🟢 提示 | 10.x | readWorkspaceFile 未被调用，待回填 |

---

## 三、问题详述

### 🔴 问题 1：WriteMemoryWithContext 中 WithFsAppend(appendMode) 应为 WithFsAppend(true) — 严重

**Python 参考代码** (`core/memory/lite/memory_tool_ops.py:139-144`):

```python
write_result = await sys_op.fs().write_file(
    resolved_path,
    content=content,
    create_if_not_exist=True,
    prepend_newline=append,    # 仅控制是否前置换行
    append=True,               # 始终为 True
)
```

Python 中 `append=True` 始终传入，`append` 变量（函数参数）仅控制 `prepend_newline`。

**Go 问题代码** (`internal/agentcore/memory/lite/tool_ops.go:206-213`):

```go
fsOpts := []sysop.FsOption{
    sysop.WithFsCreateIfNotExist(true),
    sysop.WithFsAppend(appendMode), // ← 当 appendMode=false 时传 false
}
if appendMode {
    fsOpts = append(fsOpts, sysop.WithFsPrependNewline(true))
}
```

**修复方案**：将 `sysop.WithFsAppend(appendMode)` 改为 `sysop.WithFsAppend(true)`，对齐 Python 中 `append=True` 始终传入的行为。

**影响**：`appendMode=false` 时如果底层文件系统实现 `WithFsAppend(false)` 为覆盖写入，已有文件内容会被清空，导致数据丢失。

---

### 🔴 问题 2：ShutdownMember 缺少 FSM 状态转换校验 — 严重

**Python 参考代码** (`agent_teams/tools/team.py:556-558`):

```python
if not is_valid_transition(current_status, MemberStatus.SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS):
    return MemberOpResult.fail(f"Member {member_name} cannot shut down from status '{current_status.value}'")
```

**Go 问题代码** (`internal/agent_teams/tools/team_backend.go:521-525`):

```go
ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
    member.Status, string(atschema.MemberStatusShutdownRequested))
if !ok {
    return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
}
```

Go 直接 CAS，错误信息不包含当前状态。

**修复方案**：CAS 前添加 FSM 校验，构造精确错误消息。

---

### 🔴 问题 3：ApprovePlan 缺少前置校验 — 严重

**Python 参考代码** (`agent_teams/tools/team.py:398-462`):

```python
if not plan_id:
    team_logger.error("approve_plan requires plan_id")
    return False
plan_record = self.task_manager.get_plan_record(plan_id)
if not plan_record:
    team_logger.error("Plan %s not found", plan_id)
    return False
member_name = str(plan_record.get("member_name") or "")
if not member_name:
    team_logger.error("Plan %s has no member_name", plan_id)
    return False
member_data = await self.db.member.get_member(member_name, self.team_name)
if member_data is None:
    team_logger.error(f"Member {member_name} not found in team {self.team_name}")
    return False
```

**Go 问题代码** (`internal/agent_teams/tools/team_backend.go:797-819`):

```go
err := tb.taskManager.ApprovePlan(ctx, planID, cfg.approved, cfg.feedback)
if err != nil {
    return atschema.NewMemberOpResultFail("approve_plan failed: " + err.Error())
}
task, _ := tb.taskManager.Get(ctx, planID)
```

Go 缺少所有 4 步校验，且用 `Get(ctx, planID)` 获取 task 而非 `getPlanRecord`。

**修复方案**：添加 plan_id 空值校验、使用 getPlanRecord 获取计划记录、校验 member_name 和 member 存在性。

**流程示例**：

```
Python 流程:                                  Go 当前流程:
1. plan_id 非空? → 否 → return False          1. (缺失)
2. plan_record = get_plan_record(plan_id)     2. (缺失)
3. plan_record 存在? → 否 → return False      3. (缺失)
4. member_name 非空? → 否 → return False      4. (缺失)
5. member 存在? → 否 → return False           5. (缺失)
6. approve_plan(plan_id, ...)                 6. approve_plan(planID, ...) → 直接执行
```

---

### 🔴 问题 4：ContainerAgent.buildAgentInput 不处理非 dict 输入 — 严重

**Python 参考代码** (`core/multi_agent/teams/handoff/container_agent.py:56-62`):

```python
def _build_agent_input(self, inputs):
    msg = inputs.input_message
    if not inputs.history:
        return msg
    if isinstance(msg, dict):
        return {**msg, "handoff_history": inputs.history}
    return {"query": msg, "handoff_history": inputs.history}  # ← 非 dict 时包装
```

**Go 问题代码** (`internal/agentcore/multi_agent/teams/handoff/container_agent.go:452-475`):

```go
func (c *ContainerAgent) buildAgentInput(req *HandoffRequest) map[string]any {
    msg := req.InputMessage  // 类型为 map[string]any
    if len(req.History) == 0 {
        return msg
    }
    result := make(map[string]any, len(msg)+1)
    for k, v := range msg {
        result[k] = v
    }
    result["handoff_history"] = historyData
    return result
}
```

Go 假设 `InputMessage` 始终是 `map[string]any`，如果传入纯字符串会 panic。

**修复方案**：在类型断言失败时包装为 `{"query": msg, "handoff_history": historyData}`。

---

### 🔴 问题 5：build_team 预定义成员未传 allocation — 严重

**Python 参考代码** (`agent_teams/tools/team.py:1024`):

```python
allocation = self._allocate_model_config(member_spec.model_name) if self._allocate_model_config else None
await self.spawn_member(member_name=member_spec.member_name, ..., allocation=allocation)
```

**Go 问题代码** (`internal/agent_teams/tools/team_backend.go:631-639`):

```go
for _, pm := range tb.predefinedMembers {
    tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, memberCardID,
        string(pm.RoleType), pm.Persona, pm.PromptHint, pm.ModelName)
    // ← 缺少 WithAllocation(allocation)
}
```

**修复方案**：调用 `tb.modelConfigAllocator(pm.ModelName)` 获取 allocation，通过 `WithAllocation` 传入。

---

### 🔴 问题 6：AddInstalledPlugin 缺少 normalizePlugin 调用 — 严重

**Python 参考代码** (`jiwenswarm/server/runtime/skill/skill_manager.py:3762-3771`):

```python
def _add_installed_plugin(self, plugin: dict) -> None:
    plugins = self._state.setdefault("installed_plugins", [])
    plugin = self._normalize_plugin(plugin)  # ← 补全 enabled=True
    for i, p in enumerate(plugins):
        if p.get("name") == plugin.get("name"):
            plugins[i] = plugin
            self._save_state()
            return
    plugins.append(plugin)
    self._save_state()
```

**Go 问题代码** (`internal/swarm/server/runtime/skill/skill_manager.go:1905-1918`):

```go
func (sm *SkillManager) AddInstalledPlugin(plugin map[string]any) {
    plugins := sm.GetInstalledPlugins()
    // ← 未调用 normalizePlugin，缺少 enabled=True
    ...
}
```

**修复方案**：入口处添加 `plugin = sm.normalizePlugin(plugin)`

**流程示例**：

```
用户安装技能 → HandleSkillsInstall → AddInstalledPlugin({name: "xxx", ...})
                                           ↓
  Python: _normalize_plugin 补全 enabled=True → 保存 → GetSkillEnabled → True
  Go: 无 normalize → 缺少 enabled 字段 → GetSkillEnabled → false（零值）
```

---

### 🔴 问题 7：AddInstalledPlugin 未调用 saveState — 严重

**Python 参考代码**：同问题 6，Python 在替换和追加两条路径都调用了 `self._save_state()`。

**Go 问题代码**：两条路径都没有调用 `sm.saveState()`。

**修复方案**：在替换和追加两条路径末尾各添加 `sm.saveState()`。

**流程示例**：

```
安装技能 → AddInstalledPlugin → 写入 sm.state["installed_plugins"]
                                    ↓
  Python: _save_state() → 写入磁盘 → 重启后仍在
  Go: 无 saveState → 仅写入内存 → 重启后丢失
```

---

### 🔴 问题 8：buildTeamskillsPublishZip README.md 路径可能错误 — 严重

**Go 问题代码** (`internal/swarm/server/runtime/skill/plugin_yaml.go:77-80, 150`):

```go
// L77-80: skillDir 可能已被修改为子目录（SKILL.md 搜索）
// L150: 但这里用 filepath.Dir(skillDir) 来找 README.md
readmePath := filepath.Join(filepath.Dir(skillDir), "README.md")
```

**修复方案**：保存原始 skillDir，README.md 在原始目录下查找。

---

### 🔴 问题 9：experience/scorer.go parseTimestamp 不支持无时区 ISO 时间戳 — 严重

**Python 参考代码** (`openjiuwen/core/evolving/experience/scorer.py:calc_freshness`):

```python
dt = datetime.fromisoformat(record.timestamp.replace("Z", "+00:00"))
if dt.tzinfo is None:
    dt = dt.replace(tzinfo=timezone.utc)
```

Python 的 `fromisoformat` 可以解析无时区的时间戳（如 `"2025-01-01T00:00:00"`），解析后补 UTC。

**Go 问题代码** (`internal/evolving/experience/scorer.go:660-674`):

```go
func parseTimestamp(s string) (time.Time, error) {
    // 尝试 RFC3339Nano 和 RFC3339 — 都要求有时区后缀
    t, err := time.Parse(time.RFC3339Nano, s)
    if err != nil {
        t, err = time.Parse(time.RFC3339, s)
    }
    if err != nil {
        return time.Time{}, err  // ← 无时区时间戳解析失败
    }
    return t.UTC(), nil
}
```

Go 的 `time.Parse(time.RFC3339, ...)` 要求有时区后缀（如 `+00:00` 或 `Z`），无时区的 ISO 时间戳会解析失败，导致 freshness 返回默认值 0.5，而 Python 正确计算天数差。

**修复方案**：增加对无时区 ISO 时间戳的解析支持：
```go
func parseTimestamp(s string) (time.Time, error) {
    // 先尝试标准格式
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

**流程示例**：

```
时间戳 "2025-01-01T00:00:00"（无时区后缀）
    ↓
Python: fromisoformat → 解析成功 → 补 UTC → 正确计算 freshness = f(days)
Go:     time.Parse(RFC3339, ...) → 解析失败 → 返回 error → freshness = 0.5
    ↓
结果：Go 中旧记录的 freshness 永远为 0.5，新记录的 freshness 接近 1.0
     → 评分偏差：旧经验被过度惩罚
```

---

### 🟡 问题 10：ReloadAgentsConfig env override 空字符串语义与 Python 不一致 — 一般

**Python 参考代码** (`jiwenswarm/server/runtime/agent_manager.py:310-316`):

```python
self._latest_env_overrides = dict(env) if isinstance(env, dict) else {}
for env_key, env_value in self._latest_env_overrides.items():
    if env_value is None:
        os.environ.pop(key, None)       # None → 删除
    else:
        os.environ[key] = str(env_value)  # "" → 设置为空字符串
```

Python 用 `env_value is None` 判断删除，空字符串 `""` 会被 `os.environ[key] = ""` 设置。

**Go 问题代码** (`internal/swarm/server/runtime/agent_manager.go:331-336`):

```go
for key, val := range envOverrides {
    if val == "" {
        _ = os.Unsetenv(key)  // 空字符串 → 删除（与 Python 不一致）
    } else {
        _ = os.Setenv(key, val)
    }
}
```

Go 的 `latestEnvOverrides` 类型为 `map[string]string`，无法表达 `nil` 语义，空字符串被当作删除。Python 中空字符串是合法的 env value（设置为空），Go 会错误地删除该环境变量。

**修复方案**：将 `latestEnvOverrides` 类型改为 `map[string]*string`，nil 表示删除，空字符串表示设置为空：
```go
type AgentManager struct {
    latestEnvOverrides map[string]*string  // nil = unset, "" = set empty
    ...
}

for key, val := range envOverrides {
    if val == nil {
        _ = os.Unsetenv(key)
    } else {
        _ = os.Setenv(key, *val)
    }
}
```

---

### 🟡 问题 11-14：7.x Memory 一般问题

| # | 问题 | Python 样例 | Go 问题 | 修复方案 |
|---|------|------------|---------|---------|
| 11 | MockEmbeddingProvider 返回空向量 | `[random.uniform(-1,1) for _ in range(128)]` | `return make([]float64, 0), nil` | 实现 128 维确定性随机向量或返回 128 个 0.0 |
| 12 | ResolveEmbeddingConfigFromEnv model_name 默认 "default" | `model_name` 为空时返回 None | `envModelName = "default"` | 删除 `envModelName = "default"` 逻辑 |
| 13 | baseEmbeddingAdapter.dims 未设置默认值 | `self.dims = 1024` | `dims` 默认为 0 | 设置 `dims` 默认值为 1024 |
| 14 | ListFragmentMemories 返回 nil 切片 | `return []` | `return nil, nil` | 改为 `return []*index.MemoryDoc{}, nil` |

---

### 🟡 问题 15-22：9.x Teams 一般问题

| # | 问题 | Python 行为 | Go 问题 | 修复方案 |
|---|------|------------|---------|---------|
| 15 | CancelMember/SendMessage 失败返回 success | `return False` | `return NewMemberOpResultSuccess()` | 检查 error，失败返回 fail |
| 16 | ShutdownMember 缺少消息发送失败处理 | `logger.warning(...)` | `_, _ = SendMessage(...)` | 检查 error，记录 warning |
| 17 | CleanTeam 额外允许 ERROR 状态 | 只检查 SHUTDOWN | `!= SHUTDOWN && != ERROR` | 移除 ERROR 豁免 |
| 18 | ForceCleanTeam 始终返回 true | `return success` (可能 False) | `return true, nil` | 获取 ForceDeleteTeamSession 结果 |
| 19 | spawn_human_agent 缺少 AgentCard | 创建 `AgentCard(id=f"{team}_{member}")` | 传空字符串 `""` | 构造 AgentCard 传入 |
| 20 | ContainerAgent.Invoke interrupt_signal 重复提取 | 统一提取一次 | L258 和 L268 两次提取 | 移除 L258 重复 |
| 21 | i18n T() panic vs KeyError | 抛 KeyError（可 catch） | panic（不可恢复） | 改为 log.Printf + 返回 key |
| 22 | CommunicableAgent 缺少 agentID 空值保护 | `raise build_error` | 只检查 `runtime == nil` | 改用 `!c.IsBound()` |

---

### 🟡 问题 23-24：10.x AgentServer 一般问题

| # | 问题 | 修复方案 |
|---|------|---------|
| 23 | defer f.Close() 在 WalkDir 循环内累积 | 改为 `io.ReadAll(f)` + `f.Close()` 立即关闭 |
| 24 | generateToolID 碰撞概率高于 Python uuid4 | 使用 `crypto/rand` 生成 16 字节（32 字符 hex） |

---

### 🟡 问题 25-27：9.72 Evolving 一般问题

| # | 问题 | Python 行为 | Go 问题 | 修复方案 |
|---|------|------------|---------|---------|
| 25 | GetTeamTrajectoryIssues 类型断言 | 保留原始 dict 类型 | `[]map[string]string` 断言，反序列化场景失败 | 改为 `[]any` + 逐项断言 |
| 26 | Evaluate LLM 失败返回 nil | `return []` | `return nil, nil` | 改为 `return []map[string]any{}, nil` |
| 27 | BaseOptimizerMixin.Bind config 无法传给子类 | 子类通过 `**config` 读取 | 子类无法获取 config | 子类覆盖 Bind 自行处理 |

---

### 🟢 问题 28-36：提示级别问题

| # | 模块 | 问题 | 修复方案 |
|---|------|------|---------|
| 28 | 7.x | 类型断言跳过时无 Warn 日志 | 添加 Warn 日志对齐 Python isinstance warning |
| 29 | 7.x | CreateEmbeddingProvider 缺少降级到 mock 时的 Warn 日志 | 添加 Warn 日志 |
| 30 | 7.x | delete 日志 memory_id 格式差异 | 使用 `.Strs("memory_id", []string{memID})` |
| 31 | 7.x | MemUpdateChecker stub 未实现 duplicate_ids 检查 | 在回填标记中补充说明 |
| 32 | 9.x | CancelMember 缺少 reset 任务计数汇总日志 | 添加 reset 成功计数和汇总日志 |
| 33 | 9.x | extractor.go 全部为 ⤵️ 占位确认 | 确认正确，待 7.2/9.65a 回填 |
| 34 | 9.x | SupervisorAgent.Create 命名可能误导 | 考虑重命名或添加注释 |
| 35 | 9.72 | FromEvaluatedCase 未使用 MakeEvolutionSignal | 改用 MakeEvolutionSignal 保持一致性 |
| 36 | 10.x | readWorkspaceFile 未被调用 | 待后续 RuntimePromptRail 实现时回填 |

---

## 四、⤵️ 回填状态汇总

| 位置 | 回填目标 | 当前状态 |
|------|---------|---------|
| `memory/manage/update/update_checker.go` | 7.8 LLM 驱动冲突检查 | stub 返回全部 ADD |
| `memory/manage/index/fragment_manager.go` | 7.8 processConflictInfo | 标记 ⤵️ |
| `memory/lite/coding_memory_tool_ops.go` | 7.8 MemUpdateChecker 冗余判断 | 跳过 SKIP 逻辑 |
| `agent_teams/memory/extractor.go` | 7.2+9.65a | 空实现 |
| `context_engine/context/session_memory_manager.go` | 6.x agent_edit 模式 | 返回 error |
| `context_engine/processor/offloader/*.go` | 5.31 WorkspaceDir | ⤵️ 标记 |
| `swarm/server/runtime/agent_manager.go` | ACP 章节 | ⤵️ 标记 |
| `agents/harness/common/prompt/prompt_builder.go` | RuntimePromptRail | readWorkspaceFile 未调用 |

---

## 五、统计

| 严重程度 | 数量 | 占比 |
|---------|------|------|
| 🔴 严重 | 9 | 25% |
| 🟡 一般 | 18 | 50% |
| 🟢 提示 | 9 | 25% |
| **合计** | **36** | **100%** |

| 模块 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| 7.x Memory | 1 | 4 | 4 | 9 |
| 9.x Teams | 4 | 9 | 3 | 16 |
| 9.72 Evolving | 1 | 3 | 1 | 5 |
| 10.x AgentServer | 3 | 2 | 1 | 6 |

---

## 六、优先修复建议

### 立即修复（严重问题，影响功能正确性）

1. **问题 9** — scorer.go parseTimestamp 不支持无时区 ISO 时间戳：旧记录 freshness 永远为 0.5，评分偏差
2. **问题 1** — `WithFsAppend(appendMode)` → `WithFsAppend(true)`：可能导致文件覆盖写入
3. **问题 6+7** — AddInstalledPlugin 缺少 normalizePlugin 和 saveState：安装记录丢失和状态错误
4. **问题 3** — ApprovePlan 缺少前置校验：可能对不存在的 plan 执行批准
5. **问题 4** — buildAgentInput 不处理非 dict 输入：纯字符串 query 会 panic
6. **问题 5** — build_team 预定义成员未传 allocation：模型分配丢失
7. **问题 8** — README.md 路径可能错误：ZIP 中缺少 README
8. **问题 2** — ShutdownMember 缺少 FSM 校验：错误信息不精确

### 短期修复（一般问题，影响行为一致性）

9. **问题 10** — env override 空字符串语义：Go 删除 vs Python 设置空串
10. **问题 15** — CancelMember/SendMessage 失败返回 success
11. **问题 17** — CleanTeam 额外允许 ERROR 状态
12. **问题 23** — defer f.Close() 在 WalkDir 循环内累积
13. **问题 25** — GetTeamTrajectoryIssues 反序列化场景类型断言失败

# 48h 逻辑审查报告 — 2026-09-17

> 审查范围：最近 7 天内完成的实现章节
> 审查日期：2026-09-17
> 审查方法：逐方法对照 Python 参考项目，检查签名/步骤/占位代码

## 审查章节

| 章节 | 状态 | 描述 |
|------|------|------|
| 9.66a | ✅ | WorktreeManager 完整实现 |
| 10.6.7 | ✅ | ProjectMemoryRail + code_adapter 回填 |
| 10.6.7 | ✅ | RuntimePromptRail + adapter 集成 |
| 9.24 P4 | ✅ | TeamSkillEvolutionRail 实现 |
| 10.3.7-11 | 🔄 | 适配器辅助（CodeAgentRail/EvolutionHelpers/Recap/SysOpBuilder） |

## 问题统计

| 严重级别 | 数量 | 问题ID |
|---------|------|--------|
| **严重** | 9 | RP-01, RP-02, PM-01, PM-02, PM-03, TE-01, TE-02, TE-03, TE-20 |
| **一般** | 22 | RP-04~08, RP-12/13, PM-04~10, TE-04~08, TE-10, TE-13, TE-14, TE-16, TE-23 |
| **提示** | 14 | RP-03/09~11, PM-11~15, TE-09, TE-11/12, TE-15, TE-17/18 |

---

## 一、RuntimePromptRail (10.6.7)

### RP-01 [严重] 缺少 `Uninit` 方法，7 个 section 在 rail 卸载时残留

**描述**：Python 的 `RuntimePromptRail` 覆写了 `uninit(self, agent)`，清理 7 个注入的 section（time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy），并将 `system_prompt_builder` 置 None。Go 没有覆写 `Uninit`，继承 `BaseRail.Uninit()` 的 no-op 默认实现。如果 RuntimePromptRail 被动态卸载，注入的 7 个 section 会残留在 system prompt builder 中。

**Python 样例**：
```python
def uninit(self, agent) -> None:
    if self.system_prompt_builder is not None:
        self.system_prompt_builder.remove_section("time")
        self.system_prompt_builder.remove_section("runtime")
        self.system_prompt_builder.remove_section("language_output")
        self.system_prompt_builder.remove_section("env")
        self.system_prompt_builder.remove_section("git_status")
        self.system_prompt_builder.remove_section("browser_tool_policy")
        self.system_prompt_builder.remove_section("trusted_dirs_policy")
    self.system_prompt_builder = None
```

**Go 问题**：`runtime_prompt_rail.go` 没有 `Uninit` 方法。

**修复方案**：
```go
func (r *RuntimePromptRail) Uninit(agent agentinterfaces.BaseAgent) error {
    if spb := agent.SystemPromptBuilder(); spb != nil {
        for _, name := range []string{"time", "runtime", "language_output",
            "env", "git_status", "browser_tool_policy", "trusted_dirs_policy"} {
            spb.RemoveSection(name)
        }
    }
    return nil
}
```

---

### RP-02 [严重] `existingDirs`/`existingDir` 缺少 `ExpandHome`（`~` 展开）

**描述**：Python 在路径规范化时调用 `os.path.expanduser(item.strip())`，将 `~/xxx` 展开为用户 home 目录。Go 只调用了 `filepath.Clean`，没有调用项目已有的 `ExpandHome` 函数。如果用户配置了 `~/project` 形式的 trusted_dirs，Go 不会展开 `~`，导致路径无法识别。

**Python 样例**：
```python
path = os.path.abspath(os.path.expanduser(item.strip()))
```

**Go 问题**（L645-646）：
```go
path := filepath.Clean(item)
key := filepath.Clean(strings.ToLower(path))
```

**修复方案**：
```go
path := filepath.Clean(pathutil.ExpandHome(item))
```

---

### RP-04 [一般] `readRuntimeStateYAML` 在 BeforeModelCall 中被调用 3 次，冗余文件 I/O

**描述**：`injectRuntimeSection`、`injectLanguageOutputSection`、`injectGitStatusSection` 各自独立调用 `readRuntimeStateYAML()`，而 Python 中只读一次 `runtime_state.yaml`。每次调用都会读取文件、解析 YAML，3 次冗余 I/O。

**Python 样例**：Python 在 `before_model_call` 中只读一次：
```python
runtime_state: dict[str, Any] = {}
try:
    with open(get_config_dir() / "runtime_state.yaml", encoding="utf-8") as f:
        runtime_state = yaml.safe_load(f) or {}
except FileNotFoundError:
    pass
```

**修复方案**：在 `BeforeModelCall` 中读一次 `runtimeState`，作为参数传给各 inject 方法。

---

### RP-05 [一般] `injectTimeSection` 中文分支 cn/en content 与 Python 不一致

**描述**：Python 的 time section 在中文分支时 cn 和 en 内容完全相同（都是中文时间说明）。Go 的中文分支中 cn 用中文、en 用英文。

**Python 样例**：
```python
content={"cn": time_content, "en": time_content},  # 两者相同
```

**Go 问题**：
```go
Content: map[string]string{"cn": timeContentCN, "en": timeContentEN},  // 不同
```

**修复方案**：对齐 Python，中文分支时 cn/en 都用中文内容。

---

### RP-06 [一般] `osVersion` 使用 `GOARCH` 而非 Python `platform.release()`

**描述**：Python 使用 `platform.release()` 获取内核版本号（如 "6.5.0-44-generic"），Go 使用 `runtime.GOARCH`（如 "amd64"），语义不同。

**Python**：`os_version = f"{plat.system()} {plat.release()}"` → "Linux 6.5.0-44-generic"  
**Go**：`osVersion := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)` → "linux amd64"

**修复方案**：通过 `syscall.Uname` 获取内核版本，或加注释标注差异。

---

### RP-07 [一般] `WriteRuntimeStateYAML` 字段与 Python 不对齐

**描述**：Go 写入了 `platform`、`go_version` 字段（Python 不存在），缺少 `timezone_offset` 字段。

**修复方案**：移除 Go 扩展字段或标注为 Go 扩展；添加 `timezone_offset` 字段。

---

### RP-08 [一般] `existingDirs` 返回 nil 而非空切片

**描述**：JSON 序列化时 nil → null，空切片 → []。

**修复方案**：`result := make([]string, 0)`

---

### RP-03 [提示] `SetRuntimePaths` 空白字符串处理

**描述**：当前 Go 代码逻辑上与 Python 等价，但若结构体字段改为 `*string` 则差异显现。建议加注释。

---

### RP-09 [提示] `NewRuntimePromptRail` 缺少 `timezone_offset` 参数

**描述**：Python 有 `timezone_offset=8` 参数，Go 省略。Python 中仅写入 YAML，before_model_call 不使用。加注释标注省略原因即可。

---

### RP-10 [提示] `injectTimeSection` 中文分支缺少 `\n` 后缀

**描述**：影响极小，可忽略。

---

### RP-11 [提示] `buildRuntimePromptRail` 空字符串 sessionID 行为

**描述**：Go 显式传空字符串，Python 隐式依赖实例属性默认值。行为一致。

---

### RP-12 [一般] `workspace.AgentRootDir()` 与 Python `get_agent_workspace_dir()` 可能不等价

**描述**：需验证路径结构是否一致。

---

## 二、ProjectMemoryRail (10.6.7)

### PM-01 [严重] `SetAdditionalDirectories` 去重使用 `filepath.Abs` 而非 `EvalSymlinks`

**描述**：Python 使用 `os.path.realpath()` 解析符号链接后去重。Go 使用 `filepath.Abs()` 不解析符号链接。如果 `/dirA` 是 `/dirB` 的符号链接，Python 会去重，Go 不会——导致同一目录被扫描两次。

**Python 样例**：
```python
base_resolved = {os.path.realpath(d) for d in self._additional_directories}
```

**Go 问题**（L181-186）：
```go
if resolved, err := filepath.Abs(d); err == nil {
    baseResolved[resolved] = struct{}{}
}
```

**修复方案**：替换为 `safeResolve(d)`（project_memory 包中已有，调用 `filepath.EvalSymlinks`）。

---

### PM-02 [严重] `buildProjectMemoryRail` 缺少 `instance_overrides` 和 `config_cache` 的 additional_directories 源

**描述**：Python 的查找顺序：instance_overrides → config_cache → 环境变量。Go 仅从环境变量读取，完全缺失前两个来源。instance_overrides 配置的额外目录完全失效。

**Python 样例**：
```python
raw_additional_dirs = self._instance_overrides.get(
    "project_memory_additional_directories",
    self._config_cache.get("project_memory", {}).get("additional_directories"),
)
if raw_additional_dirs is None:
    raw_additional_dirs = os.getenv("JIUWENSWARM_ADDITIONAL_DIRECTORIES", "")
```

**Go 问题**（`code_adapter.go` L1053-1064）：
```go
rawEnv := os.Getenv("UAPCLAWSWARM_ADDITIONAL_DIRECTORIES")
```

**修复方案**：在 CodeAdapter 中实现 `instance_overrides` 和 `config_cache` 的等价查找，按 Python 优先级回退。

---

### PM-03 [严重] `buildProjectMemoryRail` 缺少异常保护

**描述**：Python 在整个构建函数外包裹 try/except，任何异常返回 nil。Go 没有recover 保护，nil pointer panic 会导致程序崩溃。

**Python 样例**：
```python
def _build_project_memory_rail(self) -> ProjectMemoryRail | None:
    try:
        # ... 构建逻辑 ...
        return rail
    except Exception as exc:
        logger.warning("[JiuwenClawCodeAdapter] ProjectMemoryRail create failed: %s", exc)
        return None
```

**修复方案**：添加 `defer func() { if r := recover(); r != nil { ... } }()` 或 nil 检查。

---

### PM-04 [一般] `BeforeModelCall` 缺少 discover 失败时的顶层异常日志

**描述**：Go 的 `DiscoverAndLoadMemoryFiles` 不返回 error，Python 有 `logger.exception` 记录整体失败。

---

### PM-05 [一般] `detectGitWorktree` 每次调用都重新 LookPath，Python 有 lru_cache

**修复方案**：使用 `sync.Once` 缓存 git 路径。

---

### PM-06 [一般] `isRelativeTo` 实现与 Python `_is_relative_to` 语义差异

**描述**：Python 先 resolve 再判断，Go 用 `filepath.Rel` 不先 resolve。符号链接场景行为不同。

**修复方案**：在 `isRelativeTo` 内部先对两边调用 `safeResolve`。

---

### PM-07 [一般] `DiscoverAndLoadMemoryFiles` 对空 workspace 处理不一致

**描述**：空字符串 workspace 在 Go 中 `filepath.Abs("")` 返回 cwd，Python `Path("")` 抛异常。

**修复方案**：入口检查 workspace 为空，返回空列表并记录警告。

---

### PM-08 [一般] 缓存键用 joined string 可能碰撞

**描述**：Python 用 `tuple[str, ...]`，Go 用 `strings.Join(result, PathListSeparator)`。路径含 `:` 时可能碰撞。

**修复方案**：用 `\x00` 分隔或改为排序后 hash。

---

### PM-09 [一般] `extractPathsGlobs` 缺少 `[]any` 类型处理

**描述**：Python `isinstance(raw, (list, tuple))` 处理混合类型列表，Go 只处理 `string` 和 `[]string`。

**修复方案**：添加 `case []any:` 分支。

---

### PM-10 [一般] `buildProjectMemoryRail` 仅支持 string 类型的 additional_dirs

**描述**：Python 支持 str/list/tuple/set 类型，Go 仅支持 string 环境变量。需配合 PM-02 修复。

---

### PM-11 [提示] `safeResolveDir` 是死代码

**修复方案**：删除或添加预留注释。

---

### PM-12 [提示] `remove_section` 使用 recover 可能掩盖真实错误

**修复方案**：检查 `RemoveSection` 签名，如返回 error 应检查而非 recover。

---

### PM-13 [提示] candidates 使用 slice vs Python set

**描述**：风格差异，功能影响极小。

---

### PM-14 [提示] DeepAgentRail 嵌入方式与 Python 继承语义差异

**修复方案**：确认 `DeepAgentRail` 零值安全性。

---

### PM-15 [提示] `normalizeAdditionalDirectories` 返回 string 需再次 split

**修复方案**：改为返回 `[]string`，cacheKey 相应调整。

---

## 三、TeamSkillEvolutionRail (9.24 P4)

### TE-01 [严重] `SnapshotForEvolution` 缺少 `cbc == nil` 分支

**描述**：Python 在 `ctx is None` 时返回只含 trajectory+空 messages 的快照。Go 缺少此分支，当 cbc 为 nil 时可能 panic。

**Python 样例**：
```python
async def _snapshot_for_evolution(self, trajectory, ctx=None):
    if not getattr(self, "_auto_scan", True):
        return None
    if ctx is None:
        return EvolutionSnapshot(
            trajectory=trajectory, messages=[], skill_name="team-skill",
        ).to_legacy_dict()
    snapshot = await super()._snapshot_for_evolution(trajectory, ctx)
    ...
```

**修复方案**：添加 `cbc == nil` 早期返回分支。

---

### TE-02 [严重] `detectExperienceDetailRead` 使用 `context.Background()` 而非传入 ctx

**描述**：Go 版在 `isTeamSkill` 调用时使用 `context.Background()` 而非 `OnAfterToolCall` 传入的 ctx，导致 context 传播丢失（如超时/cancel 无法传递）。

**Go 问题**：
```go
if r.isTeamSkill(context.Background(), skillName) {
```

**修复方案**：将 `ctx` 传入 `detectExperienceDetailRead` → `isTeamSkill`。

---

### TE-03 [严重] 路径解析缺少 `expanduser/resolve`

**描述**：Python 使用 `Path(file_path).expanduser().resolve()` 规范化路径，Go 只用 `filepath.Clean`。含 `~` 或符号链接的路径无法正确匹配到技能目录。

**Python 样例**：
```python
read_path = Path(file_path).expanduser().resolve()
```

**Go 问题**：
```go
readPath := filepath.Clean(filePath)
```

**修复方案**：使用 `ExpandHome` + `filepath.EvalSymlinks` + `filepath.Abs`。

---

### TE-20 [严重] `findTeamSkillRail` 是占位符，审批功能不可用

**描述**：`findTeamSkillRail()` 在 `d.teamSkillEvolutionRail == nil` 时返回 nil，而非从 `instance.rails` 中查找。注释标注 `⤵️ 10.6.3-10`。如果 TeamSkillEvolutionRail 未被显式注入，审批功能完全不可用。

**Go 问题**（`deep_adapter_team.go`）：
```go
func (d *DeepAdapter) findTeamSkillRail() *evolution.TeamSkillEvolutionRail {
    if d.teamSkillEvolutionRail != nil {
        return d.teamSkillEvolutionRail
    }
    // ⤵️ 10.6.3-10: 在 instance.rails 中查找 TeamSkillEvolutionRail
    return nil
}
```

**修复方案**：实现 instance.rails 遍历查找逻辑，或在 DeepAdapter 初始化时确保注入。

---

### TE-04 [一般] `teamExtractToolContent` 缺少 `data.content` → `result.content` 回退链

**描述**：Python 的 `_extract_tool_content` 有三级回退：`data.skill_content` → `data.content` → `tool_msg.content` → `result.content`。Go 版缺少 `result` 为结构体且有 `content` 字段的情况处理。

---

### TE-05 [一般] `run_evolution` 中 user_intent 信号处理：Go 去重，Python 不去重

**描述**：Go 调用 `teamAppendUniqueSignal` 去重，Python 直接 `signals.append`。行为差异。

**修复方案**：为保持一致，改为直接 append 不去重。

---

### TE-06 [一般] `RunEvolution` 常规 error 路径缺少 `emitProgress("failed")`

**描述**：Python 在 except 块中调用 `_emit_progress("failed", ...)`，Go 只在 panic recover 中调用。常规 error 路径（如 detectUsedTeamSkill 返回空、handleEvolutionFromSignals 返回 err）没有 emit "failed" progress。

---

### TE-07 [一般] `EvolutionConfig` 缺少 `max_concurrent_evolution` 字段

**描述**：Python 返回 7 个字段，Go 返回 6 个，缺少 `max_concurrent_evolution`。

---

### TE-08 [一般] `isActiveRequestSubject` 简化了 Python 的异常处理

**描述**：Python 有 try/except 保护 `skill_exists` 和 `resolve_skill_dir`，Go 没有。内部 panic 不会优雅降级。

---

### TE-10 [一般] `emitApprovalRequest` 检查粒度差异

**描述**：Go 检查 `stagedReq == nil`，Python 检查 `stagedReq.pending_change is not None`。粒度不同。

---

### TE-13 [一般] `inferTeamSkillFromTrajectory` 中 LLM 步骤收集是死代码

**描述**：Go 版额外添加了 LLM 类型步骤的文本收集代码，但 `step.Kind != StepKindTool` 已 continue 跳过，所以 LLM 分支永远不会执行。是死代码，应删除。

---

### TE-14 [一般] `RecordPresentedExperiences` 的 sessionID 与 Python session 对象语义不同

**描述**：Python 传入 session 对象，Go 传入 sessionID string。需确认 ExperienceTracker 内部处理一致。

---

### TE-16 [一般] `OnRejectSimplify` 缺少 skillName 日志字段

**描述**：SkillEvolutionRail 的版本有 skillName 日志，TeamSkillEvolutionRail 版本缺少。

---

### TE-23 [一般] `UpdateLLM` 缺少 ExperienceScorer 更新

**描述**：Go 版只更新了 `generator` 和 `teamSignalDetector`，缺少 `r.scorer.UpdateLLM`，导致 scorer 使用旧 LLM 引用。

**修复方案**：添加 `r.scorer.UpdateLLM(llmModel, model)` 调用。

---

### TE-09 [提示] `eval_interval < 1` 时 Go 静默修正为 1，Python 抛 ValueError

**描述**：Go 风格合理，建议在日志中记录修正行为。

---

### TE-11 [提示] `emitApprovalRequest` 为 nil vs Python 的 lambda: None

**描述**：功能等价，无需修改。

---

### TE-12 [提示] `knownSkills` 是 `[]string` 而非 set

**描述**：低优先级，`inferSkillFromTexts` 内部不影响结果。

---

### TE-15 [提示] `_all_tasks_completed` 提升为包级函数

**描述**：组织方式差异，可接受。

---

### TE-17 [提示] `requestID` 指针与空字符串差异

**修复方案**：空字符串时设 `RequestID: nil`。

---

### TE-18 [提示] 条件检查顺序等价

**描述**：无需修改。

---

### TE-19 [提示] `emitProgress` 的 `WithSkillName` 一致

**描述**：已一致，无需修改。

---

## 四、⤵️ 回填标记验证

以下标记经确认确实尚未实现（摘录影响较大的）：

| 标记位置 | 回填目标 | 状态 |
|---------|---------|------|
| `deep_adapter_rails.go` L91, L260+ | 10.6.3-10 SkillCreateRail/JiuClawStreamEventRail/MemoryRail/ExternalMemoryRail/ResponsePromptRail | 未实现 |
| `code_adapter.go` L65, L73 | 10.6.3-10 LspRail/WorktreeRail | ⚠️ WorktreeRail 已实现但 code_adapter 仍标记⤵️ |
| `deep_adapter.go` L115-216 | 10.6.3-10 SkillUseRail/JiuClawStreamEventRail/ResponsePromptRail/MemoryRail 等 | 未实现 |
| `deep_adapter_team.go` | findTeamSkillRail 占位 | 未实现 |
| `code_adapter.go` L388, L534, L600 | 工具 workspace_path/update_tools_for_mode/session_tools | 部分实现 |

**关键发现**：`code_adapter.go` L73 标记 `⤵️ 10.6.3-10: WorktreeRail`，但 WorktreeRail 已在 9.66a 中实现且 `buildWorktreeRail` 也已实现。此⤵️标记应更新为已回填。

---

## 五、适配器辅助 (10.3.7-11)

### AD-01 [严重] CodeAdapter 中 WorktreeRail ⤵️ 标记过时

**描述**：`code_adapter.go` L73 仍标记 `⤵️ 10.6.3-10: WorktreeRail`，但 `buildWorktreeRail()` 已在 commit `60a4ab47` 中实现。应在 code_adapter 中使用 `c.buildWorktreeRail()` 替换 nil 占位。

---

## 修复优先级建议

### P0 — 必须立即修复（功能缺陷/崩溃风险）

| 优先级 | 问题ID | 描述 | 预估工时 |
|--------|--------|------|---------|
| 1 | RP-01 | RuntimePromptRail 缺少 Uninit，section 残留 | 0.5h |
| 2 | RP-02 | existingDirs 缺少 ExpandHome | 0.5h |
| 3 | PM-02 | buildProjectMemoryRail 缺少 instance_overrides/config_cache 源 | 2h |
| 4 | PM-03 | buildProjectMemoryRail 缺少异常保护 | 0.5h |
| 5 | TE-01 | SnapshotForEvolution 缺少 cbc==nil 分支 | 0.5h |
| 6 | TE-02 | detectExperienceDetailRead 使用 context.Background() | 1h |
| 7 | TE-03 | 路径解析缺少 ExpandHome/EvalSymlinks | 1h |
| 8 | TE-20 | findTeamSkillRail 是占位符 | 2h |
| 9 | AD-01 | CodeAdapter WorktreeRail ⤵️ 标记过时 | 0.5h |

### P1 — 本迭代内修复

| 问题ID | 描述 |
|--------|------|
| PM-01 | SetAdditionalDirectories 去重用 Abs 应改 EvalSymlinks |
| RP-04 | readRuntimeStateYAML 冗余 3 次 I/O |
| RP-05 | injectTimeSection 中文分支 cn/en 不一致 |
| TE-04 | teamExtractToolContent 回退链不完整 |
| TE-06 | RunEvolution error 路径缺少 emitProgress("failed") |
| TE-07 | EvolutionConfig 缺少 max_concurrent_evolution |
| TE-23 | UpdateLLM 缺少 scorer 更新 |

### P2 — 后续迭代修复

其余一般和提示级别问题。

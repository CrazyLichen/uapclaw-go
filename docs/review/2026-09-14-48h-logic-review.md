# 48 小时代码逻辑审查报告

> 审查时间：2026-09-14
> 审查范围：48 小时内 22 个提交（`538364c3..edccc217`），重点章节 9.80a ExperienceSharing / 9.24 P3 SkillEvolutionRail / 7.10 Memory Index / Memory any 类型收紧 / 10.6.6 Interrupt Rail
> Python 参考：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 一、提交概览与涉及章节

| 提交 | 描述 | 涉及章节 | 变更文件数 |
|------|------|---------|-----------|
| `edccc217` | 修复 errcheck/staticcheck lint 错误 | 全局 | 10+ |
| `1b067591` | 修复单元测试预期和 errcheck lint | 全局 | 5+ |
| `62764446` | 修复 CI 流水线 — gofmt + stubSearchManager | CI | 3+ |
| `a131e7df` | 更新 memory any tightening 实施计划状态 | docs | 1 |
| `c7acb5e8` | 用 typed struct 替换 map[string]any 紧缩 any | 7.10 | 20+ |
| `29f9e971` | 添加 memory any 类型收紧实施计划 | docs | 1 |
| `c11f26ca` | 实现 SkillEvolutionRail P3 单 Agent 技能演进护栏 | 9.24 | 15+ |
| `1a18b44c` | 添加 memory any 类型收紧设计文档 | docs | 1 |
| `1847a362` | 审查修复 — 接口集中、mirrorBundle 返回 error | 9.80a | 8+ |
| `b4efcdb2` | 2026-09-10 审查修复（33 项） | 10.3/10.6 | 30+ |
| `196e9b4c` | 10.6.6 Interrupt Rail 对齐修复 | 10.6.6 | 5+ |
| `fd75fc67` | 实现 9.80a ExperienceSharing 跨 Agent 经验共享 | 9.80a | 20+ |
| `588ccb36` | 更新 index 包文档 | 7.10 | 1 |
| `fc1c8305` | 添加 7.10 Memory Index 逻辑补齐设计文档 | 7.10 | 1 |
| `4c0865d1` | 7.10 Memory Index 逻辑补齐 — 9 项修复 | 7.10 | 10+ |
| `b63c3e44` | 对齐 Python _CODE_SECTION_GENERATORS 列表结构 | 10.6.1 | 2 |
| `46a224d4` | 修复 SA4006 config.New 返回的 err 未检查 | 10.6 | 1 |
| `218c82a9` | gofmt 格式问题 + 声明顺序/注释/any 遗留修正 | 全局 | 120+ |
| `59cbd188` | 对齐 Python 修复 2026-09-11 审查 10 项问题 | 10.3 | 6 |
| `1ae5e700` | loadYAML ctx 传播 + 删除已完成审查文档 | 10.6 | 10+ |
| `538364c3` | 残留 interface{} 统一替换为 any | 全局 | 20+ |

**重点审查章节**：9.80a（ExperienceSharing 跨 Agent 经验共享）、9.24 P3（SkillEvolutionRail 技能演进护栏）、7.10（Memory Index 逻辑）、Memory any 类型收紧

---

## 二、问题汇总

| 编号 | 严重级别 | 章节 | 文件 | 问题摘要 |
|------|---------|------|------|---------|
| **严重** | | | | |
| S-01 | 严重 | 9.80a | `sharing/types.go:488` | `newBundleID()` 基于时间戳低 38 位，碰撞概率高且可预测，Python 用 UUID 随机 40 位 |
| S-02 | 严重 | 9.24 | `skill_evolution_rail.go:1321-1331` | `filterDuplicateSharedRecords` 未实现 LLM 去重，直接返回全部共享记录 |
| S-03 | 严重 | 9.24 | `skill_evolution_rail.go` | `generate_and_emit_experience` + `_legacy_user_intent` 方法完全缺失 |
| S-04 | 严重 | 9.24 | `skill_evolution_rail.go:1069` | `NewShareStager` 传入 `qcScoreThreshold=0`，Python 默认 0.6，所有记录通过 QC |
| S-05 | 严重 | 9.24 | `skill_evolution_rail.go:1873` | `extractConversationExcerpt` 的 failureRe 正则缺少 `error = None` 排除，且缺少 `assistant_responses` 收集 |
| S-06 | 严重 | 7.10 | `fragment_manager.go:129,161,215` | `FragmentMemoryManager.AddMemories` 返回原始列表而非 Python 中经过冲突检查后的实际处理列表 |
| S-07 | 严重 | 9.24 | `skill_evolution_rail.go:322-326` | `WithDisabledSkillsSet` 空实现 + `normalizeNameSet` 返回空切片，禁用技能永远不会生效 |
| S-08 | 严重 | 7.10 | `coding_memory_tool_ops.go:157` | `CodingMemoryWriteWithContext` 返回类型为 map[string]any，错误路径手写 dict 缺少 mode/type 字段，与 Python WriteResult.to_dict() 不一致 |
| S-09 | 严重 | 7.10 | `manager_impl.go:204-325` | `MemoryIndexManager.Search` 的 opts 参数为 map[string]any，内部大量嵌套类型断言不安全，应改为 typed SearchOpts struct |
| S-10 | 严重 | 10.6.6 | `deep_adapter_rails.go:550-578` | `updateRailsForMode` 仅实现 evolution 分支（1/9），Python 有 plan/agent 模式分支 + 8 个 Rail 动态注册/注销 |
| S-11 | 严重 | 10.6.6 | `deep_adapter_rails.go:412-414` | `buildStreamEventRail` 返回 nil（⤵️ 占位），Python 完整实现，导致中断交互流式输出缺失 |
| S-12 | 严重 | 10.6.6 | `interrupt/confirm_rail.go:175-202` | `isAutoConfirmed` 对字符串值返回 False，Python `bool("random")` → True，语义不一致 |
| **一般** | | | | |
| M-14 | 一般 | 10.6.6 | `deep_adapter_rails.go:431-438` | `buildSecurityRail` 的 recover 匿名函数无效——`NewSafetyPromptRail()` 在函数外调用，panic 不会被捕获 |
| M-15 | 一般 | 10.6.6 | `deep_adapter_rails.go:502-541` | `buildPermissionRail` 缺少 3 条关键日志（called/tools_config keys/Building with），Python 有 4 条 |
| M-16 | 一般 | 10.6.6 | `tool_security_rail.go:952-975` | `PermissionInterruptRail.getUserInput` 覆盖基类方法但完全没有日志，Python 基类有 4 条日志 |
| M-17 | 一般 | 10.6.6 | `deep_adapter_rails.go` | 5 个 Rail builder 返回 nil 未实现（SkillCreate/Memory/ExternalMemory/RuntimePrompt/ResponsePrompt） |
| M-01 | 一般 | 9.80a | `share_stager.go:185` | `wrap` 中 `UploadAt` 用 `time.Now()` 缺少 `.UTC()`，且用 `RFC3339` 而非 `RFC3339Nano`，与 `NewSharingMeta` 不一致 |
| M-02 | 一般 | 9.80a | `sharing/types.go:530-546` | `toStringSlice` 过滤空字符串，Python `from_dict` 保留空字符串 |
| M-03 | 一般 | 9.80a | `experience_sharer.go:284-287` | `FlushPendingUploads` 的 backoff `time.Sleep` 不响应 context 取消 |
| M-04 | 一般 | 9.80a | `sharing/types.go:420-428` | `SkillSearchResult.ToDict`/`QueryKeywords.ToDict` 中 `Keywords` 为 nil 时序列化为 JSON `null`，Python 始终为列表 |
| M-05 | 一般 | 9.24 | `skill_evolution_rail.go:1078-1081` | `buildExperienceSharer` 只从环境变量读取 `hub_path`，缺少从 `sharing_config` 读取 `hub_path`/`local_cache_dir`/`max_upload_retries` |
| M-06 | 一般 | 9.24 | `skill_evolution_rail.go:1077-1094` | `buildExperienceSharer` 缺少异常捕获和失败降级，Python 有 try-except 返回 nil |
| M-07 | 一般 | 9.24 | `skill_evolution_rail.go:1198` | `downloadSharedExperiences`/`stageRecordsForShare`/`flushShareUploads` 缺少错误隔离，Python 每个方法都有 try-except |
| M-08 | 一般 | 9.24 | `skill_evolution_rail.go:370-372` | `ApprovalRuntime()` 直接返回构造时引用，Python 有惰性重绑定逻辑 |
| M-09 | 一般 | 9.24 | `skill_evolution_rail.go:252-258` | `NewSkillEvolutionRail` 未设置 `WithDefaultMemberRole("teammate")`，Python `_DEFAULT_MEMBER_ROLE = "teammate"` |
| M-10 | 一般 | 9.24 | `skill_evolution_rail.go:274-276` | `WithEvalInterval` 不验证 `interval >= 1`，Python `eval_interval < 1` 抛 ValueError |
| M-11 | 一般 | 7.10 | `summary_manager.go:92-108` | `SummaryManager.AddMemories` 空结果时返回 `originalUnits`，Python 返回 `[]` |
| M-12 | 一般 | 9.80a | `sharing/types.go:197-203` | `MakeSharedSkillBundle` summary 拼接用 `+=`，应使用 `strings.Join` |
| M-13 | 一般 | 9.24 | `skill_evolution_rail.go:304-319` | `WithSharingConfig` 环境变量解析不完整，只处理 "true"/"1"，缺少 "yes"/"on" |
| **提示** | | | | |
| T-01 | 提示 | 9.80a | `sharing/types.go:370-377` | `FromDictSharedSkillBundle` 解析失败时 `continue` 丢弃错误，缺日志 |
| T-02 | 提示 | 9.80a | `backend/local_file.go` | `writeGlobalIndex` 忽略写入错误，用 `_ =` 静默丢弃 |
| T-03 | 提示 | 9.80a | `backend/local_file.go` | `readIndex`/`readGlobalIndex` 返回 nil，Python 返回空列表 `[]` |
| T-04 | 提示 | 9.80a | `backend/local_file.go` | `readIndex` 行号从 0 开始，Python 从 1 开始，日志中行号差 1 |
| T-05 | 提示 | 9.80a | `backend/local_file.go` | `SearchSkills` 中 `append(keywords, ...)` 可能修改 `keywords` 底层数组 |
| T-06 | 提示 | 9.24 | `skill_evolution_rail.go:1005-1010` | 缺少 `on_approve`/`on_reject` 兼容别名方法 |
| T-07 | 提示 | 9.24 | `skill_evolution_rail.go:851-865` | `RollbackSkill` 归档版本选择未显式排序，依赖 `os.ReadDir` 隐式顺序 |
| T-08 | 提示 | 9.24 | `skill_evolution_rail.go:1905` | `extractConversationExcerpt` 只查找 `"name"` 键，Python 还回退 `"tool_name"` |
| T-09 | 提示 | 7.10 | `fragment_manager.go:477` | timestamp 用 `UTC()` 而 Python 用 `astimezone()` 本地时区偏移 |
| T-10 | 提示 | 9.80a | `sharing/interface.go` | `SharingBackend` 接口 `DownloadBundles`/`SearchSkills` 不返回 error，依赖 recover 捕获 panic |

---

## 三、严重问题详细分析

### S-01: `newBundleID()` 基于时间戳，碰撞概率高且可预测

**文件**: `internal/evolving/sharing/types.go:488`

**Python 参考代码** (`sharing/types.py:24-25`):
```python
def _new_bundle_id() -> str:
    return f"sb_{uuid.uuid4().hex[:10]}"
```
UUID v4 的 hex 前 10 位 = 40 bit 熵，碰撞概率极低。

**Go 问题代码** (`sharing/types.go:487-489`):
```go
func newBundleID() string {
    return fmt.Sprintf("sb_%010x", time.Now().UnixNano()&0x3FFFFFFFFF)
}
```
使用时间戳低 38 位。同一纳秒内生成两个 bundle_id 完全相同。且可预测。

**修复方案**:
```go
import "crypto/rand"

func newBundleID() string {
    var buf [5]byte // 5 bytes = 40 bit，与 Python 一致
    _, _ = crypto_rand.Read(buf[:])
    return fmt.Sprintf("sb_%010x", buf)
}
```

---

### S-02: `filterDuplicateSharedRecords` 未实现 LLM 去重

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1321-1331`

**Python 参考代码** (`skill_evolution_sharing.py:413-426`):
```python
async def _filter_duplicate_shared_records(self, skill_name, shared_records):
    if not shared_records:
        return []
    existing_desc = await self._evolution_store.get_pending_records(skill_name, EvolutionTarget.DESCRIPTION)
    existing_body = await self._evolution_store.get_pending_records(skill_name, EvolutionTarget.BODY)
    existing_records = existing_desc + existing_body
    if not existing_records:
        return shared_records
    return await self._llm_check_duplicates(skill_name, shared_records, existing_records)
```
Python 先获取本地已有记录，然后通过 LLM 判断共享记录是否与本地经验重复，过滤重复项。

**Go 问题代码** (`skill_evolution_rail.go:1321-1331`):
```go
func (r *SkillEvolutionRail) filterDuplicateSharedRecords(
    ctx context.Context, skillName string, sharedRecords []checkpointing.EvolutionRecord,
) []checkpointing.EvolutionRecord {
    if len(sharedRecords) == 0 {
        return nil
    }
    // 简化实现：直接返回共享记录（LLM 去重为可选增强）
    return sharedRecords
}
```
Go 直接跳过去重，所有共享记录直接写入，可能导致大量重复经验。

同时 Python 中以下三个辅助方法在 Go 中完全缺失：
- `_llm_check_duplicates`（调用 LLM 进行去重判断）
- `_build_duplicate_check_prompt`（构建去重提示词）
- `_parse_duplicate_check_response`（解析 LLM 返回的 JSON 数组）

**修复方案**: 实现完整的去重流程：
1. 调用 `evolutionStore.GetPendingRecords` 获取已有记录
2. 若无已有记录，直接返回共享记录
3. 构建去重提示词，调用 `InvokeTextWithRetry`
4. 解析 JSON 响应，过滤 `decision == "duplicate"` 的记录

---

### S-03: `generate_and_emit_experience` + `_legacy_user_intent` 方法完全缺失

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

**Python 参考代码** (`skill_evolution_rail.py:527-569`):
```python
async def generate_and_emit_experience(self, skill_name, signals, messages, user_query=""):
    user_intent = self._legacy_user_intent(user_query=user_query, signals=signals, messages=messages)
    result = await self.request_user_evolution(skill_name, user_intent, auto_approve=False)
    return result.request_id is not None

@staticmethod
def _legacy_user_intent(*, user_query, signals, messages):
    if user_query:
        return user_query
    for signal in signals:
        if signal.excerpt:
            return signal.excerpt
    for message in reversed(messages):
        content = message.get("content") if isinstance(message, dict) else getattr(message, "content", "")
        if isinstance(content, str) and content:
            return content
    return ""
```

**Go**: 搜索不到 `GenerateAndEmitExperience` 或 `legacyUserIntent`。

**问题**: 这是外部可调用的向后兼容入口。slash 命令处理器或其他模块可能依赖此方法。

**修复方案**:
```go
func (r *SkillEvolutionRail) GenerateAndEmitExperience(ctx context.Context, skillName string, signals []*signal.EvolutionSignal, messages []map[string]any, userQuery string) (bool, error) {
    userIntent := legacyUserIntent(userQuery, signals, messages)
    result, err := r.RequestUserEvolution(ctx, skillName, userIntent, false)
    if err != nil {
        return false, err
    }
    return result.RequestID != nil, nil
}

func legacyUserIntent(userQuery string, signals []*signal.EvolutionSignal, messages []map[string]any) string {
    if userQuery != "" {
        return userQuery
    }
    for _, sig := range signals {
        if sig.Excerpt != "" {
            return sig.Excerpt
        }
    }
    for i := len(messages) - 1; i >= 0; i-- {
        if content, ok := messages[i]["content"].(string); ok && content != "" {
            return content
        }
    }
    return ""
}
```

---

### S-04: `NewShareStager` 传入 `qcScoreThreshold=0`，Python 默认 0.6

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1069`

**Python 参考代码** (`share_stager.py:73`):
```python
class ShareStager:
    def __init__(self, keyword_extractor, sharer, qc_score_threshold=0.6, source_user_id=None):
```
Python 默认 QC 阈值为 0.6，低于此分数的经验会被丢弃。

**Go 问题代码** (`skill_evolution_rail.go:1069`):
```go
r.shareStager = sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0, nil)
```
Go 传入 `qcScoreThreshold=0`，意味着**所有记录（包括分数为 0 的低质量经验）都会通过 QC**。

**修复方案**:
```go
r.shareStager = sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0.6, nil)
```

---

### S-05: `extractConversationExcerpt` 正则缺少 `error = None` 排除且缺少 `assistant_responses` 收集

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1873`

**Python 参考代码** (`skill_evolution_sharing.py:510-520`):
```python
_FAILURE_KEYWORDS = re.compile(
    r"error(?!\s*=\s*None)|exception|traceback|..."
    , re.IGNORECASE)
```
Python 使用负向前瞻 `(?!\s*=\s*None)` 排除 `error = None` 这样的正常状态字符串。

**Go 问题代码** (`skill_evolution_rail.go:1873`):
```go
failureRe := regexp.MustCompile(`(?i)error|exception|traceback|...`)
```
Go 的 `regexp` 不支持负向前瞻，所以 `error = None` 会被误匹配为失败关键词。

此外，Python 的 `_extract_conversation_excerpt` 还收集了 `assistant_responses`（第 531、550 行）：
```python
assistant_responses: list[str] = []
...
content = msg.get("content", "")
if isinstance(content, str) and content.strip():
    assistant_responses.append(content[:max_chars])
```
Go 的 `extractConversationExcerpt` 完全缺少 `assistant_responses` 的收集，虽然 `assistant_responses` 在 Python 中被收集但当前未参与最终拼接（`priority_parts` 未包含），但这是预留字段，未来版本会使用。

**修复方案**:
1. 正则问题：匹配 `error` 后手动排除 `= None`/`= None`：
```go
// 在匹配到 "error" 后检查后续是否为 "= None"
if strings.Contains(strings.ToLower(contentStr), "error") {
    // 检查是否为 "error = None" / "error=None" 等正常状态
    normalized := strings.ToLower(strings.TrimSpace(contentStr))
    if strings.Contains(normalized, "error") && !isErrorEqualsNone(normalized) {
        // 记录为失败
    }
}
```
2. 补充 `assistant_responses` 收集逻辑。

---

### S-06: `FragmentMemoryManager.AddMemories` 返回值与 Python 不一致

**文件**: `internal/agentcore/memory/index/fragment_manager.go:129,161,215`

**Python 参考代码** (`fragment_memory_manager.py:146,160,191`):
```python
# 无新记忆时 / 直接写入 / 冲突检查后 —— 全部返回：
return list(process_result_dict.values())
```
Python 返回经过冲突检查后实际处理（ADD/UPDATE/DELETE）的记忆单元列表。

**Go 问题代码** (`fragment_manager.go:129,161,215`):
```go
return originalFragmentUnits, nil
```
Go 返回的是传入的原始碎片记忆列表，不管是否被冲突检查过滤。这意味着调用方无法知道哪些记忆实际被处理了。

**修复方案**: 将 `originalFragmentUnits` 替换为 `fragmentUnitsToMemoryUnits(mapValues(processResult))`，返回实际处理后的记忆列表。

---

### S-07: `WithDisabledSkillsSet` 空实现 + `normalizeNameSet` 返回空切片

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:322-326, 1722-1725`

**Python 参考代码** (`skill_evolution_rail.py:129-130`):
```python
super().__init__(
    trajectory_store=trajectory_store,
    team_trajectory_store=team_trajectory_store,
    disabled_skills=disabled_skills,
)
```
Python 将 `disabled_skills` 传入基类构造，在信号过滤时排除禁用技能。

**Go 问题代码**:
```go
// WithDisabledSkillsSet 选项函数是空实现
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
    return func(r *SkillEvolutionRail) {
        // 委托给基类的 WithDisabledSkills，通过 normalizeNameSet 在构造时应用
    }
}

// normalizeNameSet 总是返回空切片
func (r *SkillEvolutionRail) normalizeNameSet() []string {
    return []string{}
}
```

**问题**: `WithDisabledSkillsSet` 传入的 `names` 被完全忽略。`normalizeNameSet` 在 `NewEvolutionRail(WithDisabledSkills(r.normalizeNameSet()))` 中被调用，但总是返回空切片。结果：禁用技能列表永远不会生效，即使配置了禁用技能，演进仍会在这些技能上触发。

**修复方案**:
1. 在 `SkillEvolutionRail` 结构体中添加 `disabledSkillNames []string` 字段
2. `WithDisabledSkillsSet` 保存 names 到字段
3. `normalizeNameSet` 返回 `r.disabledSkillNames`

---

## 四、一般问题详细分析

### M-01: `wrap` 中 `UploadAt` 缺少 `.UTC()` 且用 `RFC3339` 而非 `RFC3339Nano`

**文件**: `internal/evolving/sharing/share_stager.go:185`

**Python**: `upload_at` 使用 `_now_iso()` = `datetime.now(tz=timezone.utc).isoformat()`，输出 UTC 带纳秒。

**Go**:
```go
UploadAt: time.Now().Format(time.RFC3339),  // 缺少 .UTC()，无纳秒
```
而 `NewSharingMeta` 用 `time.Now().UTC().Format(time.RFC3339Nano)`。

**修复方案**:
```go
UploadAt: time.Now().UTC().Format(time.RFC3339Nano),
```

---

### M-02: `toStringSlice` 过滤空字符串，Python 保留

**文件**: `internal/evolving/sharing/types.go:530-546`

**Python**: `from_dict` 中 `keywords=list(data.get("keywords", []) or [])` 保留空字符串元素。

**Go**: `toStringSlice` 过滤掉空字符串 `if s != ""`。

**修复方案**: 移除空字符串过滤，或在代码中添加注释说明这是有意的行为差异（空关键词无意义）。

---

### M-03: `FlushPendingUploads` 的 backoff 不响应 context 取消

**文件**: `internal/evolving/sharing/experience_sharer.go:284-287`

**Python**:
```python
await asyncio.sleep(self._backoff_base_secs * (2 ** (attempt - 1)))
```

**Go**:
```go
time.Sleep(time.Duration(delay * float64(time.Second)))
```

**修复方案**:
```go
select {
case <-time.After(time.Duration(delay * float64(time.Second))):
case <-ctx.Done():
    return UploadResult{OK: false, Reason: ctx.Err().Error()}
}
```

---

### M-04: `SkillSearchResult.ToDict` / `QueryKeywords.ToDict` 中 Keywords 为 nil 时序列化为 JSON null

**文件**: `internal/evolving/sharing/types.go:420-428, 451-457`

**Python**: 始终序列化为列表 `[]`。

**Go**: `Keywords` 为 nil 时 JSON 输出 `"keywords": null`。

**修复方案**:
```go
keywords := r.Keywords
if keywords == nil {
    keywords = []string{}
}
```

---

### M-05: `buildExperienceSharer` 只从环境变量读取 hub_path，缺少从 config 读取

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1078-1081`

**Python** (`skill_evolution_sharing.py:126`):
```python
hub_path = os.getenv("EVOLUTION_SHARING_HUB_PATH") or str(config.get("hub_path") or "").strip() or None
local_cache_dir = str(config.get("local_cache_dir") or "").strip() or None
raw_retries = config.get("max_upload_retries", _DEFAULT_SHARING_MAX_UPLOAD_RETRIES)
```

**Go**: 只从环境变量读取 `hub_path`，缺少 `local_cache_dir`、`max_upload_retries` 从 config 读取。

**修复方案**: `WithSharingConfig` 中保存 config 到结构体字段，`buildExperienceSharer` 从中读取所有参数。

---

### M-06: `buildExperienceSharer` 缺少异常捕获和失败降级

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1077-1094`

**Python** (`skill_evolution_sharing.py:134-146`):
```python
try:
    backend = LocalFileBackend(hub_path=hub_path)
    return ExperienceSharer(backend=backend, ...)
except Exception as exc:
    logger.warning("failed to build ExperienceSharer (%s); sharing disabled", exc)
    return None
```

**Go**: 直接创建，无错误处理。如果 `NewLocalFileBackend` 失败不会降级为 sharing disabled。

**修复方案**: 添加 recover 或让 `NewLocalFileBackend` 返回 error，失败时返回 nil 并记录警告。

---

### M-07: `downloadSharedExperiences`/`stageRecordsForShare`/`flushShareUploads` 缺少错误隔离

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

**Python** 中每个方法都有 `try-except` 包裹，单个技能下载失败不影响其他技能。

**Go**: 无 recover 或 error 处理，panic 会中断整个流程。

**修复方案**: 在关键调用点添加 recover 或让方法返回 error 并逐个处理。

---

### M-08: `ApprovalRuntime()` 直接返回构造时引用

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:370-372`

**Python** (`skill_evolution_rail.py:266-279`):
```python
@property
def approval_runtime(self):
    runtime = getattr(self, "_approval_runtime", None)
    if runtime is None or runtime._manager is not self._manager or ...:
        runtime = EvolutionApprovalRuntime(manager=self._manager, ...)
        self._approval_runtime = runtime
    return runtime
```

**Go**: 直接返回 `r.approvalRuntime`，如果 `manager` 或 `pendingApprovalSnapshots` 被替换，引用过期。

**修复方案**: 添加引用一致性检查，不匹配时重建。

---

### M-09: 未设置 `WithDefaultMemberRole("teammate")`

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:252-258`

**Python** (`skill_evolution_rail.py:85`):
```python
_DEFAULT_MEMBER_ROLE = "teammate"
```

**Go**: 构造 `EvolutionRail` 基类时未设置 `WithDefaultMemberRole("teammate")`。

**修复方案**:
```go
railOpts := []EvolutionRailOption{
    WithDisabledSkills(r.normalizeNameSet()),
    WithDefaultMemberRole("teammate"),
}
```

---

### M-10: `WithEvalInterval` 不验证 `interval >= 1`

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:274-276`

**Python** (`skill_evolution_rail.py:123-124`):
```python
if eval_interval < 1:
    raise ValueError("eval_interval must be >= 1")
```

**Go**: 无验证，可以传入 0 或负值。

**修复方案**:
```go
func WithEvalInterval(interval int) SkillEvolutionRailOption {
    return func(r *SkillEvolutionRail) {
        if interval < 1 {
            interval = 1
        }
        r.evalInterval = interval
    }
}
```

---

### M-11: `SummaryManager.AddMemories` 空结果时返回 `originalUnits`

**文件**: `internal/agentcore/memory/index/summary_manager.go:92-108`

**Python** (`summary_manager.py:79-81`):
```python
if not memory_docs:
    return []
# ...
return memories[self.mem_type]
```

**Go**: `summaryUnits` 为空时返回 `originalUnits`（可能不为空），Python 返回 `[]`。

**修复方案**: 当 `summaryUnits` 为空时返回空切片 `[]MemoryUnit{}`。

---

### M-12: `MakeSharedSkillBundle` summary 拼接用 `+=`

**文件**: `internal/evolving/sharing/types.go:197-203`

**Go**:
```go
for i, p := range parts {
    if i > 0 {
        summaryAggregate += "; "
    }
    summaryAggregate += p
}
```

**修复方案**:
```go
summaryAggregate = strings.Join(parts, "; ")
```

---

### M-13: `WithSharingConfig` 环境变量解析不完整

**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:308-310`

**Python** (`skill_evolution_sharing.py:85-92`): `_resolve_bool` 支持 "yes"/"on"。

**Go**: `envEnabled == "true" || envEnabled == "1"`，缺少 "yes"/"on"。

**修复方案**: 使用已有的 `resolveSharingBool` 处理环境变量值。

---

## 五、提示问题简述

### T-01: `FromDictSharedSkillBundle` 解析失败缺日志
静默 `continue`，应添加 Warn 日志。

### T-02: `writeGlobalIndex` 忽略写入错误
`_ = os.WriteFile(...)` 应添加错误日志。

### T-03: `readIndex`/`readGlobalIndex` 返回 nil vs Python 空列表
改为返回 `[]map[string]any{}` 保持一致。

### T-04: `readIndex` 行号从 0 开始
日志中使用 `lineNo+1` 对齐 Python。

### T-05: `SearchSkills` 中 `append` 修改底层数组
`searchTerms := append(keywords, ...)` 可能修改 `keywords`，应显式创建新切片。

### T-06: 缺少 `on_approve`/`on_reject` 兼容别名
Python 有 `on_approve = approve_record` 别名，Go 缺失。如果无外部调用可忽略。

### T-07: `RollbackSkill` 归档版本未显式排序
Python 使用 `sorted(reverse=True)[0]`，Go 遍历覆盖。应显式 `sort.Slice`。

### T-08: `extractConversationExcerpt` 缺少 `tool_name` 回退
Python: `msg.get("name") or msg.get("tool_name")`，Go 只查 `"name"`。

### T-09: timestamp UTC vs astimezone 差异
Go 用 `UTC()`，Python 用 `astimezone()`。差异很小，UTC 更规范。

### T-10: `SharingBackend` 接口不返回 error
`DownloadBundles`/`SearchSkills` 无 error 返回，依赖 recover。建议后续重构为 error 返回模式。

---

## 七、10.6.6 Interrupt Rail 对齐审查

### S-10: `updateRailsForMode` 仅实现 1/9 逻辑

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go:550-578`

**Python 参考代码** (`interface_deep.py` — `_update_plan_mode_rails` + `_update_agent_mode_rails`):

Python 的模式切换逻辑包含 9 个 Rail 的动态注册/注销：

```
_update_plan_mode_rails:
  1. TaskPlanningRail 注册
  2. 卸载 multi-session 工具
  3. MemoryRail 按 config 注册/卸载
  4. ExternalMemoryRail 注册
  5. ContextAssembleRail 按 plan 模式注册
  6. ContextProcessorRail 按 context_engine_config 注册/卸载
  7. SkillEvolutionRail 按 evolution.enabled 注册/卸载
  8. SkillCreateRail 按 skill_create 配置注册
  9. SubagentRail 注册

_update_agent_mode_rails:
  1. 卸载 plan 专属 rails
  2. MemoryRail 按 config 注册/卸载
  3. ExternalMemoryRail 注册
  4. ContextAssembleRail 按非 plan 模式注册
  5. ContextProcessorRail 注册
```

**Go 问题代码** (`deep_adapter_rails.go:550-578`):
```go
func (d *DeepAdapter) updateRailsForMode(mode string) {
    // 仅处理 evolution rail 注册/注销
}
```

**修复方案**: 补充 `_update_plan_mode_rails` 和 `_update_agent_mode_rails` 的完整逻辑，逐一对齐 Python 的 9 个 Rail 注册/注销。

---

### S-11: `buildStreamEventRail` 返回 nil（占位未实现）

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go:412-414`

**Python 参考代码** (`interface_deep.py:2001-2009`):
```python
def _build_stream_event_rail():
    try:
        stream_event_rail = JiuClawStreamEventRail()
        logger.info("[JiuWenClawDeepAdapter] JiuClawStreamEventRail create success")
    except Exception as exc:
        logger.warning("[JiuWenClawDeepAdapter] JiuClawStreamEventRail create failed: %s", exc)
        stream_event_rail = None
    return stream_event_rail
```

**Go 问题代码**:
```go
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 JiuClawStreamEventRail
    return nil
}
```

StreamEventRail 是流式输出、暂停/恢复控制、interrupt 事件捕获的核心组件。

**修复方案**: 实现 `JiuClawStreamEventRail`，包含 try-except 保护和日志。

---

### S-12: `isAutoConfirmed` 字符串语义与 Python 不一致

**文件**: `internal/agentcore/harness/rails/interrupt/confirm_rail.go:175-202`

**Python 参考代码** (`confirm_rail.py:94-98`):
```python
@staticmethod
def _is_auto_confirmed(config, key):
    if config is None:
        return False
    return config.get(key, False)
```
Python 使用 `bool()` 语义：`bool("random")` → `True`。

**Go 问题代码** (`confirm_rail.go:175-202`):
```go
func isAutoConfirmed(config map[string]any, key string) bool {
    // ...
    if s, ok := val.(string); ok {
        s = strings.TrimSpace(strings.ToLower(s))
        return s != "" && s != "false" && s != "no" && s != "0"
    }
    return false
}
```
Go 对非 "true"/"1" 字符串返回 `False`，例如 `isAutoConfirmed({"k": "random"}, "k")` → Go `false`，Python `True`。

**修复方案**: `isAutoConfirmed` 应对齐 Python 的简单语义：非空字符串视为 True（除非是 "false"/"0"/"no"）。或者更简单地改为 `resolveSharingBool(val)` 统一处理。

---

### M-14: `buildSecurityRail` 的 recover 保护无效

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go:431-438`

**Go 问题代码**:
```go
func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("panic", r).Msg("SafetyPromptRail 创建失败，跳过")
        }
    }()
}()  // 匿名函数立即执行，defer 也立即执行，但什么都没保护
rail := secrail.NewSafetyPromptRail()  // ← 在匿名函数外，panic 不会被 recover
```

**修复方案**: 将 `rail := secrail.NewSafetyPromptRail()` 移到匿名函数内部。

---

### M-15: `buildPermissionRail` 缺少关键日志

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go:502-541`

**Python** 有 4 条日志（`build_permission_rail called` / `tools_config keys` / `Building with tool_names/llm/model_name` / `created successfully`），Go 只有创建成功/失败 2 条。

**修复方案**: 补充缺失的 3 条日志。

---

### M-16: `PermissionInterruptRail.getUserInput` 完全没有日志

**文件**: `internal/agentcore/harness/security/tool_security_rail.go:952-975`

**Python** 基类 `_get_user_input` 有 4 条日志（raw_input_type / user_inputs keys / MATCHED / NO MATCH）。Go 的 `PermissionInterruptRail` 覆盖基类方法但没有任何日志输出。

**修复方案**: 对齐 Python 的 4 条日志。

---

### M-17: 5 个 Rail builder 返回 nil 未实现

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go`

| Builder | 行号 | Python 等价 |
|---------|------|------------|
| `buildSkillCreateRail` | 404-407 | `_build_skill_create_rail` |
| `buildMemoryRail` | 447-450 | `_build_memory_rail` |
| `buildExternalMemoryRail` | 454-457 | `_build_external_memory_rail` |
| `buildRuntimePromptRail` | 471-474 | `_build_runtime_prompt_rail` |
| `buildResponsePromptRail` | 479-482 | `_build_response_prompt_rail` |

其中 MemoryRail 和 ExternalMemoryRail 是 S-10（updateRailsForMode）的前置条件；RuntimePromptRail 和 ResponsePromptRail 在 Python 的 rail build list 中排在前两位，影响提示词注入。

**修复方案**: 逐个实现，优先级：MemoryRail > RuntimePromptRail > ResponsePromptRail > SkillCreateRail > ExternalMemoryRail。

---

## 八、修复优先级建议

### 必须修复（影响功能正确性）
| 编号 | 修复工作量 | 说明 |
|------|-----------|------|
| S-01 | 小 | `newBundleID()` 改用 `crypto/rand` |
| S-04 | 极小 | `NewShareStager` 阈值改为 0.6 |
| S-07 | 中 | 实现禁用技能传递链 |
| S-08 | 中 | `CodingMemoryWriteWithContext` 返回值改为 `*WriteResult` |
| S-12 | 小 | `isAutoConfirmed` 对齐 Python bool 语义 |
| M-01 | 极小 | `wrap` 中 `UploadAt` 加 `.UTC()` 和 `RFC3339Nano` |
| M-04 | 极小 | `ToDict` 中 nil→空切片 |
| M-14 | 极小 | `buildSecurityRail` recover 修复（移入匿名函数） |

### 建议修复（影响数据质量/健壮性）
| 编号 | 修复工作量 | 说明 |
|------|-----------|------|
| S-02 | 大 | 实现 LLM 去重（3 个方法 + 提示词模板） |
| S-03 | 中 | 添加 `GenerateAndEmitExperience` + `legacyUserIntent` |
| S-05 | 中 | 正则排除逻辑 + `assistant_responses` |
| S-06 | 中 | `AddMemories` 返回值修复 |
| S-10 | 大 | `updateRailsForMode` 补充完整逻辑（9 个 Rail） |
| S-11 | 大 | 实现 `JiuClawStreamEventRail` |
| M-03 | 小 | backoff 改用 select + ctx |
| M-05 | 中 | config 参数传递 |
| M-06 | 小 | buildExperienceSharer 错误处理 |
| M-07 | 中 | 3 个方法的错误隔离 |
| M-09 | 极小 | 添加 `WithDefaultMemberRole("teammate")` |
| M-10 | 极小 | `WithEvalInterval` 验证 |
| M-11 | 小 | `SummaryManager.AddMemories` 空结果 |
| M-15 | 小 | `buildPermissionRail` 补充日志 |
| M-16 | 小 | `PermissionInterruptRail.getUserInput` 补充日志 |

### 可延后修复（低优先级/提示级别）
| 编号 | 说明 |
|------|------|
| S-09 | `Search opts` 改为 typed struct（影响面广，需专门重构） |
| M-02 | toStringSlice 过滤空字符串差异 |
| M-08 | approval_runtime 惰性重绑定 |
| M-12 | strings.Join 简化 |
| M-13 | 环境变量解析完整性 |
| M-17 | 5 个 Rail builder 返回 nil（依赖 10.6.3-10 回填计划） |
| T-01~T-10 | 日志、兼容性、代码风格等 |

---

## 九、统计

| 严重级别 | 数量 | 分布 |
|---------|------|------|
| 严重 (S) | 12 | 9.80a:1, 9.24:5, 7.10:3, 10.6.6:3 |
| 一般 (M) | 17 | 9.80a:8, 9.24:5, 7.10:1, 10.6.6:4 |
| 提示 (T) | 10 | 9.80a:6, 9.24:2, 7.10:1, 10.6.6:1 |
| **合计** | **39** | |

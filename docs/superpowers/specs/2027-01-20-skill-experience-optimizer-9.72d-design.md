# 9.72d SkillExperienceOptimizer 设计文档

## 概述

SkillExperienceOptimizer 和 TeamSkillExperienceOptimizer 是 `domain="skill_experience"` 的两个优化器，负责将来自 SkillEvolutionRail 的信号转化为可沉淀的 EvolutionRecord。

SkillExperienceOptimizer（个体优化器）通过 LLM 三通道分析（预检测信号 / 对话历史分析 / 脚本工件提取）生成经验草稿，并具备完整的 JSON 解析重试机制。

TeamSkillExperienceOptimizer（团队优化器）在此基础上增加了双路径生成逻辑（聚合 LLM 流 + 逐信号 patch 流）、用户改进意图 patch、轨迹问题 patch 和 body 重写功能。

## 流程位置

在自演化系统中，位于以下链路：

```
SignalDetector (9.73 ✅)
  → 识别对话中的演化信号 (execution_failure / user_correction / script_artifact 等)
  → OnlineEvolutionOrchestrator (9.79 ✅)
    → 将信号路由到对应 domain 的 optimizer
    → SkillExperienceOptimizer (9.72d ☐)
      → 根据 signal 和 conversation context，调用 LLM 生成经验草稿
      → 解析草稿为 EvolutionRecord
      → 通过 Step() 返回更新映射
    → ExperienceManager (9.79 ✅)
      → stage/approve/reject 经验记录
    → SkillExperienceOperator (9.70a ✅)
      → ApplyUpdate() 将经验应用到技能文件
```

核心作用：SkillExperienceOptimizer 是 `domain="skill_experience"` 的优化器，负责将信号转化为 EvolutionRecord。它通过 LLM 三通道分析生成经验草稿，并具备完整的 JSON 解析重试机制。

## 包结构

**目标包**：`internal/evolving/optimizer/skill_call/`

| 文件 | 职责 | 对应 Python | 预估行数 |
|------|------|-------------|---------|
| `doc.go` | 包文档 + 文件目录树 | `__init__.py` | ~30 |
| `base.go` | 共享常量 + `SkillExperienceOptimizerBase` 基类（嵌入 Mixin + Domain/DefaultTargets/RequiresForwardData/Bind） | `experience_optimizer.py` 部分字段 | ~80 |
| `draft_parser.go` | `ParsedExperienceDraft` 结构体 + `NormalizeKeywords`/`NormalizeSummary`/`ParseExperienceDraft`/`ParseExperienceDraftsWithError` + `ExtractJSONWithError`/`LooksTruncated`/`FixJSONText` | `experience_draft_parser.py` + `experience_optimizer.py` 中的 JSON 辅助函数 | ~200 |
| `experience_optimizer.go` | `SkillExperienceOptimizer` — 个体技能经验优化器（bind/_backward/_step/generate_records/retry_parse/retry_parse_drafts/_generate_drafts_with_retries + 辅助函数） | `experience_optimizer.py` 主类 | ~450 |
| `team_optimizer.go` | `TeamSkillExperienceOptimizer` — 团队技能经验优化器（generate_records 双路径 + generate_user_patch + generate_trajectory_patch + regenerate_body + _call_llm + 内容摘要辅助） | `team_skill_experience_optimizer.py` 主类 | ~550 |
| `templates.go` | 提示词模板（CN+EN 双语，个体/团队两类，含 JSON_FIX/USER_PATCH/TRAJECTORY_PATCH/TEAM_EXPERIENCE 等） — 必须一比一复刻 Python 原文 | `templates.py` 全量 | ~800 |
| `base_test.go` | SkillExperienceOptimizerBase 单元测试 | — | ~100 |
| `draft_parser_test.go` | draft_parser 单元测试 | — | ~120 |
| `experience_optimizer_test.go` | SkillExperienceOptimizer 单元测试 | — | ~200 |
| `team_optimizer_test.go` | TeamSkillExperienceOptimizer 单元测试 | — | ~250 |

**总计**：约 10 个文件，约 2780 行。

关键说明：
- `base.go` 只放两个优化器共享的字段和方法（Domain/DefaultTargets/RequiresForwardData/Bind + online_contexts 管理 + INITIAL_SCORE_BY_SIGNAL 常量）
- `draft_parser.go` 合并了 Python 中分散在两个文件里的 JSON 提取/解析辅助函数（统一为一个共享实现，不复用 signal 包的同名函数）
- `templates.go` 必须一比一复刻 Python 原文，不做自行翻译
- 两个优化器都 `domain="skill_experience"`，共享 `DefaultTargets=["experiences"]`

## 核心类型设计

### ParsedExperienceDraft

```go
type ParsedExperienceDraft struct {
    Patch    checkpointing.EvolutionPatch
    Summary  *string
    Keywords []string
}
```

### SkillExperienceOptimizerBase — 两个优化器共享的基类

```go
type SkillExperienceOptimizerBase struct {
    optimizer.BaseOptimizerMixin
    llm            *llm.Model
    model          string
    language       string
    onlineContexts map[string]*experience.EvolutionContext
}
```

共享方法：
- `Domain() → "skill_experience"`
- `DefaultTargets() → ["experiences"]`（即 `schema.ExperiencesTarget`）
- `RequiresForwardData() → true`
- `Bind(operators, targets, config) → int` — 从 config 提取 `online_contexts`，委托 Mixin.Bind
- `UpdateLLM(llm, model)` — 更新 LLM 客户端

### SkillExperienceOptimizer — 个体优化器

```go
type SkillExperienceOptimizer struct {
    SkillExperienceOptimizerBase
    generateRecordsLLMPolicy llm_resilience.LLMInvokePolicy
}
```

### TeamSkillExperienceOptimizer — 团队优化器

```go
type TeamSkillExperienceOptimizer struct {
    SkillExperienceOptimizerBase
    debugDir         string
    recordLLMPolicy  llm_resilience.LLMInvokePolicy
    evolutionStore   *checkpointing.EvolutionStore
}

// TeamSkillOptimizer 团队技能经验优化器别名，对齐 Python
type TeamSkillOptimizer = TeamSkillExperienceOptimizer
```

### 常量定义（base.go）

```go
const (
    SkillContentMaxChars     = 6000
    SectionPreviewChars      = 200
    ContextMaxChars          = 500
    RetryParseTimeoutSecs    = 20
)

var InitialScoreBySignal = map[string]float64{
    "execution_failure":   0.65,
    "user_correction":     0.70,
    "script_artifact":     0.60,
    "conversation_review": 0.50,
}

var GenerateRecordsLLMPolicy = llm_resilience.LLMInvokePolicy{
    AttemptTimeoutSecs: 150,
    TotalBudgetSecs:    300,
    MaxAttempts:        2,
}

var TeamSkillRecordLLMPolicy = llm_resilience.LLMInvokePolicy{
    AttemptTimeoutSecs: 120,
    TotalBudgetSecs:    420,
    MaxAttempts:        3,
}

var TeamInitialScoreBySignal = map[string]float64{
    "trajectory_issue": 0.65,
    "user_intent":      0.70,
    "team_skill_mixed": 0.68,
}
```

## TextualParameter.Gradients 类型变更（方案 B）

Python 的 `TextualParameter.gradients` 是 `Dict[str, Any]`，Go 当前是 `map[string]string`。SkillExperienceOptimizer 需要存 `[]EvolutionRecord` 列表，string 类型无法满足。

**变更方案**：将 `TextualParameter.Gradients` 从 `map[string]string` 改为 `map[string]any`，完全对齐 Python。

### 影响范围

| 文件 | 修改内容 |
|------|---------|
| `internal/evolving/optimizer/base.go` | `Gradients` 从 `map[string]string` → `map[string]any`；`SetGradient(name, string)` → `SetGradient(name, any)`；`GetGradient(name) string` → `GetGradient(name) any`；空字符串""表示"未设置"改为 nil 表示"未设置"（对齐 Python None） |
| `internal/evolving/optimizer/base_test.go` | `GetGradient` 返回值从 string → any，比较方式调整；`GetGradient("nonexistent") != ""` → `GetGradient("nonexistent") != nil` |
| `internal/evolving/optimizer/llm_call/instruction_optimizer.go` | `GetGradient` 返回值需要类型断言 `.（string）`；空字符串清除改为 nil 清除 |

**不需要修改的文件**：
- `tool_call/` — 不使用 GetGradient/SetGradient
- `memory_call/` — 不使用 GetGradient/SetGradient

### SkillExperienceOptimizer 使用方式

```go
// Backward 中写入:
records := o.generateRecords(ctx, evoCtx)
existingAny := param.GetGradient(schema.ExperiencesTarget)
var existing []checkpointing.EvolutionRecord
if existingAny != nil {
    existing, _ = existingAny.([]checkpointing.EvolutionRecord)
}
param.SetGradient(schema.ExperiencesTarget, append(existing, records))

// Step 中读取:
for opID, param := range o.Parameters() {
    recordsAny := param.GetGradient(schema.ExperiencesTarget)
    if recordsAny != nil {
        records, ok := recordsAny.([]checkpointing.EvolutionRecord)
        if ok && len(records) > 0 {
            updates[schema.UpdateKey{opID, schema.ExperiencesTarget}] = records
        }
    }
}
```

### InstructionOptimizer 使用方式（改动后）

```go
// 之前:
param.SetGradient("system_prompt_optimized", "")
if sysVal := param.GetGradient("system_prompt_optimized"); sysVal != "" { ... }
gradient := param.GetGradient("system_prompt")

// 之后:
param.SetGradient("system_prompt_optimized", nil)  // nil 替代空字符串表示清除
sysValAny := param.GetGradient("system_prompt_optimized")
sysVal, _ := sysValAny.(string)                    // 类型断言
if sysVal != "" { ... }
gradientAny := param.GetGradient("system_prompt")
gradient, _ := gradientAny.(string)                // 类型断言
```

## 核心流程设计

### SkillExperienceOptimizer — Backward → GenerateRecords 全流程

```
Backward(ctx, signals)
  ├─ 遍历 _operators
  │   ├─ 提取 skill_name = opID.removeprefix("skill_experience_")
  │   ├─ 过滤 skill_signals 匹配 skill_name
  │   ├─ _buildEvolutionContext(skill_name, op, skill_signals)
  │   │     ├─ 查找 onlineContexts[skill_name] → 存在直接返回
  │   │     └─ 不存在 → 返回错误 (TOOLCHAIN_AGENT_PARAM_ERROR)，continue 跳过
  │   ├─ GenerateRecords(ctx, evoCtx)
  │   │     ├─ 构建 conversation_snippet / signals_json / desc_summary / body_summary / skill_content
  │   │     ├─ 构建 primary_prompt 和 retry_prompt
  │   │     ├─ generateDraftsWithRetries(ctx, prompt, retryPrompt)
  │   │     │     ├─ invokeTextWithRetryAndPrompt → raw, promptUsed
  │   │     │     ├─ ParseExperienceDraftsWithError → 成功返回 drafts
  │   │     │     └─ 失败 → RetryParseDrafts 循环 (attempt 2-3)
  │   │     │          ├─ truncated → 重新生成 (original_prompt)
  │   │     │          ├─ 格式错误 attempt<3 → JSON_FIX_PROMPT 修复
  │   │     │          ├─ 格式错误 attempt≥3 → JSON_FIX_PROMPT_STRICT
  │   │     │          └─ 最终失败 → ValueError
  │   │     ├─ 遍历 drafts，限制数量 (text≤2, script≤1)
  │   │     └─ return text_records + script_records
  │   ├─ 写入 gradient: param.SetGradient(ExperiencesTarget, existing + records)
  │   └─ 记录日志
  └─ return nil (Backward 永远不向上抛错)

Step()
  ├─ 遍历 _parameters
  │   ├─ recordsAny = param.GetGradient(ExperiencesTarget)
  │   ├─ 类型断言为 []EvolutionRecord
  │   └─ updates[(opID, ExperiencesTarget)] = records
  └─ return updates
```

### TeamSkillExperienceOptimizer — GenerateRecords 双路径

```
GenerateRecords(ctx, evoCtx)
  ├─ 检查 trajectory.steps 是否有 kind 属性
  │
  ├─ 【路径A: 逐信号 patch】(steps 缺少 kind)
  │   ├─ 遍历 signals
  │   │   ├─ user_intent → GenerateUserPatch
  │   │   │     ├─ 构建 USER_PATCH_PROMPT
  │   │   │     ├─ callLLM → parsePatchResponse
  │   │   │     └─ need_patch==false → return nil
  │   │   ├─ 其他 → GenerateTrajectoryPatch
  │   │   │     ├─ 构建 TRAJECTORY_PATCH_PROMPT
  │   │   │     ├─ callLLM → parsePatchResponse
  │   │   │     └─ need_patch==false → return nil
  │   └─ return generated 列表
  │
  ├─ 【路径B: 聚合 LLM 流】(steps 有 kind — 正常情况)
  │   ├─ 构建 trajectory_summary / signals_json / skill_content / 已有经验摘要
  │   ├─ generateDraftsWithRetries (使用 TEAM_EXPERIENCE_GENERATE_PROMPT)
  │   ├─ 遍历 drafts，限制数量 (text≤2, script≤1)
  │   └─ return text_records + script_records
```

### _buildEvolutionContext 差异

| 优化器 | Python 行为 | Go 对齐 |
|--------|-------------|---------|
| SkillExperienceOptimizer | online_ctx 存在 → 直接返回；不存在 → raise build_error | 同上 |
| TeamSkillExperienceOptimizer | online_ctx 存在 → 检查 trajectory 是否为 nil，nil 时填充 default_trajectory 返回副本；不存在 → raise build_error | 同上 |

### draft_parser 统一 JSON 提取

```go
func ExtractJSONWithError(raw string) (any, string)       // 健壮 JSON 提取
func LooksTruncated(text string) bool                      // 截断检测
func FixJSONText(text string) string                       // 格式修复
func ParseExperienceDraft(data map[string]any) *ParsedExperienceDraft  // 单条解析
func ParseExperienceDraftsWithError(raw string, extractFn func(string) (any, string)) ([]ParsedExperienceDraft, string) // 批量解析
```

注意：draft_parser 的 ExtractJSONWithError/FixJSONText 是优化器专用实现，不复用 signal 包的同名函数（逻辑不完全相同）。

### callLLM 双路径

```go
func (o *TeamSkillExperienceOptimizer) callLLM(
    ctx context.Context,
    prompt string,
    retryPrompt string,                           // 可选（指针为 nil）
    policy *llm_resilience.LLMInvokePolicy,       // 可选
    isResultUsable func(string) bool,              // 可选
) (string, error)
```

无 policy → 直接调用 llm.Invoke；有 policy → InvokeTextWithRetry / InvokeTextWithRetryAndPrompt。

## 错误处理

Backward 的容错策略（对齐 Python）：
- `_buildEvolutionContext` 失败 → Error 日志 + continue（不中断循环）
- `GenerateRecords` 失败 → Warn 日志 + continue（不中断循环）
- Backward 本身永远返回 nil，所有错误消化在内部日志中

日志组件：所有 skill_call optimizer 使用 `logger.ComponentAgentCore`。

日志字段对齐规则（逐条对照 Python）：
- 个体优化器日志前缀 `[SkillExperienceOptimizer]`
- 团队优化器日志前缀 `[TeamSkillOptimizer]`
- Python f-string 变量在 Go 中以结构化字段体现（如 `.Str("skill_name", skillName).Int("record_count", n)`）

## 回填设计

| 回填方向 | 内容 | 目标 |
|----------|------|------|
| ⤴️ 9.72d → 9.79 | SkillExperienceOptimizer 和 TeamSkillExperienceOptimizer 作为 OnlineEvolutionOrchestrator 的路由目标 | 9.79 experience/orchestrator.go |
| ⤴️ 9.72d → optimizer/doc.go | 添加 skill_call 子包的导出和文件目录更新 | 9.72e optimizer/doc.go |
| ⤴️ base.go Gradients 类型变更 | TextualParameter.Gradients 从 string → any | 影响 optimizer/base.go + llm_call/instruction_optimizer.go + base_test.go |

不需要回填：9.70（Trainer 纯接口桩骨架）、9.72a/b/c（各优化器独立）

## 测试设计

### 文件与覆盖率

| 测试文件 | 覆盖目标 | 预估测试数 |
|----------|---------|-----------|
| `base_test.go` | SkillExperienceOptimizerBase + 常量 | ~8 |
| `draft_parser_test.go` | 解析辅助全量 | ~15 |
| `experience_optimizer_test.go` | SkillExperienceOptimizer 全流程 | ~12 |
| `team_optimizer_test.go` | TeamSkillExperienceOptimizer 全流程 | ~12 |

整体覆盖率预估 ~88%，达标（≥85%）。

### LLM Mock 模式

复用 `llm_resilience_test.go` 的 `newMockModel` 模式：通过 `model_clients.GetClientRegistry().Register` 注册 `mockBaseModelClient`，创建真实 `llm.Model` 实例。

### 关键测试用例

**draft_parser_test.go**：NormalizeKeywords/NormalizeSummary/ParseExperienceDraft/ParseExperienceDraftsWithError/ExtractJSONWithError/LooksTruncated/FixJSONText 各场景。

**experience_optimizer_test.go**：Backward 无信号/有信号/上下文缺失、Step 读取 any 类型 gradient、GenerateRecords LLM 失败/skip/数量限制、RetryParse 截断/格式错误、辅助函数（buildConversationSnippet/summarizeSkillContent/buildExistingSummary）。

**team_optimizer_test.go**：Backward 使用 Trajectory、GenerateRecords 双路径、GenerateUserPatch 需要/不需要、GenerateTrajectoryPatch、RegenerateBody、callLLM 无/有 Policy。

### templates.go 不单独写测试

纯常量文件（提示词字符串变量），无需单独测试覆盖。

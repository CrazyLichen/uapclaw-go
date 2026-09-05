# SkillUseRail 设计文档

> 对应 IMPLEMENTATION_PLAN.md 9.19-23 中 `Skill(☐)` 项
> 对应 Python: `openjiuwen/harness/rails/skills/skill_use_rail.py`

## 1. 概述

SkillUseRail 是技能使用护栏，负责：

1. **技能加载**：从 `skills_dir` 增量扫描 SKILL.md 文件，用 mtime 判断是否需要重新解析 YAML front matter
2. **技能过滤**：`enabled_skills`（白名单）/ `disabled_skills`（黑名单）
3. **工具注册**：`Init()` 中注册 SkillTool + (includeTools ? ReadFileTool/CodeTool/BashTool : nil) + (auto_list ? ListSkillTool : nil)，幂等注册到 ResourceMgr + AbilityManager
4. **提示词注入**：`BeforeModelCall()` 中调用 `systemPromptBuilder.AddSection()` 注入技能节
5. **演化经验附加**：如果 `evolutionStore` 不为 nil，在 `BeforeInvoke()` 中 fetch 演化经验文本附加到 skill description
6. **生命周期**：`BeforeInvoke()` → 加载技能 → `BeforeModelCall()` → 注入提示词 → `AfterInvoke()` → 空操作；`Init()/Uninit()` → 注册/注销工具

### 在 Agent 会话中的流程位置

```
DeepAgent.invoke() / stream()
  └── ReActAgent 循环
        ├── before_invoke(ctx)
        │     └── SkillUseRail.BeforeInvoke → 加载 skill 缓存 + 演化经验
        ├── before_model_call(ctx)
        │     └── SkillUseRail.BeforeModelCall → 注入 skill section 到系统提示词
        └── after_invoke(ctx)
              └── SkillUseRail.AfterInvoke → 空操作
```

### 与其他 Rail 的关系

| 场景 | SysOperationRail | SkillUseRail `includeTools` | 重复？ |
|------|------------------|-----|------|
| Deep 模式 (default) | ✅ 固定挂载 | `false` | ❌ 不重复 |
| Code 模式 (非 ACP) | ✅ 固定挂载 | `true` | ⚠️ 幂等覆盖 |
| Code 模式 (ACP) | ✅ 固定挂载 | `false` | ❌ 不重复 |

两者同时挂载时，均采用幂等注册（先 Remove 再 Add），后 Init 的覆盖前者注册的工具实例。

## 2. 新增文件

| 文件 | 作用 |
|------|------|
| `internal/agentcore/harness/rails/skills/doc.go` | 子包文档 |
| `internal/agentcore/harness/rails/skills/skill_use_rail.go` | SkillUseRail 结构体 + 所有方法 |
| `internal/agentcore/harness/rails/skills/skill_use_rail_test.go` | 单元测试 |

## 3. 修改文件

| 文件 | 变更 |
|------|------|
| `internal/agentcore/harness/prompts/sections/skills.go` | 补 4 个函数 + 改 `BuildSkillsSection` 签名 + 删 `buildSkillLinesText` |
| `internal/agentcore/harness/prompts/sections/sections_test.go` | 更新测试 |
| `internal/agentcore/harness/rails/doc.go` | 添加 `skills/` 子包条目 |
| `internal/agentcore/single_agent/skills/skill.go` | Skill 结构体补 `UpdateAt time.Time` 字段 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | `buildSkillRail()` 从返回 nil 改为构建 SkillUseRail |
| `internal/swarm/server/adapter/deep_adapter_helpers_test.go` | 更新 findTeamSkillRail 测试 |

## 4. Skill 结构体变更

```go
// Skill 表示一个技能的元数据。
type Skill struct {
    // Name 技能名称（通常为 SKILL.md 所在目录的目录名）
    Name string
    // Description 技能描述（从 SKILL.md YAML front matter 的 description 字段提取）
    Description string
    // Directory 技能所在目录路径
    Directory string
    // UpdateAt 文件修改时间，用于增量缓存判断
    UpdateAt time.Time
}
```

新增 `UpdateAt` 字段后需同步更新：
- `NewSkill()` 构造函数
- `AsDict()` 方法
- `String()` / `GoString()` 方法
- `SkillManager.createSkillFromPath()` 中调用 `NewSkill()` 时传入 mtime

## 5. prompts/sections/skills.go 变更

### 5.1 提示词常量

**要求：提示词必须一比一复刻 Python 原文，禁止翻译修改。**

现有常量已与 Python 对齐，保持不变。

### 5.2 函数变更对照

| Python 函数 | 现有 Go 函数 | 变更 |
|---|---|---|
| `build_skill_line(index, skill_name, description)` | 无 | **新增** `BuildSkillLine(index int, skillName, description string) string` |
| `build_skill_lines(lines)` | `buildSkillLinesText(skillPaths []string)` | **替换**：删 `buildSkillLinesText`，新增 `BuildSkillLines(lines []string) string` |
| `build_all_mode_skill_prompt(skill_lines, language)` | 无 | **新增** `BuildAllModeSkillPrompt(skillLines, lang string) string` |
| `build_auto_list_mode_skill_prompt(language)` | 无 | **新增** `BuildAutoListModeSkillPrompt(lang string) string` |
| `build_skills_section(skill_lines, language, mode)` | `BuildSkillsSection(mode, skillPaths []string, lang)` | **改签名**：`BuildSkillsSection(mode, skillLines, lang string)` |
| `get_list_skill_system_prompt(language)` | `GetListSkillSystemPrompt(lang)` | ✅ 已对齐，不变 |

### 5.3 新增函数签名

```go
// BuildSkillLine 生成单行技能描述行。
// 对齐 Python: build_skill_line(index, skill_name, description)
func BuildSkillLine(index int, skillName, description string) string

// BuildSkillLines 拼接多行技能描述行（用 "\n\n" 分隔）。
// 对齐 Python: build_skill_lines(lines)
func BuildSkillLines(lines []string) string

// BuildAllModeSkillPrompt 构建 all 模式技能提示词。
// 对齐 Python: build_all_mode_skill_prompt(skill_lines, language)
func BuildAllModeSkillPrompt(skillLines, lang string) string

// BuildAutoListModeSkillPrompt 构建 auto_list 模式技能提示词。
// 对齐 Python: build_auto_list_mode_skill_prompt(language)
func BuildAutoListModeSkillPrompt(lang string) string
```

### 5.4 改签名

```go
// 改前
func BuildSkillsSection(mode string, skillPaths []string, lang string) saprompt.PromptSection

// 改后
func BuildSkillsSection(mode string, skillLines string, lang string) saprompt.PromptSection
```

### 5.5 删除

```go
// 删除（被 BuildSkillLines 替代）
func buildSkillLinesText(skillPaths []string) string
```

## 6. SkillUseRail 核心结构

```go
// SkillUseRail 技能使用护栏，管理 skill 提示词注入和工具注册。
// 对齐 Python: SkillUseRail (openjiuwen/harness/rails/skills/skill_use_rail.py)
type SkillUseRail struct {
    rails.DeepAgentRail

    // ── 配置字段（构造时设置） ──
    // skillsDir 技能根目录（支持多个）
    skillsDir []string
    // skillMode 技能暴露模式："all" 或 "auto_list"
    skillMode string
    // listSkillModel list_skill 工具使用的 LLM 模型（可选）
    listSkillModel *llm.Model
    // enableCache 是否缓存已加载技能
    enableCache bool
    // includeTools 是否注册 read_file / code / bash 工具
    includeTools bool
    // enabledSkills 白名单
    enabledSkills map[string]struct{}
    // disabledSkills 黑名单
    disabledSkills map[string]struct{}
    // evolutionStore 可选演进存储
    evolutionStore *checkpointing.EvolutionStore

    // ── 运行时状态 ──
    // skills 当前已加载技能列表
    skills []*skills.Skill
    // systemPromptBuilder 系统提示词构建器引用（Init 中获取）
    systemPromptBuilder saprompt.SystemPromptBuilderInterface

    // ── 缓存 ──
    // skillCache 增量缓存 absPath → Skill
    skillCache map[string]*skills.Skill
    // skillUpdateAt 增量缓存 absPath → mtime
    skillUpdateAt map[string]time.Time
    // skillOrder 有序 absPath 列表
    skillOrder []string
    // evolutionTexts 演化经验文本 skillName → text
    evolutionTexts map[string]string

    // ── 工具跟踪 ──
    // ownedToolNames 已注册到 AbilityManager 的工具名称
    ownedToolNames map[string]struct{}
    // ownedToolIDs 已注册到 ResourceMgr 的工具 ID
    ownedToolIDs map[string]struct{}
}
```

## 7. Option 函数

```go
type SkillUseRailOption func(*SkillUseRail)

func WithSkillMode(mode string) SkillUseRailOption
func WithListSkillModel(model *llm.Model) SkillUseRailOption
func WithEnableCache(enabled bool) SkillUseRailOption
func WithIncludeTools(enabled bool) SkillUseRailOption
func WithEnabledSkills(names []string) SkillUseRailOption
func WithDisabledSkills(names []string) SkillUseRailOption
func WithEvolutionStore(store *checkpointing.EvolutionStore) SkillUseRailOption
```

## 8. 核心方法列表

### 8.1 构造函数

| Go 方法 | Python 对应 | 说明 |
|---------|-----------|------|
| `NewSkillUseRail(skillsDir []string, opts ...SkillUseRailOption) *SkillUseRail` | `__init__` | Option 模式构造，校验 skillMode |

### 8.2 生命周期钩子

| Go 方法 | Python 对应 | 说明 |
|---------|-----------|------|
| `Init(agent BaseAgent) error` | `init()` | 获取 systemPromptBuilder + 注册工具（SkillTool, ListSkillTool, ReadFileTool, CodeTool, BashTool） |
| `Uninit(agent BaseAgent) error` | `uninit()` | 从 AbilityManager + ResourceMgr 注销工具 |
| `BeforeInvoke(ctx, cbc) error` | `before_invoke()` | 调用 refreshSkillPrompt |
| `BeforeModelCall(ctx, cbc) error` | `before_model_call()` | 构建 skills section 并注入 systemPromptBuilder |
| `AfterInvoke(ctx, cbc) error` | `after_invoke()` | 空操作 |

### 8.3 公共方法

| Go 方法 | Python 对应 | 说明 |
|---------|-----------|------|
| `SkillsMeta() []*Skill` | `skills_meta` property | 返回当前技能列表副本 |
| `ReloadSkills(ctx) error` | `reload_skills()` | 重新加载技能 + 演化经验 |
| `ClearSkills()` | `clear_skills()` | 清空缓存 |
| `LoadSkillsFromDir(ctx, skillsDir) ([]*Skill, error)` | `load_skills_from_dir()` | 类方法：静态加载 |

### 8.4 内部方法

| Go 方法 | Python 对应 | 说明 |
|---------|-----------|------|
| `refreshSkillPrompt(ctx)` | `refresh_skill_prompt()` | prepare + fetch evolution |
| `prepareSkills(ctx) error` | `_prepare_skills()` | 增量刷新 + 过滤 |
| `refreshSkillsIncrementally(ctx) error` | `_refresh_skills_incrementally()` | 遍历 skillsDir，mtime 比对增量加载 |
| `loadSkill(ctx, dir, modTime) (*Skill, error)` | `_load_skill()` | 加载单个 SKILL.md |
| `loadYAML(ctx, path) (map[string]any, string, error)` | `_load_yaml()` | YAML front matter 解析 |
| `loadDescription(ctx, path) (string, error)` | `_load_description()` | 提取 description 字段 |
| `collectSkillsInOrder() []*Skill` | `_collect_skills_in_order()` | 按序收集 + 去重 |
| `filterSkills(skills) []*Skill` | `_filter_skills()` | enabled/disabled 过滤 |
| `fetchEvolutionTexts(ctx)` | `_fetch_evolution_texts()` | 从 EvolutionStore 读取 |
| `getSkillDescription(skill) string` | `_get_skill_description()` | 附加演化经验文本到 description |
| `buildSkillsSection() *saprompt.PromptSection` | `_build_skills_section()` | 构建 PromptSection |
| `buildAllModePrompt() string` | `_build_all_mode_prompt()` | all 模式文本 |
| `normalizeNameList(raw) []string` | `_normalize_name_list()` | 规范化名称列表（支持逗号/分号） |
| `normalizeNameSet(raw) map[string]struct{}` | `_normalize_name_set()` | 规范化名称集合 |
| `normalizeSkillDirs() []string` | `_normalize_skill_dirs()` | 规范化目录列表为绝对路径 |
| `parseSkillDirs(raw) []string` | `_parse_skill_dirs()` | 解析分号/逗号分隔字符串 |

### 8.5 Go 特有辅助（Python 中为静态方法，Go独立）

| Go 方法 | 说明 |
|---------|------|
| `skillMDPath(skill *Skill) string` | 返回 `filepath.Join(skill.Directory, "SKILL.md")` |

## 9. Init/Uninit 工具注册逻辑

### 9.1 Init

对齐 Python `SkillUseRail.init()` L237-306：

1. 从 `agent.SystemPromptBuilder()` 获取 systemPromptBuilder 引用和 language
2. 从 `agent.Card().ID` 获取 agentID
3. 创建工具列表：
   - 始终：`NewSkillTool(op, getSkills, language, agentID)`
   - 如果 `includeTools`：`NewReadFileTool`, `NewCodeTool`, `NewBashTool`
   - 如果 `skillMode == "auto_list"`：`NewListSkillTool`
4. 幂等注册到 ResourceMgr：先 `GetTool` 检查，存在则 `RemoveTool`，再 `AddTool`
5. 注册到 AbilityManager：`am.Add(tool.Card())`，记录 `ownedToolNames`
6. 记录 `ownedToolIDs`

### 9.2 Uninit

对齐 Python `SkillUseRail.uninit()` L308-321：

1. 从 AbilityManager 移除 `ownedToolNames` 中的所有工具
2. 从 ResourceMgr 移除 `ownedToolIDs` 中的所有工具
3. 清空 `ownedToolNames` / `ownedToolIDs`

## 10. BeforeModelCall 提示词注入逻辑

对齐 Python `SkillUseRail.before_model_call()` L359-372：

```go
func (r *SkillUseRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
    if r.systemPromptBuilder == nil {
        return nil
    }
    section := r.buildSkillsSection()
    if section != nil {
        r.systemPromptBuilder.AddSection(*section)
    } else {
        r.systemPromptBuilder.RemoveSection(sections.SectionSkills)
    }
    return nil
}
```

`buildSkillsSection()` 内部逻辑：
- `skillMode == "all"`：遍历 skills 调用 `BuildSkillLine` → `BuildSkillLines` → `BuildSkillsSection("all", skillLines, lang)`
- `skillMode == "auto_list"`：`BuildSkillsSection("auto_list", "", lang)`

## 11. 增量加载逻辑

对齐 Python `_refresh_skills_incrementally()` L123-175：

1. 如果 `!enableCache`，清空缓存
2. 遍历 `normalizeSkillDirs()` 返回的根目录列表
3. 每个根目录按名称排序遍历子目录
4. 检查子目录下是否存在 SKILL.md
5. 如果存在，获取文件 mtime
6. 与 `skillUpdateAt[key]` 比对，如果缓存未命中或 mtime 不同则重新加载
7. 清理已消失的技能（stale keys）

注意：Go 中使用 `os.Stat()` + `ModTime()` 替代 Python 的 `path.stat().st_mtime`。
SkillUseRail 不依赖 SkillManager，有独立的加载逻辑。`loadYAML` / `loadDescription` 使用 SysOperation.ReadFile 或直接 os.ReadFile。

## 12. 演化经验附加

对齐 Python `_fetch_evolution_texts()` L333-346 和 `_get_skill_description()` L348-354：

```go
func (r *SkillUseRail) fetchEvolutionTexts(ctx context.Context) {
    if r.evolutionStore == nil {
        return
    }
    for _, skill := range r.skills {
        text := r.evolutionStore.FormatDescExperienceText(ctx, skill.Name, 5)
        r.evolutionTexts[skill.Name] = text
    }
}

func (r *SkillUseRail) getSkillDescription(skill *skills.Skill) string {
    desc := skill.Description
    if evoText, ok := r.evolutionTexts[skill.Name]; ok && evoText != "" {
        desc = desc + "\n  演进经验:\n" + evoText
    }
    return desc
}
```

## 13. DeepAdapter 回填

### 13.1 buildSkillRail()

从返回 nil 改为：

```go
func (d *DeepAdapter) buildSkillRail() sainterfaces.AgentRail {
    skillsDir := d.getAgentSkillsDir()  // 获取技能目录
    if skillsDir == "" {
        return nil
    }
    skillMode := d.resolveSkillMode()  // 解析 skill_mode
    var disabled []string
    if d.skillManager != nil {
        disabled = d.skillManager.ListExecutionDisabledSkills()
    }
    rail := skillrails.NewSkillUseRail(
        []string{skillsDir},
        skillrails.WithSkillMode(skillMode),
        skillrails.WithIncludeTools(false),  // DeepAdapter 默认 false
        skillrails.WithDisabledSkills(disabled),
    )
    return rail
}
```

### 13.2 CodeAdapter

后续 CodeAdapter 实现时，`buildSkillRail` 调用：
```go
skillrails.NewSkillUseRail(
    []string{skillsDir},
    skillrails.WithSkillMode(skillMode),
    skillrails.WithIncludeTools(!isACPToolProfile),  // 非 ACP 为 true
    skillrails.WithDisabledSkills(disabled),
)
```

## 14. 日志同步

对齐 Python 中的 `logger.debug` / `logger.warning` 调用：

| Python 日志点 | Go 日志 |
|---|---|
| `_refresh_skills_incrementally`: skills_dir does not exist | `logger.Debug` skills_dir does not exist |
| `_refresh_skills_incrementally`: skills_dir is not a directory | `logger.Debug` skills_dir is not a directory |
| `_collect_skills_in_order`: duplicate skill name | `logger.Warn` duplicate skill name |
| `_load_skill`: Failed to load description | `logger.Warn` failed to load description |
| `_load_skill`: skip setting update_at | `logger.Debug` skip setting update_at |
| `init`: failed to add tool resource | `logger.Warn` failed to add tool resource |
| `init`: failed to add tool card | `logger.Warn` failed to add tool card |
| `uninit`: failed to remove tool | `logger.Warn` failed to remove tool |
| `_fetch_evolution_texts`: failed to fetch evolution text | `logger.Warn` failed to fetch evolution text |
| `load_skills_from_dir`: skills_dir does not exist | `logger.Debug` skills_dir does not exist |
| `load_skills_from_dir`: skills_dir is not a directory | `logger.Debug` skills_dir is not a directory |
| `load_skills_from_dir`: duplicate skill name | `logger.Warn` duplicate skill name |

使用 `logger.ComponentAgentCore` 作为 logComponent。

## 15. 测试要求

### 15.1 覆盖率目标

≥ 85%

### 15.2 关键测试场景

- NewSkillUseRail 构造（正常 / 无效 skillMode）
- Init 工具注册（skillMode=all / auto_list / includeTools=true/false）
- Uninit 工具注销
- BeforeInvoke 增量加载（mtime 变化 / enableCache=false）
- BeforeModelCall 提示词注入（all 模式 / auto_list 模式 / 空 skills）
- filterSkills（enabled / disabled / 同时设置）
- normalizeNameList / normalizeNameSet / normalizeSkillDirs
- LoadSkillsFromDir 静态加载
- fetchEvolutionTexts（有 store / 无 store）
- getSkillDescription（有 / 无演化经验）
- buildSkillsSection / buildAllModePrompt
- 幂等注册（同一工具重复 Add）
- BuildSkillLine / BuildSkillLines / BuildAllModeSkillPrompt / BuildAutoListModeSkillPrompt

### 15.3 Mock 策略

- `sys_operation.SysOperation`：通过接口 mock
- `EvolutionStore`：创建包含预设数据的真实 EvolutionStore（使用 t.TempDir()）
- `BaseAgent`：使用 mock agent 提供 SystemPromptBuilder / AbilityManager / Card
- 文件系统：使用 `t.TempDir()` 创建临时 SKILL.md 文件

## 16. 特殊处理

### 16.1 Python async → Go 同步

Python 的 `_load_yaml` / `_load_description` 使用 `await self.sys_operation.fs().read_file()`。
Go 中 `SkillUseRail` 的 `loadYAML` / `loadDescription` 直接使用 `os.ReadFile()`（因为 `DeepAgentRail.SysOperation()` 返回的是同步接口，且 SkillUseRail 在 BeforeInvoke 中已有 ctx）。

如果需要走 SysOperation 路径，则使用 `r.SysOperation().ReadFile()` 对应的方法。

### 16.2 Python setattr(skill, "update_at", ...) → Go 直接赋值

Python 中 `setattr(skill, "update_at", update_at)` 带 try/except 是因为 Pydantic model 可能不允许设置额外属性。Go 中 `Skill.UpdateAt` 是普通字段，直接赋值即可，无需 try/except 等价逻辑。

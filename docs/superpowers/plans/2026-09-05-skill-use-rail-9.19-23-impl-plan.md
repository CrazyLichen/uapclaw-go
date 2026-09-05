# SkillUseRail 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SkillUseRail 技能使用护栏，完成 9.19-23 中 Skill(☐) 项

**Architecture:** SkillUseRail 继承 DeepAgentRail，独立于 SkillManager 实现增量技能加载。创建 `harness/rails/skills/` 子包，补齐 `prompts/sections/skills.go` 缺失函数，给 Skill 结构体补充 UpdateAt 字段，回填 DeepAdapter 的 buildSkillRail() 占位。

**Tech Stack:** Go 1.23, gopkg.in/yaml.v3, 内置 os/path/filepath/filepath 标准库

---

### Task 1: Skill 结构体补充 UpdateAt 字段

**Files:**
- Modify: `internal/agentcore/single_agent/skills/skill.go`
- Modify: `internal/agentcore/single_agent/skills/skill_test.go`
- Modify: `internal/agentcore/single_agent/skills/skill_manager.go`

- [ ] **Step 1: 给 Skill 结构体新增 UpdateAt 字段**

在 `internal/agentcore/single_agent/skills/skill.go` 的 Skill 结构体中添加：

```go
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

添加 `import "time"` 到文件头。

- [ ] **Step 2: 更新 NewSkill 构造函数**

```go
func NewSkill(name, description, directory string) *Skill {
    return &Skill{
        Name:        name,
        Description: description,
        Directory:   directory,
    }
}
```

注意：NewSkill 不传 UpdateAt，保持现有签名不变。UpdateAt 由调用方在构造后单独赋值（对齐 Python 的 `setattr(skill, "update_at", update_at)` 模式）。

- [ ] **Step 3: 更新 AsDict 方法**

在 `AsDict` 中添加 `update_at` 字段：

```go
func (s *Skill) AsDict(includeDirectory bool) map[string]any {
    result := map[string]any{
        "name":        s.Name,
        "description": s.Description,
    }
    if includeDirectory {
        result["directory"] = s.Directory
    }
    if !s.UpdateAt.IsZero() {
        result["update_at"] = s.UpdateAt.Format(time.RFC3339)
    }
    return result
}
```

- [ ] **Step 4: 更新 SkillManager.createSkillFromPath 传入 mtime**

在 `internal/agentcore/single_agent/skills/skill_manager.go` 的 `createSkillFromPath` 方法中，获取文件 mtime 并赋值给 `skill.UpdateAt`：

```go
func (sm *SkillManager) createSkillFromPath(skillMDPath string) (*Skill, error) {
    description, err := sm.loadDescription(skillMDPath)
    if err != nil {
        return nil, err
    }
    skillDir := filepath.Dir(skillMDPath)
    skillName := filepath.Base(skillDir)
    skill := NewSkill(skillName, description, skillDir)
    // 获取文件修改时间
    if info, err := os.Stat(skillMDPath); err == nil {
        skill.UpdateAt = info.ModTime()
    }
    return skill, nil
}
```

- [ ] **Step 5: 运行 skills 包现有测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test ./internal/agentcore/single_agent/skills/... -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/single_agent/skills/skill.go internal/agentcore/single_agent/skills/skill_manager.go
git commit -m "feat(skills): add UpdateAt field to Skill struct for incremental cache"
```

---

### Task 2: 补齐 prompts/sections/skills.go 缺失函数

**Files:**
- Modify: `internal/agentcore/harness/prompts/sections/skills.go`
- Modify: `internal/agentcore/harness/prompts/sections/sections_test.go`

- [ ] **Step 1: 新增 BuildSkillLine 函数**

在 `skills.go` 导出函数区块添加：

```go
// BuildSkillLine 生成单行技能描述行。
// 对齐 Python: build_skill_line(index, skill_name, description)
func BuildSkillLine(index int, skillName, description string) string {
    return fmt.Sprintf("%d. %s: %s", index, skillName, description)
}
```

添加 `import "fmt"` 到文件头。

- [ ] **Step 2: 新增 BuildSkillLines 函数**

```go
// BuildSkillLines 拼接多行技能描述行（用 "\n\n" 分隔）。
// 对齐 Python: build_skill_lines(lines)
func BuildSkillLines(lines []string) string {
    var filtered []string
    for _, line := range lines {
        if line != "" {
            filtered = append(filtered, line)
        }
    }
    return strings.Join(filtered, "\n\n")
}
```

添加 `import "strings"` 到文件头。

- [ ] **Step 3: 新增 BuildAllModeSkillPrompt 函数**

```go
// BuildAllModeSkillPrompt 构建 all 模式技能提示词。
// 对齐 Python: build_all_mode_skill_prompt(skill_lines, language)
func BuildAllModeSkillPrompt(skillLines, lang string) string {
    text := strings.TrimSpace(skillLines)
    if text == "" {
        if lang == "en" {
            return skillRailNoSkillPromptEN
        }
        return skillRailNoSkillPromptCN
    }
    if lang == "en" {
        return skillRailAllModeHeaderEN + text + skillRailAllModeInstructionEN
    }
    return skillRailAllModeHeaderCN + text + skillRailAllModeInstructionCN
}
```

- [ ] **Step 4: 新增 BuildAutoListModeSkillPrompt 函数**

```go
// BuildAutoListModeSkillPrompt 构建 auto_list 模式技能提示词。
// 对齐 Python: build_auto_list_mode_skill_prompt(language)
func BuildAutoListModeSkillPrompt(lang string) string {
    if lang == "en" {
        return skillRailAutoListModePromptEN
    }
    return skillRailAutoListModePromptCN
}
```

- [ ] **Step 5: 修改 BuildSkillsSection 签名**

将签名从 `(mode string, skillPaths []string, lang string)` 改为 `(mode string, skillLines string, lang string)`，内部使用 `BuildAllModeSkillPrompt` 和 `BuildAutoListModeSkillPrompt`：

```go
// BuildSkillsSection 构建技能节。
// 对齐 Python: build_skills_section(skill_lines, language, mode)
func BuildSkillsSection(mode string, skillLines string, lang string) saprompt.PromptSection {
    var content string

    switch mode {
    case "all":
        content = BuildAllModeSkillPrompt(skillLines, lang)
    case "auto_list":
        content = BuildAutoListModeSkillPrompt(lang)
    case "no_skill":
        if lang == "en" {
            content = skillRailNoSkillPromptEN
        } else {
            content = skillRailNoSkillPromptCN
        }
    default:
        if lang == "en" {
            content = skillRailNoSkillPromptEN
        } else {
            content = skillRailNoSkillPromptCN
        }
    }

    return saprompt.PromptSection{
        Name:     SectionSkills,
        Content:  map[string]string{lang: content},
        Priority: 40,
    }
}
```

- [ ] **Step 6: 删除 buildSkillLinesText 函数**

删除整个 `buildSkillLinesText` 函数（已被 `BuildSkillLines` 替代）。

- [ ] **Step 7: 更新 sections_test.go 中 BuildSkillsSection 调用**

将测试中所有 `BuildSkillsSection(mode, []string{...}, lang)` 改为 `BuildSkillsSection(mode, "预渲染行文本", lang)`。

新增测试：

```go
func TestBuildSkillLine(t *testing.T) {
    got := sections.BuildSkillLine(0, "implement", "实现代码")
    want := "0. implement: 实现代码"
    if got != want {
        t.Errorf("BuildSkillLine() = %q, want %q", got, want)
    }
}

func TestBuildSkillLines(t *testing.T) {
    got := sections.BuildSkillLines([]string{"1. a: b", "2. c: d"})
    want := "1. a: b\n\n2. c: d"
    if got != want {
        t.Errorf("BuildSkillLines() = %q, want %q", got, want)
    }
}

func TestBuildAllModeSkillPrompt(t *testing.T) {
    // 有技能
    got := sections.BuildAllModeSkillPrompt("1. foo: bar", "cn")
    if !strings.Contains(got, "可用技能") {
        t.Errorf("BuildAllModeSkillPrompt(cn) missing header")
    }
    // 无技能
    got2 := sections.BuildAllModeSkillPrompt("", "cn")
    if !strings.Contains(got2, "当前任务没有选择任何技能") {
        t.Errorf("BuildAllModeSkillPrompt(empty cn) missing no_skill fallback")
    }
}

func TestBuildAutoListModeSkillPrompt(t *testing.T) {
    got := sections.BuildAutoListModeSkillPrompt("cn")
    if !strings.Contains(got, "list_skill") {
        t.Errorf("BuildAutoListModeSkillPrompt(cn) missing list_skill")
    }
}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test ./internal/agentcore/harness/prompts/sections/... -v -count=1`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/agentcore/harness/prompts/sections/skills.go internal/agentcore/harness/prompts/sections/sections_test.go
git commit -m "feat(sections): add BuildSkillLine/BuildSkillLines/BuildAllModeSkillPrompt/BuildAutoListModeSkillPrompt, change BuildSkillsSection signature"
```

---

### Task 3: 创建 skills 子包 doc.go

**Files:**
- Create: `internal/agentcore/harness/rails/skills/doc.go`
- Modify: `internal/agentcore/harness/rails/doc.go`

- [ ] **Step 1: 创建 skills/doc.go**

```go
// Package skills 提供技能使用护栏实现。
//
// SkillUseRail 负责技能提示词注入和工具注册，从 skills_dir 增量加载 SKILL.md 文件，
// 根据 skill_mode 决定注入方式（all/auto_list），并可选附加演化经验文本。
//
// 文件目录：
//
//	skills/
//	├── doc.go               # 包文档
//	├── skill_use_rail.go    # SkillUseRail 技能使用护栏
//
// 对应 Python 代码：openjiuwen/harness/rails/skills/
package skills
```

- [ ] **Step 2: 更新 harness/rails/doc.go 添加 skills 子包条目**

在 doc.go 文件目录中添加：

```
//	├── skills/            # 技能使用护栏子包
//	    ├── doc.go                # 包文档
//	    ├── skill_use_rail.go     # SkillUseRail 技能使用护栏（提示词注入 + 工具注册）
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/rails/skills/doc.go internal/agentcore/harness/rails/doc.go
git commit -m "feat(rails): create skills/ sub-package with doc.go"
```

---

### Task 4: 实现 SkillUseRail 结构体和构造函数

**Files:**
- Create: `internal/agentcore/harness/rails/skills/skill_use_rail.go`

- [ ] **Step 1: 创建 skill_use_rail.go 骨架**

包含：package 声明、import、常量、SkillUseRail 结构体定义、Option 函数、NewSkillUseRail 构造函数、编译时接口检查。

结构体字段和 Option 函数按设计文档第 6、7 节实现。

NewSkillUseRail 构造函数需校验 skillMode 有效性，无效时 panic（对齐 Python ValueError）。

常量：

```go
const (
    // SkillModeAll 将所有技能注入系统提示词
    SkillModeAll = "all"
    // SkillModeAutoList 添加 list_skill 工具让模型自主查看技能
    SkillModeAutoList = "auto_list"
)

var validSkillModes = map[string]struct{}{
    SkillModeAll:      {},
    SkillModeAutoList: {},
}
```

- [ ] **Step 2: 实现辅助方法**

按设计文档 8.4 节实现以下非导出方法（不含生命周期钩子，仅纯逻辑方法）：

- `normalizeNameList(raw any) []string`
- `normalizeNameSet(raw any) map[string]struct{}`
- `parseSkillDirs(raw string) []string`
- `normalizeSkillDirs() []string` — 将 skillsDir 规范化为绝对路径列表
- `skillMDPath(skill *skillpkg.Skill) string`

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail.go
git commit -m "feat(skills): add SkillUseRail struct, constructor, and helper methods"
```

---

### Task 5: 实现技能加载逻辑

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go`

- [ ] **Step 1: 实现 loadYAML 和 loadDescription**

对齐 Python `_load_yaml()` 和 `_load_description()`。使用 `os.ReadFile()` 读取文件，`gopkg.in/yaml.v3` 解析 YAML front matter。

```go
func (r *SkillUseRail) loadYAML(path string) (map[string]any, string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
    }
    text := string(data)
    if strings.HasPrefix(text, "---") {
        parts := strings.SplitN(text, "---", 3)
        if len(parts) >= 3 {
            yamlBlock := parts[1]
            body := strings.TrimLeft(parts[2], "\n\r")
            var yamlData map[string]any
            if err := yaml.Unmarshal([]byte(yamlBlock), &yamlData); err != nil {
                return nil, text, nil
            }
            return yamlData, body, nil
        }
    }
    return nil, text, nil
}

func (r *SkillUseRail) loadDescription(path string) (string, error) {
    yamlData, _, err := r.loadYAML(path)
    if err != nil {
        return "", err
    }
    if yamlData == nil {
        return "", errors.New("SKILL.md 文件不包含 YAML front matter")
    }
    descVal, ok := yamlData["description"]
    if !ok {
        return "", errors.New("SKILL.md 文件不包含 description 字段")
    }
    desc, ok := descVal.(string)
    if !ok {
        return "", fmt.Errorf("SKILL.md description 字段类型错误，期望 string，实际 %T", descVal)
    }
    return desc, nil
}
```

- [ ] **Step 2: 实现 loadSkill**

对齐 Python `_load_skill()`。加载单个 SKILL.md，返回带 UpdateAt 的 Skill。

```go
func (r *SkillUseRail) loadSkill(dir string, modTime time.Time) (*skillpkg.Skill, error) {
    skillMDPath := filepath.Join(dir, "SKILL.md")
    description, err := r.loadDescription(skillMDPath)
    if err != nil {
        logger.Warn(logComponent).
            Str("path", skillMDPath).
            Err(err).
            Msg("Failed to load description")
        description = fmt.Sprintf("Skill located in %s", dir)
    }
    skill := skillpkg.NewSkill(filepath.Base(dir), description, dir)
    skill.UpdateAt = modTime
    return skill, nil
}
```

- [ ] **Step 3: 实现 refreshSkillsIncrementally**

对齐 Python `_refresh_skills_incrementally()` L123-175。遍历 skillsDir，mtime 增量比对。

- [ ] **Step 4: 实现 collectSkillsInOrder**

对齐 Python `_collect_skills_in_order()`。按 skillOrder 顺序收集，按 name 去重。

- [ ] **Step 5: 实现 filterSkills**

对齐 Python `_filter_skills()`。enabled/disabled 过滤。

- [ ] **Step 6: 实现 prepareSkills**

对齐 Python `_prepare_skills()`。调用 refreshSkillsIncrementally + collectSkillsInOrder + filterSkills。

- [ ] **Step 7: 实现 fetchEvolutionTexts 和 getSkillDescription**

对齐 Python `_fetch_evolution_texts()` 和 `_get_skill_description()`。按设计文档第 12 节实现。

- [ ] **Step 8: 实现 refreshSkillPrompt、ReloadSkills、ClearSkills、SkillsMeta**

对齐 Python 对应方法。

- [ ] **Step 9: 实现 LoadSkillsFromDir 类方法**

对齐 Python `load_skills_from_dir()` 静态方法。创建临时 SkillUseRail 实例加载。

- [ ] **Step 10: Commit**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail.go
git commit -m "feat(skills): implement skill loading, filtering, and evolution text logic"
```

---

### Task 6: 实现生命周期钩子和提示词注入

**Files:**
- Modify: `internal/agentcore/harness/rails/skills/skill_use_rail.go`

- [ ] **Step 1: 实现 Init 方法**

对齐 Python `init()` L237-306。获取 systemPromptBuilder，创建工具列表，幂等注册到 ResourceMgr + AbilityManager。

- [ ] **Step 2: 实现 Uninit 方法**

对齐 Python `uninit()` L308-321。从 AbilityManager + ResourceMgr 注销工具。

- [ ] **Step 3: 实现 BeforeInvoke**

对齐 Python `before_invoke()`。调用 refreshSkillPrompt。

- [ ] **Step 4: 实现 BeforeModelCall**

对齐 Python `before_model_call()`。构建 skills section 并注入 systemPromptBuilder。内部调用 buildSkillsSection。

- [ ] **Step 5: 实现 buildSkillsSection 和 buildAllModePrompt**

对齐 Python `_build_skills_section()` 和 `_build_all_mode_prompt()`。

- [ ] **Step 6: 实现 AfterInvoke**

空操作，返回 nil。

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail.go
git commit -m "feat(skills): implement Init/Uninit/BeforeInvoke/BeforeModelCall/AfterInvoke lifecycle hooks"
```

---

### Task 7: 编写单元测试

**Files:**
- Create: `internal/agentcore/harness/rails/skills/skill_use_rail_test.go`

- [ ] **Step 1: 编写 NewSkillUseRail 构造测试**

测试正常构造和无效 skillMode panic。

- [ ] **Step 2: 编写 normalizeNameList / normalizeNameSet / normalizeSkillDirs 测试**

测试字符串输入、切片输入、逗号/分号分隔、空值、nil 等。

- [ ] **Step 3: 编写 filterSkills 测试**

测试 enabled only / disabled only / 同时设置 / 空 skills。

- [ ] **Step 4: 编写技能加载测试**

使用 t.TempDir() 创建临时 SKILL.md 文件，测试 refreshSkillsIncrementally / loadYAML / loadDescription / collectSkillsInOrder / enableCache=false。

- [ ] **Step 5: 编写 buildSkillsSection / buildAllModePrompt 测试**

测试 all 模式 / auto_list 模式 / 空 skills / 中文/英文。

- [ ] **Step 6: 编写 getSkillDescription 测试**

测试有/无演化经验文本。

- [ ] **Step 7: 编写 Init/Uninit 工具注册测试**

需要 mock BaseAgent（提供 SystemPromptBuilder / AbilityManager / Card）。测试 skillMode=all / auto_list / includeTools=true/false 的工具注册组合。

- [ ] **Step 8: 编写 BeforeModelCall 提示词注入测试**

测试注入 section / systemPromptBuilder 为 nil 时的空操作。

- [ ] **Step 9: 编写 LoadSkillsFromDir 测试**

使用 t.TempDir() 创建多技能目录，测试静态加载和重复名称去重。

- [ ] **Step 10: 运行测试确认覆盖率 ≥ 85%**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test -cover ./internal/agentcore/harness/rails/skills/... -v -count=1`

Expected: coverage ≥ 85%

- [ ] **Step 11: Commit**

```bash
git add internal/agentcore/harness/rails/skills/skill_use_rail_test.go
git commit -m "test(skills): add unit tests for SkillUseRail (coverage ≥ 85%)"
```

---

### Task 8: 回填 DeepAdapter buildSkillRail()

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`

- [ ] **Step 1: 添加 import**

在 `deep_adapter_rails.go` 的 import 中添加：

```go
skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
```

- [ ] **Step 2: 实现 resolveSkillMode 方法**

对齐 Python `_resolve_skill_mode()`。从 d.configCache 读取 skill_mode，无效值回退到 SkillModeAll。

```go
func (d *DeepAdapter) resolveSkillMode() string {
    rawSkillMode := "all" // 默认值
    if d.configCache != nil {
        if sm, ok := d.configCache["skill_mode"]; ok {
            if s, ok := sm.(string); ok {
                rawSkillMode = s
            }
        }
    }
    if _, ok := skillrails.ValidSkillModes[rawSkillMode]; ok {
        return rawSkillMode
    }
    logger.Info(logComponent).
        Str("raw_skill_mode", fmt.Sprintf("%v", rawSkillMode)).
        Str("fallback", skillrails.SkillModeAll).
        Msg("invalid skill_mode, fallback to all")
    return skillrails.SkillModeAll
}
```

注意：需要在 SkillUseRail 包中导出 `ValidSkillModes` 变量：

```go
var ValidSkillModes = map[string]struct{}{
    SkillModeAll:      {},
    SkillModeAutoList: {},
}
```

- [ ] **Step 3: 实现 buildSkillRail 方法**

替换现有的 `return nil` 占位：

```go
func (d *DeepAdapter) buildSkillRail() sainterfaces.AgentRail {
    skillsDir := skill.GetAgentSkillsDir()
    if skillsDir == "" {
        return nil
    }
    skillMode := d.resolveSkillMode()
    var disabled []string
    if d.skillManager != nil {
        disabled = d.skillManager.ListExecutionDisabledSkills()
    }
    rail := skillrails.NewSkillUseRail(
        []string{skillsDir},
        skillrails.WithSkillMode(skillMode),
        skillrails.WithIncludeTools(false),
        skillrails.WithDisabledSkills(disabled),
    )
    logger.Info(logComponent).
        Str("skill_mode", skillMode).
        Int("disabled_count", len(disabled)).
        Msg("SkillUseRail create success")
    return rail
}
```

需要在 `skill` 包中确认 `GetAgentSkillsDir()` 或等价函数已导出。查看 `internal/swarm/server/runtime/skill/state_utils.go` 中的 `getAgentSkillsDir()`（当前为非导出），需要新增导出版本或使用 `workspace.AgentSkillsDir()`。

实际实现时使用 `workspace.AgentSkillsDir()`（已在多个地方使用），添加 import：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go build ./internal/swarm/server/adapter/...`

Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/agentcore/harness/rails/skills/skill_use_rail.go
git commit -m "feat(adapter): wire buildSkillRail to SkillUseRail in DeepAdapter"
```

---

### Task 9: 更新 DeepAdapter 相关测试

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_helpers_test.go`

- [ ] **Step 1: 更新 findTeamSkillRail 测试**

现有测试断言 `findTeamSkillRail() == nil`。改为断言 `findTeamSkillRail()` 返回的 Rail 不是 `*skillrails.SkillUseRail` 类型（findTeamSkillRail 查找的是 TeamSkillEvolutionRail，不是 SkillUseRail）。

确认 `findTeamSkillRail` 仍应返回 nil（TeamSkillEvolutionRail 尚未实现），所以现有断言可能不需要改变。但需要确认测试编译通过。

- [ ] **Step 2: 运行 adapter 测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test ./internal/swarm/server/adapter/... -v -count=1 -timeout 120s`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/adapter/
git commit -m "test(adapter): update DeepAdapter tests for SkillUseRail wiring"
```

---

### Task 10: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.19-23 行状态**

将 `Skill(☐)` 改为 `Skill(✅)`，添加实现细节摘要：

```
| 9.19-23 | ☐ | 其他 Rails | Security(✅ ...)/Interrupt(✅)/Skill(✅ SkillUseRail: 增量加载+YAML解析+enabled/disabled过滤+SkillTool/ListSkillTool注册+all/auto_list提示词注入+演化经验附加+includeTools幂等注册)/ContextEngine(✅)/Memory(✅)/Verification(⤴️9.29✅)/Subagent(⤴️9.29✅) Rails | `openjiuwen/harness/rails/` |
```

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update IMPLEMENTATION_PLAN.md 9.19-23 Skill(✅)"
```

---

### Task 11: 全量编译和测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go build ./...`

Expected: 编译通过

- [ ] **Step 2: 运行受影响包的测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test ./internal/agentcore/single_agent/skills/... ./internal/agentcore/harness/prompts/sections/... ./internal/agentcore/harness/rails/skills/... ./internal/swarm/server/adapter/... -v -count=1 -timeout 300s`

Expected: 全部 PASS

- [ ] **Step 3: 确认覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true && go test -cover ./internal/agentcore/harness/rails/skills/... -count=1`

Expected: coverage ≥ 85%

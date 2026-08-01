# Skills 工具（SkillTool + ListSkillTool）实现设计 — 9.38

## 概述

实现 Harness 工具集中的 Skills 工具（SkillTool + ListSkillTool），对应 Python `openjiuwen/harness/tools/skills/` 的 `skill_tool.py` 和 `list_skill.py`。

本次实现范围：
- **SkillTool**：查看特定技能内容的工具，依赖 SysOperation + getSkills 回调
- **ListSkillTool**：列出可用技能或为当前任务选择相关技能的工具，依赖 getSkills 回调 + 可选的 LLM 路由模型

**不包含**：SkillUseRail（9.19-23），后续回填。

## 流程位置与作用

Skills 工具在 Agent 会话中的流程位置：

```
1. Agent 初始化时 → SkillUseRail.init()（后续回填）
   - 注册 SkillTool + ListSkillTool（+可选的 ReadFile/Code/Bash）
   - 注入工具卡片到 ability_manager + resource_manager

2. 每次 invoke 前 → SkillUseRail.before_invoke()（后续回填）
   - 重新加载 skills（从 skills_dir 增量刷新）

3. 模型调用前 → SkillUseRail.before_model_call()（后续回填）
   - 根据 skill_mode 注入 skills section 到 system prompt

4. 模型决定调用工具 → SkillTool.Invoke() / ListSkillTool.Invoke()
   - SkillTool：按 skill_name 查找 → SysOperation.Fs().ReadFile() 读取技能文件
   - ListSkillTool：无 query→全部；有 query+model→LLM 路由；有 query 无 model→fallback

5. invoke 后 → SkillUseRail.after_invoke()（后续回填）
```

**作用**：Skills 工具让 Agent 在执行任务时能发现和阅读技能文件（SKILL.md），获取领域知识、操作指南、最佳实践等结构化信息。

## 文件结构

```
internal/agentcore/harness/tools/skills/
├── doc.go           # 包文档（含文件目录树）
├── skill_tool.go    # SkillTool 结构体 + Invoke/Stream/GetSkillByName
├── list_skill.go    # ListSkillTool 结构体 + Invoke/Stream + 路由逻辑
└── skills_test.go   # 两个工具的单元测试
```

包名：`skills`

对外导出：
- `SkillTool` 结构体
- `ListSkillTool` 结构体
- `NewSkillTool()` 构造函数
- `NewListSkillTool()` 构造函数

## SkillTool 设计

### 结构体

```go
// SkillTool 查看特定技能内容的工具。
//
// 对应 Python: SkillTool
type SkillTool struct {
    // card 工具配置卡片
    card *tool.ToolCard
    // operation 系统操作，用于读取技能文件
    operation sys_operation.SysOperation
    // getSkills 返回当前已启用技能列表的回调函数
    getSkills func() []*skills.Skill
    // language 语言标识（"cn"/"en"）
    language string
}
```

### 构造函数

```go
// NewSkillTool 创建 SkillTool 实例。
//
// 对应 Python: SkillTool.__init__(operation, get_skills, language, agent_id)
func NewSkillTool(
    operation sys_operation.SysOperation,
    getSkills func() []*skills.Skill,
    language string,
    agentID string,
) *SkillTool
```

构造时使用 `build_tool_card("skill_tool", "SkillTool", language, agentID)` 生成 ToolCard，与 Python 一比一复刻。

### Invoke 方法逻辑

一比一复刻 Python `SkillTool.invoke()`：

1. 提取 `inputs["skill_name"]` → 去空白
2. 提取 `inputs["relative_file_path"]` → 去空白，默认 `"SKILL.md"`
3. 调用 `getSkillByName(skillName)` 查找技能
   - 未找到 → 返回 `map{"success": false, "error": "Skill not found: {name}"}`
4. 构建文件路径：`skill.Directory + "/" + relativeFilePath`
5. 调用 `operation.Fs().ReadFile(ctx, filePath)` 读取
   - `result.Code != 0` → 返回 `map{"success": false, "error": result.Message}`
6. 成功 → 返回 `map{"success": true, "data": map{"skill_directory": skill.Directory, "skill_content": result.Data.Content}}`
7. 异常 → 返回 `map{"success": false, "error": err.Error()}`

### 辅助方法

```go
// getSkillByName 按名称查找技能。
//
// 对应 Python: SkillTool._get_skill_by_name(skill_name)
func (t *SkillTool) getSkillByName(name string) *skills.Skill
```

逻辑：遍历 getSkills() 返回列表，构建 name→skill 映射，按 name 查找。

### Stream 方法

返回 `ErrStreamNotSupported`，与 Python 的 `if False: yield None` 一致。

### Card 方法

```go
func (t *SkillTool) Card() *tool.ToolCard { return t.card }
```

## ListSkillTool 设计

### 结构体

```go
// ListSkillTool 列出可用技能或为当前任务选择相关技能的工具。
//
// 对应 Python: ListSkillTool
type ListSkillTool struct {
    // card 工具配置卡片
    card *tool.ToolCard
    // getSkills 返回当前已启用技能列表的回调函数
    getSkills func() []*skills.Skill
    // listSkillModel 可选的 LLM 模型，用于技能路由
    listSkillModel model.Model
    // language 语言标识（"cn"/"en"）
    language string
}
```

### 构造函数

```go
// NewListSkillTool 创建 ListSkillTool 实例。
//
// 对应 Python: ListSkillTool.__init__(get_skills, list_skill_model, language, agent_id)
func NewListSkillTool(
    getSkills func() []*skills.Skill,
    listSkillModel model.Model,
    language string,
    agentID string,
) *ListSkillTool
```

`listSkillModel` 为 nil 时走 fallback 逻辑（返回全部技能），与 Python 一致。

### Invoke 方法逻辑

一比一复刻 Python `ListSkillTool.invoke()`：

1. 提取 `inputs["query"]` → 去空白
2. query 为空 → 返回 `map{"success": true, "data": map{"skills": dumpAllSkills(), "mode": "all"}}`
3. listSkillModel 为 nil → 返回 `map{"success": true, "data": map{"skills": dumpAllSkills(), "mode": "all", "message": "list_skill_model is not configured, fallback to all skills."}}`
4. 否则走 LLM 路由：
   a. `selectedNames = routeSkills(ctx, query)` — 调用 LLM 选择相关技能
   b. `selectedSkills = selectSkillsByNames(selectedNames)`
   → 返回 `map{"success": true, "data": map{"skills": dumpSkills(selectedSkills), "mode": "filtered", "selected_skill_names": [...]}}`
5. 异常 → 返回 `map{"success": false, "error": err.Error()}`

### LLM 路由方法

一比一复刻 Python `_route_skills()`：

```go
// routeSkills 用 listSkillModel 路由选择相关技能。
//
// 对应 Python: ListSkillTool._route_skills(query)
func (t *ListSkillTool) routeSkills(ctx context.Context, query string) ([]string, error)
```

逻辑：
1. `payload = dumpAllSkills()` — 所有技能的序列化列表
2. 调用 `listSkillModel.Invoke()` 发送：
   - SystemMessage: `sections.GetListSkillSystemPrompt(language)`
   - UserMessage: `"User task:\n{query}\n\nAvailable skills:\n{json.Marshal(payload)}\n\nReturn only the names of the skills that are relevant to the task."`
3. 解析模型返回 → `parseSelectedSkillNames(content)`

**提示词一比一复刻 Python 原文，不做自行翻译。**

### UserMessage 模板

一比一复刻 Python `list_skill.py` 第 109-117 行：

```python
f"User task:\n{query}\n\n"
"Available skills:\n"
f"{json.dumps(payload, ensure_ascii=False, indent=2)}\n\n"
"Return only the names of the skills that are relevant to the task."
```

Go 中：
```go
userContent := fmt.Sprintf(
    "User task:\n%s\n\nAvailable skills:\n%s\n\nReturn only the names of the skills that are relevant to the task.",
    query,
    jsonPayload,
)
```

字符串部分与 Python 一比一复刻，不翻译。

### 解析方法

一比一复刻 Python `_parse_selected_skill_names()`：

```go
// parseSelectedSkillNames 从模型输出解析选中的技能名称。
//
// 对应 Python: ListSkillTool._parse_selected_skill_names(content)
func (t *ListSkillTool) parseSelectedSkillNames(content string) []string
```

逻辑：
1. strip content，空则返回 `[]string{}`
2. 如果以 `"```"` 开头 → 去掉代码块标记（第一行和最后的 `"```"` 行）
3. `json.Unmarshal` → 取 `"skills"` 数组
4. 过滤为非空字符串列表

### 序列化方法

```go
// dumpAllSkills 序列化所有已启用技能。
//
// 对应 Python: ListSkillTool._dump_all_skills()
func (t *ListSkillTool) dumpAllSkills() []map[string]any

// dumpSkills 序列化指定技能列表。
//
// 对应 Python: ListSkillTool._dump_skills(skills)
func (t *ListSkillTool) dumpSkills(skillList []*skills.Skill) []map[string]any
```

每个 skill 构建 `map{"name": ..., "description": ..., "directory": ..., "skill_md_path": ...}`，
其中 `skill_md_path = skill.Directory + "/SKILL.md"`。

一比一复刻 Python 中 `skill.asdict(include_directory=True)` + `skill_md_path` 的追加逻辑。

### 选择方法

```go
// selectSkillsByNames 按名称选择技能。
//
// 对应 Python: ListSkillTool._select_skills_by_names(names)
func (t *ListSkillTool) selectSkillsByNames(names []string) []*skills.Skill
```

### Stream 方法

返回 `ErrStreamNotSupported`。

### Card 方法

```go
func (t *ListSkillTool) Card() *tool.ToolCard { return t.card }
```

## SysOperation 对接

Python 中 `SkillTool` 使用 `self.operation.fs().read_file(file_path)` 读取文件。

Go 对应调用：
```go
readFileResult, err := t.operation.Fs().ReadFile(ctx, filePath)
```

判断逻辑一比一对齐：
- Python: `read_file_result.code != 0` → Go: `readFileResult.Code != 0`
- Python: `read_file_result.message` → Go: `readFileResult.Message`
- Python: `read_file_result.data.content` → Go: `readFileResult.Data.Content`

Go 的 `ReadFileResult` 结构（BaseResult{Code, Message} + Data{Path, Content, Mode})已经是一比一复刻 Python 的 `read_file_result`。

## 提示词一比一复刻

ListSkillTool 的 LLM 路由中使用的提示词，Go 已有实现（`prompts/sections/skills.go`）中的常量与 Python `sections/skills.py` 完全一致：

| 常量 | Python 原文 | Go 已有 |
|------|-------------|---------|
| SKILL_RAIL_LIST_SKILL_SYSTEM_PROMPT_CN | `你是一个技能选择器...` | ✅ 一致 |
| SKILL_RAIL_LIST_SKILL_SYSTEM_PROMPT_EN | `You are a list_skill selector...` | ✅ 一致 |

ListSkillTool 中的 UserMessage 模板直接使用 Python 原文格式字符串，不翻译。

## 返回值模式

沿用 Go 项目 Tool 接口签名 `Invoke(ctx, inputs) → (map[string]any, error)`：

- 成功时返回 data map（内嵌 `"success": true` + `"data": ...`）
- 软失败时返回 map（内嵌 `"success": false` + `"error": ...`），不返回 Go error
- 系统异常时返回 `(nil, error)`

与 Python 的 `ToolOutput(success, data, error)` 语义等价，但不引入独立结构体。

## 依赖关系

| 依赖包 | 用途 |
|--------|------|
| `foundation/tool` | Tool 接口 + ToolCard + ErrStreamNotSupported |
| `single_agent/skills` | Skill 结构体 |
| `sys_operation` | SysOperation 接口（SkillTool 读文件） |
| `foundation/llm/model` | Model 接口（ListSkillTool LLM 路由） |
| `harness/prompts/sections` | GetListSkillSystemPrompt 提示词 |
| `harness/prompts/tools` | build_tool_card / SkillToolMetadataProvider / ListSkillMetadataProvider |
| `foundation/llm/schema/message` | SystemMessage / UserMessage |

## 已有实现（不需要额外工作）

- ✅ `Skill` 结构体（`skills/skill.go`）
- ✅ `SkillManager`（`skills/skill_manager.go`）
- ✅ `SkillToolMetadataProvider`（`prompts/tools/skill_tool.go`）
- ✅ `ListSkillMetadataProvider`（`prompts/tools/list_skill.go`）
- ✅ 提示词 sections（`prompts/sections/skills.go`）
- ✅ SysOperation + FsOperation 接口（`sys_operation/sys_operation.go` + `fs.go`）
- ✅ Tool 基础框架（`foundation/tool/base.go`）
- ✅ Model 接口（`foundation/llm/model`）

## 后续回填（本次不做）

- ⤵️ 9.19-23 SkillUseRail — SkillTool/ListSkillTool 的注册/管理 Rail
- ⤵️ 9.19-23 SkillCreateRail / TeamSkillRail 等其他 Skill Rails

## 测试设计

### SkillTool 测试

| 测试名 | 场景 | 验证 |
|--------|------|------|
| `TestSkillTool_Invoke_技能存在` | skill_name 有效，默认 SKILL.md | success=true, data 包含 skill_directory + skill_content |
| `TestSkillTool_Invoke_指定文件路径` | skill_name + relative_file_path | 成功读取指定路径文件 |
| `TestSkillTool_Invoke_技能不存在` | skill_name 无效 | success=false, error="Skill not found: xxx" |
| `TestSkillTool_Invoke_文件读取失败` | 技能存在但 SysOperation.Fs().ReadFile 返回 Code!=0 | success=false, error 包含 message |
| `TestSkillTool_Invoke_技能名为空` | skill_name="" | success=false, error="Skill not found: " |
| `TestSkillTool_Invoke_异常捕获` | SysOperation 抛出异常 | success=false, error 包含异常信息 |
| `TestSkillTool_GetSkillByName` | 名称查找逻辑 | 找到返回 Skill，未找到返回 nil |
| `TestSkillTool_Stream_不支持` | 调用 Stream | 返回 ErrStreamNotSupported |

### ListSkillTool 测试

| 测试名 | 场景 | 验证 |
|--------|------|------|
| `TestListSkillTool_Invoke_无query返回全部` | query="" | success=true, mode="all", skills=全部列表 |
| `TestListSkillTool_Invoke_有query无model回退` | query有值, model=nil | success=true, mode="all", message="list_skill_model is not configured..." |
| `TestListSkillTool_Invoke_有query有model路由` | query+model | success=true, mode="filtered", selected_skill_names |
| `TestListSkillTool_Invoke_异常捕获` | 内部异常 | success=false, error 包含异常信息 |
| `TestListSkillTool_Stream_不支持` | 调用 Stream | 返回 ErrStreamNotSupported |
| `TestListSkillTool_DumpAllSkills` | 序列化所有技能 | 包含 name/description/directory/skill_md_path |
| `TestListSkillTool_DumpSkills` | 序列化选定技能 | 只包含指定技能 |
| `TestListSkillTool_ParseSelectedSkillNames_正常JSON` | 标准 JSON 输入 | 返回技能名列表 |
| `TestListSkillTool_ParseSelectedSkillNames_代码块` | ```json``` 包裹 | 正确去除标记并解析 |
| `TestListSkillTool_ParseSelectedSkillNames_空内容` | 空/无效内容 | 返回空列表 |
| `TestListSkillTool_ParseSelectedSkillNames_非数组skills` | skills 不是 list | 返回空列表 |
| `TestListSkillTool_SelectSkillsByNames` | 按名选择 | 只返回匹配的技能 |
| `TestListSkillTool_SelectSkillsByNames_空名` | names=[] | 返回空列表 |

### Mock 方式

- **SysOperation**：mock `FsOperation.ReadFile()` 返回预设 `ReadFileResult`
- **getSkills 回调**：返回固定技能列表
- **listSkillModel**：mock `Model.Invoke()` 返回预设 LLM 响应（含 JSON 格式技能名列表）

## 实现步骤（9.38 回填点）

本步骤在 IMPLEMENTATION_PLAN.md 中属于 9.38-49 Harness 工具集的 skills 子部分。完成后需要更新 9.38-49 的状态描述，将 `☐ Skills` 改为 `✅ Skills(SkillTool+ListSkillTool)`。

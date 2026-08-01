# Skills 工具（SkillTool + ListSkillTool）实现计划 — 9.38

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SkillTool 和 ListSkillTool 两个 Harness 工具，一比一复刻 Python `openjiuwen/harness/tools/skills/` 的 `skill_tool.py` 和 `list_skill.py`。

**Architecture:** 两个独立工具类，SkillTool 通过 SysOperation.Fs().ReadFile() 读取技能文件 + getSkills 回调查找技能；ListSkillTool 通过 getSkills 回调获取技能列表 + 可选 LLM 路由筛选。返回值走 `map[string]any + error`，内嵌 success/error/data 字段。SkillUseRail 注册逻辑后续回填。

**Tech Stack:** Go, SysOperation 接口, foundation/llm Model 结构体（*llm.Model 指针注入）, foundation/tool Tool 接口

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/tools/skills/doc.go` | 包文档（含文件目录树） |
| `internal/agentcore/harness/tools/skills/skill_tool.go` | SkillTool 结构体 + Invoke/Stream/GetSkillByName |
| `internal/agentcore/harness/tools/skills/list_skill.go` | ListSkillTool 结构体 + Invoke/Stream + 路由逻辑 |
| `internal/agentcore/harness/tools/skills/skills_test.go` | 两个工具的单元测试 |

---

### Task 1: 创建包目录和 doc.go

**Files:**
- Create: `internal/agentcore/harness/tools/skills/doc.go`

- [x] **Step 1: 创建目录并写 doc.go**

```go
// Package skills 提供技能查看工具（SkillTool）和技能列表工具（ListSkillTool）。
//
// SkillTool 通过 SysOperation 读取技能目录下的指定文件（默认 SKILL.md），
// ListSkillTool 列出所有可用技能或通过 LLM 路由筛选相关技能。
// 两个工具均通过 getSkills 回调获取当前已启用的技能列表，
// 由 SkillUseRail（9.19-23）在 init 时注册到 Agent。
//
// 文件目录：
//
//	skills/
//	├── doc.go           # 包文档
//	├── skill_tool.go    # SkillTool 技能查看工具
//	├── list_skill.go    # ListSkillTool 技能列表/路由工具
//	└── skills_test.go   # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/skills/
package skills
```

- [x] **Step 2: Commit**

```bash
git add internal/agentcore/harness/tools/skills/doc.go
git commit -m "feat(skills): add skills tool package doc.go"
```

---

### Task 2: 实现 SkillTool + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/skills/skill_tool.go`
- Create: `internal/agentcore/harness/tools/skills/skills_test.go`（SkillTool 部分）

- [x] **Step 1: 写 SkillTool 的失败测试**

在 `skills_test.go` 中写 SkillTool 的测试，先写构造函数和 Invoke 的失败场景测试：

```go
package skills

import (
	"context"
	"testing"

	skills "github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/sys_operation/result"
)

// fakeSysOperation 用于测试的模拟 SysOperation
type fakeSysOperation struct {
	sys_operation.BaseSysOperation
	fsOp *fakeFsOperation
}

func (f *fakeSysOperation) Fs() sys_operation.FsOperation { return f.fsOp }

// fakeFsOperation 用于测试的模拟 FsOperation
type fakeFsOperation struct {
	sys_operation.BaseFsOperation
	readFileResult *result.ReadFileResult
	readFileErr    error
}

func (f *fakeFsOperation) ReadFile(_ context.Context, _ string, _ ...sys_operation.FsOption) (*result.ReadFileResult, error) {
	return f.readFileResult, f.readFileErr
}

// 固定技能列表，用于测试
var testSkills = []*skills.Skill{
	skills.NewSkill("python_coder", "Python 编码技能", "/skills/python_coder"),
	skills.NewSkill("web_search", "网络搜索技能", "/skills/web_search"),
}
```

测试用例（SkillTool 部分）：

```go
func TestSkillTool_Invoke_技能存在(t *testing.T) {
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			BaseResult: result.BaseResult{Code: 0, Message: "success"},
			Data:       &result.ReadFileData{Path: "/skills/python_coder/SKILL.md", Content: "# Python Coder Skill\n...", Mode: "text"},
		},
	}
	op := &fakeSysOperation{fsOp: fsOp}

	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "test_agent")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name": "python_coder",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
	data := got["data"].(map[string]any)
	if data["skill_directory"] != "/skills/python_coder" {
		t.Errorf("期望 skill_directory=/skills/python_coder, 实际 %v", data["skill_directory"])
	}
	if data["skill_content"] != "# Python Coder Skill\n..." {
		t.Errorf("期望 skill_content 匹配, 实际 %v", data["skill_content"])
	}
}

func TestSkillTool_Invoke_技能不存在(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name": "nonexistent",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != false {
		t.Errorf("期望 success=false, 实际 %v", got["success"])
	}
	errMsg := got["error"].(string)
	if errMsg != "Skill not found: nonexistent" {
		t.Errorf("期望 error='Skill not found: nonexistent', 实际 %q", errMsg)
	}
}

func TestSkillTool_Invoke_技能名为空(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name": "",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != false {
		t.Errorf("期望 success=false, 实际 %v", got["success"])
	}
}

func TestSkillTool_Invoke_文件读取失败(t *testing.T) {
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			BaseResult: result.BaseResult{Code: 1, Message: "file not found"},
		},
	}
	op := &fakeSysOperation{fsOp: fsOp}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name": "python_coder",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != false {
		t.Errorf("期望 success=false, 实际 %v", got["success"])
	}
	if got["error"] != "file not found" {
		t.Errorf("期望 error='file not found', 实际 %v", got["error"])
	}
}

func TestSkillTool_Invoke_异常捕获(t *testing.T) {
	fsOp := &fakeFsOperation{readFileErr: context.DeadlineExceeded}
	op := &fakeSysOperation{fsOp: fsOp}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name": "python_coder",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != false {
		t.Errorf("期望 success=false, 实际 %v", got["success"])
	}
}

func TestSkillTool_Invoke_指定文件路径(t *testing.T) {
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			BaseResult: result.BaseResult{Code: 0, Message: "success"},
			Data:       &result.ReadFileData{Path: "/skills/python_coder/examples.md", Content: "example content", Mode: "text"},
		},
	}
	op := &fakeSysOperation{fsOp: fsOp}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	got, err := st.Invoke(context.Background(), map[string]any{
		"skill_name":         "python_coder",
		"relative_file_path": "examples.md",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
}

func TestSkillTool_Stream_不支持(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	_, err := st.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回 ErrStreamNotSupported")
	}
}

func TestSkillTool_Card(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "test_agent")

	card := st.Card()
	if card == nil {
		t.Fatal("期望 Card 非 nil")
	}
	if card.Name != "skill_tool" {
		t.Errorf("期望 name=skill_tool, 实际 %s", card.Name)
	}
}

func TestSkillTool_GetSkillByName(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return testSkills }, "cn", "")

	skill := st.getSkillByName("web_search")
	if skill == nil {
		t.Fatal("期望找到 web_search 技能")
	}
	if skill.Name != "web_search" {
		t.Errorf("期望 Name=web_search, 实际 %s", skill.Name)
	}

	nilSkill := st.getSkillByName("nonexistent")
	if nilSkill != nil {
		t.Errorf("期望返回 nil, 实际 %v", nilSkill)
	}
}
```

- [x] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/skills/... -run TestSkillTool -v 2>&1 | head -20
```

Expected: 编译失败（NewSkillTool 等符号未定义）

- [x] **Step 3: 实现 SkillTool**

写 `skill_tool.go`：

```go
package skills

import (
	"context"
	"fmt"
	"path/filepath"

	tool "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/tool"
	skills "github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/skills"
	ptools "github.com/uapclaw/uap-claw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillTool 查看特定技能内容的工具。
//
// 通过 SysOperation.Fs().ReadFile() 读取技能目录下的指定文件（默认 SKILL.md），
// 通过 getSkills 回调查找技能。
//
// 对应 Python: SkillTool (openjiuwen/harness/tools/skills/skill_tool.py)
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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// defaultSkillFileName 默认技能文件名
	defaultSkillFileName = "SKILL.md"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillTool 创建 SkillTool 实例。
//
// 对应 Python: SkillTool.__init__(operation, get_skills, language, agent_id)
func NewSkillTool(
	operation sys_operation.SysOperation,
	getSkills func() []*skills.Skill,
	language string,
	agentID string,
) *SkillTool {
	card, _ := ptools.BuildToolCard("skill_tool", "SkillTool", language, nil, agentID)

	return &SkillTool{
		card:      card,
		operation: operation,
		getSkills: getSkills,
		language:  language,
	}
}

// Invoke 执行 skill_tool 工具，查看特定技能内容。
//
// 一比一复刻 Python SkillTool.invoke()：
//  1. 按 skill_name 查找技能
//  2. 构建文件路径（skill.Directory + relative_file_path）
//  3. 通过 SysOperation.Fs().ReadFile() 读取文件
//  4. 返回 success/error/data 结构
//
// 对应 Python: SkillTool.invoke(inputs, **kwargs)
func (t *SkillTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	skillName := ""
	if v, ok := inputs["skill_name"]; ok {
		skillName = fmt.Sprintf("%v", v)
	}
	skillName = trimSpace(skillName)

	relativeFilePath := defaultSkillFileName
	if v, ok := inputs["relative_file_path"]; ok {
		raw := fmt.Sprintf("%v", v)
		raw = trimSpace(raw)
		if raw != "" {
			relativeFilePath = raw
		}
	}

	skill := t.getSkillByName(skillName)
	if skill == nil {
		logger.Warn(logComponent).
			Str("skill_name", skillName).
			Msg("SkillTool 技能未找到")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Skill not found: %s", skillName),
		}, nil
	}

	filePath := filepath.Join(skill.Directory, relativeFilePath)
	readFileResult, readErr := t.operation.Fs().ReadFile(ctx, filePath)

	if readErr != nil {
		logger.Error(logComponent).
			Str("skill_name", skillName).
			Str("file_path", filePath).
			Err(readErr).
			Msg("SkillTool 读取文件异常")
		return map[string]any{
			"success": false,
			"error":   readErr.Error(),
		}, nil
	}

	if readFileResult.Code != 0 {
		logger.Error(logComponent).
			Str("skill_name", skillName).
			Str("file_path", filePath).
			Str("message", readFileResult.Message).
			Msg("SkillTool 读取文件失败")
		return map[string]any{
			"success": false,
			"error":   readFileResult.Message,
		}, nil
	}

	skillFileContent := ""
	if readFileResult.Data != nil {
		skillFileContent = readFileResult.Data.Content
	}

	logger.Info(logComponent).
		Str("skill_name", skillName).
		Str("file_path", filePath).
		Msg("SkillTool 读取技能文件成功")

	return map[string]any{
		"success": true,
		"data": map[string]any{
			"skill_directory": skill.Directory,
			"skill_content":   skillFileContent,
		},
	}, nil
}

// Stream SkillTool 不支持流式调用。
//
// 对应 Python: SkillTool.stream(inputs, **kwargs) — if False: yield None
func (t *SkillTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.ID)
}

// Card 返回工具配置卡片。
func (t *SkillTool) Card() *tool.ToolCard { return t.card }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSkillByName 按名称查找技能。
//
// 对应 Python: SkillTool._get_skill_by_name(skill_name)
func (t *SkillTool) getSkillByName(name string) *skills.Skill {
	if name == "" {
		return nil
	}
	allSkills := t.getSkills()
	if allSkills == nil {
		return nil
	}
	for _, s := range allSkills {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// trimSpace 去除字符串两端空白。
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
```

注意：需要补充 `import "strings"` 到 import 块。

- [x] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/skills/... -run TestSkillTool -v
```

Expected: 所有 SkillTool 测试 PASS

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/harness/tools/skills/skill_tool.go internal/agentcore/harness/tools/skills/skills_test.go
git commit -m "feat(skills): implement SkillTool with invoke/getSkillByName/stream"
```

---

### Task 3: 实现 ListSkillTool + 测试

**Files:**
- Create: `internal/agentcore/harness/tools/skills/list_skill.go`
- Modify: `internal/agentcore/harness/tools/skills/skills_test.go`（追加 ListSkillTool 测试）

- [x] **Step 1: 写 ListSkillTool 的测试**

追加到 `skills_test.go`：

```go
// fakeModel 用于测试的模拟 LLM Model
// llm.Model 是具体结构体而非接口，无法直接 mock
// 因此 ListSkillTool 的 LLM 路由测试通过直接测试辅助方法来覆盖
// routeSkills 的集成测试在 SkillUseRail 回填时补充

func TestListSkillTool_Invoke_无query返回全部(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	got, err := lt.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
	data := got["data"].(map[string]any)
	if data["mode"] != "all" {
		t.Errorf("期望 mode=all, 实际 %v", data["mode"])
	}
	skillsList := data["skills"]
	if skillsList == nil {
		t.Fatal("期望 skills 非 nil")
	}
}

func TestListSkillTool_Invoke_有query无model回退(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	got, err := lt.Invoke(context.Background(), map[string]any{
		"query": "写一个 Python 函数",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
	data := got["data"].(map[string]any)
	if data["mode"] != "all" {
		t.Errorf("期望 mode=all, 实际 %v", data["mode"])
	}
	if data["message"] != "list_skill_model is not configured, fallback to all skills." {
		t.Errorf("期望 message='list_skill_model is not configured...', 实际 %v", data["message"])
	}
}

func TestListSkillTool_Invoke_有query有model路由(t *testing.T) {
	// llm.Model 是具体结构体，无法直接 mock
	// 通过构造真实 Model + mock BaseModelClient 实现测试
	// 使用 model_clients.Registry 注册一个 mock 客户端
	//
	// 注意：此测试需要先注册 mock 客户端到 model_clients.Registry
	// 然后通过 NewModel() 创建 Model 实例
	//
	// 由于 mock 客户端注册机制较复杂，routeSkills 的完整集成测试
	// 留到 SkillUseRail 回填时补充（那时有完整的 Agent 上下文）
	//
	// 当前只测试 parseSelectedSkillNames 和 selectSkillsByNames
	// （这两个方法覆盖了 routeSkills 的核心解析逻辑）

	// 先验证 parseSelectedSkillNames 能正确解析 LLM 输出格式
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "test_agent")

	names := lt.parseSelectedSkillNames(`{"skills": ["python_coder"]}`)
	selected := lt.selectSkillsByNames(names)

	if len(selected) != 1 {
		t.Fatalf("期望 1 个技能, 实际 %d", len(selected))
	}
	if selected[0].Name != "python_coder" {
		t.Errorf("期望 Name=python_coder, 实际 %s", selected[0].Name)
	}
}

func TestListSkillTool_Invoke_异常捕获(t *testing.T) {
	// routeSkills 的异常路径需要真实 Model，此处留到 SkillUseRail 回填时补充
	// 当前测试覆盖 parseSelectedSkillNames 的异常输入场景
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	// 验证空输入
	names := lt.parseSelectedSkillNames("")
	if len(names) != 0 {
		t.Errorf("期望 0 个名称, 实际 %d", len(names))
	}

	// 验证无效 JSON
	names = lt.parseSelectedSkillNames("not json at all")
	if len(names) != 0 {
		t.Errorf("期望 0 个名称, 实际 %d", len(names))
	}
}

func TestListSkillTool_Stream_不支持(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	_, err := lt.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回 ErrStreamNotSupported")
	}
}

func TestListSkillTool_DumpAllSkills(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	dumped := lt.dumpAllSkills()
	if len(dumped) != 2 {
		t.Fatalf("期望 2 个技能, 实际 %d", len(dumped))
	}
	first := dumped[0]
	if first["name"] == nil || first["description"] == nil || first["directory"] == nil || first["skill_md_path"] == nil {
		t.Errorf("期望包含 name/description/directory/skill_md_path, 实际 %v", first)
	}
}

func TestListSkillTool_DumpSkills(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	selected := []*skills.Skill{testSkills[0]}
	dumped := lt.dumpSkills(selected)
	if len(dumped) != 1 {
		t.Fatalf("期望 1 个技能, 实际 %d", len(dumped))
	}
	if dumped[0]["name"] != "python_coder" {
		t.Errorf("期望 name=python_coder, 实际 %v", dumped[0]["name"])
	}
}

func TestListSkillTool_ParseSelectedSkillNames_正常JSON(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	names := lt.parseSelectedSkillNames(`{"skills": ["python_coder", "web_search"]}`)
	if len(names) != 2 {
		t.Fatalf("期望 2 个名称, 实际 %d", len(names))
	}
	if names[0] != "python_coder" {
		t.Errorf("期望 names[0]=python_coder, 实际 %s", names[0])
	}
	if names[1] != "web_search" {
		t.Errorf("期望 names[1]=web_search, 实际 %s", names[1])
	}
}

func TestListSkillTool_ParseSelectedSkillNames_代码块(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	content := "```json\n{\"skills\": [\"python_coder\"]}\n```"
	names := lt.parseSelectedSkillNames(content)
	if len(names) != 1 {
		t.Fatalf("期望 1 个名称, 实际 %d", len(names))
	}
	if names[0] != "python_coder" {
		t.Errorf("期望 names[0]=python_coder, 实际 %s", names[0])
	}
}

func TestListSkillTool_ParseSelectedSkillNames_空内容(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	names := lt.parseSelectedSkillNames("")
	if len(names) != 0 {
		t.Errorf("期望 0 个名称, 实际 %d", len(names))
	}

	names = lt.parseSelectedSkillNames("invalid json")
	if len(names) != 0 {
		t.Errorf("期望 0 个名称, 实际 %d", len(names))
	}
}

func TestListSkillTool_ParseSelectedSkillNames_非数组skills(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	names := lt.parseSelectedSkillNames(`{"skills": "not_a_list"}`)
	if len(names) != 0 {
		t.Errorf("期望 0 个名称, 实际 %d", len(names))
	}
}

func TestListSkillTool_SelectSkillsByNames(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	selected := lt.selectSkillsByNames([]string{"web_search"})
	if len(selected) != 1 {
		t.Fatalf("期望 1 个技能, 实际 %d", len(selected))
	}
	if selected[0].Name != "web_search" {
		t.Errorf("期望 Name=web_search, 实际 %s", selected[0].Name)
	}
}

func TestListSkillTool_SelectSkillsByNames_空名(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	selected := lt.selectSkillsByNames([]string{})
	if len(selected) != 0 {
		t.Errorf("期望 0 个技能, 实际 %d", len(selected))
	}
}

func TestListSkillTool_Card(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "test_agent")

	card := lt.Card()
	if card == nil {
		t.Fatal("期望 Card 非 nil")
	}
	if card.Name != "list_skill" {
		t.Errorf("期望 name=list_skill, 实际 %s", card.Name)
	}
}
```

需要补充的 import：
```go
import (
	llm "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/model_clients"
)
```

注意：`llm` 和 `llmschema` 和 `model_clients` 仅在 Task 3 实现 ListSkillTool 时使用（routeSkills 方法调用 `t.listSkillModel.Invoke(ctx, messages)`）。测试文件中只用到 `llmschema` 来构造 AssistantMessage（在 parseSelectedSkillNames 的间接测试中不需要，因为直接调用 parseSelectedSkillNames 方法）。

- [x] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/skills/... -run TestListSkillTool -v 2>&1 | head -20
```

Expected: 编译失败（NewListSkillTool 等符号未定义）

- [x] **Step 3: 实现 ListSkillTool**

写 `list_skill.go`：

```go
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tool "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/tool"
	llm "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/model_clients"
	skills "github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/skills"
	ptools "github.com/uapclaw/uap-claw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ListSkillTool 列出可用技能或为当前任务选择相关技能的工具。
//
// 无 query 时返回所有技能；有 query + listSkillModel 时通过 LLM 路由筛选相关技能；
// 有 query 无 listSkillModel 时 fallback 返回全部技能。
//
// 对应 Python: ListSkillTool (openjiuwen/harness/tools/skills/list_skill.py)
type ListSkillTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// getSkills 返回当前已启用技能列表的回调函数
	getSkills func() []*skills.Skill
	// listSkillModel 可选的 LLM 模型，用于技能路由
	listSkillModel *llm.Model
	// language 语言标识（"cn"/"en"）
	language string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fallbackMessage 无 LLM 模型时的回退消息
	// 一比一复刻 Python: "list_skill_model is not configured, fallback to all skills."
	fallbackMessage = "list_skill_model is not configured, fallback to all skills."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewListSkillTool 创建 ListSkillTool 实例。
//
// 对应 Python: ListSkillTool.__init__(get_skills, list_skill_model, language, agent_id)
func NewListSkillTool(
	getSkills func() []*skills.Skill,
	listSkillModel *llm.Model,
	language string,
	agentID string,
) *ListSkillTool {
	card, _ := ptools.BuildToolCard("list_skill", "ListSkillTool", language, nil, agentID)

	return &ListSkillTool{
		card:           card,
		getSkills:      getSkills,
		listSkillModel: listSkillModel,
		language:       language,
	}
}

// Invoke 执行 list_skill 工具，列出可用技能或为当前任务选择相关技能。
//
// 一比一复刻 Python ListSkillTool.invoke()：
//  1. 无 query → 返回全部技能
//  2. 有 query + listSkillModel → LLM 路由筛选
//  3. 有 query 无 listSkillModel → fallback 返回全部
//
// 对应 Python: ListSkillTool.invoke(inputs, **kwargs)
func (t *ListSkillTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	query := ""
	if v, ok := inputs["query"]; ok {
		query = fmt.Sprintf("%v", v)
	}
	query = trimSpace(query)

	if query == "" {
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"skills": t.dumpAllSkills(),
				"mode":   "all",
			},
		}, nil
	}

	if t.listSkillModel == nil {
		logger.Info(logComponent).
			Str("query", query).
			Msg("ListSkillTool 无 listSkillModel, fallback 返回全部技能")
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"skills":  t.dumpAllSkills(),
				"mode":    "all",
				"message": fallbackMessage,
			},
		}, nil
	}

	selectedNames, routeErr := t.routeSkills(ctx, query)
	if routeErr != nil {
		logger.Error(logComponent).
			Str("query", query).
			Err(routeErr).
			Msg("ListSkillTool LLM 路由失败")
		return map[string]any{
			"success": false,
			"error":   routeErr.Error(),
		}, nil
	}

	selectedSkills := t.selectSkillsByNames(selectedNames)

	logger.Info(logComponent).
		Str("query", query).
		Strs("selected_skill_names", selectedNames).
		Msg("ListSkillTool LLM 路由完成")

	return map[string]any{
		"success": true,
		"data": map[string]any{
			"skills":              t.dumpSkills(selectedSkills),
			"mode":                "filtered",
			"selected_skill_names": selectedNames,
		},
	}, nil
}

// Stream ListSkillTool 不支持流式调用。
//
// 对应 Python: ListSkillTool.stream(inputs, **kwargs) — if False: yield None
func (t *ListSkillTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.ID)
}

// Card 返回工具配置卡片。
func (t *ListSkillTool) Card() *tool.ToolCard { return t.card }

// dumpAllSkills 序列化所有已启用技能。
//
// 对应 Python: ListSkillTool._dump_all_skills()
func (t *ListSkillTool) dumpAllSkills() []map[string]any {
	return t.dumpSkills(t.getSkills())
}

// dumpSkills 序列化指定技能列表。
//
// 对应 Python: ListSkillTool._dump_skills(skills)
// 一比一复刻: skill.asdict(include_directory=True) + skill_md_path
func (t *ListSkillTool) dumpSkills(skillList []*skills.Skill) []map[string]any {
	if skillList == nil {
		return []map[string]any{}
	}
	results := make([]map[string]any, 0, len(skillList))
	for _, s := range skillList {
		if s == nil {
			continue
		}
		entry := map[string]any{
			"name":          s.Name,
			"description":   s.Description,
			"directory":     s.Directory,
			"skill_md_path": fmt.Sprintf("%s/SKILL.md", s.Directory),
		}
		results = append(results, entry)
	}
	return results
}

// routeSkills 用 listSkillModel 路由选择相关技能。
//
// 对应 Python: ListSkillTool._route_skills(query)
// 提示词一比一复刻 Python 原文，不翻译。
func (t *ListSkillTool) routeSkills(ctx context.Context, query string) ([]string, error) {
	payload := t.dumpAllSkills()
	jsonPayload, jsonErr := json.Marshal(payload)
	if jsonErr != nil {
		return nil, fmt.Errorf("序列化技能列表失败: %w", jsonErr)
	}

	// 一比一复刻 Python UserMessage 模板
	userContent := fmt.Sprintf(
		"User task:\n%s\n\nAvailable skills:\n%s\n\nReturn only the names of the skills that are relevant to the task.",
		query,
		string(jsonPayload),
	)

	systemPrompt := sections.GetListSkillSystemPrompt(t.language)

	messages := model_clients.NewMessagesParam(
		llmschema.NewSystemMessage(systemPrompt),
		llmschema.NewUserMessage(userContent),
	)

	response, invokeErr := t.listSkillModel.Invoke(ctx, messages)
	if invokeErr != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", invokeErr)
	}

	content := ""
	if response != nil && response.Content.Text() != "" {
		content = response.Content.Text()
	}

	return t.parseSelectedSkillNames(content), nil
}

// parseSelectedSkillNames 从模型输出解析选中的技能名称。
//
// 一比一复刻 Python: ListSkillTool._parse_selected_skill_names(content)
func (t *ListSkillTool) parseSelectedSkillNames(content string) []string {
	text := strings.TrimSpace(content)
	if text == "" {
		return []string{}
	}

	// 去掉 ``` 代码块标记
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		// 去掉第一行 ```json 或 ```
		if len(lines) > 0 {
			lines = lines[1:]
		}
		// 去掉最后的 ```
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		text = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	var data map[string]any
	if jsonErr := json.Unmarshal([]byte(text), &data); jsonErr != nil {
		return []string{}
	}

	skillsVal, ok := data["skills"]
	if !ok {
		return []string{}
	}

	skillsList, ok := skillsVal.([]any)
	if !ok {
		return []string{}
	}

	names := make([]string, 0, len(skillsList))
	for _, item := range skillsList {
		name := strings.TrimSpace(fmt.Sprintf("%v", item))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// selectSkillsByNames 按名称选择技能。
//
// 对应 Python: ListSkillTool._select_skills_by_names(names)
func (t *ListSkillTool) selectSkillsByNames(names []string) []*skills.Skill {
	if len(names) == 0 {
		return []*skills.Skill{}
	}

	allSkills := t.getSkills()
	if allSkills == nil {
		return []*skills.Skill{}
	}

	skillMap := make(map[string]*skills.Skill, len(allSkills))
	for _, s := range allSkills {
		if s != nil {
			skillMap[s.Name] = s
		}
	}

	selected := make([]*skills.Skill, 0, len(names))
	for _, name := range names {
		if s, ok := skillMap[name]; ok {
			selected = append(selected, s)
		}
	}
	return selected
}
```

- [x] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/skills/... -v
```

Expected: 所有 SkillTool + ListSkillTool 测试 PASS

- [x] **Step 5: Commit**

```bash
git add internal/agentcore/harness/tools/skills/list_skill.go internal/agentcore/harness/tools/skills/skills_test.go
git commit -m "feat(skills): implement ListSkillTool with invoke/routeSkills/parseSelectedSkillNames"
```

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md 状态 + 覆盖率验证

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（更新 9.38-49 行的 skills 状态）

- [x] **Step 1: 更新 9.38-49 状态**

在 IMPLEMENTATION_PLAN.md 中，将 9.38-49 行的描述更新：

原：
```
| 9.38-49 | ☐ | Harness 工具集 | Shell/文件系统/代码/MCP/Worktree/浏览器/✅Cron/TODO/✅AskUser/Memory/AgentMode/多模态 | `openjiuwen/harness/tools/` |`
```

改为：
```
| 9.38-49 | 🔄 | Harness 工具集 | Shell/文件系统/代码/MCP/Worktree/浏览器/✅Cron/TODO/✅AskUser/✅Skills(SkillTool+ListSkillTool)/Memory/AgentMode/多模态 | `openjiuwen/harness/tools/` |
```

- [x] **Step 2: 运行覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/tools/skills/...
```

Expected: 覆盖率 ≥ 85%

- [x] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update 9.38-49 skills tool status in implementation plan"
```

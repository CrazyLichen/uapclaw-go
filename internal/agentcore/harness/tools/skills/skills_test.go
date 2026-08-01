package skills

import (
	"context"
	"encoding/json"
	"testing"

	skills "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSysOperation 用于测试的模拟 SysOperation
type fakeSysOperation struct {
	sys_operation.BaseSysOperation
	fsOp *fakeFsOperation
}

// fakeFsOperation 用于测试的模拟 FsOperation
type fakeFsOperation struct {
	sys_operation.BaseFsOperation
	readFileResult *result.ReadFileResult
	readFileErr    error
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeSysOperation) Fs() sys_operation.FsOperation { return f.fsOp }

func (f *fakeFsOperation) ReadFile(_ context.Context, _ string, _ ...sys_operation.FsOption) (*result.ReadFileResult, error) {
	return f.readFileResult, f.readFileErr
}

// ──────────────────────────── 全局变量 ────────────────────────────

// testSkills 固定技能列表，用于测试
var testSkills = []*skills.Skill{
	skills.NewSkill("python_coder", "Python 编码技能", "/skills/python_coder"),
	skills.NewSkill("web_search", "网络搜索技能", "/skills/web_search"),
}

// ──────────────────────────── SkillTool 测试 ────────────────────────────

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
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("期望 data 为 map[string]any, 实际 %T", got["data"])
	}
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
	errMsg, ok := got["error"].(string)
	if !ok {
		t.Fatalf("期望 error 为 string, 实际 %T", got["error"])
	}
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

	emptySkill := st.getSkillByName("")
	if emptySkill != nil {
		t.Errorf("期望空名返回 nil, 实际 %v", emptySkill)
	}
}

// ──────────────────────────── ListSkillTool 测试 ────────────────────────────

func TestListSkillTool_Invoke_无Query返回全部(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "test_agent")

	got, err := lt.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("期望 data 为 map[string]any, 实际 %T", got["data"])
	}
	if data["mode"] != "all" {
		t.Errorf("期望 mode=all, 实际 %v", data["mode"])
	}
	skillsList, ok := data["skills"].([]map[string]any)
	if !ok {
		t.Fatalf("期望 skills 为 []map[string]any, 实际 %T", data["skills"])
	}
	if len(skillsList) != 2 {
		t.Errorf("期望 2 个技能, 实际 %d", len(skillsList))
	}
}

func TestListSkillTool_Invoke_有Query无Model回退全部(t *testing.T) {
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
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("期望 data 为 map[string]any, 实际 %T", got["data"])
	}
	if data["mode"] != "all" {
		t.Errorf("期望 mode=all, 实际 %v", data["mode"])
	}
	msg, ok := data["message"].(string)
	if !ok {
		t.Fatalf("期望 message 为 string, 实际 %T", data["message"])
	}
	if msg != "list_skill_model is not configured, fallback to all skills." {
		t.Errorf("期望 fallback message, 实际 %q", msg)
	}
}

func TestListSkillTool_Invoke_Query为空字符串返回全部(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	got, err := lt.Invoke(context.Background(), map[string]any{
		"query": "  ",
	})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	data := got["data"].(map[string]any)
	if data["mode"] != "all" {
		t.Errorf("空格 query 应视为无 query, 期望 mode=all, 实际 %v", data["mode"])
	}
}

func TestListSkillTool_Stream_不支持(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	_, err := lt.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回 ErrStreamNotSupported")
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

func TestListSkillTool_DumpAllSkills(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	dumped := lt.dumpAllSkills()
	if len(dumped) != 2 {
		t.Fatalf("期望 2 个技能, 实际 %d", len(dumped))
	}

	// 一比一复刻 Python: skill.asdict(include_directory=True) + skill_md_path
	first := dumped[0]
	if first["name"] != "python_coder" {
		t.Errorf("期望 name=python_coder, 实际 %v", first["name"])
	}
	if first["description"] != "Python 编码技能" {
		t.Errorf("期望 description=Python 编码技能, 实际 %v", first["description"])
	}
	if first["directory"] != "/skills/python_coder" {
		t.Errorf("期望 directory=/skills/python_coder, 实际 %v", first["directory"])
	}
	if first["skill_md_path"] != "/skills/python_coder/SKILL.md" {
		t.Errorf("期望 skill_md_path=/skills/python_coder/SKILL.md, 实际 %v", first["skill_md_path"])
	}
}

func TestListSkillTool_DumpSkills(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	single := []*skills.Skill{testSkills[0]}
	dumped := lt.dumpSkills(single)
	if len(dumped) != 1 {
		t.Fatalf("期望 1 个技能, 实际 %d", len(dumped))
	}
	if dumped[0]["skill_md_path"] != "/skills/python_coder/SKILL.md" {
		t.Errorf("期望 skill_md_path, 实际 %v", dumped[0]["skill_md_path"])
	}
}

func TestListSkillTool_SelectSkillsByNames(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	// 正常选择
	selected := lt.selectSkillsByNames([]string{"python_coder", "web_search"})
	if len(selected) != 2 {
		t.Fatalf("期望 2 个, 实际 %d", len(selected))
	}
	if selected[0].Name != "python_coder" {
		t.Errorf("期望 python_coder, 实际 %s", selected[0].Name)
	}

	// 部分匹配
	partial := lt.selectSkillsByNames([]string{"python_coder", "nonexistent"})
	if len(partial) != 1 {
		t.Fatalf("期望 1 个匹配, 实际 %d", len(partial))
	}

	// 空名称列表
	empty := lt.selectSkillsByNames([]string{})
	if len(empty) != 0 {
		t.Errorf("期望 0 个, 实际 %d", len(empty))
	}

	// 全部不匹配
	none := lt.selectSkillsByNames([]string{"unknown"})
	if len(none) != 0 {
		t.Errorf("期望 0 个, 实际 %d", len(none))
	}
}

// ──────────────────────────── parseSelectedSkillNames 测试 ────────────────────────────

func TestParseSelectedSkillNames_正常JSON(t *testing.T) {
	content := `{"skills": ["python_coder", "web_search"]}`
	names := parseSelectedSkillNames(content)
	if len(names) != 2 {
		t.Fatalf("期望 2 个, 实际 %d", len(names))
	}
	if names[0] != "python_coder" {
		t.Errorf("期望 python_coder, 实际 %s", names[0])
	}
	if names[1] != "web_search" {
		t.Errorf("期望 web_search, 实际 %s", names[1])
	}
}

func TestParseSelectedSkillNames_代码块包裹(t *testing.T) {
	// 一比一复刻 Python: 去除 ```json 和 ``` 包裹
	content := "```json\n{\"skills\": [\"python_coder\"]}\n```"
	names := parseSelectedSkillNames(content)
	if len(names) != 1 {
		t.Fatalf("期望 1 个, 实际 %d", len(names))
	}
	if names[0] != "python_coder" {
		t.Errorf("期望 python_coder, 实际 %s", names[0])
	}
}

func TestParseSelectedSkillNames_空内容(t *testing.T) {
	names := parseSelectedSkillNames("")
	if len(names) != 0 {
		t.Errorf("期望 0 个, 实际 %d", len(names))
	}

	names = parseSelectedSkillNames("   ")
	if len(names) != 0 {
		t.Errorf("期望 0 个, 实际 %d", len(names))
	}
}

func TestParseSelectedSkillNames_无效JSON(t *testing.T) {
	names := parseSelectedSkillNames("not json at all")
	if len(names) != 0 {
		t.Errorf("无效 JSON 期望 0 个, 实际 %d", len(names))
	}
}

func TestParseSelectedSkillNames_无Skills字段(t *testing.T) {
	content := `{"other": "data"}`
	names := parseSelectedSkillNames(content)
	if len(names) != 0 {
		t.Errorf("无 skills 字段期望 0 个, 实际 %d", len(names))
	}
}

func TestParseSelectedSkillNames_Skills非列表(t *testing.T) {
	content := `{"skills": "not_a_list"}`
	names := parseSelectedSkillNames(content)
	if len(names) != 0 {
		t.Errorf("skills 非列表期望 0 个, 实际 %d", len(names))
	}
}

func TestParseSelectedSkillNames_过滤空字符串(t *testing.T) {
	// 一比一复刻 Python: [str(item).strip() for item in skills if str(item).strip()]
	content := `{"skills": ["python_coder", "", "  ", "web_search"]}`
	names := parseSelectedSkillNames(content)
	if len(names) != 2 {
		t.Fatalf("期望 2 个（过滤空串）, 实际 %d", len(names))
	}
	if names[0] != "python_coder" {
		t.Errorf("期望 python_coder, 实际 %s", names[0])
	}
	if names[1] != "web_search" {
		t.Errorf("期望 web_search, 实际 %s", names[1])
	}
}

func TestParseSelectedSkillNames_代码块无语言标记(t *testing.T) {
	// 一比一复刻 Python: ``` 开头但无语言标记
	content := "```\n{\"skills\": [\"web_search\"]}\n```"
	names := parseSelectedSkillNames(content)
	if len(names) != 1 {
		t.Fatalf("期望 1 个, 实际 %d", len(names))
	}
	if names[0] != "web_search" {
		t.Errorf("期望 web_search, 实际 %s", names[0])
	}
}

func TestListSkillTool_Invoke_getSkills返回nil(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return nil }, nil, "cn", "")

	got, err := lt.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	if got["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", got["success"])
	}
	data := got["data"].(map[string]any)
	skillsList := data["skills"].([]map[string]any)
	if len(skillsList) != 0 {
		t.Errorf("期望 0 个技能, 实际 %d", len(skillsList))
	}
}

func TestListSkillTool_DumpSkills_SkillMdPath(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	dumped := lt.dumpAllSkills()
	// 验证 skill_md_path 字段一比一复刻 Python: Path(skill.directory) / "SKILL.md"
	for _, d := range dumped {
		dir, _ := d["directory"].(string)
		expectedPath := dir + "/SKILL.md"
		actualPath, _ := d["skill_md_path"].(string)
		if actualPath != expectedPath {
			t.Errorf("skill_md_path: 期望 %s, 实际 %s", expectedPath, actualPath)
		}
	}

	// 验证可序列化为 JSON（一比一复刻 Python: json.dumps(payload)）
	_, jsonErr := json.Marshal(dumped)
	if jsonErr != nil {
		t.Fatalf("技能列表应可 JSON 序列化: %v", jsonErr)
	}
}

func TestSkillTool_GetSkillByName_getSkills返回nil(t *testing.T) {
	op := &fakeSysOperation{fsOp: &fakeFsOperation{}}
	st := NewSkillTool(op, func() []*skills.Skill { return nil }, "cn", "")

	skill := st.getSkillByName("python_coder")
	if skill != nil {
		t.Errorf("getSkills 返回 nil 时期望返回 nil, 实际 %v", skill)
	}
}

func TestListSkillTool_SelectSkillsByNames_getSkills返回nil(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return nil }, nil, "cn", "")

	selected := lt.selectSkillsByNames([]string{"python_coder"})
	if len(selected) != 0 {
		t.Errorf("getSkills 返回 nil 时期望 0 个匹配, 实际 %d", len(selected))
	}
}

func TestListSkillTool_Invoke_inputs无query字段(t *testing.T) {
	lt := NewListSkillTool(func() []*skills.Skill { return testSkills }, nil, "cn", "")

	got, err := lt.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回 Go error: %v", err)
	}
	data := got["data"].(map[string]any)
	if data["mode"] != "all" {
		t.Errorf("无 query 字段应视为无 query, 期望 mode=all, 实际 %v", data["mode"])
	}
}

// TODO: routeSkills 集成测试延后到 SkillUseRail（9.19-23）实现时，
// 因为 llm.Model 是具体结构体（非接口），无法在单元测试中 mock。
//届时将用真实 LLM API 调用配合 //go:build llm 标签进行端到端验证。

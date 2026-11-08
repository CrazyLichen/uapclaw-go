package fileguard

import "testing"

// TestLookupFileToolSpecs_内置18条 测试内置工具都有 spec
func TestLookupFileToolSpecs_内置18条(t *testing.T) {
	cases := map[string]struct {
		wantCount int
		wantFirst FileToolSpec
	}{
		"read_file":        {2, FileToolSpec{ToolName: "read_file", ArgName: "file_path", Action: FileActionRead}},
		"write_file":       {2, FileToolSpec{ToolName: "write_file", ArgName: "file_path", Action: FileActionWrite}},
		"edit_file":        {1, FileToolSpec{ToolName: "edit_file", ArgName: "file_path", Action: FileActionWrite}},
		"read_text_file":   {1, FileToolSpec{ToolName: "read_text_file", ArgName: "file_path", Action: FileActionRead}},
		"write_text_file":  {1, FileToolSpec{ToolName: "write_text_file", ArgName: "file_path", Action: FileActionWrite}},
		"write":            {1, FileToolSpec{ToolName: "write", ArgName: "file_path", Action: FileActionWrite}},
		"read":             {1, FileToolSpec{ToolName: "read", ArgName: "file_path", Action: FileActionRead}},
		"search_replace":   {3, FileToolSpec{ToolName: "search_replace", ArgName: "file_path", Action: FileActionWrite}},
		"grep":             {2, FileToolSpec{ToolName: "grep", ArgName: "path", Action: FileActionRead}},
		"glob_file_search": {1, FileToolSpec{ToolName: "glob_file_search", ArgName: "glob_pattern", Action: FileActionRead}},
		"glob":             {1, FileToolSpec{ToolName: "glob", ArgName: "pattern", Action: FileActionRead}},
		"list_dir":         {1, FileToolSpec{ToolName: "list_dir", ArgName: "path", Action: FileActionRead}},
		"list_files":       {1, FileToolSpec{ToolName: "list_files", ArgName: "path", Action: FileActionRead}},
	}
	for tool, want := range cases {
		specs := LookupFileToolSpecs(tool)
		if len(specs) != want.wantCount {
			t.Errorf("LookupFileToolSpecs(%q) 数量 = %d, want %d", tool, len(specs), want.wantCount)
			continue
		}
		if specs[0] != want.wantFirst {
			t.Errorf("LookupFileToolSpecs(%q)[0] = %+v, want %+v", tool, specs[0], want.wantFirst)
		}
	}
}

// TestLookupFileToolSpecs_总条数18 测试内置 spec 总数与 Python 一致
func TestLookupFileToolSpecs_总条数18(t *testing.T) {
	want := map[string]int{
		"read_file": 2, "write_file": 2, "edit_file": 1, "read_text_file": 1,
		"write_text_file": 1, "write": 1, "read": 1, "search_replace": 3,
		"grep": 2, "glob_file_search": 1, "glob": 1, "list_dir": 1, "list_files": 1,
	}
	total := 0
	for tool, n := range want {
		got := LookupFileToolSpecs(tool)
		if len(got) != n {
			t.Errorf("%s: %d 条, want %d", tool, len(got), n)
		}
		total += len(got)
	}
	if total != 18 {
		t.Errorf("内置 spec 总数 = %d, want 18", total)
	}
}

// TestLookupFileToolSpecs_未知工具 测试未注册工具返回 nil
func TestLookupFileToolSpecs_未知工具(t *testing.T) {
	if specs := LookupFileToolSpecs("no_such_tool"); specs != nil {
		t.Errorf("LookupFileToolSpecs(no_such_tool) = %v, want nil", specs)
	}
}

// TestRegisterFileTool_同名参数替换 测试同 arg_name 重复注册时替换而非追加
func TestRegisterFileTool_同名参数替换(t *testing.T) {
	const tool = "test_tool_replace"
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionRead})
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionWrite})

	specs := LookupFileToolSpecs(tool)
	if len(specs) != 1 {
		t.Fatalf("数量 = %d, want 1（同 arg_name 应替换）", len(specs))
	}
	if specs[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write（后者胜出）", specs[0].Action)
	}
}

// TestRegisterFileTool_不同参数追加 测试不同 arg_name 追加
func TestRegisterFileTool_不同参数追加(t *testing.T) {
	const tool = "test_tool_append"
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "path", Action: FileActionRead})
	RegisterFileTool(FileToolSpec{ToolName: tool, ArgName: "file_path", Action: FileActionRead})

	if specs := LookupFileToolSpecs(tool); len(specs) != 2 {
		t.Errorf("数量 = %d, want 2", len(specs))
	}
}

// TestLookupFileToolSpecs_返回副本 测试修改返回值不影响注册表
func TestLookupFileToolSpecs_返回副本(t *testing.T) {
	specs := LookupFileToolSpecs("read_file")
	specs[0].ArgName = "hacked"
	again := LookupFileToolSpecs("read_file")
	if again[0].ArgName == "hacked" {
		t.Error("返回的是共享切片，修改影响了注册表")
	}
}

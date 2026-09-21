package coding_memory

import (
	"context"
	"testing"

	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// TestCreateCodingMemoryTools_默认 验证返回 3 个 Tool
func TestCreateCodingMemoryTools_默认(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")

	if len(tools) != 3 {
		t.Fatalf("期望 3 个 Tool，实际 %d 个", len(tools))
	}

	// 验证类型
	if _, ok := tools[0].(*CodingMemoryReadTool); !ok {
		t.Errorf("tools[0] 期望 CodingMemoryReadTool，实际 %T", tools[0])
	}
	if _, ok := tools[1].(*CodingMemoryWriteTool); !ok {
		t.Errorf("tools[1] 期望 CodingMemoryWriteTool，实际 %T", tools[1])
	}
	if _, ok := tools[2].(*CodingMemoryEditTool); !ok {
		t.Errorf("tools[2] 期望 CodingMemoryEditTool，实际 %T", tools[2])
	}

	// 验证 Card 非空
	for i, tool := range tools {
		if tool.Card() == nil {
			t.Errorf("tools[%d].Card() 不应为 nil", i)
		}
	}
}

// TestCodingMemoryReadTool_Invoke_路径缺失 path 为空返回错误
func TestCodingMemoryReadTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	readTool := tools[0].(*CodingMemoryReadTool)

	_, err := readTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}
}

// TestCodingMemoryWriteTool_Invoke_路径缺失 path 为空返回错误
func TestCodingMemoryWriteTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	writeTool := tools[1].(*CodingMemoryWriteTool)

	_, err := writeTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}

	// path 存在但 content 缺失
	_, err = writeTool.Invoke(context.Background(), map[string]any{"path": "test.md"})
	if err == nil {
		t.Fatal("content 缺失时应返回错误")
	}
}

// TestCodingMemoryEditTool_Invoke_路径缺失 path/old_text/new_text 为空返回错误
func TestCodingMemoryEditTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	editTool := tools[2].(*CodingMemoryEditTool)

	// path 缺失
	_, err := editTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}

	// old_text 缺失
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md"})
	if err == nil {
		t.Fatal("old_text 缺失时应返回错误")
	}

	// new_text 缺失
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md", "old_text": "old"})
	if err == nil {
		t.Fatal("new_text 缺失时应返回错误")
	}
}

// TestCodingReadResultToMap_全部字段 测试完整结果转 map
func TestCodingReadResultToMap_全部字段(t *testing.T) {
	r := &lite.CodingReadResult{
		Success:     true,
		Path:        "/test/path.md",
		Content:     "hello world",
		TotalLines:  10,
		StartLine:   1,
		EndLine:     5,
		Truncated:   true,
		Error:       "",
	}
	m := codingReadResultToMap(r)
	if m["success"] != true {
		t.Error("success 应为 true")
	}
	if m["path"] != "/test/path.md" {
		t.Error("path 不匹配")
	}
	if m["content"] != "hello world" {
		t.Error("content 不匹配")
	}
	if m["total_lines"] != 10 {
		t.Error("total_lines 不匹配")
	}
	if m["start_line"] != 1 {
		t.Error("start_line 不匹配")
	}
	if m["end_line"] != 5 {
		t.Error("end_line 不匹配")
	}
	if m["truncated"] != true {
		t.Error("truncated 应为 true")
	}
	if _, ok := m["error"]; ok {
		t.Error("空 error 不应出现在 map 中")
	}
}

// TestCodingReadResultToMap_最小字段 测试仅必填字段
func TestCodingReadResultToMap_最小字段(t *testing.T) {
	r := &lite.CodingReadResult{
		Success: false,
		Path:    "/err.md",
		Error:   "读取失败",
	}
	m := codingReadResultToMap(r)
	if m["success"] != false {
		t.Error("success 应为 false")
	}
	if m["error"] != "读取失败" {
		t.Error("error 不匹配")
	}
	if _, ok := m["content"]; ok {
		t.Error("空 content 不应出现在 map 中")
	}
	if _, ok := m["total_lines"]; ok {
		t.Error("0 total_lines 不应出现在 map 中")
	}
}

// TestCodingEditResultToMap_全部字段 测试完整编辑结果转 map
func TestCodingEditResultToMap_全部字段(t *testing.T) {
	r := &lite.CodingEditResult{
		Success:     true,
		Path:        "/test/path.md",
		NewContent:  "new content",
		Error:       "",
	}
	m := codingEditResultToMap(r)
	if m["success"] != true {
		t.Error("success 应为 true")
	}
	if m["path"] != "/test/path.md" {
		t.Error("path 不匹配")
	}
	if m["new_content"] != "new content" {
		t.Error("new_content 不匹配")
	}
	if _, ok := m["error"]; ok {
		t.Error("空 error 不应出现在 map 中")
	}
}

// TestCodingEditResultToMap_仅成功字段 测试仅成功标记
func TestCodingEditResultToMap_仅成功字段(t *testing.T) {
	r := &lite.CodingEditResult{
		Success: false,
		Error:   "编辑失败",
	}
	m := codingEditResultToMap(r)
	if m["success"] != false {
		t.Error("success 应为 false")
	}
	if m["error"] != "编辑失败" {
		t.Error("error 不匹配")
	}
	if _, ok := m["path"]; ok {
		t.Error("空 path 不应出现在 map 中")
	}
}

// TestCodingMemoryStream_不支持 测试 Stream 方法返回不支持错误
func TestCodingMemoryStream_不支持(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")

	readTool := tools[0].(*CodingMemoryReadTool)
	_, err := readTool.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Error("ReadTool.Stream 应返回错误")
	}

	writeTool := tools[1].(*CodingMemoryWriteTool)
	_, err = writeTool.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Error("WriteTool.Stream 应返回错误")
	}

	editTool := tools[2].(*CodingMemoryEditTool)
	_, err = editTool.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Error("EditTool.Stream 应返回错误")
	}
}

package result

import (
	"encoding/json"
	"testing"
)

// TestExecuteCodeResult_构造 测试 ExecuteCodeResult 构造与 JSON 序列化
func TestExecuteCodeResult_构造(t *testing.T) {
	exitCode := 0
	r := ExecuteCodeResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &ExecuteCodeData{
			CodeContent: "print('hello')",
			Language:    "python",
			ExitCode:    &exitCode,
			Stdout:      "hello\n",
			Stderr:      "",
		},
	}
	if !r.IsSuccess() {
		t.Error("应为成功")
	}
	if r.Data.Language != "python" {
		t.Errorf("期望 Language='python'，实际 %q", r.Data.Language)
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ExecuteCodeResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Data.CodeContent != "print('hello')" {
		t.Errorf("期望 CodeContent='print(hello)'，实际 %q", decoded.Data.CodeContent)
	}
}

// TestExecuteCodeChunkData_JSON 测试流式块数据序列化
func TestExecuteCodeChunkData_JSON(t *testing.T) {
	stdoutType := "stdout"
	chunk := ExecuteCodeChunkData{
		Text:       "output line",
		Type:       &stdoutType,
		ChunkIndex: 1,
		ExitCode:   nil,
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ExecuteCodeChunkData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Text != "output line" {
		t.Errorf("期望 Text='output line'，实际 %q", decoded.Text)
	}
	if decoded.ChunkIndex != 1 {
		t.Errorf("期望 ChunkIndex=1，实际 %d", decoded.ChunkIndex)
	}
}

// TestExecuteCodeResult_错误 测试错误结果构造
func TestExecuteCodeResult_错误(t *testing.T) {
	exitCode := 1
	r := ExecuteCodeResult{
		BaseResult: BuildOperationErrorResult(199005, "code execution failed"),
		Data: &ExecuteCodeData{
			CodeContent: "invalid code",
			Language:    "python",
			ExitCode:    &exitCode,
			Stderr:      "SyntaxError",
		},
	}
	if r.IsSuccess() {
		t.Error("应为失败")
	}
	if r.Code != 199005 {
		t.Errorf("期望 Code=199005，实际 %d", r.Code)
	}
}

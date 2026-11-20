package result

import (
	"encoding/json"
	"testing"
)

// TestBuildOperationErrorResult 测试构造标准化错误结果
func TestBuildOperationErrorResult(t *testing.T) {
	r := BuildOperationErrorResult(1, "test error")
	if r.Code != 1 {
		t.Errorf("期望 Code=1，实际 %d", r.Code)
	}
	if r.Message != "test error" {
		t.Errorf("期望 Message='test error'，实际 %q", r.Message)
	}
}

// TestBaseResult_IsSuccess 测试 IsSuccess 判断
func TestBaseResult_IsSuccess(t *testing.T) {
	succ := BaseResult{Code: 0}
	fail1 := BaseResult{Code: 1}
	fail2 := BaseResult{Code: -1}
	if !succ.IsSuccess() {
		t.Error("Code=0 应为成功")
	}
	if fail1.IsSuccess() {
		t.Error("Code=1 应为失败")
	}
	if fail2.IsSuccess() {
		t.Error("Code=-1 应为失败")
	}
}

// TestExecuteCmdResult_构造 测试 ExecuteCmdResult 构造与 JSON 序列化
func TestExecuteCmdResult_构造(t *testing.T) {
	exitCode := 0
	r := ExecuteCmdResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &ExecuteCmdData{
			Command:  "echo hello",
			Cwd:      "/tmp",
			ExitCode: &exitCode,
			Stdout:   "hello\n",
			Stderr:   "",
		},
	}
	if !r.IsSuccess() {
		t.Error("应为成功")
	}
	if r.Data.Command != "echo hello" {
		t.Errorf("期望 Command='echo hello'，实际 %q", r.Data.Command)
	}

	// JSON 序列化
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ExecuteCmdResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Data.Command != "echo hello" {
		t.Errorf("反序列化后 Command 期望 'echo hello'，实际 %q", decoded.Data.Command)
	}
}

// TestExecuteCmdChunkData_JSON 测试流式块数据 JSON 序列化
func TestExecuteCmdChunkData_JSON(t *testing.T) {
	stdoutType := "stdout"
	chunk := ExecuteCmdChunkData{
		Text:       "hello",
		Type:       &stdoutType,
		ChunkIndex: 0,
		ExitCode:   nil,
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ExecuteCmdChunkData
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Text != "hello" {
		t.Errorf("期望 Text='hello'，实际 %q", decoded.Text)
	}
	if decoded.ChunkIndex != 0 {
		t.Errorf("期望 ChunkIndex=0，实际 %d", decoded.ChunkIndex)
	}
}

// TestExecuteCmdBackgroundResult_构造 测试后台执行结果构造
func TestExecuteCmdBackgroundResult_构造(t *testing.T) {
	pid := 12345
	r := ExecuteCmdBackgroundResult{
		BaseResult: BaseResult{Code: 0, Message: "success"},
		Data: &ExecuteCmdBackgroundData{
			Command: "sleep 100",
			Cwd:     "/tmp",
			Pid:     &pid,
		},
	}
	if !r.IsSuccess() {
		t.Error("应为成功")
	}
	if *r.Data.Pid != 12345 {
		t.Errorf("期望 Pid=12345，实际 %d", *r.Data.Pid)
	}
}

// TestExecuteCmdResult_空Data 测试 Data 为 nil 时的 JSON 序列化
func TestExecuteCmdResult_空Data(t *testing.T) {
	r := ExecuteCmdResult{
		BaseResult: BaseResult{Code: 1, Message: "error"},
		Data:       nil,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded ExecuteCmdResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Data != nil {
		t.Errorf("期望 Data=nil，实际非 nil")
	}
}

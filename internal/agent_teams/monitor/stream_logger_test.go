package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestNewTeamStreamLogger_创建文件 校验文件创建
func TestNewTeamStreamLogger_创建文件(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}
	// 验证文件存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("文件未创建")
	}
	sl.Flush()
}

// TestNewTeamStreamLogger_创建目录 校验含子目录的路径
func TestNewTeamStreamLogger_创建目录(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("文件未创建")
	}
	sl.Flush()
}

// TestTeamStreamLogger_Feed_离散型 校验 tool_call 立即写入
func TestTeamStreamLogger_Feed_离散型(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}

	member := "m1"
	role := atschema.TeamRoleLeader
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkToolCall,
			Payload: map[string]any{"tool_name": "test_tool", "tool_args": "arg1"},
		},
		&member,
		&role,
	))
	sl.Flush()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "tool_call") {
		t.Errorf("日志中未包含 'tool_call'，实际内容: %s", content)
	}
	if !strings.Contains(content, "tool_name=test_tool") {
		t.Errorf("日志中未包含 'tool_name=test_tool'，实际内容: %s", content)
	}
}

// TestTeamStreamLogger_Feed_累积型 校验 llm_output 缓冲后刷新
func TestTeamStreamLogger_Feed_累积型(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}

	member := "m1"
	role := atschema.TeamRoleLeader

	// 累积 llm_output
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkLLMOutput,
			Payload: map[string]any{"content": "hello "},
		},
		&member,
		&role,
	))
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkLLMOutput,
			Payload: map[string]any{"content": "world"},
		},
		&member,
		&role,
	))

	// 触发刷新：发送离散型块
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkToolCall,
			Payload: map[string]any{"tool_name": "flush_trigger"},
		},
		&member,
		&role,
	))
	sl.Flush()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "hello world") {
		t.Errorf("累积型内容未合并，实际: %s", content)
	}
}

// TestTeamStreamLogger_Feed_去重answer 校验 llm_output 后 answer 被丢弃
func TestTeamStreamLogger_Feed_去重answer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}

	member := "m1"
	role := atschema.TeamRoleLeader

	// 先 llm_output
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkLLMOutput,
			Payload: map[string]any{"content": "text"},
		},
		&member,
		&role,
	))
	// 再 answer，应被丢弃
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkAnswer,
			Payload: map[string]any{"content": "duplicate"},
		},
		&member,
		&role,
	))
	sl.Flush()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "duplicate") {
		t.Error("answer chunk 未被去重")
	}
}

// TestTeamStreamLogger_Flush 校验 Flush 后数据写入
func TestTeamStreamLogger_Flush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}

	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{
			Type:    chunkToolCall,
			Payload: map[string]any{"tool_name": "t1"},
		},
		nil,
		nil,
	))
	sl.Flush()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if !strings.Contains(string(data), "stream end") {
		t.Error("Flush 后应写入 'stream end' 摘要")
	}
}

// TestTeamStreamLogger_Feed_绝不panic 校验 nil chunk 和非 TeamOutputSchema 不 panic
func TestTeamStreamLogger_Feed_绝不panic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	sl, err := NewTeamStreamLogger(path)
	if err != nil {
		t.Fatalf("NewTeamStreamLogger 返回 error: %v", err)
	}

	// nil chunk 不 panic
	sl.Feed(nil)
	// 非 TeamOutputSchema 的 chunk（如 TraceSchema）被跳过，不 panic
	sl.Feed(&stream.TraceSchema{Type: "trace", Payload: "data"})
	// 空 Type 的 TeamOutputSchema 不 panic
	sl.Feed(atschema.NewTeamOutputSchema(
		stream.OutputSchema{Type: ""},
		nil,
		nil,
	))
	sl.Flush()
}

// TestClassify 校验分类函数
func TestClassify(t *testing.T) {
	tests := []struct {
		ctype string
		want  string
	}{
		{chunkLLMOutput, "text"},
		{chunkAnswer, "text"},
		{chunkLLMReasoning, "reasoning"},
		{chunkToolCall, "tool_call"},
		{chunkToolResult, "tool_result"},
		{chunkToolUpdate, "tool_update"},
		{chunkInteraction, "interaction"},
		{chunkControllerOutput, "controller_output"},
		{chunkMessage, "message"},
		{chunkTodoUpdated, "todo"},
		{"unknown_type", "other"},
	}
	for _, tt := range tests {
		got := classify(tt.ctype, nil)
		if got != tt.want {
			t.Errorf("classify(%q) = %q, want %q", tt.ctype, got, tt.want)
		}
	}

	// 特殊：message + runtime_ready event
	got := classify(chunkMessage, map[string]any{"event_type": runtimeReadyEvent})
	if got != "runtime_ready" {
		t.Errorf("classify(message, runtime_ready) = %q, want %q", got, "runtime_ready")
	}
}

// TestCapStr 校验截断函数
func TestCapStr(t *testing.T) {
	if capStr("short", 100) != "short" {
		t.Error("短字符串不应被截断")
	}
	long := strings.Repeat("x", 300)
	result := capStr(long, genericCap)
	// genericCap=2000，300<len(genericCap)，所以不会截断
	if result != long {
		t.Error("300 字符不应被截断（limit=2000）")
	}

	// 用小 limit 测试截断
	result2 := capStr(long, 100)
	if !strings.Contains(result2, "truncated") {
		t.Error("截断标记缺失")
	}
}

// 确保无关 import 不报错
var _ = context.Background

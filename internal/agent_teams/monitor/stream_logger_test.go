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

// ──────────────────────────── 摘要函数测试 ────────────────────────────

// TestToolResultSummary_标准payload map[string]any 格式输出摘要
func TestToolResultSummary_标准payload(t *testing.T) {
	payload := map[string]any{
		"tool_name":   "bash",
		"tool_args":   "ls -la",
		"tool_result": "file1.txt\nfile2.txt",
	}
	result := toolResultSummary(payload)
	if !strings.Contains(result, "tool_name=bash") {
		t.Errorf("缺少 tool_name=bash，实际: %s", result)
	}
	if !strings.Contains(result, "tool_args=") {
		t.Errorf("缺少 tool_args，实际: %s", result)
	}
	if !strings.Contains(result, "result:") {
		t.Errorf("缺少 result:，实际: %s", result)
	}
}

// TestToolResultSummary_非map类型 非字典类型走截断路径
func TestToolResultSummary_非map类型(t *testing.T) {
	result := toolResultSummary("just a string")
	if !strings.Contains(result, "just a string") {
		t.Errorf("非 map 类型应保留原始内容，实际: %s", result)
	}
}

// TestToolResultSummary_空字段 全部字段为空时走截断路径
func TestToolResultSummary_空字段(t *testing.T) {
	payload := map[string]any{}
	result := toolResultSummary(payload)
	// 空 map 的 fmt.Sprintf 输出是 "map[]"，应被截断输出
	if result == "" {
		t.Error("空字段不应返回空串")
	}
}

// TestToolUpdateSummary_标准payload map[string]any 格式输出摘要
func TestToolUpdateSummary_标准payload(t *testing.T) {
	payload := map[string]any{
		"tool_update": map[string]any{
			"tool_name":    "bash",
			"status":       "running",
			"tool_call_id": "call-123",
			"arguments":    "ls -la",
		},
	}
	result := toolUpdateSummary(payload)
	if !strings.Contains(result, "tool_name=bash") {
		t.Errorf("缺少 tool_name=bash，实际: %s", result)
	}
	if !strings.Contains(result, "status=running") {
		t.Errorf("缺少 status=running，实际: %s", result)
	}
	if !strings.Contains(result, "tool_call_id=call-123") {
		t.Errorf("缺少 tool_call_id=call-123，实际: %s", result)
	}
}

// TestToolUpdateSummary_无tool_update 无 tool_update 字段时走截断路径
func TestToolUpdateSummary_无tool_update(t *testing.T) {
	payload := map[string]any{"other_key": "value"}
	result := toolUpdateSummary(payload)
	if result == "" {
		t.Error("无 tool_update 时不应返回空串")
	}
}

// TestToolUpdateSummary_非map类型 非字典类型走截断路径
func TestToolUpdateSummary_非map类型(t *testing.T) {
	result := toolUpdateSummary(42)
	if result == "" {
		t.Error("非 map 类型不应返回空串")
	}
}

// TestControllerOutputSummary_TaskFailed 类型包含 task_failed 时提取文本
func TestControllerOutputSummary_TaskFailed(t *testing.T) {
	payload := map[string]any{
		"type": "task_failed",
		"data": []any{
			map[string]any{"text": "  错误信息A  "},
			map[string]any{"text": "错误信息B"},
			map[string]any{"other": "ignored"},
		},
	}
	result := controllerOutputSummary(payload)
	if !strings.Contains(result, "错误信息A") {
		t.Errorf("缺少错误信息A，实际: %s", result)
	}
	if !strings.Contains(result, "错误信息B") {
		t.Errorf("缺少错误信息B，实际: %s", result)
	}
}

// TestControllerOutputSummary_非TaskFailed 类型不包含 task_failed 时走截断路径
func TestControllerOutputSummary_非TaskFailed(t *testing.T) {
	payload := map[string]any{
		"type": "task_completed",
		"data": "some data",
	}
	result := controllerOutputSummary(payload)
	if result == "" {
		t.Error("非 task_failed 时不应返回空串")
	}
}

// TestControllerOutputSummary_非map类型 非字典类型走截断路径
func TestControllerOutputSummary_非map类型(t *testing.T) {
	result := controllerOutputSummary("simple string")
	if !strings.Contains(result, "simple string") {
		t.Errorf("非 map 类型应保留内容，实际: %s", result)
	}
}

// TestControllerOutputSummary_TaskFailed_空数据 data 为空切片时走截断路径
func TestControllerOutputSummary_TaskFailed_空数据(t *testing.T) {
	payload := map[string]any{
		"type": "task_failed",
		"data": []any{},
	}
	result := controllerOutputSummary(payload)
	// data 为空切片，texts 为空，走 capStr 路径
	if result == "" {
		t.Error("空数据不应返回空串")
	}
}

// TestRuntimeReadySummary_标准payload map[string]any 格式输出摘要
func TestRuntimeReadySummary_标准payload(t *testing.T) {
	payload := map[string]any{
		"team_name":       "my-team",
		"session_id":      "sess-001",
		"activation_kind": "manual",
	}
	result := runtimeReadySummary(payload)
	if !strings.Contains(result, "team=my-team") {
		t.Errorf("缺少 team=my-team，实际: %s", result)
	}
	if !strings.Contains(result, "session=sess-001") {
		t.Errorf("缺少 session=sess-001，实际: %s", result)
	}
	if !strings.Contains(result, "activation=manual") {
		t.Errorf("缺少 activation=manual，实际: %s", result)
	}
}

// TestRuntimeReadySummary_非map类型 非字典类型走截断路径
func TestRuntimeReadySummary_非map类型(t *testing.T) {
	result := runtimeReadySummary(123)
	if result == "" {
		t.Error("非 map 类型不应返回空串")
	}
}

// TestInteractionSummary_有interaction_id 提取交互 ID
func TestInteractionSummary_有interaction_id(t *testing.T) {
	payload := map[string]any{
		"interaction_id": "inter-001",
		"other":          "data",
	}
	result := interactionSummary(payload)
	if !strings.Contains(result, "interaction_id=inter-001") {
		t.Errorf("缺少 interaction_id=inter-001，实际: %s", result)
	}
}

// TestInteractionSummary_无interaction_id 无交互 ID 时使用 unknown
func TestInteractionSummary_无interaction_id(t *testing.T) {
	payload := map[string]any{"other": "data"}
	result := interactionSummary(payload)
	if !strings.Contains(result, "interaction_id=unknown") {
		t.Errorf("无 interaction_id 时应使用 unknown，实际: %s", result)
	}
}

// TestInteractionSummary_非map类型 非字典类型使用 unknown
func TestInteractionSummary_非map类型(t *testing.T) {
	result := interactionSummary("plain text")
	if !strings.Contains(result, "interaction_id=unknown") {
		t.Errorf("非 map 类型应使用 unknown，实际: %s", result)
	}
}

// TestGenericSummary_有content payload 中有 content 字段
func TestGenericSummary_有content(t *testing.T) {
	payload := map[string]any{"content": "hello world"}
	result := genericSummary(payload)
	if result != "hello world" {
		t.Errorf("有 content 时应直接返回 content，实际: %s", result)
	}
}

// TestGenericSummary_有output payload 中有 output 字段
func TestGenericSummary_有output(t *testing.T) {
	payload := map[string]any{"output": "output text"}
	result := genericSummary(payload)
	if result != "output text" {
		t.Errorf("有 output 时应直接返回 output，实际: %s", result)
	}
}

// TestGenericSummary_字符串类型 字符串 payload 直接返回
func TestGenericSummary_字符串类型(t *testing.T) {
	result := genericSummary("just a string")
	if result != "just a string" {
		t.Errorf("字符串 payload 应直接返回，实际: %s", result)
	}
}

// TestGenericSummary_其他类型 其他类型走 fmt.Sprintf + capStr
func TestGenericSummary_其他类型(t *testing.T) {
	result := genericSummary(42)
	if !strings.Contains(result, "42") {
		t.Errorf("其他类型应包含值的字符串表示，实际: %s", result)
	}
}

// TestDiscreteSummary_各分类 校验 discreteSummary 分派到对应摘要函数
func TestDiscreteSummary_各分类(t *testing.T) {
	// tool_call
	toolCallResult := discreteSummary("tool_call", map[string]any{"tool_name": "t1", "tool_args": "a1"})
	if !strings.Contains(toolCallResult, "tool_name=t1") {
		t.Errorf("tool_call 分类错误，实际: %s", toolCallResult)
	}

	// tool_result
	toolResultResult := discreteSummary("tool_result", map[string]any{"tool_name": "t2", "tool_result": "r2"})
	if !strings.Contains(toolResultResult, "tool_name=t2") {
		t.Errorf("tool_result 分类错误，实际: %s", toolResultResult)
	}

	// tool_update
	toolUpdateResult := discreteSummary("tool_update", map[string]any{"tool_update": map[string]any{"tool_name": "t3", "status": "done"}})
	if !strings.Contains(toolUpdateResult, "tool_name=t3") {
		t.Errorf("tool_update 分类错误，实际: %s", toolUpdateResult)
	}

	// controller_output
	ctrlResult := discreteSummary("controller_output", map[string]any{"type": "other"})
	if ctrlResult == "" {
		t.Error("controller_output 分类不应返回空串")
	}

	// runtime_ready
	rrResult := discreteSummary("runtime_ready", map[string]any{"team_name": "team1"})
	if !strings.Contains(rrResult, "team=team1") {
		t.Errorf("runtime_ready 分类错误，实际: %s", rrResult)
	}

	// interaction
	interResult := discreteSummary("interaction", map[string]any{"interaction_id": "i1"})
	if !strings.Contains(interResult, "interaction_id=i1") {
		t.Errorf("interaction 分类错误，实际: %s", interResult)
	}

	// default (other category)
	otherResult := discreteSummary("other", "some text")
	if otherResult != "some text" {
		t.Errorf("other 分类错误，实际: %s", otherResult)
	}
}

// TestExtractContent_从content提取 payload 中有 content 字段
func TestExtractContent_从content提取(t *testing.T) {
	payload := map[string]any{"content": "hello"}
	result := extractContent(payload)
	if result != "hello" {
		t.Errorf("期望 hello，实际: %s", result)
	}
}

// TestExtractContent_从output提取 payload 中有 output 字段但无 content
func TestExtractContent_从output提取(t *testing.T) {
	payload := map[string]any{"output": "world"}
	result := extractContent(payload)
	if result != "world" {
		t.Errorf("期望 world，实际: %s", result)
	}
}

// TestExtractContent_字符串类型 直接返回
func TestExtractContent_字符串类型(t *testing.T) {
	result := extractContent("direct text")
	if result != "direct text" {
		t.Errorf("期望 direct text，实际: %s", result)
	}
}

// TestExtractContent_其他类型 使用 fmt.Sprintf
func TestExtractContent_其他类型(t *testing.T) {
	result := extractContent(99)
	if !strings.Contains(result, "99") {
		t.Errorf("其他类型应包含值的字符串表示，实际: %s", result)
	}
}

// TestRenderRole nil 时返回空串
func TestRenderRole(t *testing.T) {
	if renderRole(nil) != "" {
		t.Error("nil role 应返回空串")
	}
	if renderRole("leader") != "leader" {
		t.Error("字符串 role 应返回原值")
	}
	if renderRole(42) != "42" {
		t.Error("数字 role 应返回字符串表示")
	}
}

// 确保无关 import 不报错
var _ = context.Background

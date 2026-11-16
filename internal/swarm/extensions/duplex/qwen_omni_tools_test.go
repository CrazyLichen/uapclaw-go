package duplex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
)

// TestQwenOmniTools_六个工具定义 对齐 qwen_omni_tools.py:43-121+216-306。
func TestQwenOmniTools_六个工具定义(t *testing.T) {
	tools := QwenOmniTools()
	if len(tools) != 6 {
		t.Fatalf("应 6 个工具（五件套+delegate）, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		function := tool["function"].(map[string]any)
		names[function["name"].(string)] = true
		if function["description"].(string) == "" {
			t.Fatalf("工具 %s 描述不得为空", function["name"])
		}
	}
	for _, expected := range []string{"jiuwen_delegate", "jiuwen_task_query", "jiuwen_task_cancel", "jiuwen_task_modify", "jiuwen_task_reorder", "jiuwen_task_answer"} {
		if !names[expected] {
			t.Fatalf("缺工具 %s", expected)
		}
	}
	delegate := tools[len(tools)-1]["function"].(map[string]any)
	if delegate["name"] != QwenOmniDelegateToolName {
		t.Fatal("delegate 应在末位（对齐 append 顺序）")
	}
	params := delegate["parameters"].(map[string]any)
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "task" {
		t.Fatalf("delegate required 应 [task]: %v", required)
	}
}

// TestParseQwenOmniToolCall_合法三种 对齐 qwen_omni_tools.py:124-214。
func TestParseQwenOmniToolCall_合法三种(t *testing.T) {
	// delegate（arguments 为 JSON 字符串）
	got, err := ParseQwenOmniToolCall(map[string]any{
		"name": QwenOmniDelegateToolName, "call_id": "c1",
		"arguments": `{"task":"写一份报告","independent":true,"depends_on":["j1"],"resources":[]}`,
	})
	if err != nil || got.Task != "写一份报告" || got.CallID != "c1" || !got.Arguments["independent"].(bool) {
		t.Fatalf("delegate 解析不符: %v %+v", err, got)
	}
	if got.Query() != "写一份报告" {
		t.Fatal("Query 别名应等 Task")
	}
	// research 旧名（仅 query 字段）
	got, err = ParseQwenOmniToolCall(map[string]any{
		"name": QwenOmniResearchToolName, "call_id": "c2", "arguments": map[string]any{"query": " 天气 "},
	})
	if err != nil || got.Task != "天气" {
		t.Fatalf("research 解析不符: %v %+v", err, got)
	}
	// task_query
	got, err = ParseQwenOmniToolCall(map[string]any{
		"name": "jiuwen_task_query", "call_id": "c3",
		"arguments": map[string]any{"job_id": "j1", "status": "completed"},
	})
	if err != nil || got.Task != "" {
		t.Fatalf("task_query 解析不符: %v", err)
	}
	// task_answer（answers 数组）
	got, err = ParseQwenOmniToolCall(map[string]any{
		"name": "jiuwen_task_answer", "call_id": "c4",
		"arguments": map[string]any{"job_id": "j1", "interaction_id": "i1", "answers": []any{"5000元"}},
	})
	if err != nil {
		t.Fatalf("task_answer 解析不符: %v", err)
	}
}

// TestParseQwenOmniToolCall_拒绝边界 对齐 :134-214 的校验分支。
func TestParseQwenOmniToolCall_拒绝边界(t *testing.T) {
	// 非对象
	if _, err := ParseQwenOmniToolCall("not-object"); err == nil || err.Error() != "tool call must be an object" {
		t.Fatalf("非对象: %v", err)
	}
	// 未知工具
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "evil_tool", "call_id": "c", "arguments": "{}"}); err == nil || err.Error() != "unsupported Qwen tool: evil_tool" {
		t.Fatalf("未知工具: %v", err)
	}
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "", "call_id": "c", "arguments": "{}"}); err == nil || err.Error() != "unsupported Qwen tool: <empty>" {
		t.Fatalf("空工具名: %v", err)
	}
	// call_id 空/超限
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_cancel", "call_id": " ", "arguments": `{"job_id":"j"}`}); err == nil {
		t.Fatal("空 call_id 应拒")
	}
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_cancel", "call_id": strings.Repeat("c", 201), "arguments": `{"job_id":"j"}`}); err == nil {
		t.Fatal("超长 call_id 应拒")
	}
	// arguments 非法 JSON
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_cancel", "call_id": "c", "arguments": "{bad"}); err == nil || err.Error() != "arguments must be valid JSON" {
		t.Fatalf("非法 JSON: %v", err)
	}
	// arguments 非 JSON 对象
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_cancel", "call_id": "c", "arguments": "[1]"}); err == nil || err.Error() != "arguments must be a JSON object" {
		t.Fatalf("非对象 JSON: %v", err)
	}
	// 任务工具：未知键
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_cancel", "call_id": "c", "arguments": map[string]any{"job_id": "j", "extra": "x"}}); err == nil || err.Error() != "Invalid task tool arguments" {
		t.Fatalf("未知键: %v", err)
	}
	// 任务工具：缺 required
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_modify", "call_id": "c", "arguments": map[string]any{"job_id": "j", "revision": 1.0}}); err == nil {
		t.Fatal("缺 instruction 应拒")
	}
	// revision < 1
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_modify", "call_id": "c", "arguments": map[string]any{"job_id": "j", "revision": 0.0, "instruction": "x"}}); err == nil || err.Error() != "Invalid revision" {
		t.Fatalf("revision<1: %v", err)
	}
	// 非法 status
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_query", "call_id": "c", "arguments": map[string]any{"status": "bogus"}}); err == nil || err.Error() != "Invalid task status" {
		t.Fatalf("非法 status: %v", err)
	}
	// reorder: 非法 action
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_reorder", "call_id": "c", "arguments": map[string]any{"job_id": "j", "queue_version": 1.0, "action": "top"}}); err == nil {
		t.Fatal("非法 action 应拒")
	}
	// reorder: before 缺 before_job_id
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_reorder", "call_id": "c", "arguments": map[string]any{"job_id": "j", "queue_version": 1.0, "action": "before"}}); err == nil || !strings.Contains(err.Error(), "Only before requires before_job_id") {
		t.Fatalf("before 缺参: %v", err)
	}
	// delegate: 无任务字段
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": QwenOmniDelegateToolName, "call_id": "c", "arguments": map[string]any{"independent": false}}); err == nil || err.Error() != "arguments must contain exactly one task field" {
		t.Fatalf("缺任务字段: %v", err)
	}
	// delegate: independent 非布尔
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": QwenOmniDelegateToolName, "call_id": "c", "arguments": map[string]any{"task": "x", "independent": "yes"}}); err == nil || err.Error() != "independent must be a boolean" {
		t.Fatalf("independent 类型: %v", err)
	}
	// delegate: depends_on 元素非法
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": QwenOmniDelegateToolName, "call_id": "c", "arguments": map[string]any{"task": "x", "depends_on": []any{"  "}}}); err == nil || err.Error() != "Invalid depends_on" {
		t.Fatalf("depends_on 空元素: %v", err)
	}
	// task 为空串
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": QwenOmniDelegateToolName, "call_id": "c", "arguments": map[string]any{"task": "  "}}); err == nil || !strings.Contains(err.Error(), "must contain 1-16000") {
		t.Fatalf("空 task: %v", err)
	}
	// task 超长
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": QwenOmniDelegateToolName, "call_id": "c", "arguments": map[string]any{"task": strings.Repeat("字", 16001)}}); err == nil {
		t.Fatal("超长 task 应拒")
	}
	// answers 边界：空数组
	if _, err := ParseQwenOmniToolCall(map[string]any{"name": "jiuwen_task_answer", "call_id": "c", "arguments": map[string]any{"job_id": "j", "interaction_id": "i", "answers": []any{}}}); err == nil || err.Error() != "Invalid answers" {
		t.Fatalf("空 answers: %v", err)
	}
}

// TestTaskReceiptFollowup_JSON语义 对齐 prompts.py:57-63（紧凑输出与转义；
// 键序偏差 O-5：Go map 无插入序且为模型面向文本无消费方，不作序对齐）。
func TestTaskReceiptFollowup_JSON语义(t *testing.T) {
	got := tasks.TaskReceiptFollowup(map[string]any{"b": 1, "a": "中文"})
	if !strings.Contains(got, `"a":"中文"`) || !strings.Contains(got, `"b":1`) {
		t.Fatalf("JSON 应紧凑且中文不转义: %s", got)
	}
	if !strings.HasPrefix(got, "Actual task tool receipt") {
		t.Fatal("前缀文案不符")
	}
	_ = json.Marshal // 保留引用
}

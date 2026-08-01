package cron

import (
	"context"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCreateCronTools_不含遗留 测试 includeLegacyCompat=false → 仅 1 个工具
// 对齐 Python: include_legacy_compat=False → [cron_tool]
func TestCreateCronTools_不含遗留(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	tools := CreateCronTools(backend, cronCtx, "cn", nil, "", false, "agent_1")
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	card := tools[0].Card()
	if card.Name != "cron" {
		t.Errorf("tool name = %q, want 'cron'", card.Name)
	}
	// 验证 ToolID 包含 scope 和 agentID
	scope := toolScope(cronCtx)
	wantIDPrefix := "cron_" + scope + "_agent_1"
	if !strings.HasPrefix(card.ID, wantIDPrefix) {
		t.Errorf("tool ID = %q, want prefix %q", card.ID, wantIDPrefix)
	}
}

// TestCreateCronTools_含遗留 测试 includeLegacyCompat=true → 8 个工具
// 对齐 Python: include_legacy_compat=True → [cron + 7 legacy tools]
func TestCreateCronTools_含遗留(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	tools := CreateCronTools(backend, cronCtx, "cn", nil, "", true, "agent_1")
	if len(tools) != 8 {
		t.Fatalf("len(tools) = %d, want 8", len(tools))
	}
	if tools[0].Card().Name != "cron" {
		t.Errorf("first tool name = %q, want 'cron'", tools[0].Card().Name)
	}
	legacyNames := []string{"cron_list_jobs", "cron_get_job", "cron_create_job",
		"cron_update_job", "cron_delete_job", "cron_toggle_job", "cron_preview_job"}
	for i, wantName := range legacyNames {
		gotName := tools[i+1].Card().Name
		if gotName != wantName {
			t.Errorf("legacy tool[%d] name = %q, want %q", i+1, gotName, wantName)
		}
	}
}

// TestCreateCronTools_agentID为空 测试 agentID="" → 用 scope 作为 finalAgentID
// 对齐 Python: final_agent_id = agent_id or scope
func TestCreateCronTools_agentID为空(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	tools := CreateCronTools(backend, cronCtx, "cn", nil, "", true, "")
	if len(tools) != 8 {
		t.Fatalf("len(tools) = %d, want 8", len(tools))
	}
	scope := toolScope(cronCtx)
	card := tools[0].Card()
	wantIDPrefix := "cron_" + scope + "_" + scope
	if !strings.HasPrefix(card.ID, wantIDPrefix) {
		t.Errorf("tool ID = %q, want prefix %q", card.ID, wantIDPrefix)
	}
}

// TestCreateCronTools_遗留工具调用 测试遗留工具 Invoke 正确路由到 backend
func TestCreateCronTools_遗留工具调用(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	tools := CreateCronTools(backend, cronCtx, "cn", nil, "", true, "agent_1")

	// cron_list_jobs (index 1)
	result, err := tools[1].Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("cron_list_jobs invoke error: %v", err)
	}
	if _, ok := result["jobs"]; !ok {
		t.Errorf("cron_list_jobs result missing 'jobs' key")
	}

	// cron_get_job (index 2)
	result, err = tools[2].Invoke(context.Background(), map[string]any{"job_id": "job1"})
	if err != nil {
		t.Fatalf("cron_get_job invoke error: %v", err)
	}
	if _, ok := result["job"]; !ok {
		t.Errorf("cron_get_job result missing 'job' key")
	}

	// cron_create_job (index 3)
	result, err = tools[3].Invoke(context.Background(), map[string]any{
		"name": "测试任务", "cron_expr": "0 0 9 * * ? *", "timezone": "Asia/Shanghai",
		"description": "每天9点提醒",
	})
	if err != nil {
		t.Fatalf("cron_create_job invoke error: %v", err)
	}
	if len(backend.createdJobs) < 1 {
		t.Errorf("expected at least 1 created job")
	}

	// cron_update_job (index 4)
	result, err = tools[4].Invoke(context.Background(), map[string]any{
		"job_id": "job1", "patch": map[string]any{"enabled": false},
	})
	if err != nil {
		t.Fatalf("cron_update_job invoke error: %v", err)
	}

	// cron_delete_job (index 5)
	result, err = tools[5].Invoke(context.Background(), map[string]any{"job_id": "job1"})
	if err != nil {
		t.Fatalf("cron_delete_job invoke error: %v", err)
	}
	if deleted, ok := result["deleted"].(bool); !ok || !deleted {
		t.Errorf("cron_delete_job result['deleted'] = %v, want true", result["deleted"])
	}
}

// TestCreateCronTools_遗留工具cronToggleJob 测试 cron_toggle_job Invoke
func TestCreateCronTools_遗留工具cronToggleJob(t *testing.T) {
	backend := newFakeCronBackend()
	cronCtx := &CronToolContext{ChannelID: "wechat", SessionID: "sess_1"}
	tools := CreateCronTools(backend, cronCtx, "cn", nil, "", true, "agent_1")

	// cron_toggle_job (index 6)
	_, err := tools[6].Invoke(context.Background(), map[string]any{"job_id": "job1", "enabled": false})
	if err != nil {
		t.Fatalf("cron_toggle_job invoke error: %v", err)
	}
	if backend.toggledJobs["job1"] != false {
		t.Errorf("toggledJobs[job1] = %v, want false", backend.toggledJobs["job1"])
	}
}

// TestTargetSchema_有枚举 测试 targetChannels 非空 → enum 值
// 对齐 Python: _target_schema(target_channels=["wechat", "dingtalk"], ...)
func TestTargetSchema_有枚举(t *testing.T) {
	schema := targetSchema([]string{"wechat", "dingtalk"}, "")
	enumVals, ok := schema["enum"]
	if !ok {
		t.Fatalf("schema missing 'enum' key")
	}
	enumSlice, ok := enumVals.([]string)
	if !ok {
		t.Fatalf("enum type = %T, want []string", enumVals)
	}
	if len(enumSlice) != 2 || enumSlice[0] != "wechat" || enumSlice[1] != "dingtalk" {
		t.Errorf("enum = %v, want [wechat, dingtalk]", enumSlice)
	}
}

// TestTargetSchema_有默认值 测试 defaultTargetChannel 非空 → default
// 对齐 Python: _target_schema(..., default_target_channel="wechat")
func TestTargetSchema_有默认值(t *testing.T) {
	schema := targetSchema(nil, "wechat")
	if schema["default"] != "wechat" {
		t.Errorf("schema['default'] = %v, want 'wechat'", schema["default"])
	}
}

// TestTargetSchema_空值 测试无 targetChannels 和 defaultTargetChannel
// 对齐 Python: _target_schema(None, None) → 仅 type + description
func TestTargetSchema_空值(t *testing.T) {
	schema := targetSchema(nil, "")
	if _, ok := schema["enum"]; ok {
		t.Errorf("schema should not contain 'enum' when targetChannels is nil")
	}
	if _, ok := schema["default"]; ok {
		t.Errorf("schema should not contain 'default' when defaultTargetChannel is empty")
	}
	if schema["type"] != "string" {
		t.Errorf("schema['type'] = %v, want 'string'", schema["type"])
	}
}

// TestTargetSchema_空格过滤 测试 targetChannels 中有前后空格
// 对齐 Python: enum_values = [str(item).strip() for item in list(...) if str(item).strip()]
func TestTargetSchema_空格过滤(t *testing.T) {
	schema := targetSchema([]string{"  wechat  ", "  ", ""}, "  dingtalk  ")
	enumVals, ok := schema["enum"].([]string)
	if !ok {
		t.Fatalf("schema['enum'] type = %T, want []string", schema["enum"])
	}
	// 空白字符串和纯空格应被过滤
	if len(enumVals) != 1 || enumVals[0] != "wechat" {
		t.Errorf("enum = %v, want [wechat]", enumVals)
	}
	// defaultTargetChannel 的空格应被去除
	if schema["default"] != "dingtalk" {
		t.Errorf("schema['default'] = %v, want 'dingtalk'", schema["default"])
	}
}

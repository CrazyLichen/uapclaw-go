package memory

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewCodingMemoryRail_默认值 测试默认值和优先级
func TestNewCodingMemoryRail_默认值(t *testing.T) {
	embCfg := &embedding.EmbeddingConfig{}
	rail := NewCodingMemoryRail("/tmp/cm", embCfg, "cn")

	if rail.Priority() != codingMemoryRailPriority {
		t.Errorf("Priority() = %d, want %d", rail.Priority(), codingMemoryRailPriority)
	}
	if rail.codingMemoryDir != "/tmp/cm" {
		t.Errorf("codingMemoryDir = %q, want %q", rail.codingMemoryDir, "/tmp/cm")
	}
	if rail.language != "cn" {
		t.Errorf("language = %q, want %q", rail.language, "cn")
	}
	if rail.embeddingConfig != embCfg {
		t.Error("embeddingConfig 未设置")
	}
	if rail.managerInitialized {
		t.Error("managerInitialized 应为 false")
	}
	if len(rail.ownedToolNames) != 0 {
		t.Error("ownedToolNames 应为空")
	}
	if len(rail.ownedToolIDs) != 0 {
		t.Error("ownedToolIDs 应为空")
	}
}

// TestCodingMemoryRail_Priority 测试优先级
func TestCodingMemoryRail_Priority(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")
	if rail.Priority() != 80 {
		t.Errorf("Priority() = %d, want 80", rail.Priority())
	}
}

// TestCodingMemoryRail_ExtractLastUserQuery 测试用户查询提取
func TestCodingMemoryRail_ExtractLastUserQuery(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")

	// 测试 InvokeInputs 有 Query 的情况
	query := agentinterfaces.NewInvokeQueryString("hello world")
	invokeInputs := &agentinterfaces.InvokeInputs{
		Query: query,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, invokeInputs, nil)
	result := rail.extractLastUserQuery(cbc)
	if result != "hello world" {
		t.Errorf("extractLastUserQuery = %q, want %q", result, "hello world")
	}

	// 测试非 InvokeInputs 的情况
	mapInputs := &agentinterfaces.MapInputs{Data: map[string]any{}}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, mapInputs, nil)
	result2 := rail.extractLastUserQuery(cbc2)
	if result2 != "" {
		t.Errorf("extractLastUserQuery 非 InvokeInputs 应返回空字符串，得到 %q", result2)
	}

	// 测试 Query 为 nil
	invokeInputsNil := &agentinterfaces.InvokeInputs{Query: nil}
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, invokeInputsNil, nil)
	result3 := rail.extractLastUserQuery(cbc3)
	if result3 != "" {
		t.Errorf("extractLastUserQuery Query=nil 应返回空字符串，得到 %q", result3)
	}
}

// TestCodingMemoryRail_IsReadOnly 测试 cron/heartbeat 判断
func TestCodingMemoryRail_IsReadOnly(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")

	// 正常模式
	invokeInputs := &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindNormal,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, invokeInputs, nil)
	if rail.isReadOnly(cbc) {
		t.Error("正常模式不应为只读")
	}

	// cron 模式
	invokeInputsCron := &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindCron,
	}
	cbcCron := agentinterfaces.NewAgentCallbackContext(nil, invokeInputsCron, nil)
	if !rail.isReadOnly(cbcCron) {
		t.Error("cron 模式应为只读")
	}

	// heartbeat 模式
	invokeInputsHeartbeat := &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindHeartbeat,
	}
	cbcHeartbeat := agentinterfaces.NewAgentCallbackContext(nil, invokeInputsHeartbeat, nil)
	if !rail.isReadOnly(cbcHeartbeat) {
		t.Error("heartbeat 模式应为只读")
	}

	// 非 InvokeInputs
	mapInputs := &agentinterfaces.MapInputs{Data: map[string]any{}}
	cbcMap := agentinterfaces.NewAgentCallbackContext(nil, mapInputs, nil)
	if rail.isReadOnly(cbcMap) {
		t.Error("非 InvokeInputs 不应为只读")
	}
}

// TestCodingMemoryRail_BeforeInvoke_无Manager 测试无 manager 时正常执行
func TestCodingMemoryRail_BeforeInvoke_无Manager(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")
	invokeInputs := &agentinterfaces.InvokeInputs{
		Query:   agentinterfaces.NewInvokeQueryString("test"),
		RunKind: agentinterfaces.RunKindNormal,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, invokeInputs, nil)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeInvoke() 不应返回错误: %v", err)
	}
}

// TestCodingMemoryRail_BeforeModelCall_无Builder 测试无 systemPromptBuilder 时直接返回
func TestCodingMemoryRail_BeforeModelCall_无Builder(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")
	invokeInputs := &agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindNormal,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, invokeInputs, nil)

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeModelCall() 不应返回错误: %v", err)
	}
}

// TestCodingMemoryRail_GetCallbacks 测试回调映射
func TestCodingMemoryRail_GetCallbacks(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")
	callbacks := rail.GetCallbacks()

	// 应包含 BeforeInvoke 和 BeforeModelCall
	if _, ok := callbacks[agentinterfaces.CallbackBeforeInvoke]; !ok {
		t.Error("GetCallbacks 应包含 CallbackBeforeInvoke")
	}
	if _, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]; !ok {
		t.Error("GetCallbacks 应包含 CallbackBeforeModelCall")
	}
}

// TestCodingMemoryRail_BuildDateTag 测试日期标签构建
func TestCodingMemoryRail_BuildDateTag(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")

	// nil frontmatter
	if tag := rail.buildDateTag(nil); tag != "" {
		t.Errorf("nil frontmatter 应返回空字符串，得到 %q", tag)
	}

	// 有 updated_at
	fm := map[string]string{"updated_at": "2026-01-15"}
	if tag := rail.buildDateTag(fm); tag != " (updated: 2026-01-15)" {
		t.Errorf("buildDateTag = %q, want %q", tag, " (updated: 2026-01-15)")
	}

	// 只有 created_at
	fm2 := map[string]string{"created_at": "2026-01-10"}
	if tag := rail.buildDateTag(fm2); tag != " (updated: 2026-01-10)" {
		t.Errorf("buildDateTag = %q, want %q", tag, " (updated: 2026-01-10)")
	}

	// 无日期字段
	fm3 := map[string]string{"name": "test"}
	if tag := rail.buildDateTag(fm3); tag != "" {
		t.Errorf("无日期字段应返回空字符串，得到 %q", tag)
	}
}

// TestSetToSortedSlice 测试 set 到排序切片的转换
func TestSetToSortedSlice(t *testing.T) {
	s := map[string]struct{}{
		"coding_memory_read":  {},
		"coding_memory_write": {},
		"coding_memory_edit":  {},
	}
	result := memorySetToSortedSlice(s)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	// 应排序
	if result[0] != "coding_memory_edit" {
		t.Errorf("result[0] = %q, want %q", result[0], "coding_memory_edit")
	}
	if result[1] != "coding_memory_read" {
		t.Errorf("result[1] = %q, want %q", result[1], "coding_memory_read")
	}
	if result[2] != "coding_memory_write" {
		t.Errorf("result[2] = %q, want %q", result[2], "coding_memory_write")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

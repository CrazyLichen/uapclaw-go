package modules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewIntentToolkits 测试意图工具集构造
func TestNewIntentToolkits(t *testing.T) {
	event, err := schema.FromUserInput("hello")
	require.NoError(t, err)

	cfg := defaultTestControllerConfig()
	toolkits := NewIntentToolkits(event, cfg.IntentConfidenceThreshold)
	assert.NotNil(t, toolkits)
	assert.Equal(t, event, toolkits.event)
	assert.Equal(t, cfg.IntentConfidenceThreshold, toolkits.confidenceThreshold)
	assert.Len(t, toolkits.toolSchemaChoices, 8)
}

// TestIntentToolkits_CreateTask_高置信度 测试创建任务意图（高置信度）
func TestIntentToolkits_CreateTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("帮我查天气")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.CreateTask(0.9, "查询天气")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentCreateTask, intent.IntentType)
	assert.Equal(t, "查询天气", intent.TargetTaskDescription)
	assert.NotEmpty(t, intent.TargetTaskID)
	assert.Contains(t, result, "已创建并提交执行")
}

// TestIntentToolkits_CreateTask_低置信度 测试创建任务意图（低置信度）
func TestIntentToolkits_CreateTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.CreateTask(0.3, "不确定")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
	assert.Contains(t, result, "自动转换为 unknown_task")
}

// TestIntentToolkits_PauseTask_高置信度 测试暂停任务意图（高置信度）
func TestIntentToolkits_PauseTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("暂停任务")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.PauseTask(0.9, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentPauseTask, intent.IntentType)
	assert.Equal(t, "task-123", intent.TargetTaskID)
	assert.Contains(t, result, "已暂停")
}

// TestIntentToolkits_PauseTask_低置信度 测试暂停任务意图（低置信度）
func TestIntentToolkits_PauseTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.PauseTask(0.3, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
	assert.Contains(t, result, "自动转换为 unknown_task")
}

// TestIntentToolkits_CancelTask_高置信度 测试取消任务意图
func TestIntentToolkits_CancelTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("取消任务")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.CancelTask(0.9, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentCancelTask, intent.IntentType)
	assert.Contains(t, result, "已取消")
}

// TestIntentToolkits_CancelTask_低置信度 测试取消任务意图（低置信度）
func TestIntentToolkits_CancelTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.CancelTask(0.3, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_ResumeTask_高置信度 测试恢复任务意图
func TestIntentToolkits_ResumeTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("恢复任务")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.ResumeTask(0.9, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentResumeTask, intent.IntentType)
	assert.Contains(t, result, "已恢复")
}

// TestIntentToolkits_ResumeTask_低置信度 测试恢复任务意图（低置信度）
func TestIntentToolkits_ResumeTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.ResumeTask(0.3, "task-123")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_UnknownTask_高置信度 测试未知任务意图
func TestIntentToolkits_UnknownTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("什么意思")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.UnknownTask(0.9, "请问您要做什么？")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
	assert.Equal(t, "请问您要做什么？", intent.ClarificationPrompt)
	assert.Contains(t, result, "等待用户响应")
}

// TestIntentToolkits_UnknownTask_低置信度 测试未知任务意图（低置信度）
func TestIntentToolkits_UnknownTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.UnknownTask(0.3, "请问？")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_CreateDependentTask_高置信度 测试创建依赖任务意图
func TestIntentToolkits_CreateDependentTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("在任务A完成后执行任务B")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.CreateDependentTask(0.9, "执行任务B", []string{"task-a"})
	require.NoError(t, err)
	assert.Equal(t, schema.IntentContinueTask, intent.IntentType)
	assert.Equal(t, "执行任务B", intent.TargetTaskDescription)
	assert.Equal(t, []string{"task-a"}, intent.DependTaskID)
	assert.Contains(t, result, "已创建并提交执行")
}

// TestIntentToolkits_CreateDependentTask_低置信度 测试创建依赖任务意图（低置信度）
func TestIntentToolkits_CreateDependentTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.CreateDependentTask(0.3, "任务B", []string{"task-a"})
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_ModifyTask_高置信度 测试修改任务意图
func TestIntentToolkits_ModifyTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("修改任务描述")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.ModifyTask(0.9, "task-123", "新描述")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentModifyTask, intent.IntentType)
	assert.Equal(t, "新描述", intent.TargetTaskDescription)
	assert.Equal(t, []string{"task-123"}, intent.DependTaskID)
	assert.Equal(t, "新描述", intent.ModificationDetails)
	assert.Contains(t, result, "已创建并提交执行")
}

// TestIntentToolkits_ModifyTask_低置信度 测试修改任务意图（低置信度）
func TestIntentToolkits_ModifyTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.ModifyTask(0.3, "task-123", "新描述")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_SupplementTask_高置信度 测试补充任务意图
func TestIntentToolkits_SupplementTask_高置信度(t *testing.T) {
	event, _ := schema.FromUserInput("补充信息")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result, err := toolkits.SupplementTask(0.9, "task-123", "额外信息")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentSupplementTask, intent.IntentType)
	assert.Equal(t, "task-123", intent.TargetTaskID)
	assert.Equal(t, "额外信息", intent.SupplementaryInfo)
	assert.Contains(t, result, "补充信息已提交")
}

// TestIntentToolkits_SupplementTask_低置信度 测试补充任务意图（低置信度）
func TestIntentToolkits_SupplementTask_低置信度(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, _, err := toolkits.SupplementTask(0.3, "task-123", "额外信息")
	require.NoError(t, err)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
}

// TestIntentToolkits_GetOpenAIToolSchemas_全量 测试获取全部 Schema
func TestIntentToolkits_GetOpenAIToolSchemas_全量(t *testing.T) {
	event, _ := schema.FromUserInput("hello")
	toolkits := NewIntentToolkits(event, 0.7)

	schemas := toolkits.GetOpenAIToolSchemas()
	assert.Len(t, schemas, 8)
}

// TestIntentToolkits_GetOpenAIToolSchemas_过滤 测试获取指定 Schema
func TestIntentToolkits_GetOpenAIToolSchemas_过滤(t *testing.T) {
	event, _ := schema.FromUserInput("hello")
	toolkits := NewIntentToolkits(event, 0.7)

	schemas := toolkits.GetOpenAIToolSchemas("create_task", "pause_task")
	assert.Len(t, schemas, 2)
}

// TestIntentToolkits_GetOpenAIToolSchemas_空选择 测试传入不存在的名称
func TestIntentToolkits_GetOpenAIToolSchemas_空选择(t *testing.T) {
	event, _ := schema.FromUserInput("hello")
	toolkits := NewIntentToolkits(event, 0.7)

	schemas := toolkits.GetOpenAIToolSchemas("nonexistent")
	assert.Len(t, schemas, 0)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestIntentToolkits_lowConfidenceIntent 测试低置信度转换
func TestIntentToolkits_lowConfidenceIntent(t *testing.T) {
	event, _ := schema.FromUserInput("嗯")
	toolkits := NewIntentToolkits(event, 0.7)

	intent, result := toolkits.lowConfidenceIntent(0.3)
	assert.Equal(t, schema.IntentUnknownTask, intent.IntentType)
	assert.Equal(t, 0.3, intent.Confidence)
	assert.NotEmpty(t, intent.ClarificationPrompt)
	assert.Contains(t, result, "自动转换为 unknown_task")
}

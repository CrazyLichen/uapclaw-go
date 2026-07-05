package task_loop

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSessionSpawnExecutor 测试创建执行器
func TestNewSessionSpawnExecutor(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeDeepAgentProvider{}
	executor := NewSessionSpawnExecutor(deps, provider)
	if executor == nil {
		t.Fatal("NewSessionSpawnExecutor 返回 nil")
	}
}

// TestSessionSpawnExecutor_CanPause 测试不支持暂停
func TestSessionSpawnExecutor_CanPause(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, reason, err := executor.CanPause(context.Background(), "task-1", nil)
	if ok || err != nil {
		t.Fatalf("期望 (false, nil), 实际 (%v, %v)", ok, err)
	}
	if reason == "" {
		t.Fatal("应有不可暂停原因")
	}
}

// TestSessionSpawnExecutor_Pause 测试暂停返回 false
func TestSessionSpawnExecutor_Pause(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, err := executor.Pause(context.Background(), "task-1", nil)
	if ok || err != nil {
		t.Fatalf("期望 (false, nil), 实际 (%v, %v)", ok, err)
	}
}

// TestSessionSpawnExecutor_CanCancel 测试支持取消
func TestSessionSpawnExecutor_CanCancel(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, reason, err := executor.CanCancel(context.Background(), "task-1", nil)
	if !ok || err != nil {
		t.Fatalf("期望 (true, nil), 实际 (%v, %v)", ok, err)
	}
	if reason != "" {
		t.Fatalf("不应有不可取消原因, 实际: %s", reason)
	}
}

// TestSessionSpawnExecutor_Cancel 测试取消返回 true
func TestSessionSpawnExecutor_Cancel(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	ok, err := executor.Cancel(context.Background(), "task-1", nil)
	if !ok || err != nil {
		t.Fatalf("期望 (true, nil), 实际 (%v, %v)", ok, err)
	}
}

// TestBuildSessionSpawnExecutor 测试工厂闭包
func TestBuildSessionSpawnExecutor(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	factory := BuildSessionSpawnExecutor(provider)
	if factory == nil {
		t.Fatal("BuildSessionSpawnExecutor 返回 nil")
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := factory(deps)
	if executor == nil {
		t.Fatal("工厂闭包返回 nil")
	}
	// 验证返回的类型为 *SessionSpawnExecutor
	if _, ok := executor.(*SessionSpawnExecutor); !ok {
		t.Error("工厂闭包返回值不是 *SessionSpawnExecutor 类型")
	}
}

// TestSessionSpawnExecutor_ExecuteAbility_任务未找到 测试任务不存在时发送错误分片
func TestSessionSpawnExecutor_ExecuteAbility_任务未找到(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	deps := &modules.TaskExecutorDependencies{TaskManager: tm}
	provider := &fakeDeepAgentProvider{}
	executor := NewSessionSpawnExecutor(deps, provider)

	ch, err := executor.ExecuteAbility(context.Background(), "nonexistent-task", nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	out := <-ch
	if out.Type != string(cschema.EventTaskFailed) {
		t.Fatalf("期望 TASK_FAILED, 实际 %s", out.Type)
	}
}

// TestSessionSpawnExecutor_buildErrorChunk 测试错误分片构建
func TestSessionSpawnExecutor_buildErrorChunk(t *testing.T) {
	executor := NewSessionSpawnExecutor(nil, nil)
	chunk := executor.buildErrorChunk("task-1", "网络错误")
	if chunk.Type != string(cschema.EventTaskFailed) {
		t.Fatalf("期望 TASK_FAILED, 实际 %s", chunk.Type)
	}
	if !chunk.IsLastSchema {
		t.Fatal("应为最后分片")
	}
	payload, ok := chunk.Payload.(*cschema.ControllerOutputPayload)
	if !ok {
		t.Fatal("Payload 类型断言失败")
	}
	taskType, _ := payload.Metadata["task_type"].(string)
	if taskType != SessionSpawnTaskType {
		t.Fatalf("期望 %s, 实际 %s", SessionSpawnTaskType, taskType)
	}
	taskID, _ := payload.Metadata["task_id"].(string)
	if taskID != "task-1" {
		t.Fatalf("期望 task-1, 实际 %s", taskID)
	}
}

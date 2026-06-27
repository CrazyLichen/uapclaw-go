package modules

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeTaskExecutor 用于测试的模拟任务执行器
type fakeTaskExecutor struct {
	// name 标识名称，用于区分不同 builder 创建的实例
	name string
}

// ExecuteAbility 模拟执行任务
func (f *fakeTaskExecutor) ExecuteAbility(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
	ch := make(chan *schema.ControllerOutputChunk)
	close(ch)
	return ch, nil
}

// CanPause 模拟检查是否可暂停
func (f *fakeTaskExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Pause 模拟暂停任务
func (f *fakeTaskExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) error {
	return nil
}

// CanCancel 模拟检查是否可取消
func (f *fakeTaskExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Cancel 模拟取消任务
func (f *fakeTaskExecutor) Cancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) error {
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTaskExecutorRegistry_注册获取 验证 Add + Get 正常工作
func TestTaskExecutorRegistry_注册获取(t *testing.T) {
	reg := NewTaskExecutorRegistry()
	builder := func(deps *TaskExecutorDependencies) TaskExecutor {
		return &fakeTaskExecutor{name: "test-executor"}
	}
	reg.AddTaskExecutor("test-type", builder)

	executor, err := reg.GetTaskExecutor("test-type", &TaskExecutorDependencies{})
	assert.NoError(t, err)
	assert.NotNil(t, executor)

	fe, ok := executor.(*fakeTaskExecutor)
	assert.True(t, ok)
	assert.Equal(t, "test-executor", fe.name)
}

// TestTaskExecutorRegistry_未注册返回错误 验证 Get 不存在的类型返回错误
func TestTaskExecutorRegistry_未注册返回错误(t *testing.T) {
	reg := NewTaskExecutorRegistry()

	executor, err := reg.GetTaskExecutor("non-existent", &TaskExecutorDependencies{})
	assert.Nil(t, executor)
	assert.Error(t, err)

	// 验证错误码
	baseErr, ok := err.(*exception.BaseError)
	assert.True(t, ok)
	assert.Equal(t, exception.StatusAgentControllerTaskExecutionError, baseErr.Status())
}

// TestTaskExecutorRegistry_移除 验证 Remove 后 Get 返回错误
func TestTaskExecutorRegistry_移除(t *testing.T) {
	reg := NewTaskExecutorRegistry()
	builder := func(deps *TaskExecutorDependencies) TaskExecutor {
		return &fakeTaskExecutor{name: "to-remove"}
	}
	reg.AddTaskExecutor("remove-type", builder)

	// 确认注册成功
	executor, err := reg.GetTaskExecutor("remove-type", &TaskExecutorDependencies{})
	assert.NoError(t, err)
	assert.NotNil(t, executor)

	// 移除后获取应报错
	reg.RemoveTaskExecutor("remove-type")
	executor, err = reg.GetTaskExecutor("remove-type", &TaskExecutorDependencies{})
	assert.Nil(t, executor)
	assert.Error(t, err)
}

// TestTaskExecutorRegistry_重复注册覆盖 验证 Add 两次后 Get 返回最新的
func TestTaskExecutorRegistry_重复注册覆盖(t *testing.T) {
	reg := NewTaskExecutorRegistry()

	builder1 := func(deps *TaskExecutorDependencies) TaskExecutor {
		return &fakeTaskExecutor{name: "v1"}
	}
	builder2 := func(deps *TaskExecutorDependencies) TaskExecutor {
		return &fakeTaskExecutor{name: "v2"}
	}

	reg.AddTaskExecutor("override-type", builder1)
	reg.AddTaskExecutor("override-type", builder2)

	executor, err := reg.GetTaskExecutor("override-type", &TaskExecutorDependencies{})
	assert.NoError(t, err)
	assert.NotNil(t, executor)

	fe, ok := executor.(*fakeTaskExecutor)
	assert.True(t, ok)
	assert.Equal(t, "v2", fe.name) // 应返回最新的 builder2 创建的实例
}

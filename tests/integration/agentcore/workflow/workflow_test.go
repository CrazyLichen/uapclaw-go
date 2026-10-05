//go:build integration

package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	workflow "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowBaseSuite 工作流基础类型的集成测试。
// 验证 WorkflowOutput 构造、状态枚举值、状态转换和错误传播。
type WorkflowBaseSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestWorkflowBaseSuite(t *testing.T) {
	suite.Run(t, new(WorkflowBaseSuite))
}

// TestWorkflowOutput_构造 验证 WorkflowOutput 构造和字段访问。
func (s *WorkflowBaseSuite) TestWorkflowOutput_构造() {
	output := &workflow.WorkflowOutput{
		Result: "执行成功",
		State:  workflow.WorkflowExecutionStateCompleted,
	}
	s.Equal("执行成功", output.Result)
	s.Equal(workflow.WorkflowExecutionStateCompleted, output.State)
}

// TestWorkflowOutput_空结果 验证 WorkflowOutput 零值状态。
func (s *WorkflowBaseSuite) TestWorkflowOutput_空结果() {
	var output workflow.WorkflowOutput
	s.Nil(output.Result)
	s.Empty(output.State)
}

// TestWorkflowExecutionState_枚举值 验证所有状态枚举值与 Python 一致。
func (s *WorkflowBaseSuite) TestWorkflowExecutionState_枚举值() {
	s.Equal(workflow.WorkflowExecutionState("COMPLETED"), workflow.WorkflowExecutionStateCompleted)
	s.Equal(workflow.WorkflowExecutionState("INPUT_REQUIRED"), workflow.WorkflowExecutionStateInputRequired)
	s.Equal(workflow.WorkflowExecutionState("ERROR"), workflow.WorkflowExecutionStateError)
}

// TestWorkflowExecutionState_字符串表示 验证状态枚举的字符串输出。
func (s *WorkflowBaseSuite) TestWorkflowExecutionState_字符串表示() {
	states := map[workflow.WorkflowExecutionState]string{
		workflow.WorkflowExecutionStateCompleted:     "COMPLETED",
		workflow.WorkflowExecutionStateInputRequired: "INPUT_REQUIRED",
		workflow.WorkflowExecutionStateError:         "ERROR",
	}
	for state, expected := range states {
		s.Equal(expected, string(state), "WorkflowExecutionState 字符串表示不匹配")
	}
}

// TestWorkflowOutput_状态管理 验证 WorkflowOutput 在不同状态下的语义正确性。
func (s *WorkflowBaseSuite) TestWorkflowOutput_状态管理() {
	// 完成状态
	completed := workflow.WorkflowOutput{
		Result: map[string]any{"answer": "42"},
		State:  workflow.WorkflowExecutionStateCompleted,
	}
	s.Equal(workflow.WorkflowExecutionStateCompleted, completed.State)
	s.NotNil(completed.Result)

	// 需要输入状态
	inputRequired := workflow.WorkflowOutput{
		Result: nil,
		State:  workflow.WorkflowExecutionStateInputRequired,
	}
	s.Equal(workflow.WorkflowExecutionStateInputRequired, inputRequired.State)

	// 错误状态
	errOutput := workflow.WorkflowOutput{
		Result: fmt.Errorf("执行失败: 参数缺失"),
		State:  workflow.WorkflowExecutionStateError,
	}
	s.Equal(workflow.WorkflowExecutionStateError, errOutput.State)
}

// TestWorkflowOutput_错误传播 验证错误通过 WorkflowOutput 传播。
func (s *WorkflowBaseSuite) TestWorkflowOutput_错误传播() {
	err := fmt.Errorf("步骤3执行失败: 数据库连接超时")
	output := workflow.WorkflowOutput{
		Result: err,
		State:  workflow.WorkflowExecutionStateError,
	}
	s.Equal(workflow.WorkflowExecutionStateError, output.State)

	// 验证 Result 中包含错误信息
	resultErr, ok := output.Result.(error)
	s.True(ok, "Result 应可断言为 error 类型")
	s.Contains(resultErr.Error(), "步骤3执行失败")
	s.Contains(resultErr.Error(), "数据库连接超时")
}

// TestWorkflowOutput_步骤注册 验证多步骤工作流的输出组合。
func (s *WorkflowBaseSuite) TestWorkflowOutput_步骤注册() {
	steps := []struct {
		name   string
		result any
		state  workflow.WorkflowExecutionState
	}{
		{"意图识别", "旅游", workflow.WorkflowExecutionStateCompleted},
		{"信息收集", map[string]any{"location": "杭州"}, workflow.WorkflowExecutionStateCompleted},
		{"插件调用", "杭州今天晴 30°C", workflow.WorkflowExecutionStateCompleted},
	}

	for i, step := range steps {
		output := workflow.WorkflowOutput{
			Result: step.result,
			State:  step.state,
		}
		s.Equal(workflow.WorkflowExecutionStateCompleted, output.State,
			"步骤 %d (%s) 应为 COMPLETED 状态", i, step.name)
		s.NotNil(output.Result, "步骤 %d (%s) 应有结果", i, step.name)
	}
}

// TestWorkflowOutput_中间中断 验证工作流在 INPUT_REQUIRED 状态下可暂停。
func (s *WorkflowBaseSuite) TestWorkflowOutput_中间中断() {
	// 模拟信息收集阶段需要用户输入
	output := workflow.WorkflowOutput{
		Result: map[string]any{
			"missing_fields": []string{"date"},
			"collected":      map[string]any{"location": "杭州"},
		},
		State: workflow.WorkflowExecutionStateInputRequired,
	}
	s.Equal(workflow.WorkflowExecutionStateInputRequired, output.State)

	result, ok := output.Result.(map[string]any)
	s.True(ok)
	missing, _ := result["missing_fields"].([]string)
	s.Contains(missing, "date")
}

// TestWorkflowExecutionState_比较 验证状态枚举可比较性。
func (s *WorkflowBaseSuite) TestWorkflowExecutionState_比较() {
	state1 := workflow.WorkflowExecutionStateCompleted
	state2 := workflow.WorkflowExecutionStateCompleted
	state3 := workflow.WorkflowExecutionStateError

	s.Equal(state1, state2)
	s.NotEqual(state1, state3)
}

// TestWorkflowOutput_结果类型 验证 Result 字段可承载不同类型数据。
func (s *WorkflowBaseSuite) TestWorkflowOutput_结果类型() {
	// 字符串结果
	s1 := workflow.WorkflowOutput{Result: "文本结果", State: workflow.WorkflowExecutionStateCompleted}
	s.Equal("文本结果", s1.Result)

	// map 结果
	m := map[string]any{"key": "value"}
	s2 := workflow.WorkflowOutput{Result: m, State: workflow.WorkflowExecutionStateCompleted}
	s.Equal(m, s2.Result)

	// 切片结果
	sl := []string{"a", "b", "c"}
	s3 := workflow.WorkflowOutput{Result: sl, State: workflow.WorkflowExecutionStateCompleted}
	s.Equal(sl, s3.Result)

	// nil 结果
	s4 := workflow.WorkflowOutput{Result: nil, State: workflow.WorkflowExecutionStateCompleted}
	s.Nil(s4.Result)
}

// TestInterface_接口满足 验证 WorkflowOutput 与回调框架的协作能力。
func (s *WorkflowBaseSuite) TestInterface_接口满足() {
	// 验证 AgentRail 接口定义存在（编译时检查）
	var _ agentinterfaces.AgentRail

	// 验证回调选项可构造
	_ = cb.WithPriority(10)
	_ = cb.WithOnce()
}

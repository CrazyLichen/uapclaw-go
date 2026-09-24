package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeAgentFlowService 用于测试的模拟 Agent 执行服务
type fakeAgentFlowService struct {
	// answers 每个 sessionID 对应的回答
	answers map[string]string
	// trajectories 每个 sessionID 对应的轨迹
	trajectories map[string]string
	// err 每个 sessionID 对应的错误
	err map[string]error
	// calls 记录所有调用
	calls []callRecord
}

// callRecord 调用记录
type callRecord struct {
	query     string
	sessionID string
}

// Execute 实现 AgentFlowService 接口
func (f *fakeAgentFlowService) Execute(ctx context.Context, query string, sessionID string, _ ...cecontext.AgentFlowOption) (*cecontext.TrajectoryResult, error) {
	f.calls = append(f.calls, callRecord{query: query, sessionID: sessionID})

	if err, ok := f.err[sessionID]; ok {
		return nil, err
	}

	answer := f.answers[sessionID]
	trajectory := f.trajectories[sessionID]

	return &cecontext.TrajectoryResult{
		Answer:     answer,
		Trajectory: trajectory,
		Success:    true,
	}, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestFormatTrajectory_正常格式化(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "I need to calculate this.", ToolName: "calculator", ToolArgs: `{"expr": "2+2"}`},
		{Role: "tool", Content: "4"},
		{Role: "assistant", Content: "The answer is 4."},
	}

	result := formatTrajectory(messages)

	expected := []string{
		"USER: What is 2+2?",
		"THOUGHT: I need to calculate this.",
		"ACTION: calculator({\"expr\": \"2+2\"})",
		"OBSERVATION: 4",
		"THOUGHT: The answer is 4.",
	}
	for _, line := range expected {
		if !strings.Contains(result, line) {
			t.Errorf("轨迹缺少 %q", line)
		}
	}
}

func TestFormatTrajectory_去除记忆注入块(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Question: What is Go?\nSome Related Experience to help you complete the task\nExperience1\nExperience2"},
	}

	result := formatTrajectory(messages)

	if strings.Contains(result, "Some Related Experience") {
		t.Error("应去除记忆注入块")
	}
	if !strings.Contains(result, "USER: What is Go?") {
		t.Errorf("应保留 Question 之后的内容，得到: %q", result)
	}
}

func TestFormatTrajectory_去除Task前缀(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Task:\nFind the bug in the code."},
	}

	result := formatTrajectory(messages)

	if strings.Contains(result, "Task:") {
		t.Error("应去除 Task: 前缀")
	}
	if !strings.Contains(result, "USER: Find the bug in the code.") {
		t.Errorf("应保留 Task 之后的内容，得到: %q", result)
	}
}

func TestEvaluateTrial_有GroundTruth成功(t *testing.T) {
	feedback, score := EvaluateTrial("What is 2+2?", "The answer is four", "four")
	if feedback != "success" {
		t.Errorf("期望 feedback=success，得到 %q", feedback)
	}
	if score != 1 {
		t.Errorf("期望 score=1，得到 %d", score)
	}
}

func TestEvaluateTrial_有GroundTruth失败(t *testing.T) {
	feedback, score := EvaluateTrial("What is 2+2?", "The answer is five", "four")
	if feedback != "failure" {
		t.Errorf("期望 feedback=failure，得到 %q", feedback)
	}
	if score != 0 {
		t.Errorf("期望 score=0，得到 %d", score)
	}
}

func TestEvaluateTrial_无GroundTruth(t *testing.T) {
	feedback, score := EvaluateTrial("What is 2+2?", "Some answer", "")
	if feedback != "success" {
		t.Errorf("期望 feedback=success，得到 %q", feedback)
	}
	if score != 1 {
		t.Errorf("期望 score=1，得到 %d", score)
	}
}

func TestRunTrials_none模式(t *testing.T) {
	agent := &fakeAgentFlowService{
		answers:      map[string]string{"trial_0": "answer0"},
		trajectories: map[string]string{"trial_0": "USER: q\nASSISTANT: a"},
		err:          map[string]error{},
	}

	results, err := RunTrials(context.Background(), agent, RunTrialsInput{
		Agent:       agent,
		Question:    "test question",
		GroundTruth: "",
		MattsK:      5, // none 模式下应忽略
		MattsMode:   "none",
	})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("none 模式应只执行 1 次试验，得到 %d 次", len(results))
	}
}

func TestRunTrials_parallel模式(t *testing.T) {
	k := 3
	agent := &fakeAgentFlowService{
		answers: map[string]string{
			"trial_0": "answer0",
			"trial_1": "answer1",
			"trial_2": "answer2",
		},
		trajectories: map[string]string{
			"trial_0": "traj0",
			"trial_1": "traj1",
			"trial_2": "traj2",
		},
		err: map[string]error{},
	}

	results, err := RunTrials(context.Background(), agent, RunTrialsInput{
		Agent:       agent,
		Question:    "test question",
		GroundTruth: "",
		MattsK:      k,
		MattsMode:   "parallel",
	})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != k {
		t.Errorf("parallel 模式应执行 %d 次试验，得到 %d 次", k, len(results))
	}

	// parallel 模式：每次查询都应是 "Question: {question}"
	for _, call := range agent.calls {
		expected := "Question: test question"
		if call.query != expected {
			t.Errorf("parallel 模式每次查询应为 %q，得到 %q", expected, call.query)
		}
	}
}

func TestRunTrials_sequential模式(t *testing.T) {
	k := 3
	agent := &fakeAgentFlowService{
		answers: map[string]string{
			"trial_0": "answer0",
			"trial_1": "answer1",
			"trial_2": "answer2",
		},
		trajectories: map[string]string{
			"trial_0": "USER: q\nTHOUGHT: thinking0",
			"trial_1": "USER: q\nTHOUGHT: thinking1",
			"trial_2": "USER: q\nTHOUGHT: thinking2",
		},
		err: map[string]error{},
	}

	results, err := RunTrials(context.Background(), agent, RunTrialsInput{
		Agent:       agent,
		Question:    "test question",
		GroundTruth: "",
		MattsK:      k,
		MattsMode:   "sequential",
	})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != k {
		t.Errorf("sequential 模式应执行 %d 次试验，得到 %d 次", k, len(results))
	}

	// 第 1 次调用应为普通 "Question: ..." 格式
	if !strings.HasPrefix(agent.calls[0].query, "Question: ") {
		t.Errorf("第 1 次查询应为普通格式，得到 %q", agent.calls[0].query)
	}

	// 第 2、3 次调用应包含 "Previous attempt:" 和自纠正提示词
	for i := 1; i < k; i++ {
		if !strings.Contains(agent.calls[i].query, "Previous attempt:") {
			t.Errorf("第 %d 次查询应包含 Previous attempt，得到 %q", i+1, agent.calls[i].query)
		}
		if !strings.Contains(agent.calls[i].query, selfRefinePrompt[:30]) {
			t.Errorf("第 %d 次查询应包含自纠正提示词，得到 %q", i+1, agent.calls[i].query)
		}
	}
}

func TestRunTrials_AgentFlow失败(t *testing.T) {
	k := 3
	agent := &fakeAgentFlowService{
		answers: map[string]string{
			"trial_0": "answer0",
			"trial_2": "answer2",
		},
		trajectories: map[string]string{
			"trial_0": "traj0",
			"trial_2": "traj2",
		},
		err: map[string]error{
			"trial_1": fmt.Errorf("agent execution failed"),
		},
	}

	results, err := RunTrials(context.Background(), agent, RunTrialsInput{
		Agent:       agent,
		Question:    "test question",
		GroundTruth: "",
		MattsK:      k,
		MattsMode:   "parallel",
	})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != k {
		t.Errorf("应返回 %d 个结果（含失败），得到 %d 个", k, len(results))
	}

	// 第 2 次试验应为空值
	if results[1].Trajectory != "" || results[1].Feedback != "" || results[1].Score != 0 {
		t.Errorf("失败试验应有空值，得到 Trajectory=%q Feedback=%q Score=%d",
			results[1].Trajectory, results[1].Feedback, results[1].Score)
	}

	// 第 1、3 次试验应有结果
	if results[0].Feedback != "success" {
		t.Errorf("第 1 次试验应为成功，得到 Feedback=%q", results[0].Feedback)
	}
	if results[2].Feedback != "success" {
		t.Errorf("第 3 次试验应为成功，得到 Feedback=%q", results[2].Feedback)
	}
}

// TestIsReMeFamily 验证 ReMe 系列判断。
func TestIsReMeFamily(t *testing.T) {
	tests := []struct {
		algo     string
		expected bool
	}{
		{"ReMe", true},
		{"RefCon", true},
		{"DivCon", true},
		{"ACE", false},
		{"ReasoningBank", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.algo, func(t *testing.T) {
			assert.Equal(t, tc.expected, isReMeFamily(tc.algo))
		})
	}
}

// TestGetUseGoldLabel 验证从 ceconfig 读取 USE_GOLDLABEL。
func TestGetUseGoldLabel(t *testing.T) {
	// 默认值
	ceconfig.Delete("USE_GOLDLABEL")
	assert.False(t, getUseGoldLabel())

	// 设置为 true
	ceconfig.Set("USE_GOLDLABEL", "true")
	assert.True(t, getUseGoldLabel())

	// 清理
	ceconfig.Delete("USE_GOLDLABEL")
}

// TestGetUseGroundTruth 验证从 ceconfig 读取 USE_GROUNDTRUTH。
func TestGetUseGroundTruth(t *testing.T) {
	// 默认值
	ceconfig.Delete("USE_GROUNDTRUTH")
	assert.False(t, getUseGroundTruth())

	// 设置为 true
	ceconfig.Set("USE_GROUNDTRUTH", "true")
	assert.True(t, getUseGroundTruth())

	// 清理
	ceconfig.Delete("USE_GROUNDTRUTH")
}

// TestSummarizeTrajectories_ACE 验证 ACE 算法 SummarizeTrajectories。
func TestSummarizeTrajectories_ACE(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ACE",
		summaryFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("memories", []*schema.VectorNode{
					schema.NewVectorNode("id1", "content", []float64{0.1}, nil),
				})
			},
		},
	}

	ceconfig.Delete("USE_GROUNDTRUTH")
	result, err := SummarizeTrajectories(context.Background(), svc, "user1", SummarizeTrajectoriesInput{
		Trajectory: []string{"traj1"},
		Feedback:   []string{"success"},
		Score:      []int{1},
		MattsMode:  "parallel",
		Query:      "test query",
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ACE", result.Algorithm)
}

// TestSummarizeTrajectories_Sequential截断 验证 sequential 模式只保留最后一条轨迹。
func TestSummarizeTrajectories_Sequential截断(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ReMe",
		summaryFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("memories", []*schema.VectorNode{})
			},
		},
	}

	result, err := SummarizeTrajectories(context.Background(), svc, "user1", SummarizeTrajectoriesInput{
		Trajectory: []string{"traj1", "traj2", "traj3"},
		Feedback:   []string{"fail", "fail", "success"},
		Score:      []int{0, 0, 1},
		MattsMode:  "sequential",
		Query:      "test query",
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestSummarizeTrajectories_ReMe传Score 验证 ReMe 系列传入 score。
func TestSummarizeTrajectories_ReMe传Score(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ReMe",
		summaryFlow: &mockFlowOp{
			setupFn: func(rc *cecontext.RuntimeContext) {
				rc.Set("memories", []*schema.VectorNode{})
			},
		},
	}

	result, err := SummarizeTrajectories(context.Background(), svc, "user1", SummarizeTrajectoriesInput{
		Trajectory: []string{"traj1"},
		Score:      []int{1},
		MattsMode:  "parallel",
		Query:      "test query",
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestSummarizeTrajectories_执行失败 验证 flow 失败返回错误。
func TestSummarizeTrajectories_执行失败(t *testing.T) {
	svc := &TaskMemoryService{
		summaryAlgorithm: "ACE",
		summaryFlow: &mockFlowOp{
			err: fmt.Errorf("summarize failed"),
		},
	}

	_, err := SummarizeTrajectories(context.Background(), svc, "user1", SummarizeTrajectoriesInput{
		Trajectory: []string{"traj1"},
		MattsMode:  "parallel",
		Query:      "test query",
	})
	require.Error(t, err)
}

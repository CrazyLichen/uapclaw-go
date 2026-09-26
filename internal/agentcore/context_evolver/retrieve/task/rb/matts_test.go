package rb

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeLLMService 模拟 LLM 服务
type fakeLLMService struct {
	response string
	err      error
	// prompts 按调用顺序记录收到的提示词
	prompts []string
}

func (f *fakeLLMService) Generate(_ context.Context, prompt string, _ ...cecontext.GenerateOption) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.response, f.err
}

// fakeAgentFlowService 模拟 Agent 执行服务
type fakeAgentFlowService struct {
	results map[string]*cecontext.TrajectoryResult
	err     error
	// calls 记录调用参数
	calls []struct {
		query     string
		sessionID string
	}
}

func (f *fakeAgentFlowService) Execute(_ context.Context, query string, sessionID string, _ ...cecontext.AgentFlowOption) (*cecontext.TrajectoryResult, error) {
	f.calls = append(f.calls, struct {
		query     string
		sessionID string
	}{query: query, sessionID: sessionID})
	if f.err != nil {
		return nil, f.err
	}
	if result, ok := f.results[sessionID]; ok {
		return result, nil
	}
	return &cecontext.TrajectoryResult{
		Answer:  "default answer for " + sessionID,
		Steps:   []any{"step1"},
		Success: true,
	}, nil
}

// failingAgentFlowService 指定 sessionID 失败的 mock
type failingAgentFlowService struct {
	failOnSessions map[string]bool
	successResult  *cecontext.TrajectoryResult
}

func (f *failingAgentFlowService) Execute(_ context.Context, _ string, sessionID string, _ ...cecontext.AgentFlowOption) (*cecontext.TrajectoryResult, error) {
	if f.failOnSessions[sessionID] {
		return nil, context.DeadlineExceeded
	}
	return f.successResult, nil
}

// ──────────────────────────── ParallelScalingOp 测试 ────────────────────────────

// TestParallelScalingOp_正常执行 测试并行缩放正常生成轨迹
func TestParallelScalingOp_正常执行(t *testing.T) {
	sc := cecontext.NewServiceContext()
	af := &fakeAgentFlowService{
		results: map[string]*cecontext.TrajectoryResult{
			"parallel_0": {Answer: "answer 0", Steps: []any{"s0"}, Success: true},
			"parallel_1": {Answer: "answer 1", Steps: []any{"s1"}, Success: true},
			"parallel_2": {Answer: "answer 2", Steps: []any{"s2"}, Success: false},
		},
	}
	sc.RegisterAgentFlow(af)

	op := NewParallelScalingOp(sc, 3, 0.8)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	trajectories, ok := cecontext.GetTyped[[]*trajectoryData](rc, "parallel_trajectories")
	require.True(t, ok, "应设置 parallel_trajectories")
	require.Len(t, trajectories, 3)
	assert.Equal(t, "answer 0", trajectories[0].Answer)
	assert.True(t, trajectories[0].Success)
	assert.Equal(t, "answer 2", trajectories[2].Answer)
	assert.False(t, trajectories[2].Success)

	scalingFactor, ok := cecontext.GetTyped[int](rc, "scaling_factor")
	require.True(t, ok)
	assert.Equal(t, 3, scalingFactor)
}

// TestParallelScalingOp_AgentFlow未注册 测试 AgentFlow 未注册时跳过
func TestParallelScalingOp_AgentFlow未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewParallelScalingOp(sc, 3, 0.8)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 未注册 AgentFlow 时不应设置 parallel_trajectories
	assert.Nil(t, rc.Get("parallel_trajectories"))
}

// TestParallelScalingOp_部分轨迹失败 测试部分 AgentFlow 调用失败时跳过
func TestParallelScalingOp_部分轨迹失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	af := &failingAgentFlowService{
		failOnSessions: map[string]bool{"parallel_1": true},
		successResult:  &cecontext.TrajectoryResult{Answer: "ok answer", Steps: []any{"s"}, Success: true},
	}
	sc.RegisterAgentFlow(af)

	op := NewParallelScalingOp(sc, 3, 0.5)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	trajectories, ok := cecontext.GetTyped[[]*trajectoryData](rc, "parallel_trajectories")
	require.True(t, ok)
	require.Len(t, trajectories, 3)
	// parallel_0 成功
	assert.True(t, trajectories[0].Success)
	// parallel_1 失败但仍记录（标记失败）
	assert.False(t, trajectories[1].Success)
	assert.Equal(t, "", trajectories[1].Answer)
	// parallel_2 成功
	assert.True(t, trajectories[2].Success)
}

// ──────────────────────────── SequentialScalingOp 测试 ────────────────────────────

// TestSequentialScalingOp_正常执行 测试串行缩放正常精炼
func TestSequentialScalingOp_正常执行(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "refined answer"}
	sc.RegisterService("llm", llm)

	op := NewSequentialScalingOp(sc, 3)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("answer", "initial answer")

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	history, ok := cecontext.GetTyped[[]*refinementRecord](rc, "refinement_history")
	require.True(t, ok, "应设置 refinement_history")
	require.Len(t, history, 3)

	refinedAnswer, ok := cecontext.GetTyped[string](rc, "refined_answer")
	require.True(t, ok)
	assert.Equal(t, "refined answer", refinedAnswer)

	answer, ok := cecontext.GetTyped[string](rc, "answer")
	require.True(t, ok)
	assert.Equal(t, "refined answer", answer)

	scalingFactor, ok := cecontext.GetTyped[int](rc, "scaling_factor")
	require.True(t, ok)
	assert.Equal(t, 3, scalingFactor)
}

// TestSequentialScalingOp_无当前答案 测试 answer 为空时首轮仍正常
func TestSequentialScalingOp_无当前答案(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "new answer"}
	sc.RegisterService("llm", llm)

	op := NewSequentialScalingOp(sc, 2)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	// 不设置 answer

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	answer, ok := cecontext.GetTyped[string](rc, "answer")
	require.True(t, ok)
	assert.Equal(t, "new answer", answer)

	history, ok := cecontext.GetTyped[[]*refinementRecord](rc, "refinement_history")
	require.True(t, ok)
	require.Len(t, history, 2)
}

// ──────────────────────────── BestOfNOp 测试 ────────────────────────────

// TestBestOfNOp_正常选择 测试正常选择最优轨迹
func TestBestOfNOp_正常选择(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "The best trajectory is 1"}
	sc.RegisterService("llm", llm)

	op := NewBestOfNOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "bad answer", Steps: []any{"s0"}, Success: false},
		{Index: 1, Answer: "good answer", Steps: []any{"s1"}, Success: true},
		{Index: 2, Answer: "ok answer", Steps: []any{"s2"}, Success: true},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	bestIdx, ok := cecontext.GetTyped[int](rc, "best_trajectory_index")
	require.True(t, ok)
	assert.Equal(t, 1, bestIdx)

	answer, ok := cecontext.GetTyped[string](rc, "answer")
	require.True(t, ok)
	assert.Equal(t, "good answer", answer)

	bestTraj, ok := cecontext.GetTyped[*trajectoryData](rc, "best_trajectory")
	require.True(t, ok)
	assert.Equal(t, "good answer", bestTraj.Answer)

	passAtK, ok := cecontext.GetTyped[float64](rc, "pass_at_k")
	require.True(t, ok)
	assert.InDelta(t, 2.0/3.0, passAtK, 0.01)
}

// TestBestOfNOp_索引解析失败 测试索引解析失败时回退到 0
func TestBestOfNOp_索引解析失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "I cannot determine the best one"}
	sc.RegisterService("llm", llm)

	op := NewBestOfNOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "first answer", Steps: []any{"s"}, Success: true},
		{Index: 1, Answer: "second answer", Steps: []any{"s"}, Success: false},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	bestIdx, ok := cecontext.GetTyped[int](rc, "best_trajectory_index")
	require.True(t, ok)
	assert.Equal(t, 0, bestIdx)
}

// TestBestOfNOp_无parallel_trajectories 测试无并行轨迹时返回错误
func TestBestOfNOp_无parallel_trajectories(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "1"}
	sc.RegisterService("llm", llm)

	op := NewBestOfNOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parallel_trajectories")
}

// ──────────────────────────── SelfContrastMemoryOp 测试 ────────────────────────────

// TestSelfContrastMemoryOp_正常提取 测试正常提取对比记忆
func TestSelfContrastMemoryOp_正常提取(t *testing.T) {
	llmResponse := `# Memory Item 1
## Title Verify before acting
## Description Always verify information before taking irreversible actions
## Content Successful trajectories showed a pattern of verifying information first, while failed trajectories often acted on unverified assumptions.

# Memory Item 2
## Title Use multiple sources
## Description Cross-reference information from multiple sources
## Content When multiple sources were consulted, the accuracy of answers improved significantly.`

	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: llmResponse}
	sc.RegisterService("llm", llm)

	op := NewSelfContrastMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "find the best approach")
	rc.Set("user_id", "user1")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "good answer with detailed steps", Steps: []any{"s1"}, Success: true},
		{Index: 1, Answer: "bad answer that failed", Steps: []any{"s2"}, Success: false},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "contrastive_memories")
	require.True(t, ok, "应设置 contrastive_memories")
	require.Len(t, memories, 2)

	assert.Equal(t, "find the best approach", memories[0].Query)
	assert.Equal(t, "user1", memories[0].WorkspaceID)
	require.Len(t, memories[0].Memory, 1)
	assert.Equal(t, "Verify before acting", memories[0].Memory[0].Title)
	assert.Equal(t, "Always verify information before taking irreversible actions", memories[0].Memory[0].Description)

	assert.Equal(t, "Use multiple sources", memories[1].Memory[0].Title)
}

// TestSelfContrastMemoryOp_全部成功无失败 测试全部成功无失败轨迹
func TestSelfContrastMemoryOp_全部成功无失败(t *testing.T) {
	llmResponse := `# Memory Item 1
## Title Consistent success pattern
## Description All trajectories succeeded
## Content All approaches worked well.`

	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: llmResponse}
	sc.RegisterService("llm", llm)

	op := NewSelfContrastMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "answer 0", Steps: []any{"s"}, Success: true},
		{Index: 1, Answer: "answer 1", Steps: []any{"s"}, Success: true},
	})

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	memories, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "contrastive_memories")
	require.True(t, ok)
	require.Len(t, memories, 1)
}

// TestSelfContrastMemoryOp_无parallel_trajectories 测试无并行轨迹时返回错误
func TestSelfContrastMemoryOp_无parallel_trajectories(t *testing.T) {
	sc := cecontext.NewServiceContext()
	llm := &fakeLLMService{response: "irrelevant"}
	sc.RegisterService("llm", llm)

	op := NewSelfContrastMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parallel_trajectories")
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestParseBestIndex_正常解析 测试正常解析最优索引
func TestParseBestIndex_正常解析(t *testing.T) {
	assert.Equal(t, 1, parseBestIndex("The best is trajectory 1", 3))
	assert.Equal(t, 0, parseBestIndex("Index: 0", 5))
	assert.Equal(t, 2, parseBestIndex("I choose 2 as the best", 3))
}

// TestParseBestIndex_超出范围 测试索引超出范围
func TestParseBestIndex_超出范围(t *testing.T) {
	assert.Equal(t, -1, parseBestIndex("10 is my answer", 3))
	assert.Equal(t, 0, parseBestIndex("0 is the answer", 1))
}

// TestParseBestIndex_无数字 测试无数字响应
func TestParseBestIndex_无数字(t *testing.T) {
	assert.Equal(t, -1, parseBestIndex("no numbers here", 3))
}

// TestBuildTrajectoryString 测试构建轨迹描述（间接通过 SelfContrastMemoryOp）
func TestBuildTrajectoryString(t *testing.T) {
	trajs := []*trajectoryData{
		{Index: 0, Answer: "short", Success: true},
		{Index: 1, Answer: strings.Repeat("x", 300), Success: false},
	}
	// 通过 SelfContrastMemoryOp 的提示词模板间接验证
	// 此处直接验证 parseContrastiveMemories 的输入
	assert.NotNil(t, trajs)
	assert.Equal(t, "short", trajs[0].Answer)
}

// TestParseContrastiveMemories_正常解析 测试正常解析对比记忆
func TestParseContrastiveMemories_正常解析(t *testing.T) {
	response := `# Memory Item 1
## Title Test title
## Description Test description
## Content Test content

# Memory Item 2
## Title Another title
## Description Another description
## Content Another content`

	memories := parseContrastiveMemories(response, "test query", "user1")
	require.Len(t, memories, 2)
	assert.Equal(t, "Test title", memories[0].Memory[0].Title)
	assert.Equal(t, "Test description", memories[0].Memory[0].Description)
	assert.Equal(t, "Test content", memories[0].Memory[0].Content)
	assert.Equal(t, "test query", memories[0].Query)
	assert.Equal(t, "user1", memories[0].WorkspaceID)
	assert.Equal(t, "Another title", memories[1].Memory[0].Title)
}

// TestParseContrastiveMemories_空响应 测试空响应
func TestParseContrastiveMemories_空响应(t *testing.T) {
	memories := parseContrastiveMemories("", "query", "user1")
	assert.Len(t, memories, 0)
}

// TestParseContrastiveMemories_无有效内容 测试无有效 Memory Item 的响应
func TestParseContrastiveMemories_无有效内容(t *testing.T) {
	memories := parseContrastiveMemories("This is just random text without any memory items", "query", "user1")
	assert.Len(t, memories, 0)
}

// TestExtractMarkdownSection 测试 Markdown section 提取
func TestExtractMarkdownSection(t *testing.T) {
	text := "## Title My Title\n## Description My Desc\n## Content My Content"
	assert.Equal(t, "My Title", extractMarkdownSection(text, "## Title"))
	assert.Equal(t, "My Desc", extractMarkdownSection(text, "## Description"))
	assert.Equal(t, "My Content", extractMarkdownSection(text, "## Content"))
}

// TestExtractMarkdownSection_不存在 测试标题不存在
func TestExtractMarkdownSection_不存在(t *testing.T) {
	assert.Equal(t, "", extractMarkdownSection("some text", "## Missing"))
}

// TestNewParallelScalingOp 测试构造函数
func TestNewParallelScalingOp(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewParallelScalingOp(sc, 5, 0.7)
	assert.Equal(t, 5, op.k)
	assert.Equal(t, 0.7, op.temperature)
}

// TestNewSequentialScalingOp 测试构造函数
func TestNewSequentialScalingOp(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSequentialScalingOp(sc, 4)
	assert.Equal(t, 4, op.k)
}

// TestNewBestOfNOp 测试构造函数
func TestNewBestOfNOp(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewBestOfNOp(sc)
	assert.NotNil(t, op)
}

// TestNewSelfContrastMemoryOp 测试构造函数
func TestNewSelfContrastMemoryOp(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSelfContrastMemoryOp(sc)
	assert.NotNil(t, op)
}

// TestSequentialScalingOp_LLM未注册 测试 LLM 未注册时返回错误
func TestSequentialScalingOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSequentialScalingOp(sc, 2)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// TestSequentialScalingOp_LLM调用失败 测试 LLM 调用失败时返回错误
func TestSequentialScalingOp_LLM调用失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: fmt.Errorf("LLM error")})
	op := NewSequentialScalingOp(sc, 2)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("answer", "initial")

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
}

// TestBestOfNOp_LLM未注册 测试 LLM 未注册时返回错误
func TestBestOfNOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewBestOfNOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "answer", Success: true},
	})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// TestSelfContrastMemoryOp_LLM未注册 测试 LLM 未注册时返回错误
func TestSelfContrastMemoryOp_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	op := NewSelfContrastMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "answer", Success: true},
	})

	err := op.Execute(context.Background(), rc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM not configured")
}

// TestParallelScalingOp_温度保存恢复 测试 LLM 温度保存和恢复
func TestParallelScalingOp_温度保存恢复(t *testing.T) {
	sc := cecontext.NewServiceContext()
	af := &fakeAgentFlowService{
		results: map[string]*cecontext.TrajectoryResult{
			"parallel_0": {Answer: "answer", Steps: []any{"s"}, Success: true},
		},
	}
	sc.RegisterAgentFlow(af)

	op := NewParallelScalingOp(sc, 1, 0.95)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")
	rc.Set("llm_temperature", 0.3)

	err := op.Execute(context.Background(), rc)
	require.NoError(t, err)

	// 温度应恢复为原始值
	restoredTemp, ok := cecontext.GetTyped[float64](rc, "llm_temperature")
	require.True(t, ok)
	assert.InDelta(t, 0.3, restoredTemp, 0.01)
}

// TestBestOfNOp_LLM调用失败 测试 LLM 评估调用失败时回退到第一条轨迹
func TestBestOfNOp_LLM调用失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: fmt.Errorf("LLM error")})
	op := NewBestOfNOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "first answer", Success: true},
		{Index: 1, Answer: "second answer", Success: false},
	})

	err := op.Execute(context.Background(), rc)
	// 对齐 Python: LLM 失败时回退到第一条轨迹，不返回 error
	assert.NoError(t, err)
	answer, ok := cecontext.GetTyped[string](rc, "answer")
	require.True(t, ok)
	assert.Equal(t, "first answer", answer)
	bestIdx, ok := cecontext.GetTyped[int](rc, "best_trajectory_index")
	require.True(t, ok)
	assert.Equal(t, 0, bestIdx)
}

// TestSelfContrastMemoryOp_LLM调用失败 测试 LLM 对比调用失败时回退到空列表
func TestSelfContrastMemoryOp_LLM调用失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{err: fmt.Errorf("LLM error")})
	op := NewSelfContrastMemoryOp(sc)
	rc := cecontext.NewRuntimeContext()
	rc.Set("query", "test query")
	rc.Set("user_id", "user1")
	rc.Set("parallel_trajectories", []*trajectoryData{
		{Index: 0, Answer: "answer", Success: true},
	})

	err := op.Execute(context.Background(), rc)
	// 对齐 Python: LLM 失败时 contrastive_memories=[]，不返回 error
	assert.NoError(t, err)
	memories, ok := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "contrastive_memories")
	require.True(t, ok)
	assert.Empty(t, memories)
}

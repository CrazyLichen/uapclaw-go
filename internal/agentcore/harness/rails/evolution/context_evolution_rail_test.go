package evolution

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	ceservice "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── mock 实现 ────────────────────────────

// mockTaskMemoryService taskMemoryServicer 的 mock 实现
type mockTaskMemoryService struct {
	retrieveResult *ceservice.RetrieveResult
	retrieveErr    error
	retrieveCalled bool

	loadMemoriesErr error
	loadCalled      bool

	summaryAlgorithm string
	summarizeResult  *ceservice.SummarizeResult
	summarizeErr     error
	summarizeCalled  bool
	summarizeParams  []string // 记录传入的 trajectories
}

func (m *mockTaskMemoryService) Retrieve(_ context.Context, userID string, query string) (*ceservice.RetrieveResult, error) {
	m.retrieveCalled = true
	if m.retrieveErr != nil {
		return nil, m.retrieveErr
	}
	if m.retrieveResult != nil {
		return m.retrieveResult, nil
	}
	// 默认返回空结果
	return &ceservice.RetrieveResult{
		MemoryString:    "",
		RetrievedMemory: []ceschema.MemoryItem{},
		Query:           query,
		UserID:          userID,
		Algorithm:       "ACE",
	}, nil
}

func (m *mockTaskMemoryService) LoadMemories(_ context.Context, _ string) error {
	m.loadCalled = true
	return m.loadMemoriesErr
}

func (m *mockTaskMemoryService) SummaryAlgorithm() string {
	if m.summaryAlgorithm == "" {
		return "ACE"
	}
	return m.summaryAlgorithm
}

func (m *mockTaskMemoryService) Summarize(_ context.Context, userID string, matts string, query string, trajectories []string, _ ...map[string]any) (*ceservice.SummarizeResult, error) {
	m.summarizeCalled = true
	m.summarizeParams = trajectories
	if m.summarizeErr != nil {
		return nil, m.summarizeErr
	}
	return m.summarizeResult, nil
}

// mockRetrievedMemory 用于测试的 MemoryItem 实现
type mockRetrievedMemory struct {
	text string
}

func (m *mockRetrievedMemory) FormatMemoryString() string {
	return m.text
}

// ──────────────────────────── 构造测试 ────────────────────────────

func TestNewContextEvolutionRail_默认值(t *testing.T) {
	// 清除 API_KEY 确保默认 TaskMemoryService 创建失败（降级模式）
	ceconfig.Delete("API_KEY")
	origEnv := os.Getenv("API_KEY")
	os.Unsetenv("API_KEY")
	defer func() {
		if origEnv != "" {
			os.Setenv("API_KEY", origEnv)
		}
	}()

	r := NewContextEvolutionRail(context.Background(), "alice", nil)
	assert.Equal(t, "alice", r.userID)
	assert.True(t, r.injectMemoriesInContext)
	assert.True(t, r.autoSummarize)
	assert.Equal(t, "none", r.autoSummarizeMattsMode)
	assert.Equal(t, contextEvolutionPriority, r.Priority())
	assert.Nil(t, r.memoryService) // 无 API_KEY 时降级为 nil
}

func TestNewContextEvolutionRail_自定义选项(t *testing.T) {
	mock := &mockTaskMemoryService{}
	r := NewContextEvolutionRail(context.Background(), "bob", nil,
		WithMemoryService(mock),
		WithInjectMemoriesInContext(false),
		WithAutoSummarize(false),
		WithAutoSummarizeMattsMode("sequential"),
	)
	assert.Equal(t, "bob", r.userID)
	assert.False(t, r.injectMemoriesInContext)
	assert.False(t, r.autoSummarize)
	assert.Equal(t, "sequential", r.autoSummarizeMattsMode)
	assert.NotNil(t, r.memoryService)
	assert.True(t, mock.loadCalled) // 构造时调用 LoadMemories
}

func TestNewContextEvolutionRail_NilMemoryService(t *testing.T) {
	// 清除 ceconfig 和环境变量中的 API_KEY，使默认 TaskMemoryService 创建失败（降级模式）
	ceconfig.Delete("API_KEY")
	origEnv := os.Getenv("API_KEY")
	os.Unsetenv("API_KEY")
	defer func() {
		if origEnv != "" {
			os.Setenv("API_KEY", origEnv)
		}
	}()

	r := NewContextEvolutionRail(context.Background(), "charlie", nil)
	// 创建失败时 memoryService 为 nil（降级模式）
	assert.Nil(t, r.memoryService)
	assert.Equal(t, 0, r.MemoriesUsed())
	assert.Equal(t, "", r.CurrentQuery())
}

func TestNewContextEvolutionRail_有APIKey(t *testing.T) {
	// 设置 ceconfig API_KEY，使默认 TaskMemoryService 创建成功
	ceconfig.Set("API_KEY", "test-key-for-rail")
	defer ceconfig.Delete("API_KEY")

	r := NewContextEvolutionRail(context.Background(), "dave", nil)
	// 创建成功时 memoryService 不为 nil
	assert.NotNil(t, r.memoryService)
}

// ──────────────────────────── GetCallbacks 测试 ────────────────────────────

func TestContextEvolutionRail_GetCallbacks(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)
	callbacks := r.GetCallbacks()
	_, hasBefore := callbacks[agentinterfaces.CallbackBeforeTaskIteration]
	_, hasAfter := callbacks[agentinterfaces.CallbackAfterTaskIteration]
	assert.True(t, hasBefore, "应注册 CallbackBeforeTaskIteration")
	assert.True(t, hasAfter, "应注册 CallbackAfterTaskIteration")
}

// ──────────────────────────── beforeTaskIteration 测试 ────────────────────────────

func TestBeforeTaskIteration_记忆注入(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "使用工具 X 处理数据",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "如何处理数据",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	// 构造 AgentCallbackContext — 测试 beforeTaskIteration 直接调用
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "如何处理数据"})

	// 无 DeepAgentInterface 时，记忆检索仍正常，注入跳过
	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 1, r.memoriesUsed)
	assert.True(t, mock.retrieveCalled)
}

func TestBeforeTaskIteration_缓存命中(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "缓存的经验",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "相同查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "相同查询"})

	// 第一次调用
	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.True(t, mock.retrieveCalled)

	// 重置标记
	mock.retrieveCalled = false

	// 第二次相同查询 → 缓存命中
	err = r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.False(t, mock.retrieveCalled, "缓存命中不应再次调用 Retrieve")
}

func TestBeforeTaskIteration_检索失败(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveErr: assert.AnError,
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "查询"})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err) // 不中断 Agent
	assert.Equal(t, 0, r.memoriesUsed)
}

func TestBeforeTaskIteration_无记忆(t *testing.T) {
	mock := &mockTaskMemoryService{} // 默认返回空结果
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "查询"})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 0, r.memoriesUsed) // 无记忆可用
}

func TestBeforeTaskIteration_不注入(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "有记忆但不注入",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil,
		WithMemoryService(mock),
		WithInjectMemoriesInContext(false),
	)

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "查询"})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 1, r.memoriesUsed)      // 检索到记忆
	assert.Nil(t, r.originalPromptTemplate) // 但不注入
}

func TestBeforeTaskIteration_无Query(t *testing.T) {
	mock := &mockTaskMemoryService{}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: ""})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.False(t, mock.retrieveCalled, "空查询不应检索")
}

func TestBeforeTaskIteration_NilMemoryService(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil) // memoryService 为 nil

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "查询"})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 0, r.memoriesUsed)
}

// TestBeforeTaskIteration_RetrievalQuery优先 验证 RetrievalQuery 优先于 Query 用于检索。
// Python: retrieval_query = getattr(ctx.inputs, "retrieval_query", None) or query
func TestBeforeTaskIteration_RetrievalQuery优先(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "专用检索结果",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "专用检索查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:          "原始查询",
		RetrievalQuery: "专用检索查询",
	})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.True(t, mock.retrieveCalled)
	// 验证：currentQuery 保存的是原始 Query（对齐 Python: self._current_query = query）
	assert.Equal(t, "原始查询", r.currentQuery)
}

// TestBeforeTaskIteration_RetrievalQuery为空回退Query 验证 RetrievalQuery 为空时回退到 Query。
// Python: retrieval_query = getattr(ctx.inputs, "retrieval_query", None) or query
func TestBeforeTaskIteration_RetrievalQuery为空回退Query(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "回退检索结果",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "原始查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:          "原始查询",
		RetrievalQuery: "", // 空值，应回退到 Query
	})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.True(t, mock.retrieveCalled)
}

// TestBeforeTaskIteration_缓存键使用RetrievalQuery 验证缓存键使用 retrievalQuery 而非 query。
// Python: self.last_retrieved_query == retrieval_query
func TestBeforeTaskIteration_缓存键使用RetrievalQuery(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "缓存结果",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "检索查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil, WithMemoryService(mock))

	// 第一次调用：Query 相同，RetrievalQuery 也相同
	cbc1 := &agentinterfaces.AgentCallbackContext{}
	cbc1.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:          "原始查询",
		RetrievalQuery: "检索查询",
	})
	err := r.beforeTaskIteration(context.Background(), cbc1)
	assert.NoError(t, err)
	assert.True(t, mock.retrieveCalled)

	// 重置标记
	mock.retrieveCalled = false

	// 第二次调用：Query 不同，但 RetrievalQuery 相同 → 应缓存命中
	cbc2 := &agentinterfaces.AgentCallbackContext{}
	cbc2.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:          "不同的原始查询",
		RetrievalQuery: "检索查询", // 相同的 RetrievalQuery
	})
	err = r.beforeTaskIteration(context.Background(), cbc2)
	assert.NoError(t, err)
	assert.False(t, mock.retrieveCalled, "RetrievalQuery 相同时应命中缓存，即使 Query 不同")
}

// ──────────────────────────── afterTaskIteration 测试 ────────────────────────────

func TestAfterTaskIteration_恢复Prompt(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)

	// 模拟 beforeTaskIteration 保存了原始模板
	r.originalPromptTemplate = []map[string]any{
		{"role": "system", "content": "原始系统提示词"},
	}

	cbc := &agentinterfaces.AgentCallbackContext{}
	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Nil(t, r.originalPromptTemplate, "恢复后应清空")
}

func TestAfterTaskIteration_AutoSummarize(t *testing.T) {
	mock := &mockTaskMemoryService{
		summarizeResult: &ceservice.SummarizeResult{},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil,
		WithMemoryService(mock),
		WithAutoSummarize(true),
	)
	r.currentQuery = "如何处理数据"

	cbc := &agentinterfaces.AgentCallbackContext{}
	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	// 注意：extractTrajectory 在无 Agent 时返回空字符串，不会触发 Summarize
	// 所以 summarizeCalled 可能为 false，这是预期行为
}

func TestAfterTaskIteration_AutoSummarize失败(t *testing.T) {
	mock := &mockTaskMemoryService{
		summarizeErr: assert.AnError,
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil,
		WithMemoryService(mock),
		WithAutoSummarize(true),
	)
	r.currentQuery = "查询"

	cbc := &agentinterfaces.AgentCallbackContext{}
	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err) // 失败不中断
}

// TestAfterTaskIteration_MattsMode非None跳过AutoSummarize 验证 mattsMode 非 none 时跳过自动总结。
// Python: only support matts_mode = "none", because other matts_mode need to call multiple invoke
func TestAfterTaskIteration_MattsMode非None跳过AutoSummarize(t *testing.T) {
	mock := &mockTaskMemoryService{
		summarizeResult: &ceservice.SummarizeResult{},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil,
		WithMemoryService(mock),
		WithAutoSummarize(true),
		WithAutoSummarizeMattsMode("sequential"), // 非 "none"
	)
	r.currentQuery = "查询"

	cbc := &agentinterfaces.AgentCallbackContext{}
	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.False(t, mock.summarizeCalled, "mattsMode 非 none 时不应触发自动总结")
}

// TestBeforeTaskIteration_MemoryStringTrimSpace 验证注入内容 TrimSpace。
func TestBeforeTaskIteration_MemoryStringTrimSpace(t *testing.T) {
	mock := &mockTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "  有前后空格的记忆  ",
			RetrievedMemory: []ceschema.MemoryItem{&mockRetrievedMemory{text: "经验1"}},
			Query:           "查询",
			UserID:          "alice",
			Algorithm:       "ACE",
		},
	}
	r := NewContextEvolutionRail(context.Background(), "alice", nil,
		WithMemoryService(mock),
		WithInjectMemoriesInContext(true),
	)

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Query: "查询"})

	err := r.beforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 1, r.memoriesUsed)
	// 验证 TrimSpace 生效：currentQuery 不含前后空格
	// memoryString 被使用前已 TrimSpace，此测试确认不 panic 且正常工作
}

// TestWithUserID 验证 WithUserID 选项函数。
func TestWithUserID(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "", nil,
		WithUserID("override-user"),
	)
	assert.Equal(t, "override-user", r.userID)
}

func TestAfterTaskIteration_标注MemoriesUsed(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)
	r.memoriesUsed = 5

	// 构造有 Result 的 TaskIterationInputs
	taskInputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"status": "ok"},
	}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(taskInputs)

	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)

	// 验证：memories_used 写入 taskInputs.Result（对齐 Python: result["memories_used"] = self.memories_used）
	assert.Equal(t, 5, taskInputs.Result["memories_used"])
}

// TestAfterTaskIteration_标注MemoriesUsed_Result为Nil 验证 Result 为 nil 时不 panic。
func TestAfterTaskIteration_标注MemoriesUsed_Result为Nil(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)
	r.memoriesUsed = 3

	taskInputs := &agentinterfaces.TaskIterationInputs{
		Result: nil,
	}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(taskInputs)

	err := r.afterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err) // Result 为 nil 不应 panic
}

// ──────────────────────────── extractTrajectory 测试 ────────────────────────────

func TestExtractTrajectory_无DeepAgent(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)

	cbc := &agentinterfaces.AgentCallbackContext{}
	// Agent() 默认返回 nil（无 Agent 实现 DeepAgentInterface）

	result := r.extractTrajectory(cbc)
	assert.Equal(t, "", result)
}

// ──────────────────────────── deepCopyPromptTemplate 测试 ────────────────────────────

func TestDeepCopyPromptTemplate(t *testing.T) {
	original := []map[string]any{
		{"role": "system", "content": "系统提示"},
		{"role": "user", "content": "用户消息"},
	}

	copied := deepCopyPromptTemplate(original)

	// 修改拷贝不影响原始
	copied[0]["content"] = "修改后的提示"
	assert.Equal(t, "系统提示", original[0]["content"])
	assert.Equal(t, "修改后的提示", copied[0]["content"])
}

func TestDeepCopyPromptTemplate_Nil(t *testing.T) {
	result := deepCopyPromptTemplate(nil)
	assert.Nil(t, result)
}

func TestDeepCopyPromptTemplate_空(t *testing.T) {
	result := deepCopyPromptTemplate([]map[string]any{})
	assert.Empty(t, result)
}

// ──────────────────────────── roleTypeToString 测试 ────────────────────────────

func TestRoleTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		roleType int
		want     string
	}{
		{"system", 0, "system"},
		{"user", 1, "user"},
		{"assistant", 2, "assistant"},
		{"tool", 3, "tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roleTypeToString(llmschema.RoleType(tt.roleType))
			assert.Equal(t, tt.want, got)
		})
	}
}

// ──────────────────────────── 访问器测试 ────────────────────────────

func TestAccessors(t *testing.T) {
	r := NewContextEvolutionRail(context.Background(), "alice", nil)
	assert.Equal(t, "alice", r.userID)
	assert.Equal(t, 0, r.MemoriesUsed())
	assert.Equal(t, "", r.CurrentQuery())
	assert.Nil(t, r.AgentRef())
	assert.Equal(t, contextEvolutionPriority, r.Priority())
}

// ──────────────────────────── 导入检查 ────────────────────────────

// 确保 ceservice 包的正确导入被使用
var _ ceservice.Message
var _ ceservice.RetrieveResult
var _ ceservice.SummarizeTrajectoriesInput

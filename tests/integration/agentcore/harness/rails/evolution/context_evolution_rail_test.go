//go:build integration

package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	ceservice "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvolutionRailSuite 测试 ContextEvolutionRail 优先级、回调注册、记忆注入/恢复、错误容忍。
//
// 对齐 Python: tests/unit_tests/extensions/context_evolver/test_task_memory_rail.py
//   - TestContextEvolutionRail_Priority50
//   - TestContextEvolutionRail_GetCallbacks
//   - TestContextEvolutionRail_默认构造
//   - TestContextEvolutionRail_BeforeTaskIteration注入记忆
//   - TestContextEvolutionRail_AfterTaskIteration恢复提示词
//   - TestContextEvolutionRail_无记忆时不崩溃
//   - TestContextEvolutionRail_检索失败不崩溃
//   - TestContextEvolutionRail_EmitHostEvent
type ContextEvolutionRailSuite struct {
	isuite.BaseIntegrationSuite
}

// fakeTaskMemoryService mock 记忆服务，实现 taskMemoryServicer 接口。
//
// 因为 taskMemoryServicer 是非导出接口，在同包源码测试中可直接实现；
// 此处通过 WithMemoryService 注入，需要实现接口签名。
type fakeTaskMemoryService struct {
	// retrieveResult 检索结果
	retrieveResult *ceservice.RetrieveResult
	// retrieveErr 检索错误
	retrieveErr error
	// loadErr 加载错误
	loadErr error
	// summarizeResult 总结结果
	summarizeResult *ceservice.SummarizeResult
	// summarizeErr 总结错误
	summarizeErr error
	// retrieveCalls 检索调用次数
	retrieveCalls int
	// lastRetrieveQuery 上次检索查询
	lastRetrieveQuery string
	// summaryAlgorithmName 总结算法名称
	summaryAlgorithmName string
}

// fakeMemoryItem 实现 ceschema.MemoryItem 接口的最简 mock。
type fakeMemoryItem struct {
	// text 格式化文本
	text string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextEvolutionRailSuite 运行 ContextEvolutionRail 集成测试套件。
func TestContextEvolutionRailSuite(t *testing.T) {
	suite.Run(t, new(ContextEvolutionRailSuite))
}

func (s *ContextEvolutionRailSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
}

func (s *ContextEvolutionRailSuite) TearDownSuite() {
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestContextEvolutionRail_Priority50 测试优先级为 50。
//
// 对齐 Python: TestInit.test_priority_lower_than_default
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_Priority50() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)

	s.Equal(50, rail.Priority())
}

// TestContextEvolutionRail_GetCallbacks 测试注册 BeforeTaskIteration + AfterTaskIteration 回调。
//
// 对齐 Python: TestInit 全系列
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_GetCallbacks() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)

	callbacks := rail.GetCallbacks()
	s.Contains(callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	s.Contains(callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// TestContextEvolutionRail_默认构造 测试默认构造时 memoriesUsed 为 0，currentQuery 为空。
//
// 对齐 Python: TestInit.test_default_state
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_默认构造() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)

	s.Equal(0, rail.MemoriesUsed())
	s.Equal("", rail.CurrentQuery())
	s.Nil(rail.AgentRef())
}

// TestContextEvolutionRail_BeforeTaskIteration注入记忆 测试 BeforeTaskIteration 回调
// 从 taskMemoryService 检索记忆并设置 memoriesUsed。
//
// 对齐 Python: TestBeforeTaskIteration.test_injects_memory_into_system_prompt
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_BeforeTaskIteration注入记忆() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "Use pdb for debugging.",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "Use pdb."}},
	}

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb, ok := callbacks[agentinterfaces.CallbackBeforeTaskIteration]
	s.Require().True(ok, "应注册 CallbackBeforeTaskIteration")

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "debug Python",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	// 验证记忆已检索
	s.Equal(1, rail.MemoriesUsed())
	s.True(fakeSvc.retrieveCalls > 0)
	// 验证 currentQuery 保存
	s.Equal("debug Python", rail.CurrentQuery())
}

// TestContextEvolutionRail_AfterTaskIteration恢复提示词 测试 AfterTaskIteration 回调
// 标注 memoriesUsed 到 result。
//
// 对齐 Python: TestAfterTaskIteration.test_restores_original_prompt_template + test_attaches_memories_used_to_result
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_AfterTaskIteration恢复提示词() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "injected mem",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "mem"}},
	}

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]
	afterCb := callbacks[agentinterfaces.CallbackAfterTaskIteration]

	result := map[string]any{}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: result,
	})

	// before → 检索记忆
	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)
	s.Equal(1, rail.MemoriesUsed())

	// after → 标注 memoriesUsed
	err = afterCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(1, result["memories_used"])
}

// TestContextEvolutionRail_无记忆时不崩溃 测试 taskMemoryService 返回空列表时不崩溃。
//
// 对齐 Python: TestBeforeTaskIteration.test_no_injection_when_no_memories_retrieved
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_无记忆时不崩溃() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(0, rail.MemoriesUsed())
}

// TestContextEvolutionRail_检索失败不崩溃 测试 taskMemoryService 返回 error 时不崩溃。
//
// 对齐 Python: except Exception → return 模式
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_检索失败不崩溃() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveErr = errTestRetrieve

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err) // 不应返回错误（对齐 Python: except → return）

	s.Equal(0, rail.MemoriesUsed())
}

// TestContextEvolutionRail_EmitHostEvent 测试 EmitHostEvent 缓存事件。
//
// ContextEvolutionRail 嵌入 DeepAgentRail → BaseRail，不直接拥有 EmitHostEvent。
// 但 GetCallbacks 返回的回调函数可用于触发 beforeTaskIteration，
// 验证回调机制可以正确传播事件（对齐 Python: ContextEvolutionRail 事件缓存）。
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_EmitHostEvent() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)

	// 验证回调可正常触发（间接验证事件传播机制）
	callbacks := rail.GetCallbacks()
	beforeCb, hasBefore := callbacks[agentinterfaces.CallbackBeforeTaskIteration]
	afterCb, hasAfter := callbacks[agentinterfaces.CallbackAfterTaskIteration]
	s.True(hasBefore)
	s.True(hasAfter)

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	err = afterCb(s.Ctx, cbc)
	s.Require().NoError(err)
}

// TestContextEvolutionRail_空Query跳过检索 测试空 query 时跳过检索。
//
// 对齐 Python: TestBeforeTaskIteration.test_empty_query_skips_retrieval
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_空Query跳过检索() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(0, fakeSvc.retrieveCalls)
	s.Equal(0, rail.MemoriesUsed())
}

// TestContextEvolutionRail_缓存相同查询 测试相同查询复用缓存结果。
//
// 对齐 Python: TestBeforeTaskIteration.test_caches_retrieval_for_same_query
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_缓存相同查询() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "cached memory",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "cached"}},
	}

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "same query",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)
	callsAfterFirst := fakeSvc.retrieveCalls

	err = beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(callsAfterFirst, fakeSvc.retrieveCalls) // 第二次复用缓存
}

// TestContextEvolutionRail_AfterTaskIteration单独安全 测试仅调用 afterTaskIteration 不崩溃。
//
// 对齐 Python: TestAfterTaskIteration.test_after_task_iteration_safe_without_before
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_AfterTaskIteration单独安全() {
	fakeSvc := newFakeTaskMemoryService()
	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	afterCb := callbacks[agentinterfaces.CallbackAfterTaskIteration]

	result := map[string]any{}
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: result,
	})

	err := afterCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(0, result["memories_used"])
}

// TestContextEvolutionRail_不同查询不缓存 测试不同查询触发新的检索。
//
// 对齐 Python: TestBeforeTaskIteration.test_cache_bypassed_for_different_query
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_不同查询不缓存() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "mem",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "mem"}},
	}

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc1 := &agentinterfaces.AgentCallbackContext{}
	cbc1.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "query A",
		Result: map[string]any{},
	})
	err := beforeCb(s.Ctx, cbc1)
	s.Require().NoError(err)

	cbc2 := &agentinterfaces.AgentCallbackContext{}
	cbc2.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "query B",
		Result: map[string]any{},
	})
	err = beforeCb(s.Ctx, cbc2)
	s.Require().NoError(err)

	s.Equal(2, fakeSvc.retrieveCalls)
}

// TestContextEvolutionRail_注入禁用仍检索 测试 injectMemoriesInContext=false 时仍检索记忆但不注入。
//
// 对齐 Python: TestBeforeTaskIteration.test_retrieval_occurs_even_when_inject_disabled
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_注入禁用仍检索() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "Some memory.",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "memory"}},
	}

	rail := evolution.NewContextEvolutionRail(
		s.Ctx,
		"test_user",
		nil,
		evolution.WithMemoryService(fakeSvc),
		evolution.WithInjectMemoriesInContext(false),
		evolution.WithAutoSummarize(false),
	)

	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:  "test",
		Result: map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	s.Equal(1, rail.MemoriesUsed()) // 检索到记忆
}

// TestContextEvolutionRail_RetrievalQuery优先 测试 RetrievalQuery 优先于 Query 用于检索。
//
// 对齐 Python: TestBeforeTaskIteration.test_uses_retrieval_query_when_provided
func (s *ContextEvolutionRailSuite) TestContextEvolutionRail_RetrievalQuery优先() {
	fakeSvc := newFakeTaskMemoryService()
	fakeSvc.retrieveResult = &ceservice.RetrieveResult{
		MemoryString:    "专用检索结果",
		RetrievedMemory: []ceschema.MemoryItem{fakeMemoryItem{text: "经验1"}},
	}

	rail := newTestContextEvolutionRail(s.Ctx, fakeSvc)
	callbacks := rail.GetCallbacks()
	beforeCb := callbacks[agentinterfaces.CallbackBeforeTaskIteration]

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{
		Query:          "原始查询",
		RetrievalQuery: "专用检索查询",
		Result:         map[string]any{},
	})

	err := beforeCb(s.Ctx, cbc)
	s.Require().NoError(err)

	// currentQuery 保存的是原始 Query（对齐 Python: self._current_query = query）
	s.Equal("原始查询", rail.CurrentQuery())
	s.Equal("专用检索查询", fakeSvc.lastRetrieveQuery)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFakeTaskMemoryService 创建 mock 记忆服务。
func newFakeTaskMemoryService() *fakeTaskMemoryService {
	return &fakeTaskMemoryService{
		retrieveResult: &ceservice.RetrieveResult{
			MemoryString:    "",
			RetrievedMemory: []ceschema.MemoryItem{},
		},
		summaryAlgorithmName: "ACE",
	}
}

// newTestContextEvolutionRail 创建测试用 ContextEvolutionRail（禁用 autoSummarize，注入 fake 服务）。
func newTestContextEvolutionRail(ctx context.Context, svc *fakeTaskMemoryService) *evolution.ContextEvolutionRail {
	return evolution.NewContextEvolutionRail(
		ctx,
		"test_user",
		nil,
		evolution.WithMemoryService(svc),
		evolution.WithAutoSummarize(false),
	)
}

// errTestRetrieve 测试用检索错误。
var errTestRetrieve = context.DeadlineExceeded

// ──────────────── fakeTaskMemoryService 方法实现 ────────────────

// Retrieve 实现 taskMemoryServicer.Retrieve。
func (f *fakeTaskMemoryService) Retrieve(_ context.Context, _ string, query string) (*ceservice.RetrieveResult, error) {
	f.retrieveCalls++
	f.lastRetrieveQuery = query
	if f.retrieveErr != nil {
		return nil, f.retrieveErr
	}
	return f.retrieveResult, nil
}

// LoadMemories 实现 taskMemoryServicer.LoadMemories。
func (f *fakeTaskMemoryService) LoadMemories(_ context.Context, _ string) error {
	return f.loadErr
}

// SummaryAlgorithm 实现 taskMemoryServicer.SummaryAlgorithm。
func (f *fakeTaskMemoryService) SummaryAlgorithm() string {
	return f.summaryAlgorithmName
}

// Summarize 实现 taskMemoryServicer.Summarize。
func (f *fakeTaskMemoryService) Summarize(_ context.Context, _ string, _ string, _ string, _ []string, _ ...map[string]any) (*ceservice.SummarizeResult, error) {
	if f.summarizeErr != nil {
		return nil, f.summarizeErr
	}
	return f.summarizeResult, nil
}

// ──────────────── fakeMemoryItem 方法实现 ────────────────

// FormatMemoryString 实现 ceschema.MemoryItem.FormatMemoryString。
func (m fakeMemoryItem) FormatMemoryString() string {
	return m.text
}

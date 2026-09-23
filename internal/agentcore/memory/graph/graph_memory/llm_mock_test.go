package graph_memory

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mutableMockLLMClient 可变的模拟 BaseModelClient
// 每个测试可设置不同的 invokeResult/invokeErr
type mutableMockLLMClient struct {
	mu           sync.Mutex
	invokeResult *llmschema.AssistantMessage
	invokeErr    error
}

func (m *mutableMockLLMClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invokeResult, m.invokeErr
}

func (m *mutableMockLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mutableMockLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mutableMockLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mutableMockLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mutableMockLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mutableMockLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (m *mutableMockLLMClient) SupportsKVCacheRelease() bool {
	return false
}

func (m *mutableMockLLMClient) SetResponse(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invokeResult = llmschema.NewAssistantMessage(content)
	m.invokeErr = nil
}

func (m *mutableMockLLMClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invokeResult = nil
	m.invokeErr = err
}

func (m *mutableMockLLMClient) SetResponseWithUsage(content string, inputTokens, outputTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := llmschema.NewAssistantMessage(content)
	msg.UsageMetadata = &llmschema.UsageMetadata{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	m.invokeResult = msg
	m.invokeErr = nil
}

// ──────────────────────────── 常量 ────────────────────────────

// graphMemTestProviderName 测试用 LLM provider 名称
const graphMemTestProviderName = "GraphMemTestLLM"

// ──────────────────────────── 全局变量 ────────────────────────────

// globalMockLLM 全局可变 mock LLM 客户端（注册表工厂返回此实例）
var globalMockLLM = &mutableMockLLMClient{
	invokeResult: llmschema.NewAssistantMessage(`[{"name": "default"}]`),
}

// graphMemTestProviderRegistered 确保只注册一次
var graphMemTestProviderRegistered sync.Once

// ──────────────────────────── 导出函数 ────────────────────────────

// ensureMockProviderRegistered 确保 mock provider 已注册
func ensureMockProviderRegistered() {
	graphMemTestProviderRegistered.Do(func() {
		registry := model_clients.GetClientRegistry()
		registry.Register(graphMemTestProviderName, "llm", func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return globalMockLLM, nil
		})
	})
}

// newTestGraphMemoryWithMockLLM 创建带 mock LLM 的 GraphMemory
func newTestGraphMemoryWithMockLLM() (*GraphMemory, error) {
	ensureMockProviderRegistered()

	cc, err := llmschema.NewModelClientConfig(graphMemTestProviderName, "test-key", "http://localhost")
	if err != nil {
		return nil, err
	}
	model, err := llm.NewModel(cc, llmschema.NewModelRequestConfig())
	if err != nil {
		return nil, err
	}

	return newTestGraphMemory(
		WithLLMClient(model),
		WithLLMStructuredOutput(true),
	)
}

// TestInvokeLLM_有客户端正常调用 测试 InvokeLLM 有客户端时正常调用
func TestInvokeLLM_有客户端正常调用(t *testing.T) {
	globalMockLLM.SetResponse(`[{"name": "Alice", "type": 0}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, outputModel := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	result, err := gm.InvokeLLM(context.Background(), kwargs, tmpl, outputModel)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestInvokeLLM_重试失败 测试 InvokeLLM 重试失败
func TestInvokeLLM_重试失败(t *testing.T) {
	globalMockLLM.SetError(fmt.Errorf("LLM service unavailable"))
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.Config.RequestMaxRetries = 1

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)
	outputModel := extraction.ResponseFormat("EntitySummary", map[string]any{})

	_, err = gm.InvokeLLM(context.Background(), kwargs, tmpl, outputModel)
	assert.Error(t, err)
}

// TestInvokeLLM_有ExtraKwargs 测试 InvokeLLM 合并额外参数
func TestInvokeLLM_有ExtraKwargs(t *testing.T) {
	globalMockLLM.SetResponse(`[{"name": "Alice", "type": 0}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)
	outputModel := extraction.ResponseFormat("EntitySummary", map[string]any{})

	result, err := gm.InvokeLLM(context.Background(), kwargs, tmpl, outputModel,
		map[string]any{"temperature": 0.5},
	)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestInvokeLLM_Debug模式 测试 Debug 模式日志
func TestInvokeLLM_Debug模式(t *testing.T) {
	globalMockLLM.SetResponse(`[{"name": "Alice"}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.Debug = true

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, outputModel := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	result, err := gm.InvokeLLM(context.Background(), kwargs, tmpl, outputModel)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestInvokeLLM_TokenRecord 测试 Token 使用记录
func TestInvokeLLM_TokenRecord(t *testing.T) {
	globalMockLLM.SetResponseWithUsage(`[{"name": "Alice"}]`, 100, 50)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	_, err = gm.InvokeLLM(context.Background(), kwargs, tmpl, nil)
	assert.NoError(t, err)
	assert.Equal(t, 100, gm.TokenRecord["input_tokens"])
	assert.Equal(t, 50, gm.TokenRecord["output_tokens"])
}

// TestInvokeLLM_LLMExtraKwargs 测试 LLM 额外参数合并
func TestInvokeLLM_LLMExtraKwargs(t *testing.T) {
	globalMockLLM.SetResponse(`ok`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.LLMExtraKwargs = map[string]any{"top_p": 0.9}

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	result, err := gm.InvokeLLM(context.Background(), kwargs, tmpl, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestInvokeLLM_非结构化输出 测试 LLM 非结构化输出模式
func TestInvokeLLM_非结构化输出(t *testing.T) {
	globalMockLLM.SetResponse(`plain text response`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.LLMStructuredOutput = false

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	result, err := gm.InvokeLLM(context.Background(), kwargs, tmpl, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestExtractEntityDeclarations_有LLM 测试有 LLM 时的实体抽取
func TestExtractEntityDeclarations_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`[{"name": "Alice", "entity_type": 0}, {"name": "Bob", "entity_type": 1}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.DBBackend = &mockSearchGraphStore{IsEmptyResult: true}

	state := gm.initState(nil, config.EpisodeTypeDocument)
	noExisting, declarations, err := gm.extractEntityDeclarations(context.Background(), config.EpisodeTypeDocument, "Alice met Bob at the conference", state)
	assert.NoError(t, err)
	assert.True(t, noExisting)
	assert.NotEmpty(t, declarations)
}

// TestExtractEntityDeclarations_嵌套Dict 测试嵌套 dict 处理
func TestExtractEntityDeclarations_嵌套Dict(t *testing.T) {
	globalMockLLM.SetResponse(`{"entities": [{"name": "Alice", "entity_type": 0}]}`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.DBBackend = &mockSearchGraphStore{IsEmptyResult: true}

	state := gm.initState(nil, config.EpisodeTypeDocument)
	_, declarations, err := gm.extractEntityDeclarations(context.Background(), config.EpisodeTypeDocument, "Alice is here", state)
	assert.NoError(t, err)
	assert.NotEmpty(t, declarations)
}

// TestExtractEntityDeclarations_过滤保留字 测试过滤 user/assistant 等保留字
func TestExtractEntityDeclarations_过滤保留字(t *testing.T) {
	globalMockLLM.SetResponse(`[{"name": "user", "entity_type": 0}, {"name": "Alice", "entity_type": 1}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)
	gm.DBBackend = &mockSearchGraphStore{IsEmptyResult: true}

	state := gm.initState(nil, config.EpisodeTypeDocument)
	_, declarations, err := gm.extractEntityDeclarations(context.Background(), config.EpisodeTypeDocument, "User said hello", state)
	assert.NoError(t, err)
	for _, d := range declarations {
		assert.NotEqual(t, "user", d.Name)
	}
}

// TestStartRelationExtractionAsync_有LLM 测试有 LLM 客户端的异步关系抽取
func TestStartRelationExtractionAsync_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`[{"relation": "works_at", "source": "Alice", "target": "Company"}]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	state := gm.initState(nil, config.EpisodeTypeDocument)
	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}

	task := gm.startRelationExtractionAsync(context.Background(), declarations, "Alice works at a company", state, "")
	assert.NotNil(t, task)

	task.Wait()
	assert.Nil(t, task.Err)
	assert.NotEmpty(t, task.Result)
}

// TestStartEntityDedupeAsync_有LLM 测试有 LLM 客户端的异步实体去重
func TestStartEntityDedupeAsync_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`[]`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	state := gm.initState(nil, config.EpisodeTypeDocument)
	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	existing := []map[string]any{{"uuid": "e1", "name": "Alice"}}

	task := gm.startEntityDedupeAsync(context.Background(), "content", declarations, existing, state)
	assert.NotNil(t, task)

	task.Wait()
	assert.Nil(t, task.Err)
}

// TestInvokeLLMAsync_有LLM 测试有 LLM 客户端的异步调用
func TestInvokeLLMAsync_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`{"result": "ok"}`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	kwargs := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	_, tmpl, _ := extraction.ExtractEntityDeclaration(
		config.EpisodeTypeDocument, "hello world", "", "",
		[]*extraction.EntityDef{extraction.DefaultEntity}, "cn", nil, 2,
	)

	task := gm.invokeLLMAsync(context.Background(), kwargs, tmpl, nil)
	assert.NotNil(t, task)

	task.Wait()
	assert.Nil(t, task.Err)
	assert.NotEmpty(t, task.Result)
}

// TestStartTimezoneTask_有LLM 测试有 LLM 客户端的时区任务
func TestStartTimezoneTask_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`{"timezone": "UTC+8"}`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	state := gm.initState(nil, config.EpisodeTypeDocument)
	ch := gm.startTimezoneTask(context.Background(), "content at 2024-01-01", state)
	assert.NotNil(t, ch)

	result := <-ch
	assert.Nil(t, result.err)
	assert.NotEmpty(t, result.response)
}

// TestEntityEnrich_有LLM 测试有 LLM 客户端的实体摘要抽取
func TestEntityEnrich_有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`{"summary": "Alice is an engineer", "attributes": {"role": "engineer"}}`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	state := gm.initState(nil, config.EpisodeTypeDocument)
	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Name = "Alice"

	entities := []*graph.Entity{entity1}
	result, err := gm.entityEnrich(context.Background(), entities, "Alice works as an engineer", state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestEntityEnrich_有阻塞实体有LLM 测试有阻塞实体和 LLM 的摘要抽取
func TestEntityEnrich_有阻塞实体有LLM(t *testing.T) {
	globalMockLLM.SetResponse(`{"summary": "merged summary"}`)
	gm, err := newTestGraphMemoryWithMockLLM()
	assert.NoError(t, err)

	state := gm.initState(nil, config.EpisodeTypeDocument)
	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Name = "Alice"
	entity1.Content = ""

	entity2 := graph.NewEntity()
	entity2.UUID = "e2"
	entity2.Name = "Bob"

	state.PendingMerge["e1"] = &pendingMergeTask{Result: "pre-merge summary", Err: nil}

	entities := []*graph.Entity{entity1, entity2}
	result, err := gm.entityEnrich(context.Background(), entities, "Alice and Bob met", state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestParseRelationFilteringResult_非relevantRelations 测试过滤结果中无 relevant_relations 时保留全部
func TestParseRelationFilteringResult_非relevantRelations(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"
	rel.LHS = "e1"
	rel.RHS = "e2"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// 返回非 relevant_relations 格式
	filterTask := &asyncTask{Result: `{"other_key": "value"}`}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 1)
}

// TestParseRelationFilteringResult_relevantRelations非列表 测试 relevant_relations 为非列表
func TestParseRelationFilteringResult_relevantRelations非列表(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// relevant_relations 为字符串而非列表
	filterTask := &asyncTask{Result: `{"relevant_relations": "invalid"}`}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 1)
}

// TestParseRelationFilteringResult_无效ID 测试 relevant_relations 中 ID 越界
func TestParseRelationFilteringResult_无效ID(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// ID 为 99（越界）
	filterTask := &asyncTask{Result: `{"relevant_relations": [99]}`}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 0)
}

// TestParseRelationFilteringResult_有效ID 测试有效的 relevant_relations ID
func TestParseRelationFilteringResult_有效ID(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// ID=1 指向 rel（1-based）
	filterTask := &asyncTask{Result: `{"relevant_relations": [1]}`}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 1)
}

// TestFetchRelevantEntities_无嵌入器但有声明 测试无嵌入器时有声明列表
func TestFetchRelevantEntities_无嵌入器但有声明(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}

	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_嵌入失败 测试嵌入失败时返回 nil
func TestFetchRelevantEntities_嵌入失败(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.embedderVal = &failingEmbedder{}

	state := NewGraphMemState()
	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}

	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestEntityMerge_有合并但LLM失败 测试有合并对但 LLM 调用失败
func TestEntityMerge_有合并但LLM失败(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeEntities = true

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}

	existingEntity := graph.NewEntity()
	existingEntity.UUID = "existing-1"
	existingEntity.Name = "Alice"
	existingEntity.Content = "test"
	state.RetrievedEntities["existing-1"] = existingEntity
	state.LookupTable.Entities["existing-1"] = existingEntity

	dedupeTask := &asyncTask{Result: `[{"id": 1, "duplicate_ids": [2]}]`}
	state.Tasks = append(state.Tasks, dedupeTask)

	existingEntity2 := graph.NewEntity()
	existingEntity2.UUID = "existing-2"
	existingEntity2.Name = "Alice Duplicate"
	existingEntity2.Content = "test2"
	state.LookupTable.Entities["existing-2"] = existingEntity2

	existingEntitiesList := []map[string]any{
		{"uuid": "existing-1", "name": "Alice", "content": "test"},
		{"uuid": "existing-2", "name": "Alice Duplicate", "content": "test2"},
	}

	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	assert.NotNil(t, result)
	_ = err
}

// TestHandleRelationDedupe_非StringTmpBuffer 测试 TmpBuffer 中非字符串项
func TestHandleRelationDedupe_非StringTmpBuffer(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, 42)
	state.TmpBuffer = append(state.TmpBuffer, "valid string")

	queryStore := &mockSearchGraphStore{IsEmptyResult: false}
	gm.DBBackend = queryStore

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", nil, state)
	assert.NoError(t, err)
}

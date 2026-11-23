//go:build integration

package workflow_llm_comp

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
)

// LLMCompSuite 8.18 LLMComp 全链集成：ExecuteSingleComponent 十步真实链
// （workflow 会话装配 → graph.Vertex.Call → components/llm 校验/组装/格式化 →
// session/state commit → runner/callback 组件事件）。
type LLMCompSuite struct {
	suite.Suite
}

func TestLLMCompSuite(t *testing.T) {
	suite.Run(t, new(LLMCompSuite))
}

// fakeIntegrationModel 集成层模型桩（foundation/llm 边界 mock——规则 7 允许）
type fakeIntegrationModel struct {
	respContent string
}

func (f *fakeIntegrationModel) Invoke(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(f.respContent), nil
}

func (f *fakeIntegrationModel) Stream(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- llmschema.NewAssistantMessageChunk(f.respContent)
	close(ch)
	return ch, nil
}

// ── BaseModelClient 接口面补齐（AddModel 注册需要；多模态/KV 亲和不在集成
// 范围，llm 包 fakeModelClient 同款空实现）──
func (f *fakeIntegrationModel) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeIntegrationModel) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeIntegrationModel) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeIntegrationModel) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("不支持")
}
func (f *fakeIntegrationModel) Release(_ context.Context, _ ...modelclients.ReleaseOption) (bool, error) {
	return false, nil
}
func (f *fakeIntegrationModel) SupportsKVCacheRelease() bool  { return false }
func (f *fakeIntegrationModel) SupportsKVCacheAffinity() bool { return false }
func (f *fakeIntegrationModel) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return nil
}

// textConfig text 单字段配置（ModelID 走注入缝）
func textConfig() *llmcomp.LLMCompConfig {
	modelID := "fake"
	return &llmcomp.LLMCompConfig{
		ModelID: &modelID,
		TemplateContent: []any{
			map[string]any{"role": "system", "content": "be nice"},
			map[string]any{"role": "user", "content": "question: {{q}}"},
		},
		ResponseFormat: map[string]any{"type": "text"},
		OutputConfig:   map[string]any{"answer": map[string]any{"type": "string"}},
	}
}

// newComp 构造注册真实 ResourceMgr 模型的组件（8.21 连带收口：
// D-18-5 模型解析缝废弃，fake 经 AddModel 注册走真实链；
// modelID 全局唯一，跨用例重复注册先移除再注册）
func (s *LLMCompSuite) newComp(resp string, cfg *llmcomp.LLMCompConfig) *llmcomp.LLMComponent {
	if cfg.ModelID != nil {
		modelID := *cfg.ModelID
		mgr := runner.GetResourceMgr()
		if _, err := mgr.GetModel(context.Background(), []string{modelID}); err == nil {
			_, _ = mgr.RemoveModel([]string{modelID})
		}
		s.Require().NoError(mgr.AddModel(modelID,
			func(ctx context.Context, id string) (modelclients.BaseModelClient, error) {
				return &fakeIntegrationModel{respContent: resp}, nil
			}))
	}
	return llmcomp.NewLLMComponent(cfg)
}

// TestInvoke_全链批式 ExecuteSingleComponent 十步链：校验→组装→调用→格式化→commit
func (s *LLMCompSuite) TestInvoke_全链批式() {
	sess := session.NewSession()
	out, err := workflow.ExecuteSingleComponent(context.Background(), "llm", *sess,
		s.newComp("模型答案", textConfig()),
		map[string]any{"q": "hi"}, nil, nil, nil)
	s.Require().NoError(err)
	m, ok := out.(map[string]any)
	s.Require().True(ok, "输出应为 map: %T", out)
	s.Equal("模型答案", m["answer"], "text 单字段包装应贯通格式化")
}

// TestInvoke_json格式化 json 格式：围栏剥离 + 字段提取
func (s *LLMCompSuite) TestInvoke_json格式化() {
	cfg := textConfig()
	cfg.ResponseFormat = map[string]any{"type": "json"}
	cfg.OutputConfig = map[string]any{"answer": map[string]any{"type": "string", "required": true}}
	sess := session.NewSession()
	out, err := workflow.ExecuteSingleComponent(context.Background(), "llm", *sess,
		s.newComp("```json\n{\"answer\": \"结构化结果\"}\n```", cfg),
		map[string]any{"q": "hi"}, nil, nil, nil)
	s.Require().NoError(err)
	m := out.(map[string]any)
	s.Equal("结构化结果", m["answer"], "围栏剥离 + 提取应贯通")
}

// TestInvoke_启用历史 enable_history=true 时 user/assistant 写回 ModelContext
// （context_engine 子系统真实消费）
func (s *LLMCompSuite) TestInvoke_启用历史() {
	cfg := textConfig()
	cfg.EnableHistory = true
	sess := session.NewSession()
	mc := newRecordingContext()
	_, err := workflow.ExecuteSingleComponent(context.Background(), "llm", *sess,
		s.newComp("回覆", cfg), map[string]any{"q": "hi"}, nil, nil, mc)
	s.Require().NoError(err)
	s.Require().Len(mc.messages, 2)
	s.Equal("user", mc.messages[0].GetRole().String())
	s.Equal("assistant", mc.messages[1].GetRole().String())
}

// recordingContext 可记录写回的上下文桩（context_engine.ModelContext 边界替身）
type recordingContext struct {
	iface.ModelContext
	messages []llmschema.BaseMessage
}

func newRecordingContext() *recordingContext { return &recordingContext{} }

func (r *recordingContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return r.messages, nil
}

func (r *recordingContext) AddMessages(ctx context.Context, msgs []llmschema.BaseMessage, opts ...iface.Option) ([]llmschema.BaseMessage, error) {
	r.messages = append(r.messages, msgs...)
	return r.messages, nil
}

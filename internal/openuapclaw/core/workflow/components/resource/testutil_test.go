package resource

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessionconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
)

// 测试基建（仿 llm/testutil_test.go 与 simple_kb_test.go 同款惯例）

// fakeInnerSession 最小 InnerSession 桩（仅会话管线边界）
type fakeInnerSession struct {
	cfg sessionconfig.SessionConfig
	id  string
}

func (f *fakeInnerSession) Config() sessionconfig.SessionConfig              { return f.cfg }
func (f *fakeInnerSession) State() state.SessionState                        { return state.NewInMemoryWorkflowState() }
func (f *fakeInnerSession) Tracer() *tracer.Tracer                           { return nil }
func (f *fakeInnerSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeInnerSession) SessionID() string                                { return f.id }
func (f *fakeInnerSession) Checkpointer() sessioninterfaces.Checkpointer     { return nil }
func (f *fakeInnerSession) ActorManager() *stream_actor.ActorManager         { return nil }
func (f *fakeInnerSession) Close() error                                     { return nil }

// newTestNodeSession 构造真实 NodeSessionFacade（经 NewComponentSession 管线）
func newTestNodeSession(t interface{ Fatal(args ...any) }) *session.NodeSessionFacade {
	parent := &fakeInnerSession{cfg: sessionconfig.NewSessionConfig(context.Background()), id: "s1"}
	vsess := session.NewWorkflowVertexSession(parent, "kb1", "KnowledgeRetrievalComponent", false)
	compSess, err := session.NewComponentSession(vsess, false)
	if err != nil {
		panic(err)
	}
	return compSess
}

// stubVectorStore 边界替身（外部存储服务边界，真实链纪律允许；SimpleKB+SparseRetriever
// 真实管线消费本桩，验证组件 invoke 全链）
type stubVectorStore struct {
	results []retrievalcommon.SearchResult
	err     error
}

func (s *stubVectorStore) CheckVectorField() error { return nil }
func (s *stubVectorStore) Add(_ context.Context, _ []map[string]any, _ ...vector_store.StoreOption) error {
	return nil
}
func (s *stubVectorStore) Search(_ context.Context, _ []float64, _ int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]retrievalcommon.SearchResult, error) {
	return s.results, s.err
}
func (s *stubVectorStore) SparseSearch(_ context.Context, _ string, _ int, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]retrievalcommon.SearchResult, error) {
	return s.results, s.err
}
func (s *stubVectorStore) HybridSearch(_ context.Context, _ string, _ []float64, _ int, _ float64, _ query.QueryExpr, _ ...vector_store.StoreOption) ([]retrievalcommon.SearchResult, error) {
	return s.results, s.err
}
func (s *stubVectorStore) Delete(_ context.Context, _ []string, _ query.QueryExpr) (bool, error) {
	return true, nil
}
func (s *stubVectorStore) TableExists(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (s *stubVectorStore) DeleteTable(_ context.Context, _ string) error { return nil }
func (s *stubVectorStore) Close() error                                  { return nil }

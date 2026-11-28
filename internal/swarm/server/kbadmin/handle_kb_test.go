package kbadmin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newTestDomain 构造真实链 Domain（真实 SQLite + 真实 Chroma 本地向量库）
func newTestDomain(t *testing.T) *Domain {
	t.Helper()
	st := newTestStore(t)
	d, err := NewDomain(st, NewRetrievalKBFactory(t.TempDir()))
	require.NoError(t, err)
	return d
}

// TestIsKBMethod 测试方法族前缀判定
func TestIsKBMethod(t *testing.T) {
	require.True(t, IsKBMethod("kb.kb.list"))
	require.True(t, IsKBMethod("kb.search.test"))
	require.False(t, IsKBMethod("session.list"))
	require.False(t, IsKBMethod(""))
}

// TestHandleKBRequest_KBCreateGetListDelete 测试 kb.kb 五个已实现方法全链
func TestHandleKBRequest_KBCreateGetListDelete(t *testing.T) {
	d := newTestDomain(t)
	// create
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ChannelID: "c1",
		ReqMethod: schema.ReqMethodKBKBCreate,
		Params:    []byte(`{"id":"kb1","name":"测试库"}`),
	})
	require.True(t, resp.OK)
	require.Equal(t, "测试库", resp.Payload["kb"].(map[string]any)["name"])
	// get
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r2", ReqMethod: schema.ReqMethodKBKBGet,
		Params: []byte(`{"id":"kb1"}`),
	})
	require.True(t, resp.OK)
	// list
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r3", ReqMethod: schema.ReqMethodKBKBList, Params: []byte(`{}`),
	})
	require.True(t, resp.OK)
	require.Len(t, resp.Payload["kbs"].([]any), 1)
	// update
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r4", ReqMethod: schema.ReqMethodKBKBUpdate,
		Params: []byte(`{"id":"kb1","name":"改名","pinned":true}`),
	})
	require.True(t, resp.OK)
	// delete
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r5", ReqMethod: schema.ReqMethodKBKBDelete,
		Params: []byte(`{"id":"kb1"}`),
	})
	require.True(t, resp.OK)
}

// TestHandleKBRequest_GetNotFound 测试 get 不存在返回 KB_NOT_FOUND
func TestHandleKBRequest_GetNotFound(t *testing.T) {
	d := newTestDomain(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBKBGet,
		Params: []byte(`{"id":"nope"}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBNotFound, resp.Payload["error_code"])
}

// TestHandleKBRequest_未实现方法 测试 kb.doc.upload 返回 KB_NOT_IMPLEMENTED
func TestHandleKBRequest_未实现方法(t *testing.T) {
	d := newTestDomain(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload, Params: []byte(`{}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBNotImplemented, resp.Payload["error_code"])
}

// TestHandleKBRequest_未实现全族覆盖 测试 30.1 未实现方法逐一返回 KB_NOT_IMPLEMENTED
func TestHandleKBRequest_未实现全族覆盖(t *testing.T) {
	d := newTestDomain(t)
	methods := []schema.ReqMethod{
		schema.ReqMethodKBKBPin, schema.ReqMethodKBKBMove,
		schema.ReqMethodKBKBSetConfig, schema.ReqMethodKBKBStatistics,
		schema.ReqMethodKBKBGenerateProfile,
		schema.ReqMethodKBDocUpload, schema.ReqMethodKBDocImportURL,
		schema.ReqMethodKBDocImportManual, schema.ReqMethodKBDocList,
		schema.ReqMethodKBDocGet, schema.ReqMethodKBDocUpdate,
		schema.ReqMethodKBDocDelete, schema.ReqMethodKBDocBatchDelete,
		schema.ReqMethodKBDocReparse, schema.ReqMethodKBDocBatchReparse,
		schema.ReqMethodKBDocCancelParse, schema.ReqMethodKBDocRegenerateSummary,
		schema.ReqMethodKBDocPreview, schema.ReqMethodKBDocDownload,
		schema.ReqMethodKBChunkList, schema.ReqMethodKBChunkUpdate,
		schema.ReqMethodKBChunkRevisions, schema.ReqMethodKBChunkRevert,
		schema.ReqMethodKBChunkDelete, schema.ReqMethodKBChunkGenerateQuestions,
		schema.ReqMethodKBChunkListQuestions, schema.ReqMethodKBChunkerPreview,
		schema.ReqMethodKBSearchTest, schema.ReqMethodKBTaskProgress,
		schema.ReqMethodKBTaskSubscribe,
	}
	for _, m := range methods {
		resp := HandleKBRequest(d, &schema.AgentRequest{RequestID: "r", ReqMethod: m, Params: []byte(`{}`)})
		require.False(t, resp.OK, "method %s 应未实现", m)
		require.Equal(t, ErrCodeKBNotImplemented, resp.Payload["error_code"], "method %s", m)
	}
}

// TestHandleKBRequest_未知方法 测试分发 default 分支
func TestHandleKBRequest_未知方法(t *testing.T) {
	d := newTestDomain(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: "kb.unknown.x", Params: []byte(`{}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBInternal, resp.Payload["error_code"])
}

// 编译期引用守卫（context 导入占位，后续批次 respond 助手使用）
var _ = context.Background

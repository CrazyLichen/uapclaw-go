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

// TestHandleKBRequest_未实现方法 测试 kb.doc.import_url 返回 KB_NOT_IMPLEMENTED
// （30.5 已实现 kb.doc.upload，本用例改用 doc 族剩余未实现方法承载）
func TestHandleKBRequest_未实现方法(t *testing.T) {
	d := newTestDomain(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocImportURL, Params: []byte(`{}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBNotImplemented, resp.Payload["error_code"])
}

// TestHandleKBRequest_未实现全族覆盖 测试剩余未实现方法逐一返回 KB_NOT_IMPLEMENTED
// （30.2 已摘除：kb.doc.list/get/reparse/cancel_parse、kb.task.progress/subscribe）
func TestHandleKBRequest_未实现全族覆盖(t *testing.T) {
	d := newTestDomain(t)
	t.Cleanup(d.Stop)
	methods := []schema.ReqMethod{
		schema.ReqMethodKBKBPin, schema.ReqMethodKBKBMove,
		schema.ReqMethodKBKBSetConfig, schema.ReqMethodKBKBStatistics,
		schema.ReqMethodKBKBGenerateProfile,
		schema.ReqMethodKBDocImportURL,
		schema.ReqMethodKBDocImportManual, schema.ReqMethodKBDocUpdate,
		schema.ReqMethodKBDocDelete, schema.ReqMethodKBDocBatchDelete,
		schema.ReqMethodKBDocBatchReparse,
		schema.ReqMethodKBDocRegenerateSummary,
		schema.ReqMethodKBDocPreview,
		schema.ReqMethodKBChunkList, schema.ReqMethodKBChunkUpdate,
		schema.ReqMethodKBChunkRevisions, schema.ReqMethodKBChunkRevert,
		schema.ReqMethodKBChunkDelete, schema.ReqMethodKBChunkGenerateQuestions,
		schema.ReqMethodKBChunkListQuestions, schema.ReqMethodKBChunkerPreview,
		schema.ReqMethodKBSearchTest,
	}
	for _, m := range methods {
		resp := HandleKBRequest(d, &schema.AgentRequest{RequestID: "r", ReqMethod: m, Params: []byte(`{}`)})
		require.False(t, resp.OK, "method %s 应未实现", m)
		require.Equal(t, ErrCodeKBNotImplemented, resp.Payload["error_code"], "method %s", m)
	}
}

// TestHandleKBRequest_DocListGet 文档列表与详情
func TestHandleKBRequest_DocListGet(t *testing.T) {
	d := newTestDomain(t)
	t.Cleanup(d.Stop)
	_ = d.KB.store.KB().Create(&KBInfo{ID: "kb-1", Name: "n", Type: "document", CreatedAt: 1, UpdatedAt: 1})
	_ = d.KB.store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", FileName: "a.txt", ParseStatus: "completed", CreatedAt: 1, UpdatedAt: 1})
	resp := HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBDocList, Params: []byte(`{"kb_id":"kb-1"}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
	resp = HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBDocGet, Params: []byte(`{"id":"d1"}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
	resp = HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBDocGet, Params: []byte(`{"id":"nope"}`)})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBNotFound, resp.Payload["error_code"])
}

// TestHandleKBRequest_ReparseCancelParse 重解析与取消分发
func TestHandleKBRequest_ReparseCancelParse(t *testing.T) {
	d := newTestDomain(t)
	t.Cleanup(d.Stop)
	_ = d.KB.store.KB().Create(&KBInfo{ID: "kb-1", Name: "n", Type: "document", CreatedAt: 1, UpdatedAt: 1})
	_ = d.KB.store.Doc().Create(&KBDoc{ID: "d1", KBID: "kb-1", FileName: "a.txt", ParseStatus: "failed", CreatedAt: 1, UpdatedAt: 1})
	resp := HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBDocReparse, Params: []byte(`{"doc_id":"d1"}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
	// 处理中的任务可取消（无 worker 注册取消 → DAO 直接标 cancelled）
	_ = d.KB.store.Task().Create(&KBTask{ID: "t1", DocID: "d1", KBID: "kb-1", Status: "pending", CreatedAt: 1})
	resp = HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBDocCancelParse, Params: []byte(`{"task_id":"t1"}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
}

// TestHandleKBRequest_TaskProgressSubscribe kb.task.progress 全量对齐 + subscribe 注册
func TestHandleKBRequest_TaskProgressSubscribe(t *testing.T) {
	d := newTestDomain(t)
	t.Cleanup(d.Stop)
	_ = d.KB.store.Task().Create(&KBTask{ID: "t1", DocID: "d1", KBID: "kb-1",
		Status: "completed", Progress: 100, CreatedAt: 1, FinishedAt: 2})
	resp := HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBTaskProgress, Params: []byte(`{"task_ids":["t1"]}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
	resp = HandleKBRequest(d, &schema.AgentRequest{RequestID: "r",
		ReqMethod: schema.ReqMethodKBTaskSubscribe, Params: []byte(`{"channel_id":"ch-1"}`)})
	require.True(t, resp.OK, "%v", resp.Payload)
	// 订阅后事件扇出经注入的 push 函数可达
	var got []TaskEvent
	d.SetPushFunc(func(channelID string, e TaskEvent) {
		require.Equal(t, "ch-1", channelID)
		got = append(got, e)
	})
	d.Sink.EmitStatus(&ParseJob{TaskID: "t1", DocID: "d1", KBID: "kb-1"}, "completed", "")
	require.Len(t, got, 1)
	require.Equal(t, "t1", got[0].TaskID)
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

// TestBootstrapErrorResponse 测试装配失败响应构造
func TestBootstrapErrorResponse(t *testing.T) {
	resp := BootstrapErrorResponse(&schema.AgentRequest{RequestID: "r1", ChannelID: "c1"})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBInternal, resp.Payload["error_code"])
	require.Equal(t, "r1", resp.RequestID)
}

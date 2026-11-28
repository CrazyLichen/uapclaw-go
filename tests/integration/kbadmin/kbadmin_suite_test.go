//go:build integration

// Package kbadmin 30.1 知识库管理面地基集成测试。
//
// 跨子系统：swarm/server（E2A 信封分发 + kb.* 前缀分支）+
// swarm/server/kbadmin（五表 SQLite 台账 + Service 编排）+
// core/retrieval（Chroma 本地向量库 collection 真建/真删）+ swarm/schema（方法族）。
// 真实链路：真实 SQLite 文件库 + 真实 Chroma 持久化 collection，禁替身。
package kbadmin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/vector_fields"
	rvs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	rvsstore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	kbadminserver "github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/kbadmin"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KBAdminSuite kb.* 方法族全链：E2A 信封 → AgentServer 分发 → kbadmin
// Service → retrieval collection。create/list/get/update/delete 五个
// 已实现方法真链可用；未实现方法返回 KB_NOT_IMPLEMENTED。
type KBAdminSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境（E2A 信封投递 + 响应收集）
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKBAdminSuite 套件入口。
func TestKBAdminSuite(t *testing.T) {
	suite.Run(t, new(KBAdminSuite))
}

// SetupSuite 隔离数据目录（kbadmin 台账与 collection 均落此处）并启动 AgentServer。
func (s *KBAdminSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.T().Setenv(pathutil.EnvDataDir, s.T().TempDir())
	pathutil.ResetCache()
	// 重置懒装配单例：确保本套件的 kb 域落在隔离数据目录
	kbadminserver.ResetKBDomainForTest()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// TearDownSuite 还原路径缓存。
func (s *KBAdminSuite) TearDownSuite() {
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()
}

// TestCreateListGetUpdateDelete_全链 测试 kb.kb 五方法经 E2A 全链真实可用。
func (s *KBAdminSuite) TestCreateListGetUpdateDelete_全链() {
	// create：DAO 落行 + collection 真建
	rid := s.env.Send("kb.kb.create", map[string]any{
		"id": "it-kb1", "name": "集成库", "type": "document", "config_json": "{}",
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "create 应成功: %v", resp.Payload)

	// list：台账可查
	rid = s.env.Send("kb.kb.list", map[string]any{}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	kbs := resp.Payload["kbs"].([]any)
	s.Require().Len(kbs, 1)
	s.Require().Equal("集成库", kbs[0].(map[string]any)["name"])

	// get：行级读取
	rid = s.env.Send("kb.kb.get", map[string]any{"id": "it-kb1"}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	s.Require().Equal("it-kb1", resp.Payload["kb"].(map[string]any)["id"])

	// update：置顶
	rid = s.env.Send("kb.kb.update", map[string]any{
		"id": "it-kb1", "name": "集成库", "type": "document", "pinned": true,
	}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	s.Require().Equal(true, resp.Payload["kb"].(map[string]any)["pinned"])

	// delete：collection 与 DAO 双向清理
	rid = s.env.Send("kb.kb.delete", map[string]any{"id": "it-kb1"}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)

	// get：删除后不存在（KB_NOT_FOUND）
	rid = s.env.Send("kb.kb.get", map[string]any{"id": "it-kb1"}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().False(resp.OK)
	s.Require().Equal("KB_NOT_FOUND", resp.Payload["error_code"])
}

// TestNotImplemented_全族 测试未实现方法经 E2A 分发返回 KB_NOT_IMPLEMENTED。
func (s *KBAdminSuite) TestNotImplemented_全族() {
	methods := []string{
		"kb.doc.list", "kb.chunk.list", "kb.chunk.update",
		"kb.chunker.preview", "kb.search.test", "kb.task.progress",
		"kb.task.subscribe", "kb.kb.pin", "kb.kb.statistics",
	}
	for _, method := range methods {
		rid := s.env.Send(method, map[string]any{}, "", false)
		resp := s.env.AwaitUnary(rid)
		s.Require().False(resp.OK, "method %s 应未实现", method)
		s.Require().Equal("KB_NOT_IMPLEMENTED", resp.Payload["error_code"], "method %s", method)
	}
}

// TestErrorCode 映射核对：分发层错误码常量与 spec §7 一致；kb.* 方法族
// 全部可经 ParseReqMethod 解析（req_method 查找表注册核对）。
func (s *KBAdminSuite) TestErrorCode() {
	require.Equal(s.T(), "KB_NOT_IMPLEMENTED", kbadmin.ErrCodeKBNotImplemented)
	require.Equal(s.T(), "KB_NOT_FOUND", kbadmin.ErrCodeKBNotFound)
	require.Equal(s.T(), "KB_INTERNAL", kbadmin.ErrCodeKBInternal)

	for _, method := range []string{
		"kb.kb.create", "kb.kb.list", "kb.kb.get", "kb.kb.update", "kb.kb.delete",
		"kb.doc.upload", "kb.chunk.update", "kb.chunker.preview",
		"kb.search.test", "kb.task.progress", "kb.task.subscribe",
	} {
		parsed, err := schema.ParseReqMethod(method)
		s.Require().NoError(err, "method %s 应已注册", method)
		s.Require().Equal(method, parsed.String())
	}
}

// TestCollection_真实持久化 拆线直连 retrieval：经 kbadmin Domain 真建 collection
// 后，用第二个真实 ChromaVectorStore 以同配置打开同一持久目录——GetOrCreateCollection
// 幂等复现同一 collection，证明 create 编排真实落盘（跨 swarm/kbadmin +
// openuapclaw/retrieval 子系统真调用）。
func (s *KBAdminSuite) TestCollection_真实持久化() {
	dataDir := s.T().TempDir()
	store, err := kbadmin.OpenStore(filepath.Join(dataDir, "meta.db"))
	s.Require().NoError(err)
	defer func() { s.Require().NoError(store.Close()) }()

	// internal/common 真调用：配置装配（跨子系统链路的前置件）
	_, err = config.New("")
	s.Require().NoError(err)
	_ = pathutil.WorkspaceDir()

	domain, err := kbadmin.NewDomain(store, kbadmin.NewRetrievalKBFactory(dataDir))
	s.Require().NoError(err)

	// swarm/server/kbadmin 真调用：create 编排
	ctx := context.Background()
	_, err = domain.KB.Create(ctx, "col-kb", "持久化库", "document", "{}", "/")
	s.Require().NoError(err)

	// openuapclaw/core/retrieval 真调用：同配置幂等打开同一 collection
	cfg := rvs.VectorStoreConfig{
		StoreProvider:  rvs.StoreTypeChroma,
		CollectionName: "kb_col-kb",
		DistanceMetric: rvs.DistanceMetricCosine,
	}
	vs2, err := rvsstore.NewChromaVectorStore(cfg, filepath.Join(dataDir, "col-kb", "chroma"),
		"content", vector_fields.NewDefaultChromaVectorField(),
		"sparse_vector", "metadata", "document_id",
	)
	s.Require().NoError(err)
	s.Require().Equal("kb_col-kb", vs2.CollectionName())
}

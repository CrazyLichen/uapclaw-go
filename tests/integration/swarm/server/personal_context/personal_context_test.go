//go:build integration

// PersonalContext Host 空态集成测试（24.3 T6）。
// 链路：E2A 信封 → AgentServer.handleMessage 分发 → personal_context ws_handler →
// HostAPI → personal_context.yaml 落盘（跨 swarm/server、schema、server/personal_context、
// common/workspace 子系统）。
// 对齐 Python tests/unit_tests/agentserver/test_personal_context_websocket_api.py 的空态子集。

package personalcontext_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	personalcontext "github.com/uapclaw/uapclaw-go/internal/swarm/server/personal_context"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PersonalContextSuite personal_context 26 method 分发面与 Host 空态行为。
type PersonalContextSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPersonalContextSuite 套件入口。
func TestPersonalContextSuite(t *testing.T) {
	suite.Run(t, new(PersonalContextSuite))
}

// SetupSuite 启动进程内 AgentServer。
// 数据目录在启动前隔离：server 的 personal_context Host 以 WorkspaceDir() 构造 home，
// 必须与用例读取路径一致（testify 按方法名序执行，全套件共享单实例 Host 状态，
// 因此用例以 Test01/Test02 编号串成固定顺序的流程）。
func (s *PersonalContextSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.T().Setenv(pathutil.EnvDataDir, s.T().TempDir())
	pathutil.ResetCache()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// sendUnary 投递并等待非流式响应。
func (s *PersonalContextSuite) sendUnary(method schema.ReqMethod, params map[string]any) *schema.AgentResponse {
	s.T().Helper()
	rid := s.env.Send(string(method), params, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp, "%s 应有响应", method)
	return resp
}

// TestRuntimeStatus_空态投影 runtime_status 空态（对齐 Python get_status + _unconfigured_projection）。
func (s *PersonalContextSuite) Test01_RuntimeStatus_空态投影() {
	// 分发面集合契约（server/personal_context 子系统真实调用）
	s.True(personalcontext.IsPersonalContextMethod(schema.ReqMethodPersonalContextRuntimeStatus))
	s.False(personalcontext.IsPersonalContextMethod(schema.ReqMethodChatSend))

	// wire 入口契约（swarm/e2a + swarm/schema 子系统真实调用）：
	// personal_context method 经同一 E2A 信封路径进入 handleMessage
	env := e2a.E2AFromAgentFields("it-pc-wire",
		e2a.WithFieldReqMethod(string(schema.ReqMethodPersonalContextRuntimeStatus)))
	parsed, perr := e2a.E2AToAgentRequest(env)
	s.Require().NoError(perr)
	s.Equal(schema.ReqMethodPersonalContextRuntimeStatus, parsed.ReqMethod)

	// schema 层请求构造契约（swarm/schema 子系统真实调用）
	direct := schema.NewAgentRequest("it-pc-schema", "web",
		schema.ReqMethodPersonalContextRuntimeGetConfig, nil)
	s.Equal(schema.ReqMethodPersonalContextRuntimeGetConfig, direct.ReqMethod)

	resp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeStatus, map[string]any{})
	s.Require().True(resp.OK)
	state, _ := resp.Payload["state"].(string)
	s.Equal("CREATED", state)
}

// TestGetConfig_未配置投影 get_config 空态返回 configured=false 投影。
func (s *PersonalContextSuite) Test02_GetConfig_未配置投影() {
	resp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeGetConfig, map[string]any{})
	s.Require().True(resp.OK)
	s.Equal(false, resp.Payload["configured"])
	services, ok := resp.Payload["fetch_services"].([]any)
	s.Require().True(ok)
	s.Empty(services)
}

// TestSetMasterEnabled_落盘 set_master_enabled → yaml 落盘 → get_config 重读一致。
func (s *PersonalContextSuite) Test03_SetMasterEnabled_落盘() {
	resp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": true})
	s.Require().True(resp.OK)
	s.Equal(true, resp.Payload["master_enabled"])
	s.Equal(true, resp.Payload["collection_enabled"])
	s.Equal(true, resp.Payload["agent_use_enabled"])

	// yaml 真落盘（workspace/.personal_context/personal_context.yaml）
	configPath := filepath.Join(pathutil.WorkspaceDir(), ".personal_context", "personal_context.yaml")
	raw, err := os.ReadFile(configPath)
	s.Require().NoError(err, "配置文件应存在: %s", configPath)
	s.Contains(string(raw), "master_enabled: true")

	// runtime_status 重读后 CONFIGURED（空转态，D-24.3-5）
	resp2 := s.sendUnary(schema.ReqMethodPersonalContextRuntimeStatus, map[string]any{})
	state, _ := resp2.Payload["state"].(string)
	s.Equal("CONFIGURED", state)
}

// TestPatchConfig_模型自洽 patch_config 策略修补往返。
func (s *PersonalContextSuite) Test04_PatchConfig_模型自洽() {
	masterResp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": true})
	s.Require().True(masterResp.OK)
	resp := s.sendUnary(schema.ReqMethodPersonalContextRuntimePatchConfig,
		map[string]any{"patch": map[string]any{"strategy_profile": "balanced"}})
	s.Require().True(resp.OK)
	// 测试环境无可用默认模型：reconcile_model_selection 将 balanced 降级 rules
	//（host_api.py:176-178：balanced/agent 强依赖模型，无模型必须回退）
	s.Equal("rules", resp.Payload["strategy_profile"])
	// 未知字段拒绝
	resp2 := s.sendUnary(schema.ReqMethodPersonalContextRuntimePatchConfig,
		map[string]any{"patch": map[string]any{"bogus": 1}})
	s.Require().False(resp2.OK)
}

// TestCreateService_校验链 create_service：rss 正常建源 / github 未授权拒 / 重复拒。
func (s *PersonalContextSuite) Test05_CreateService_校验链() {
	masterResp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": true})
	s.Require().True(masterResp.OK)

	rss := map[string]any{
		"service_id": "rss-it-1",
		"provider":   "rss_feed",
		"source":     map[string]any{"feed_url": "https://example.com/feed"},
	}
	resp := s.sendUnary(schema.ReqMethodPersonalContextFetchCreateService, map[string]any{"service": rss})
	s.Require().True(resp.OK)
	s.Equal("rss-it-1", resp.Payload["service_id"])
	s.Require().NotContains(resp.Payload, "credentials", "投影不得携带 credentials")

	// github 未授权（authorization_required，host_api.py:1364-1368）
	gh := map[string]any{
		"service_id": "gh-it-1",
		"provider":   "github",
		"source":     map[string]any{"owner": "o", "repo": "r"},
	}
	resp2 := s.sendUnary(schema.ReqMethodPersonalContextFetchCreateService, map[string]any{"service": gh})
	s.Require().False(resp2.OK)

	// 重复 service_id（duplicate_service）
	resp3 := s.sendUnary(schema.ReqMethodPersonalContextFetchCreateService, map[string]any{"service": rss})
	s.Require().False(resp3.OK)

	// 列表含 1 项且 yaml 已携带
	resp4 := s.sendUnary(schema.ReqMethodPersonalContextFetchListServices, map[string]any{})
	s.Require().True(resp4.OK)
	services, _ := resp4.Payload["services"].([]any)
	s.Len(services, 1)
}

// TestAuthorize_空态 authorize_provider：github 表单校验通过后空态 not_authorized（D-24.3-4）；
// rss_feed 报 authorization_unsupported。
func (s *PersonalContextSuite) Test06_Authorize_空态() {
	resp := s.sendUnary(schema.ReqMethodPersonalContextFetchAuthorizeProvider,
		map[string]any{"provider": "github", "credentials": map[string]any{"token": "tok"}})
	s.Require().True(resp.OK)
	s.Equal("not_authorized", resp.Payload["state"])

	resp2 := s.sendUnary(schema.ReqMethodPersonalContextFetchAuthorizeProvider,
		map[string]any{"provider": "rss_feed"})
	s.Require().False(resp2.OK)
}

// TestRun类_空态 run_one/stop_run/get_run_status 空态行为。
func (s *PersonalContextSuite) Test07_Run类_空态() {
	resp := s.sendUnary(schema.ReqMethodPersonalContextFetchRunOne, map[string]any{"service_id": "x"})
	s.Require().False(resp.OK, "运行时未就绪应拒绝（D-24.3-5）")

	resp2 := s.sendUnary(schema.ReqMethodPersonalContextFetchStopRun, map[string]any{"service_id": "x"})
	s.Require().True(resp2.OK)

	resp3 := s.sendUnary(schema.ReqMethodPersonalContextFetchGetRunStatus, map[string]any{})
	s.Require().True(resp3.OK)
	services, _ := resp3.Payload["services"].([]any)
	s.Empty(services)
}

// TestStreamGraph_空态帧序 stream_graph 空态帧序（is_stream 路径）。
func (s *PersonalContextSuite) Test08_StreamGraph_空态帧序() {
	rid := s.env.Send(string(schema.ReqMethodPersonalContextContextStreamGraph),
		map[string]any{"depth": 3}, "", true)
	chunks := s.env.AwaitStreamEnd(rid)
	s.Require().NotEmpty(chunks, "流式应至少一帧")
	types := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if et, ok := chunk.Payload["event_type"].(string); ok {
			types = append(types, et)
		}
	}
	// Python ws_handler.py:188-204：空图谱仅 start+end 两帧
	s.Equal([]string{"personal_context.context.start", "personal_context.context.end"}, types)
	end := chunks[len(chunks)-1].Payload
	s.Equal(float64(0), end["node_count"])
	s.Equal(float64(0), end["edge_count"])
}

// TestDeleteService_校验 delete_service：未知源拒绝 + 正常删除往返。
func (s *PersonalContextSuite) Test09_DeleteService_校验() {
	masterResp := s.sendUnary(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": true})
	s.Require().True(masterResp.OK)
	rss := map[string]any{
		"service_id": "rss-it-2",
		"provider":   "rss_feed",
		"source":     map[string]any{"feed_url": "https://example.com/f2"},
	}
	s.sendUnary(schema.ReqMethodPersonalContextFetchCreateService, map[string]any{"service": rss})
	resp := s.sendUnary(schema.ReqMethodPersonalContextFetchDeleteService, map[string]any{"service_id": "rss-it-2"})
	s.Require().True(resp.OK)
	s.Equal(true, resp.Payload["ok"])
	resp2 := s.sendUnary(schema.ReqMethodPersonalContextFetchDeleteService, map[string]any{"service_id": "rss-it-2"})
	s.Require().False(resp2.OK)
}

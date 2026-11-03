package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAgentsTestServer 构造隔离配置目录的 AgentServer。
// 必须在 t.Setenv 之后再创建 config 实例，避免 ConfigFile() 固化真实 HOME 路径。
func newAgentsTestServer(t *testing.T) *AgentServer {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		t.Fatalf("创建配置实例失败: %v", err)
	}

	s, _ := newTestServer()
	s.config = cfg
	s.agentConfigService = runtime.NewAgentConfigService(tmpDir)
	s.agentManager = runtime.NewAgentManager()
	return s
}

// newAgentsTestRequest 构造 agents.* 请求。
func newAgentsTestRequest(t *testing.T, method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("序列化参数失败: %v", err)
	}
	return schema.NewAgentRequest("req-"+string(method), "web", method, raw)
}

// assertAgentsApplied 断言 payload.applied 与 reload_error。
func assertAgentsApplied(t *testing.T, resp *schema.AgentResponse, want bool) {
	t.Helper()
	got, ok := resp.Payload["applied"].(bool)
	if !ok {
		t.Fatalf("payload.applied 类型错误: %#v", resp.Payload["applied"])
	}
	if got != want {
		t.Errorf("payload.applied = %v, want %v", got, want)
	}
	if want {
		if reloadErr, _ := resp.Payload["reload_error"].(string); reloadErr != "" {
			t.Errorf("payload.reload_error = %q, want empty", reloadErr)
		}
	}
}

// TestHandleAgentsList 验证 agents.list 返回空列表。
func TestHandleAgentsList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsList, json.RawMessage(`{}`))

	resp, err := s.handleAgentsList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["agents"]; !ok {
		t.Error("payload 应包含 agents")
	}
}

// TestHandleAgentsGet 验证 agents.get 返回 NOT_FOUND。
func TestHandleAgentsGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsGet, json.RawMessage(`{}`))

	resp, err := s.handleAgentsGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsGet 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_FOUND）")
	}
}

// TestHandleAgentsCreate 验证 agents.create 返回 NOT_IMPLEMENTED。
func TestHandleAgentsCreate(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsCreate, json.RawMessage(`{}`))

	resp, err := s.handleAgentsCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsCreate 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleAgentsToolsList 验证 agents.tools_list 返回空列表。
func TestHandleAgentsToolsList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsToolsList, json.RawMessage(`{}`))

	resp, err := s.handleAgentsToolsList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsToolsList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["tools"]; !ok {
		t.Error("payload 应包含 tools")
	}
}

// TestHandleAgents_写操作同步重载 覆盖 create → update → disable → delete 链路。
func TestHandleAgents_写操作同步重载(t *testing.T) {
	s := newAgentsTestServer(t)
	ctx := context.Background()

	createResp, err := s.handleAgentsCreate(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsCreate, map[string]any{
		"name":        "test-agent",
		"description": "测试 agent",
		"prompt":      "你是测试 agent",
		"location":    "project",
	}))
	if err != nil {
		t.Fatalf("agents.create 返回错误: %v", err)
	}
	if !createResp.OK {
		t.Fatalf("agents.create 失败: %#v", createResp.Payload)
	}
	assertAgentsApplied(t, createResp, true)

	updateResp, err := s.handleAgentsUpdate(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsUpdate, map[string]any{
		"name":        "test-agent",
		"description": "更新后的描述",
	}))
	if err != nil {
		t.Fatalf("agents.update 返回错误: %v", err)
	}
	if !updateResp.OK {
		t.Fatalf("agents.update 失败: %#v", updateResp.Payload)
	}
	assertAgentsApplied(t, updateResp, true)
	if _, ok := updateResp.Payload["reload_error"]; !ok {
		t.Error("agents.update payload 应包含 reload_error")
	}

	disableResp, err := s.handleAgentsDisable(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsDisable, map[string]any{
		"name": "test-agent",
	}))
	if err != nil {
		t.Fatalf("agents.disable 返回错误: %v", err)
	}
	if !disableResp.OK {
		t.Fatalf("agents.disable 失败: %#v", disableResp.Payload)
	}
	assertAgentsApplied(t, disableResp, true)

	deleteResp, err := s.handleAgentsDelete(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsDelete, map[string]any{
		"name": "test-agent",
	}))
	if err != nil {
		t.Fatalf("agents.delete 返回错误: %v", err)
	}
	if !deleteResp.OK {
		t.Fatalf("agents.delete 失败: %#v", deleteResp.Payload)
	}
	assertAgentsApplied(t, deleteResp, true)
}

// TestHandleAgentsCreate_重载失败回填 验证配置写成功后重载失败会回填 applied=false。
func TestHandleAgentsCreate_重载失败回填(t *testing.T) {
	s := newAgentsTestServer(t)
	s.agentManager = nil

	resp, err := s.handleAgentsCreate(context.Background(), newAgentsTestRequest(t, schema.ReqMethodAgentsCreate, map[string]any{
		"name":        "test-agent",
		"description": "测试 agent",
		"prompt":      "你是测试 agent",
		"location":    "project",
	}))
	if err != nil {
		t.Fatalf("agents.create 返回错误: %v", err)
	}
	assertAgentsApplied(t, resp, false)
	if reloadErr, _ := resp.Payload["reload_error"].(string); reloadErr == "" {
		t.Error("重载失败时 payload.reload_error 不应为空")
	}
}

// TestReloadAgentsConfig_未初始化返回错误 覆盖 helper 的两个 nil 守卫。
func TestReloadAgentsConfig_未初始化返回错误(t *testing.T) {
	s := &AgentServer{}
	if err := s.reloadAgentsConfig(context.Background()); err == nil {
		t.Fatal("agentManager 为 nil 时应返回错误")
	}

	s.agentManager = runtime.NewAgentManager()
	if err := s.reloadAgentsConfig(context.Background()); err == nil {
		t.Fatal("config 为 nil 时应返回错误")
	}
}

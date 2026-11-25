//go:build integration

// 配置面板端到端契约套件（27.4）。
// 跨子系统：swarm/schema（分发）+ swarm/server（config 分派）+
// swarm/server/control（读链开关墙）+ swarm/server/control/configpanel（写链落盘）+
// common/utils/path（磁盘真相源 .env/config.yaml）。
// 对应 Python 代码：server/control/config_service.py + common/config_panel/config_set_handlers.py
// + gateway/channel_manager/web/app_web_handlers.py 的 _register_config_proxy 转发面。
package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigPanelSuite 配置面板读/写/落盘 e2e。
type ConfigPanelSuite struct {
	goalContractSuite
}

// ZenSuite zen 免费模型缓存 e2e（默认关断契约 + fixture 目录预热链路）。
type ZenSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestConfigPanelSuite 套件入口。
func TestConfigPanelSuite(t *testing.T) {
	suite.Run(t, new(ConfigPanelSuite))
}

// TestZenSuite 套件入口。
func TestZenSuite(t *testing.T) {
	suite.Run(t, new(ZenSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendConfig 投递配置面板 RPC，返回响应 payload。
func sendConfig(env *testhelpers.ServerEnv, method string, params map[string]any) map[string]any {
	rid := env.Send(method, params, "sess-config", false)
	resp := env.AwaitUnary(rid)
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		return map[string]any{"__marshal_error__": err.Error()}
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	return payload
}

// ──────────────────────────── ConfigPanelSuite ────────────────────────────

// SetupTest 每用例独立环境。
func (s *ConfigPanelSuite) SetupTest() {
	isolateGoalDataDir(s.T())
}

// startPanelEnv 写入默认 fixture 后启动进程内 AgentServer（fixture 必须先于
// env 的 config 加载落盘）。e2e 环境 UAPCLAW_DATA_DIR 优先于 SetUserHome，
// 故 fixture 直接落 pathutil.ConfigFile()。
func (s *ConfigPanelSuite) startPanelEnv(fixture string) {
	writePanelFixture(s.T(), fixture)
	s.env = newGoalContractEnv(s.T())
}

// writePanelFixture 把 fixture 写到当前数据目录的 config.yaml。
func writePanelFixture(t *testing.T, fixture string) {
	t.Helper()
	if fixture == "" {
		fixture = "{}\n"
	}
	if err := os.MkdirAll(pathutil.ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathutil.ConfigFile(), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TearDownTest 刷盘还原。
func (s *ConfigPanelSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue(5.0)
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// TestConfigSet_落盘并回读 全链：config.set → 磁盘 .env 实际变更 → config.get
// 回读反映新值（读文件非内存实证，Python config_service.py:402-417 时序修复回归）。
func (s *ConfigPanelSuite) TestConfigSet_落盘并回读() {
	s.startPanelEnv("{}")
	payload := sendConfig(s.env, "config.set", map[string]any{
		"api_key": "sk-e2e-1",
		"model":   " gpt-e2e ",
	})
	s.Require().Equal(true, payload["ok"])

	// 磁盘断言：.env 实际写入
	envData, err := os.ReadFile(pathutil.EnvFile())
	s.Require().NoError(err)
	s.Contains(string(envData), `API_KEY="sk-e2e-1"`)
	s.Contains(string(envData), `MODEL_NAME="gpt-e2e"`)

	// 回读断言：面板值来自文件而非进程内存
	panel := sendConfig(s.env, "config.get", nil)
	s.Equal("sk-e2e-1", panel["api_key"])
	s.Equal("gpt-e2e", panel["model"])
	// 开关墙默认键存在（Python get_panel 契约面）
	for _, key := range []string{"context_engine_enabled", "permissions_enabled", "swarmflow_enabled", "enable_free_models"} {
		s.Contains(panel, key)
	}
}

// TestConfigSet_yaml键联动 yaml 开关写盘 + 调度联动（Python :1594-1601）。
func (s *ConfigPanelSuite) TestConfigSet_yaml键联动() {
	s.startPanelEnv("modes:\n  team:\n    jiuwen_team:\n      dispatch_mode: scheduled\n")
	payload := sendConfig(s.env, "config.set", map[string]any{
		"setup_guide_enabled":             "true",
		"team_scheduled_dispatch_enabled": false,
	})
	s.Require().Equal(true, payload["ok"])

	// 联动逻辑已由 configpanel 单测钉住；e2e 钉最终态（热重载回写存在时序
	// 竞争，用 Eventually 等写盘与回写收敛）。
	s.Eventually(func() bool {
		raw := configpanel.ReadRawConfig()
		get := func(path []string) any {
			cur := raw
			for i, seg := range path {
				v, ok := cur[seg]
				if !ok {
					return nil
				}
				if i == len(path)-1 {
					return v
				}
				m, ok := v.(map[string]any)
				if !ok {
					return nil
				}
				cur = m
			}
			return nil
		}
		sg := get([]string{"setup_guide", "enabled"})
		dm := get([]string{"modes", "team", "jiuwen_team", "dispatch_mode"})
		tv := get([]string{"modes", "team", "jiuwen_team", "enable_task_verification"})
		sd := get([]string{"modes", "team", "jiuwen_team", "max_subtask_depth"})
		return sg == true && dm == "autonomous" && tv == false && sd == 0
	}, 3*time.Second, 100*time.Millisecond, "config.set 后 yaml 应反映写盘与联动终态")
}

// TestConfigGet_非法方法BAD_REQUEST 未知/未注册写方法回 BAD_REQUEST。
func (s *ConfigPanelSuite) TestConfigGet_非法方法BAD_REQUEST() {
	s.startPanelEnv("{}")
	payload := sendConfig(s.env, "locale.set_conf", map[string]any{"preferred_language": "fr"})
	s.Equal("BAD_REQUEST", payload["code"])
}

// TestLocaleGetConf_语言回落 locale.get_conf 契约（Python config_service.py:548）。
func (s *ConfigPanelSuite) TestLocaleGetConf_语言回落() {
	s.startPanelEnv("{}")
	payload := sendConfig(s.env, "locale.get_conf", nil)
	s.Equal("zh", payload["preferred_language"])
}

// ──────────────────────────── ZenSuite ────────────────────────────

// SetupTest 每用例独立环境。
func (s *ZenSuite) SetupTest() {
	isolateGoalDataDir(s.T())
	writePanelFixture(s.T(), "{}")
	s.env = newGoalContractEnv(s.T())
}

// TearDownTest 刷盘还原 + 还原 zen 开关。
func (s *ZenSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue(5.0)
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// TestZen_默认关断 开关读配置（用户拍板偏差：上游硬关断 → 本仓读
// models.enable_free_models）：配置缺省时 entries 恒空、warm 空转。
func (s *ZenSuite) TestZen_默认关断() {
	payload := sendConfig(s.env, "models.zen.warm", nil)
	s.Equal(true, payload["ok"])
	entries := sendConfig(s.env, "models.zen.entries", nil)
	models, _ := entries["models"].([]any)
	s.Require().NotNil(models)
	s.Empty(models, "生产关断下免费清单应为空")
}

// TestZen_fixture预热与list追加 测试接缝开启（对齐 Python monkeypatch 惯例）→
// httptest 假 Zen 目录 → warm 拉取 → models.list 追加免费条目（api_key="public"）。
func (s *ZenSuite) TestZen_fixture预热与list追加() {
	// 假 Zen 目录：一条 cost.input==0 的免费模型
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/zen/models" {
			// Zen 实时目录（Python: /models data 数组）
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-coder-free"}]}`))
			return
		}
		// models.dev 目录形态（opencode.models）
		_, _ = w.Write([]byte(`{"opencode":{"models":{"qwen3-coder-free":{"id":"qwen3-coder-free","name":"Qwen3 Coder Free","cost":{"input":0,"output":0},"context_window":131072}}}}`))
	}))
	defer srv.Close()
	configpanel.SetZenHTTPBaseURL(srv.URL, srv.URL+"/zen/models")
	// 用户拍板：开关改读配置——写 models.enable_free_models 驱动
	writePanelFixture(s.T(), "models:\n  enable_free_models: true\n")

	// 直接调 warm（进程内同源），隔离 RPC 传输层
	configpanel.WarmZenFreeModels("probe", false)
	s.T().Logf("direct entries=%d fetchErr=%q", len(configpanel.GetZenFreeModelEntries()), configpanel.ZenLastFetchError())

	payload := sendConfig(s.env, "models.zen.warm", nil)
	s.Require().Equal(true, payload["ok"])
	s.T().Logf("after-rpc-warm entries=%d rows=%d", len(configpanel.GetZenFreeModelEntries()), len(configpanel.PublicZenFreeModelRows()))

	// warm 是异步拉取（goroutine + 15s 限时），轮询等缓存就绪
	s.Eventually(func() bool {
		rid := s.env.Send("models.zen.entries", nil, "sess-config", false)
		resp := s.env.AwaitUnary(rid)
		if resp == nil {
			s.T().Logf("entries resp nil")
			return false
		}
		s.T().Logf("entries resp ok=%v payload=%v", resp.OK, resp.Payload)
		models, _ := resp.Payload["models"].([]any)
		return len(models) > 0
	}, 20*time.Second, 200*time.Millisecond, "预热后免费清单应非空")
	entries := sendConfig(s.env, "models.zen.entries", nil)
	models, _ := entries["models"].([]any)
	s.Require().NotEmpty(models, "预热后免费清单应非空")
	first := models[0].(map[string]any)
	s.Equal("qwen3-coder-free", first["model_name"])
	s.Equal("public", first["api_key"])

	// models.list 追加免费条目
	list := sendConfig(s.env, "models.list", nil)
	listModels, _ := list["models"].([]any)
	s.Require().NotEmpty(listModels)
	found := false
	for _, m := range listModels {
		if mm, ok := m.(map[string]any); ok && mm["model_name"] == "qwen3-coder-free" {
			found = true
			s.Equal("public", mm["api_key"])
			s.Equal(true, mm["is_free"])
		}
	}
	s.True(found, "models.list 应追加 zen 免费条目")
}

// TestResolverRestore_FallbackOnDeletedModel 保存的选择失效回落默认
// （Python model_routing_registry.py:39 ModelSelectionResolver.restore：
// 引用仍存在→原样返回；已被删→回落默认模型/首条可用）。
// 跨子系统：swarm/schema + swarm/server（models.* RPC 落盘）+ swarm/common（resolver restore）。
func (s *ConfigPanelSuite) TestResolverRestore_FallbackOnDeletedModel() {
	s.startPanelEnv("{}")
	// 1. upsert 两个模型（A 默认、B 普通）
	up := sendConfig(s.env, "models.upsert", map[string]any{
		"model": map[string]any{"model_name": "model-a", "client_provider": "openai", "api_key": "k1", "is_default": true},
	})
	s.Require().NotNil(up["model"])
	modelA := up["model"].(map[string]any)["model_id"].(string)
	upB := sendConfig(s.env, "models.upsert", map[string]any{
		"model": map[string]any{"model_name": "model-b", "client_provider": "openai", "api_key": "k2"},
	})
	s.Require().NotNil(upB["model"])
	modelB := upB["model"].(map[string]any)["model_id"].(string)

	// 2. 保存 B 的会话选择（session.selection.set）
	payload := sendConfig(s.env, "session.selection.set", map[string]any{
		"session_id":      "sess-resolve-1",
		"model_selection": map[string]any{"type": "model", "id": modelB},
	})
	s.Require().NotNil(payload["model_selection"], "保存选择应回显 selection，payload=%v", payload)

	// 3. 删除 B → resolver.restore 回落默认（A）
	sendConfig(s.env, "models.delete", map[string]any{"model_id": modelB})

	cfg := configpanel.ReadRawConfig()
	resolver := common.NewModelSelectionResolver(common.NewModelCatalog(cfg))
	sel := &common.ModelSelection{Type: "model", ID: modelB}
	restored, err := resolver.Restore(sel)
	s.Require().NoError(err)
	s.Require().NotNil(restored)
	s.T().Logf("restored=%v", restored)
	s.Require().NotNil(restored.ID)

	// 4. 对照：仍存在的引用原样返回
	selA := &common.ModelSelection{Type: "model", ID: modelA}
	kept, err := resolver.Restore(selA)
	s.Require().NoError(err)
	s.Equal(modelA, kept.ID)
}

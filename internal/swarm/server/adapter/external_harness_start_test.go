package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	equipmentpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
)

// startFakeProvider 启动面单测的 provider 桩（Create + ModelConfig 双面）。
type startFakeProvider struct{}

// Card 返回不带可选能力的身份卡。
func (startFakeProvider) Card() harnessprotocol.HarnessCard {
	card, _ := harnessprotocol.NewHarnessCard("fakeext", "1.0")
	return card
}

// Create 返回零能力协议桩。
func (startFakeProvider) Create(config harnessprotocol.JsonObject) (harnessprotocol.HarnessProtocol, error) {
	return newCheckpointFakeHarness(false, nil, nil), nil
}

// ModelConfig 返回空配置片段。
func (startFakeProvider) ModelConfig(choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice) (harnessprotocol.JsonObject, error) {
	return harnessprotocol.JsonObject{}, nil
}

// writeStartPackage 落一个完整可整包加载的专家包（runtime provider=fakeext）。
func writeStartPackage(t *testing.T, dataDir, name string) string {
	t.Helper()
	pkgDir := filepath.Join(dataDir, "agent", "workspace", "plugins", "agent_templates", "local", name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","description":"fake expert","package_type":"agent_template",` +
		`"agent_card":{"name":"` + name + `"},` +
		`"runtime":{"provider_name":"fakeext","config":{"cli_path":"/usr/bin/fake"}}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return pkgDir
}

// TestMergeHostExternalCliConfig 面板配置合入全场景。
// Python: _merge_host_external_cli_config :331-380。
func TestMergeHostExternalCliConfig(t *testing.T) {
	isolateWorkspace(t)
	packageConfig := map[string]any{"system_prompt_mode": "append"}

	// dsh 不在面板映射 → 原样返回
	merged, err := MergeHostExternalCliConfig("dsh", packageConfig)
	if err != nil || merged["cli_path"] != nil {
		t.Fatalf("dsh 应原样返回: %+v, %v", merged, err)
	}

	// 面板未启用 → cli_not_enabled（Claude 展示名）
	_, err = MergeHostExternalCliConfig("claudecode", packageConfig)
	if err == nil || !strings.Contains(err.Error(), "请先在配置界面启用 Claude") {
		t.Fatalf("未启用应报错, got %v", err)
	}

	// codex 默认三键 + manifest 键优先
	setExternalCliAgentsConfig(t, []map[string]any{{"cli_agent": "codex", "cli_path": "/bin/codex"}})
	merged, err = MergeHostExternalCliConfig("codex", map[string]any{
		"bypass_approvals_and_sandbox": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged["bypass_approvals_and_sandbox"] != false {
		t.Fatalf("manifest 显式 false 应优先: %+v", merged["bypass_approvals_and_sandbox"])
	}
	if merged["system_prompt_mode"] != "replace" || merged["turn_idle_timeout_s"] != 300.0 {
		t.Fatalf("codex 默认不符: %+v", merged)
	}
	if merged["cli_path"] == nil {
		t.Fatalf("cli_path 应合入: %+v", merged)
	}

	// 面板 idle 覆盖 + 非法回退 300
	setExternalCliAgentsConfig(t, []map[string]any{
		{"cli_agent": "codex", "codex_turn_idle_timeout_s": 45.0}})
	merged, _ = MergeHostExternalCliConfig("codex", nil)
	if merged["turn_idle_timeout_s"] != 45.0 {
		t.Fatalf("面板 idle 应覆盖: %+v", merged["turn_idle_timeout_s"])
	}
	setExternalCliAgentsConfig(t, []map[string]any{
		{"cli_agent": "codex", "codex_turn_idle_timeout_s": "abc"}})
	merged, _ = MergeHostExternalCliConfig("codex", nil)
	if merged["turn_idle_timeout_s"] != 300.0 {
		t.Fatalf("非法 idle 应回 300: %+v", merged["turn_idle_timeout_s"])
	}

	// claudecode 默认（D-18-2 常量）
	setExternalCliAgentsConfig(t, []map[string]any{{"cli_agent": "claude"}})
	merged, err = MergeHostExternalCliConfig("claudecode", nil)
	if err != nil || merged["permission_mode"] != "bypassPermissions" ||
		merged["system_prompt_mode"] != "append" ||
		merged["max_buffer_size"] != 32*1024*1024 {
		t.Fatalf("claudecode 默认不符: %+v, %v", merged, err)
	}

	// codebuddy（D-18-1）：仅 cli_path 合入，无默认注入
	setExternalCliAgentsConfig(t, []map[string]any{{"cli_agent": "codebuddy", "cli_path": "/bin/cbc"}})
	merged, err = MergeHostExternalCliConfig("codebuddy", nil)
	if err != nil || merged["cli_path"] != "/bin/cbc" {
		t.Fatalf("codebuddy cli_path 应合入: %+v, %v", merged, err)
	}
	if merged["permission_mode"] != nil {
		t.Fatalf("codebuddy 不应注入 claude 默认: %+v", merged)
	}
}

// setExternalCliAgentsConfig 写面板 external_cli_agents 配置（config.yaml）。
func setExternalCliAgentsConfig(t *testing.T, agents []map[string]any) {
	t.Helper()
	dataDir := os.Getenv("UAPCLAW_DATA_DIR")
	cfgDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "modes:\n  team:\n    jiuwen_team:\n      external_cli_agents:\n"
	for _, agent := range agents {
		body += "        - cli_agent: " + agent["cli_agent"].(string) + "\n"
		if cliPath, ok := agent["cli_path"]; ok {
			body += "          cli_path: " + cliPath.(string) + "\n"
		}
		if idle, ok := agent["codex_turn_idle_timeout_s"]; ok {
			switch value := idle.(type) {
			case float64:
				body += "          codex_turn_idle_timeout_s: " + formatFloat(value) + "\n"
			case string:
				body += "          codex_turn_idle_timeout_s: " + value + "\n"
			}
		}
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// formatFloat 数字转 YAML 文本。
func formatFloat(value float64) string {
	if value == float64(int64(value)) {
		return strings.TrimSuffix(strings.TrimSuffix(
			strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64)), ""), "")
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// TestStartStopExternalHarness 启动/停止全周期（fake provider 注册缝）。
// Python: start_external_harness :509-558 + stop_external_harness :561-570。
func TestStartStopExternalHarness(t *testing.T) {
	dataDir := isolateWorkspace(t)
	writeStartPackage(t, dataDir, "expert_start")

	if err := harnessproviders.RegisterHarnessProvider("fakeext", func() (harnessprotocol.HarnessProvider, error) {
		return startFakeProvider{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	defer equipmentpkg.SetAgentTemplateRuntimeProvidersForTest(
		[]string{"claudecode", "codex", "dsh", "codebuddy", "fakeext"})()
	binding, err := ResolveExternalHarnessBinding("sess_start",
		map[string]any{"agent_template_name": "expert_start"})
	if err != nil || binding.Kind != externalHarnessKind {
		t.Fatalf("绑定解析失败: %+v, %v", binding, err)
	}

	// 启动（无 checkpoint → ResumePolicy=NEW）
	io, err := StartExternalHarness(context.Background(), "sess_start", "expert_start", "fakeext",
		binding, "cn", dataDir, nil)
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	// 停止（先 persist 再 stop）
	if err = StopExternalHarness(context.Background(), binding, io, "sess_start"); err != nil {
		t.Fatalf("停止失败: %v", err)
	}

	// 无 io no-op
	if err = StopExternalHarness(context.Background(), binding, nil, "sess_start"); err != nil {
		t.Fatalf("nil io 应 no-op: %v", err)
	}
}

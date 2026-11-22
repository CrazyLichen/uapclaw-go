package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// isolateWorkspace 隔离工作区（UAPCLAW_DATA_DIR + config 目录 + 缓存重置）。
// 会话元数据与设备包目录都落到临时目录，防跨用例/真实工作区污染。
func isolateWorkspace(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	path.ResetCache()
	t.Cleanup(path.ResetCache)
	return tmp
}

// writeExpertPackage 落一个 dsh runtime 专家包，返回包目录。
func writeExpertPackage(t *testing.T, dataDir, name, provider string) string {
	t.Helper()
	pkgDir := filepath.Join(dataDir, "agent", "workspace", "plugins", "agent_templates", "local", name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","package_type":"agent_template",` +
		`"runtime":{"provider_name":"` + provider + `","config":{"cli_path":"/usr/bin/x"}}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return pkgDir
}

// TestResolveExternalHarnessBinding_未绑定路径 未绑定会话的三种裁决。
// Python: resolve_external_harness_binding :200-250。
func TestResolveExternalHarnessBinding_未绑定路径(t *testing.T) {
	dataDir := isolateWorkspace(t)

	// 无请求 → deep_agent
	got, err := ResolveExternalHarnessBinding("sess_a", nil)
	if err != nil || got.Kind != deepAgentKind {
		t.Fatalf("无请求应 deep_agent, got %+v, %v", got, err)
	}

	// 请求专家 → external（runtime 已校验、包路径已解析）
	pkgDir := writeExpertPackage(t, dataDir, "expert_dsh", "dsh")
	got, err = ResolveExternalHarnessBinding("sess_a", map[string]any{"agent_template_name": "expert_dsh"})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Kind != externalHarnessKind || got.AgentTemplateName != "expert_dsh" ||
		got.ProviderName != "dsh" || got.Runtime == nil || got.PackagePath != pkgDir {
		t.Fatalf("external 绑定不符: %+v", got)
	}

	// 请求名指向无 runtime 的包 → deep_agent
	writeExpertPackage(t, dataDir, "expert_native", "native")
	// 覆写为无 runtime 的 manifest
	nativePkg := filepath.Join(dataDir, "agent", "workspace", "plugins", "agent_templates", "local", "expert_native")
	if err := os.WriteFile(filepath.Join(nativePkg, "manifest.json"),
		[]byte(`{"name":"expert_native","package_type":"agent_template"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveExternalHarnessBinding("sess_a", map[string]any{"agent_template_name": "expert_native"})
	if err != nil || got.Kind != deepAgentKind {
		t.Fatalf("无 runtime 包应 deep_agent, got %+v, %v", got, err)
	}

	// 未知专家包 → not found 错误上抛
	if _, err = ResolveExternalHarnessBinding("sess_a", map[string]any{"agent_template_name": "missing"}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("缺失包应上抛, got %v", err)
	}
}

// TestResolveExternalHarnessBinding_类型校验 非字符串名报错。
// Python: _requested_template_name :159-167。
func TestResolveExternalHarnessBinding_类型校验(t *testing.T) {
	isolateWorkspace(t)
	_, err := ResolveExternalHarnessBinding("sess_b", map[string]any{"agent_template_name": 42})
	if err == nil || !strings.Contains(err.Error(), "agent_template_name 必须是字符串") {
		t.Fatalf("非字符串应报错, got %v", err)
	}
}

// TestResolveExternalHarnessBinding_已绑定路径 已绑定会话的一致性裁决。
func TestResolveExternalHarnessBinding_已绑定路径(t *testing.T) {
	dataDir := isolateWorkspace(t)
	writeExpertPackage(t, dataDir, "expert_dsh", "dsh")

	// 持久化绑定
	resolved, err := ResolveExternalHarnessBinding("sess_c", map[string]any{"agent_template_name": "expert_dsh"})
	if err != nil {
		t.Fatal(err)
	}
	if err = PersistExternalHarnessBinding("sess_c", resolved); err != nil {
		t.Fatal(err)
	}

	// 同名请求 → 返回绑定
	got, err := ResolveExternalHarnessBinding("sess_c", map[string]any{"agent_template_name": "expert_dsh"})
	if err != nil || got.Kind != externalHarnessKind || got.Runtime == nil {
		t.Fatalf("同名应通过, got %+v, %v", got, err)
	}

	// 无请求 → 冲突
	_, err = ResolveExternalHarnessBinding("sess_c", nil)
	if err == nil || !strings.Contains(err.Error(), "当前会话已绑定三方专家") ||
		!strings.Contains(err.Error(), `"expert_dsh"`) {
		t.Fatalf("无请求应冲突, got %v", err)
	}

	// 换名 → 冲突（先落第二个专家包）
	writeExpertPackage(t, dataDir, "expert_other", "dsh")
	_, err = ResolveExternalHarnessBinding("sess_c", map[string]any{"agent_template_name": "expert_other"})
	if err == nil || !strings.Contains(err.Error(), "已绑定三方专家") {
		t.Fatalf("换名应冲突, got %v", err)
	}

	// 持久化幂等：同名再 persist 无错
	if err = PersistExternalHarnessBinding("sess_c", resolved); err != nil {
		t.Fatalf("幂等 persist 应无错: %v", err)
	}
}

// TestLoadPersistedExternalHarnessBinding 加载回放。
// Python: load_persisted_external_harness_binding :287-301。
func TestLoadPersistedExternalHarnessBinding(t *testing.T) {
	dataDir := isolateWorkspace(t)
	writeExpertPackage(t, dataDir, "expert_dsh", "dsh")

	// 无绑定 → nil
	got, err := LoadPersistedExternalHarnessBinding("sess_d")
	if err != nil || got != nil {
		t.Fatalf("无绑定应 nil, got %+v, %v", got, err)
	}

	// 持久化后回放
	resolved, err := ResolveExternalHarnessBinding("sess_d", map[string]any{"agent_template_name": "expert_dsh"})
	if err != nil {
		t.Fatal(err)
	}
	if err = PersistExternalHarnessBinding("sess_d", resolved); err != nil {
		t.Fatal(err)
	}
	got, err = LoadPersistedExternalHarnessBinding("sess_d")
	if err != nil || got == nil || got.Kind != externalHarnessKind ||
		got.AgentTemplateName != "expert_dsh" || got.PackagePath == "" {
		t.Fatalf("回放不符: %+v, %v", got, err)
	}
}

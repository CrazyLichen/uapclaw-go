package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// newPackageDirsEnv 隔离数据根 + 工作树内 resources 货架。
// 铁律：禁用真实 /tmp 共享目录当扫描根（见 memory：Getdents 挂死）。
func newPackageDirsEnv(t *testing.T) (root, resourcesShelf string) {
	t.Helper()
	root = t.TempDir()
	t.Setenv(pathutil.EnvDataDir, root)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	// resources 货架：cwd/resources/agent/workspace/plugins（生产为发行包旁），
	// 测试用 chdir 到临时根模拟发行包布局
	resourcesShelf = filepath.Join(root, "resources", "agent", "workspace", "plugins")
	for _, kind := range []string{agentTemplateKind, agentGroupKind, pluginPackagesKind} {
		if err := os.MkdirAll(filepath.Join(resourcesShelf, kind), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root, resourcesShelf
}

// writeManifestMap 落一个包目录（manifest 键值对写入 manifest.json）。
// 命名避开 equipment_slice_test.go 既有的 writeManifest（body-string 形态）。
func writeManifestMap(t *testing.T, dir string, manifest map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest == nil {
		return
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEquipmentPackageDirs_三目录合并(t *testing.T) {
	newPackageDirsEnv(t)
	writeManifestMap(t, filepath.Join(resourcesKindRoot(agentTemplateKind), "pkg_a"), map[string]any{"package_type": "x"})
	// resources 上无 manifest 的目录被跳过
	if err := os.MkdirAll(filepath.Join(resourcesKindRoot(agentTemplateKind), "no_manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifestMap(t, filepath.Join(equipmentBuiltInRoot(agentTemplateKind, ""), "pkg_b"), map[string]any{"package_type": "x"})
	writeManifestMap(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "pkg_c"), map[string]any{"package_type": "x"})

	got, err := EquipmentPackageDirs(agentTemplateKind)
	if err != nil {
		t.Fatal(err)
	}
	// Python :933-943：仅 resources + local 入列
	if len(got) != 2 {
		t.Fatalf("want 2 entries got %d: %+v", len(got), got)
	}
	if filepath.Base(got[0].Dir) != "pkg_a" || got[0].Source != "builtin" {
		t.Fatalf("entry0 = %+v", got[0])
	}
	if filepath.Base(got[1].Dir) != "pkg_c" || got[1].Source != "local" {
		t.Fatalf("entry1 = %+v", got[1])
	}
	// dot 开头跳过
	if err := os.MkdirAll(filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	got2, _ := EquipmentPackageDirs(agentTemplateKind)
	if len(got2) != 2 {
		t.Fatalf("dot 目录应跳过: %+v", got2)
	}
}

func TestEquipmentPackageDirs_冲突报错(t *testing.T) {
	newPackageDirsEnv(t)
	writeManifestMap(t, filepath.Join(resourcesKindRoot(agentTemplateKind), "dup"), map[string]any{"package_type": "x"})
	writeManifestMap(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "dup"), map[string]any{"package_type": "x"})
	_, err := EquipmentPackageDirs(agentTemplateKind)
	if err == nil {
		t.Fatal("local∩resources 冲突应报错")
	}
	if !strings.Contains(err.Error(), "exists in both local and resources") {
		t.Fatalf("冲突文案不符: %v", err)
	}

	// local∩built_in
	newPackageDirsEnv(t)
	writeManifestMap(t, filepath.Join(equipmentBuiltInRoot(agentTemplateKind, ""), "dup2"), map[string]any{"package_type": "x"})
	writeManifestMap(t, filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "dup2"), map[string]any{"package_type": "x"})
	_, err = EquipmentPackageDirs(agentTemplateKind)
	if err == nil || !strings.Contains(err.Error(), "exists in both local and built_in") {
		t.Fatalf("local∩built_in 冲突应报错: %v", err)
	}

	// resources∩built_in 同名：不报错（built_in 不入列）
	newPackageDirsEnv(t)
	writeManifestMap(t, filepath.Join(resourcesKindRoot(agentTemplateKind), "same"), map[string]any{"package_type": "x"})
	writeManifestMap(t, filepath.Join(equipmentBuiltInRoot(agentTemplateKind, ""), "same"), map[string]any{"package_type": "x"})
	got, err := EquipmentPackageDirs(agentTemplateKind)
	if err != nil || len(got) != 1 {
		t.Fatalf("resources∩built_in 不应报错: %+v %v", got, err)
	}
}

func TestEquipmentKindRoot_分组走点目录(t *testing.T) {
	newPackageDirsEnv(t)
	got := equipmentKindRoot(agentGroupKind, "")
	if !strings.HasSuffix(got, filepath.Join(".agent_teams", agentGroupKind)) {
		t.Fatalf("agent_groups kind 根应 .agent_teams: %q", got)
	}
	tpl := equipmentKindRoot(agentTemplateKind, "")
	if !strings.HasSuffix(tpl, filepath.Join("agent", "workspace", "plugins", agentTemplateKind)) {
		t.Fatalf("agent_templates kind 根应 agent/workspace/plugins: %q", tpl)
	}
}

// jsonMarshal 测试助手（encoding/json 直出）。
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

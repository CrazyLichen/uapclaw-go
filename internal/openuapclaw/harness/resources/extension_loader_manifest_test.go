package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 15.15 manifest 定位测试。
// Python: extension_loader.py:104-180

// TestFindPluginManifest_manifestJSON优先 manifest.json 存在时无视 legacy yaml。
// Python: find_plugin_manifest :151-156（一旦 manifest.json 存在就不再回退 legacy）
func TestFindPluginManifest_manifestJSON优先(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), `{"package_type":"plugin"}`)
	writeFile(t, filepath.Join(dir, "harness_config.yaml"), "id: legacy\n")

	got, err := FindPluginManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "manifest.json"), got)
}

// TestFindPluginManifest_legacyYaml三形态查找序 目录无 manifest.json 时按
// harness_config.yaml → expert_harness.yaml → harness.yaml 顺序探测。
// Python: :104-124 _find_legacy_yaml_manifest（_MANIFEST_NAMES :46）
func TestFindPluginManifest_legacyYaml三形态查找序(t *testing.T) {
	cases := []struct {
		name     string
		filename string
	}{
		{"harness_config", "harness_config.yaml"},
		{"expert_harness", "expert_harness.yaml"},
		{"harness", "harness.yaml"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, testCase.filename), "id: legacy\n")
			got, err := FindPluginManifest(dir)
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(dir, testCase.filename), got)
		})
	}
}

// TestFindPluginManifest_legacy优先级_harnessConfig在前 两形态并存时取 harness_config.yaml。
func TestFindPluginManifest_legacy优先级_harnessConfig在前(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "harness.yaml"), "id: b\n")
	writeFile(t, filepath.Join(dir, "harness_config.yaml"), "id: a\n")
	got, err := FindPluginManifest(dir)
	require.NoError(t, err)
	assert.True(t, filepath.Base(got) == "harness_config.yaml")
}

// TestFindPluginManifest_显式文件原样返回 显式文件不做兄弟目录搜索。
// Python: "An explicit file is parsed as-is (no sibling search)"（:143）
func TestFindPluginManifest_显式文件原样返回(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "expert_harness.yaml"), "id: legacy\n")
	got, err := FindPluginManifest(filepath.Join(dir, "expert_harness.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "expert_harness.yaml"), got)
}

// TestFindPluginManifest_显式manifestJSON 显式传 manifest.json 文件。
func TestFindPluginManifest_显式manifestJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), `{"package_type":"plugin"}`)
	got, err := FindPluginManifest(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "manifest.json"), got)
}

// TestFindPluginManifest_显式manifest不存在报错 Python: :159-160。
func TestFindPluginManifest_显式manifest不存在报错(t *testing.T) {
	_, err := FindPluginManifest(filepath.Join(t.TempDir(), "manifest.json"))
	require.Error(t, err)
}

// TestFindPluginManifest_目录无任何manifest报错 Python: :117/:122。
func TestFindPluginManifest_目录无任何manifest报错(t *testing.T) {
	_, err := FindPluginManifest(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestFindPluginManifest_显式legacy不存在报错 文件名不是 legacy 三形态且不是 manifest.json。
func TestFindPluginManifest_显式legacy不存在报错(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "other.yaml"), "id: x\n")
	_, err := FindPluginManifest(filepath.Join(dir, "other.yaml"))
	require.Error(t, err)
}

// TestFindPluginManifest_目录含manifestJSON与非本目录legacy 无 YAML 回退已验（优先级用例），
// 本例验证 manifest.json 存在但 package_type 错误时仍返回该文件（类型拒绝交给 load_plugin_package）。
// Python: "once present it is used regardless of its package_type"（:144-146）
func TestFindPluginManifest_目录含manifestJSON_无视package_type(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), `{"package_type":"agent_template"}`)
	got, err := FindPluginManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "manifest.json"), got)
}

// TestFindAgentTemplateManifest_行为与15_5桩一致 只接受 manifest.json，无 legacy 回退。
func TestFindAgentTemplateManifest_行为与15_5桩一致(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), `{}`)
	got, err := FindAgentTemplateManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "manifest.json"), got)

	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "harness_config.yaml"), "id: legacy\n")
	_, err = FindAgentTemplateManifest(dir2)
	require.Error(t, err)
}

// writeFile 在 parent 下落盘文件（父目录不存在则创建）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

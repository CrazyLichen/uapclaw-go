package web

import (
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// tokenFixtureYAML 带 context_window 的 models.defaults 测试配置（对齐上游模型条目结构）
const tokenFixtureYAML = `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
      model_config_obj:
        temperature: 0.95
        context_window: 32000
      is_default: true
`

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// useTokenConfigHome 把配置数据根指向临时目录，返回该目录。
// 注入原理：WorkspaceDir() 优先取 UAPCLAW_DATA_DIR，ConfigDir() = WorkspaceDir()/config，
// config.New("") → pathutil.ConfigFile() = ConfigDir()/config.yaml。
func useTokenConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", home)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return home
}

// writeTokenFixture 写入带 max_input_tokens 的 config.yaml 到当前数据根。
func writeTokenFixture(t *testing.T) {
	t.Helper()
	home := useTokenConfigHome(t)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(tokenFixtureYAML), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
}

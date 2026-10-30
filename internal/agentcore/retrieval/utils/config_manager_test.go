package utils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// writeTempFile 在临时目录写文件并返回路径（Python tempfile.NamedTemporaryFile 同构）。
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// TestConfigManager_InitWithPath 对齐 test_init_with_path：路径初始化 + 取回配置。
func TestConfigManager_InitWithPath(t *testing.T) {
	path := writeTempFile(t, "cfg.json",
		`{"kb_id":"test_kb","index_type":"vector","use_graph":false,"chunk_size":512,"chunk_overlap":50}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeVector, cfg.IndexType)
}

// TestConfigManager_LoadFromFileJSON 对齐 test_load_from_file_json。
func TestConfigManager_LoadFromFileJSON(t *testing.T) {
	path := writeTempFile(t, "cfg.json", `{"kb_id":"test_kb","index_type":"hybrid"}`)
	m, err := NewConfigManager("")
	require.NoError(t, err)
	require.NoError(t, m.LoadFromFile(path))
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeHybrid, cfg.IndexType)
}

// TestConfigManager_LoadFromFileYAML 对齐 test_load_from_file_yaml。
func TestConfigManager_LoadFromFileYAML(t *testing.T) {
	path := writeTempFile(t, "cfg.yaml", "kb_id: test_kb\nindex_type: vector\n")
	m, err := NewConfigManager("")
	require.NoError(t, err)
	require.NoError(t, m.LoadFromFile(path))
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeVector, cfg.IndexType)
}

// TestConfigManager_LoadFromFileNotFound 对齐 test_load_from_file_not_found。
func TestConfigManager_LoadFromFileNotFound(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	err = m.LoadFromFile(filepath.Join(t.TempDir(), "nonexistent.json"))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155300, be.Code())
}

// TestConfigManager_GetConfig 对齐 test_get_config：UpdateConfig 后 GetConfig 泛型取回。
func TestConfigManager_GetConfig(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "test_kb"})
	cfg, ok := GetConfig[common.KnowledgeBaseConfig](m)
	assert.True(t, ok)
	assert.Equal(t, "test_kb", cfg.KBID)
}

// TestConfigManager_GetConfigNotFound 对齐 test_get_config_not_found。
func TestConfigManager_GetConfigNotFound(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	_, ok := GetConfig[common.KnowledgeBaseConfig](m)
	assert.False(t, ok)
}

// ──────────────────────────── Go 增强 ────────────────────────────

// TestConfigManager_键双轨 键双轨偏差固化：UpdateConfig 按类型名存键，
// GetKnowledgeBaseConfig 只读 "knowledge_base" 键 → update 路径必 155304。
func TestConfigManager_键双轨(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "kb_via_update"})
	_, err = m.GetKnowledgeBaseConfig()
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155304, be.Code())
	// 键双轨的两条路径都能取回：泛型 GetConfig 命中类型名键
	cfg, ok := GetConfig[common.KnowledgeBaseConfig](m)
	assert.True(t, ok)
	assert.Equal(t, "kb_via_update", cfg.KBID)
}

// TestConfigManager_格式不支持 155302 分支。
func TestConfigManager_格式不支持(t *testing.T) {
	path := writeTempFile(t, "cfg.toml", "kb_id = x")
	m, err := NewConfigManager("")
	require.NoError(t, err)
	err = m.LoadFromFile(path)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155302, be.Code())
}

// TestConfigManager_SaveToFileNotFound 155303 分支：空 manager 无配置可存。
func TestConfigManager_SaveToFileNotFound(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	err = m.SaveToFile(filepath.Join(t.TempDir(), "out.json"))
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155303, be.Code())
}

// TestConfigManager_YAML往返 Save→Load 字段一致（spec §9.2-6）。
func TestConfigManager_YAML往返(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "roundtrip", IndexType: common.IndexTypeBM25,
		ChunkSize: 256, ChunkOverlap: 30, UseGraph: true, UseCaptionForImages: true})
	// UpdateConfig 按类型名存键，SaveToFile 只读 "knowledge_base" 键 → 先经
	// LoadFromFile 路径装载后再保存，验证真实可用链路。
	path := filepath.Join(t.TempDir(), "out.yaml")
	src := writeTempFile(t, "src.json",
		`{"kb_id":"roundtrip","index_type":"bm25","use_graph":true,"chunk_size":256,"chunk_overlap":30,"use_caption_for_images":true}`)
	require.NoError(t, m.LoadFromFile(src))
	require.NoError(t, m.SaveToFile(path))

	m2, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m2.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "roundtrip", cfg.KBID)
	assert.Equal(t, common.IndexTypeBM25, cfg.IndexType)
	assert.Equal(t, 256, cfg.ChunkSize)
	assert.Equal(t, 30, cfg.ChunkOverlap)
	assert.True(t, cfg.UseGraph)
	assert.True(t, cfg.UseCaptionForImages)
}

// TestConfigManager_缺省字段回落 对齐 pydantic 字段默认值：文件只给 kb_id 时，
// index_type→hybrid、chunk_size→512、chunk_overlap→50。
func TestConfigManager_缺省字段回落(t *testing.T) {
	path := writeTempFile(t, "cfg.json", `{"kb_id":"only_id"}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, common.IndexTypeHybrid, cfg.IndexType)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
	assert.False(t, cfg.UseGraph)
	assert.False(t, cfg.UseCaptionForImages)
}

// TestConfigManager_YAML整数解析 YAML 解析出的整数是 int 而非 float64，
// 与 JSON 路径不同，须同样落到 ChunkSize（Python yaml.safe_load 同为 int）。
func TestConfigManager_YAML整数解析(t *testing.T) {
	path := writeTempFile(t, "cfg.yaml", "kb_id: y\nchunk_size: 1024\nchunk_overlap: 128\n")
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, 1024, cfg.ChunkSize)
	assert.Equal(t, 128, cfg.ChunkOverlap)
}

// TestConfigManager_JSON保存格式 JSON 输出 indent 2 + 非 ASCII 不转义
// （对齐 json.dump(indent=2, ensure_ascii=False)）。
func TestConfigManager_JSON保存格式(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	src := writeTempFile(t, "src.json", `{"kb_id":"中文库","index_type":"vector"}`)
	require.NoError(t, m.LoadFromFile(src))
	out := filepath.Join(t.TempDir(), "out.json")
	require.NoError(t, m.SaveToFile(out))
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "中文库")
	assert.NotContains(t, string(raw), `\u`)
	assert.Contains(t, string(raw), "\n  \"")
}

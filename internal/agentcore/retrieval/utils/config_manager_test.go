package utils

import (
	"errors"
	"math"
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

// TestConfigManager_数值越界回落默认 chunk_size 超出 int64 可表示范围时
// asInt 拒绝转换，字段保留 pydantic 默认值（不静默回绕）。
func TestConfigManager_数值越界回落默认(t *testing.T) {
	path := writeTempFile(t, "cfg.json",
		`{"kb_id":"x","chunk_size":1e300,"chunk_overlap":-1e300}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
}

// TestConfigManager_数值边界 边界内的大整数正常接受。
func TestConfigManager_数值边界(t *testing.T) {
	path := writeTempFile(t, "cfg.json", `{"kb_id":"x","chunk_size":1048576,"chunk_overlap":0}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, 1048576, cfg.ChunkSize)
	// 0 是合法值（pydantic 不拒），不应回落默认 50
	assert.Equal(t, 0, cfg.ChunkOverlap)
}

// TestConfigManager_非数值类型回落默认 字符串/布尔等非数值保持 pydantic 默认。
func TestConfigManager_非数值类型回落默认(t *testing.T) {
	path := writeTempFile(t, "cfg.json",
		`{"kb_id":"x","chunk_size":"big","chunk_overlap":true}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
}

// TestLoadFromFile_字段校验 回归（T-05/D3）：对齐 pydantic 必填/字面量约束——
// 空 kb_id 与非法 index_type 都应报 155511（配置非法），而不是索引构建类错误码。
func TestLoadFromFile_字段校验(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"kb_id为空", `{"kb_id":"","index_type":"vector"}`},
		{"kb_id缺失", `{"index_type":"vector"}`},
		{"kb_id仅空白", `{"kb_id":"   ","index_type":"vector"}`},
		{"index_type非法", `{"kb_id":"kb1","index_type":"graph"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTempFile(t, "cfg.json", c.body)
			_, err := NewConfigManager(path)
			if err == nil {
				t.Fatal("非法配置应返回错误")
			}
			var be *exception.BaseError
			if !errors.As(err, &be) {
				t.Fatalf("错误类型不符: %v", err)
			}
			if be.Code() != 155511 {
				t.Errorf("错误码 = %d, 期望 155511", be.Code())
			}
		})
	}

	// 合法配置不受影响（index_type 缺省 → hybrid）
	path := writeTempFile(t, "cfg.json", `{"kb_id":"kb1"}`)
	m, err := NewConfigManager(path)
	if err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
	cfg, err := m.GetKnowledgeBaseConfig()
	if err != nil {
		t.Fatalf("取配置失败: %v", err)
	}
	if cfg.IndexType != common.IndexTypeHybrid {
		t.Errorf("index_type = %v, 期望 hybrid（pydantic 默认值）", cfg.IndexType)
	}
}

// ──── M-12：插入序与类型解析 ────

// TestConfigManager_GetConfig按插入序 同一类型存在两个键时，必须稳定返回先插入的
// "knowledge_base"（Python dict 插入序），而不是 Go map 的随机遍历结果。
func TestConfigManager_GetConfig按插入序(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb.json")
	loaded := []byte(`{"kb_id":"from-file","index_type":"vector","chunk_size":256}`)
	if err := os.WriteFile(path, loaded, 0o600); err != nil {
		t.Fatalf("写配置文件失败: %v", err)
	}
	m, err := NewConfigManager(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	// 更新写入类型名键（第二个键，值为不同内容）
	updated := common.KnowledgeBaseConfig{KBID: "from-update", IndexType: common.IndexTypeBM25, ChunkSize: 999}
	m.UpdateConfig(updated)

	for i := 0; i < 20; i++ {
		got, ok := GetConfig[common.KnowledgeBaseConfig](m)
		if !ok {
			t.Fatal("应能取到配置")
		}
		if got.KBID != "from-file" {
			t.Fatalf("第 %d 次取值 = %q，期望恒为插入序首个 \"from-file\"", i, got.KBID)
		}
	}
}

// TestAsInt_非整值与边界 非整值浮点不再截断，2^63 边界不再溢出成负数。
func TestAsInt_非整值与边界(t *testing.T) {
	if _, ok := asInt(512.5); ok {
		t.Error("512.5 不应被接受（Python 侧 pydantic 报错）")
	}
	if got, ok := asInt(512.0); !ok || got != 512 {
		t.Errorf("512.0 应被接受为 512，实际 (%d, %v)", got, ok)
	}
	if _, ok := asInt(math.Pow(2, 63)); ok {
		t.Error("2^63 应被拒绝（否则 int() 溢出成负数）")
	}
	if _, ok := asInt(math.Pow(2, 62)); !ok {
		t.Error("2^62 应被接受")
	}
}

package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeEntryChunker registry 测试用最小 Chunker
type fakeEntryChunker struct {
	*BaseChunker
}

// ChunkText 原样返回单块
func (f *fakeEntryChunker) ChunkText(text string) ([]string, error) {
	return []string{text}, nil
}

// newFakeEntryChunkerFake 工厂：从 ChunkerOptions 构造 fakeEntryChunker
func newFakeEntryChunkerFake(opts ...ChunkerOption) (Chunker, error) {
	o := NewChunkerOptions(opts...)
	bc, err := NewBaseChunker(o.ChunkSize, o.ChunkOverlap)
	if err != nil {
		return nil, err
	}
	c := &fakeEntryChunker{BaseChunker: bc}
	bc.SetSelf(c)
	return c, nil
}

// TestRegisterChunker_空名报错 对齐 Python ValueError("chunker name must be a non-empty string")
func TestRegisterChunker_空名报错(t *testing.T) {
	err := RegisterChunker("", newFakeEntryChunkerFake, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunker name must be a non-empty string")
}

// TestRegisterChunker_重名默认报错 对齐 Python ValueError("already registered")
func TestRegisterChunker_重名默认报错(t *testing.T) {
	name := "test_dup_" + t.Name()
	assert.NoError(t, RegisterChunker(name, newFakeEntryChunkerFake, false))
	t.Cleanup(func() { unregisterForTest(name) })

	err := RegisterChunker(name, newFakeEntryChunkerFake, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

// TestRegisterChunker_重名overwrite放行 对齐 Python overwrite=True
func TestRegisterChunker_重名overwrite放行(t *testing.T) {
	name := "test_ow_" + t.Name()
	assert.NoError(t, RegisterChunker(name, newFakeEntryChunkerFake, false))
	t.Cleanup(func() { unregisterForTest(name) })
	assert.NoError(t, RegisterChunker(name, newFakeEntryChunkerFake, true))
}

// TestGetChunker_未知名报错含注册列表 对齐 Python KeyError 含注册名列表
func TestGetChunker_未知名报错含注册列表(t *testing.T) {
	_, err := GetChunker("no_such_chunker")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown chunker: no_such_chunker")
	assert.Contains(t, err.Error(), "hybrid") // 内置 hybrid 已注册
	assert.Contains(t, err.Error(), "char")   // 13.6 后内置 char 也已注册
}

// TestGetChunker_工厂透传参数 kwargs 透传到工厂
func TestGetChunker_工厂透传参数(t *testing.T) {
	name := "test_params_" + t.Name()
	assert.NoError(t, RegisterChunker(name, newFakeEntryChunkerFake, false))
	t.Cleanup(func() { unregisterForTest(name) })

	c, err := GetChunker(name, WithChunkSize(256), WithChunkOverlap(20))
	assert.NoError(t, err)
	bc := c.(*fakeEntryChunker).BaseChunker
	assert.Equal(t, 256, bc.ChunkSize)
	assert.Equal(t, 20, bc.ChunkOverlap)
}

// TestHybridFactory_inner缺省时13_8前占位报错 ⤴️ 13.6 已回填默认 CharChunker 构造校验
func TestHybridFactory_inner缺省时13_8前占位报错(t *testing.T) {
	// 无 inner → 默认 CharChunker(512/50) 构造成功 → 落到 13.8 占位
	_, err := GetChunker("hybrid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}

// TestHybridFactory_inner注入时13_8前占位报错 ⤴️ 13.8 落地 HybridChunker
func TestHybridFactory_inner注入时13_8前占位报错(t *testing.T) {
	inner, err := newFakeEntryChunkerFake()
	assert.NoError(t, err)
	_, err = GetChunker("hybrid", WithInnerChunker(inner))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}

// TestHybridFactory_多余参数报错 对齐 Python TypeError("Unknown kwargs for 'hybrid' chunker")
func TestHybridFactory_多余参数报错(t *testing.T) {
	inner, err := newFakeEntryChunkerFake()
	assert.NoError(t, err)
	_, err = GetChunker("hybrid", WithInnerChunker(inner), WithExtra("foo", "bar"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown kwargs for 'hybrid' chunker")
}

// TestHybridFactory_inner显式nil走缺省分支 WithInnerChunker(nil) 等价未设置，
// 走默认 CharChunker（inner 类型由 Chunker 接口静态保证，无需运行时断言）
func TestHybridFactory_inner显式nil走缺省分支(t *testing.T) {
	_, err := GetChunker("hybrid", WithInnerChunker(nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented until 13.8")
}

// TestNewChunkerOptions_默认值512和50 对齐 Python 默认 chunk_size/chunk_overlap
func TestNewChunkerOptions_默认值512和50(t *testing.T) {
	o := NewChunkerOptions()
	assert.Equal(t, 512, o.ChunkSize)
	assert.Equal(t, 50, o.ChunkOverlap)
	assert.Nil(t, o.InnerChunker)
	assert.Nil(t, o.NoSplitWhen)
	assert.Nil(t, o.Extra)
}

// TestWithExtra_累积设置 多次调用累积而非覆盖
func TestWithExtra_累积设置(t *testing.T) {
	o := NewChunkerOptions(WithExtra("a", 1), WithExtra("b", 2))
	assert.Equal(t, map[string]any{"a": 1, "b": 2}, o.Extra)
}

// TestGetChunker_tokenizer注册 主动增强：Python 不注册 TokenizerChunker（SDK 直接
// 实例化），Go 侧注册使 GetChunker("tokenizer") 可用（设计文档偏差 5）
func TestGetChunker_tokenizer注册(t *testing.T) {
	c, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("tokenizer", &tokByRune{}),
		WithExtra("language", "auto"),
	)
	assert.NoError(t, err)
	got, err := c.ChunkText("一二三。四五。六七。八九。")
	assert.NoError(t, err)
	assert.NotEmpty(t, got)
}

// TestGetChunker_tokenizer_extra类型错误 tokenizer Extra 必须实现 Tokenizer 接口
func TestGetChunker_tokenizer_extra类型错误(t *testing.T) {
	_, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("tokenizer", "not-a-tokenizer"),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must implement Tokenizer")
}

// TestGetChunker_tokenizer_extra缺省走nil 不传 tokenizer → nil → tiktoken fallback
func TestGetChunker_tokenizer_extra缺省走nil(t *testing.T) {
	c, err := GetChunker("tokenizer", WithChunkSize(512), WithChunkOverlap(50))
	assert.NoError(t, err)
	_, err = c.ChunkText("这是测试文本。")
	assert.NoError(t, err, "tiktoken fallback 分块应成功")
}

// TestGetChunker_tokenizer_language类型错误 language Extra 必须是 string
func TestGetChunker_tokenizer_language类型错误(t *testing.T) {
	_, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("language", 123),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be string")
}

// TestGetChunker_tokenizer_splitterConfig类型错误 splitter_config Extra 必须是 map
func TestGetChunker_tokenizer_splitterConfig类型错误(t *testing.T) {
	_, err := GetChunker("tokenizer",
		WithChunkSize(20), WithChunkOverlap(4),
		WithExtra("splitter_config", "not-a-map"),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be map[string]any")
}

// unregisterForTest 测试后清理注册项（避免污染全局 registry）
func unregisterForTest(name string) {
	registryMu.Lock()
	delete(chunkerRegistry, name)
	registryMu.Unlock()
}

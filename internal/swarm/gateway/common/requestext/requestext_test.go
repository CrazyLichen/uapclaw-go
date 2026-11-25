package requestext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildExtFromSource 白名单/大小写不敏感/列表首元素/内部头剔除。
// 对齐 Python request_ext.py:90-115。
func TestBuildExtFromSource(t *testing.T) {
	t.Setenv(EnvForwardHeaders, "X-Custom-A, x-custom-b")
	SetForwardHeaders(nil)
	defer SetForwardHeaders(nil)

	got := BuildExtFromSource(map[string]any{
		"x-custom-a": "v1",
		"X-Custom-B": []string{"b1", "b2"}, // 列表取首元素
		"X-Other":    "skip",
	})
	require.Equal(t, map[string]any{"X-Custom-A": "v1", "x-custom-b": "b1"}, got)

	// 内部载体头不得再次封装（:100-102）
	got2 := BuildExtFromSource(map[string]any{InternalHeaderName: "evil"})
	require.Nil(t, got2)

	// 空值剔除
	got3 := BuildExtFromSource(map[string]any{"X-Custom-A": ""})
	require.Nil(t, got3)
}

// TestSetForwardHeaders覆盖与撤销 运行时覆盖优先于 env。
func TestSetForwardHeaders覆盖与撤销(t *testing.T) {
	t.Setenv(EnvForwardHeaders, "X-Env-Only")
	SetForwardHeaders([]string{"X-Override"})
	got := BuildExtFromSource(map[string]any{"X-Override": "1", "X-Env-Only": "2"})
	require.Equal(t, map[string]any{"X-Override": "1"}, got)
	SetForwardHeaders(nil) // 撤销回落 env
	got2 := BuildExtFromSource(map[string]any{"X-Env-Only": "2"})
	require.Equal(t, map[string]any{"X-Env-Only": "2"}, got2)
	SetForwardHeaders([]string{}) // 空列表=关闭
	require.Nil(t, BuildExtFromSource(map[string]any{"X-Env-Only": "2"}))
	SetForwardHeaders(nil)
}

// TestRegisterForwardHeader加法注册 无覆盖时以 env 为底。
func TestRegisterForwardHeader加法注册(t *testing.T) {
	t.Setenv(EnvForwardHeaders, "X-Base")
	SetForwardHeaders(nil)
	defer SetForwardHeaders(nil)
	RegisterForwardHeader("X-Extra")
	RegisterForwardHeader("") // 空名忽略
	RegisterForwardHeader("X-Extra")
	got := BuildExtFromSource(map[string]any{"X-Base": "1", "X-Extra": "2"})
	require.Equal(t, map[string]any{"X-Base": "1", "X-Extra": "2"}, got)
}

// TestEncodeDecodeInternalHeader 往返 + 大小上限 + 非法字符。
// 对齐 Python request_ext.py:118-183。
func TestEncodeDecodeInternalHeader(t *testing.T) {
	enc, err := EncodeInternalHeader(map[string]any{"b": 2, "a": "中文"})
	require.NoError(t, err)
	dec, derr := DecodeInternalHeader(enc)
	require.NoError(t, derr)
	require.Equal(t, map[string]any{"b": float64(2), "a": "中文"}, dec)

	// 空 ext → nil
	empty, eerr := EncodeInternalHeader(nil)
	require.NoError(t, eerr)
	require.Empty(t, empty)
	dec0, derr0 := DecodeInternalHeader("")
	require.NoError(t, derr0)
	require.Nil(t, dec0)

	// 非 Base64URL 字符
	_, berr := DecodeInternalHeader("a+b/c=")
	require.Error(t, berr)

	// 非 JSON 值
	_, jerr := EncodeInternalHeader(map[string]any{"f": func() {}})
	require.Error(t, jerr)
}

// TestAttachToMetadata 无 ext 返回原 metadata、有 ext 写入键。
func TestAttachToMetadata(t *testing.T) {
	meta := map[string]any{"method": "m"}
	require.Equal(t, meta, AttachToMetadata(meta, nil)) // 无 ext 返回原 metadata（同 map）
	out := AttachToMetadata(meta, map[string]any{"X": 1})
	require.Equal(t, map[string]any{"X": 1}, out[MetadataKey])
	require.Equal(t, "m", out["method"])
}

// TestContext载体 WithRequestExt / RequestExtFromContext（D-B2-10）。
func TestContext载体(t *testing.T) {
	ctx := context.Background()
	require.Empty(t, RequestExtFromContext(ctx))
	ctx2 := WithRequestExt(ctx, map[string]any{MetadataKey: map[string]any{"X": "1"}})
	require.Equal(t, map[string]any{"X": "1"}, RequestExtFromContext(ctx2))
	// 无 ext metadata 不抬升
	ctx3 := WithRequestExt(ctx, map[string]any{})
	require.Empty(t, RequestExtFromContext(ctx3))
}

// TestLegacyEnv名 旧 env 名等价。
func TestLegacyEnv名(t *testing.T) {
	t.Setenv(EnvForwardHeadersLegacy, "X-Legacy")
	SetForwardHeaders(nil)
	defer SetForwardHeaders(nil)
	got := BuildExtFromSource(map[string]any{"X-Legacy": "v"})
	require.Equal(t, map[string]any{"X-Legacy": "v"}, got)
}

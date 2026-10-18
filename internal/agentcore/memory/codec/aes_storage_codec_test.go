package codec

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsPassthrough_key为空 返回 true
func TestIsPassthrough_key为空(t *testing.T) {
	codec, err := NewAesStorageCodec(nil)
	if err != nil {
		t.Fatalf("创建 codec 失败: %v", err)
	}
	if !codec.IsPassthrough() {
		t.Error("key 为 nil 时 IsPassthrough 应返回 true")
	}
}

// TestIsPassthrough_key为空切片 返回 true
func TestIsPassthrough_key为空切片(t *testing.T) {
	codec, err := NewAesStorageCodec([]byte{})
	if err != nil {
		t.Fatalf("创建 codec 失败: %v", err)
	}
	if !codec.IsPassthrough() {
		t.Error("key 为空切片时 IsPassthrough 应返回 true")
	}
}

// TestIsPassthrough_key非空 返回 false
func TestIsPassthrough_key非空(t *testing.T) {
	key := make([]byte, 32) // 32 字节有效 AES-256 密钥
	codec, err := NewAesStorageCodec(key)
	if err != nil {
		t.Fatalf("创建 codec 失败: %v", err)
	}
	if codec.IsPassthrough() {
		t.Error("key 非空时 IsPassthrough 应返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

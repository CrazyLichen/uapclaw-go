package codec

import (
	"testing"
)

// TestNewAesStorageCodec_空key 验证空 key 创建 passthrough 模式
func TestNewAesStorageCodec_空key(t *testing.T) {
	c, err := NewAesStorageCodec(nil)
	if err != nil {
		t.Fatalf("空 key 不应报错: %v", err)
	}
	if c.provider != nil {
		t.Error("空 key 时 provider 应为 nil")
	}
}

// TestNewAesStorageCodec_有效key 验证 32 字节 key 正常创建
func TestNewAesStorageCodec_有效key(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewAesStorageCodec(key)
	if err != nil {
		t.Fatalf("32 字节 key 不应报错: %v", err)
	}
	if c.provider == nil {
		t.Error("有效 key 时 provider 不应为 nil")
	}
}

// TestNewAesStorageCodec_key长度错误 验证非 32 字节 key 返回 error
func TestNewAesStorageCodec_key长度错误(t *testing.T) {
	key := []byte{1, 2, 3}
	_, err := NewAesStorageCodec(key)
	if err == nil {
		t.Error("非 32 字节 key 应返回 error")
	}
}

// TestAesStorageCodec_Encode_空key_passthrough 验证空 key 时不加密
func TestAesStorageCodec_Encode_空key_passthrough(t *testing.T) {
	c, _ := NewAesStorageCodec(nil)
	result, err := c.Encode("hello")
	if err != nil {
		t.Fatalf("passthrough 模式不应报错: %v", err)
	}
	if result != "hello" {
		t.Errorf("passthrough 应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Encode_空文本 验证空字符串原样返回
func TestAesStorageCodec_Encode_空文本(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewAesStorageCodec(key)
	result, err := c.Encode("")
	if err != nil {
		t.Fatalf("空文本不应报错: %v", err)
	}
	if result != "" {
		t.Errorf("空文本应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Encode_加密成功 验证正常加密返回密文
func TestAesStorageCodec_Encode_加密成功(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)
	result, err := c.Encode("hello world")
	if err != nil {
		t.Fatalf("加密不应报错: %v", err)
	}
	if result == "hello world" {
		t.Error("加密结果不应与原文相同")
	}
}

// TestAesStorageCodec_加密往返 验证 Encode → Decode 还原原文
func TestAesStorageCodec_加密往返(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	plaintext := "secret message 你好世界"
	encrypted, err := c.Encode(plaintext)
	if err != nil {
		t.Fatalf("加密不应报错: %v", err)
	}

	decrypted, err := c.Decode(encrypted)
	if err != nil {
		t.Fatalf("解密不应报错: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("解密结果 = %q, want %q", decrypted, plaintext)
	}
}

// TestAesStorageCodec_Decode_空key_passthrough 验证空 key 时不解密
func TestAesStorageCodec_Decode_空key_passthrough(t *testing.T) {
	c, _ := NewAesStorageCodec(nil)
	result, err := c.Decode("ciphertext")
	if err != nil {
		t.Fatalf("passthrough 模式不应报错: %v", err)
	}
	if result != "ciphertext" {
		t.Errorf("passthrough 应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Decode_解密失败 验证篡改密文时返回原文（容错模式，对齐 Python 行为）
func TestAesStorageCodec_Decode_解密失败(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	result, err := c.Decode("invalid_ciphertext_data")
	if err != nil {
		t.Errorf("容错模式下解密失败不应返回 error: %v", err)
	}
	if result != "invalid_ciphertext_data" {
		t.Errorf("容错模式下解密失败应返回原文, got %q", result)
	}
}

// TestAesStorageCodec_Encode_多模态内容 验证 JSON array 格式内容加解密
func TestAesStorageCodec_Encode_多模态内容(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	multimodal := `[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]`
	encrypted, err := c.Encode(multimodal)
	if err != nil {
		t.Fatalf("多模态内容加密不应报错: %v", err)
	}

	decrypted, err := c.Decode(encrypted)
	if err != nil {
		t.Fatalf("多模态内容解密不应报错: %v", err)
	}

	if decrypted != multimodal {
		t.Errorf("解密结果不匹配, got %q", decrypted)
	}
}

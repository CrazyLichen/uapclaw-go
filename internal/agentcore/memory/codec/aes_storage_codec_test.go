package codec

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
)

// TestNewAesStorageCodec_空key 验证空 key 创建 passthrough 模式
func TestNewAesStorageCodec_空key(t *testing.T) {
	c, err := NewAesStorageCodec(nil)
	if err != nil {
		t.Fatalf("空 key 不应报错: %v", err)
	}
	if len(c.key) != 0 {
		t.Error("空 key 时内部 key 应为空")
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
	if len(c.key) != 32 {
		t.Error("有效 key 时内部 key 应为 32 字节")
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
	result := c.Encode("hello")
	if result != "hello" {
		t.Errorf("passthrough 应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Encode_空文本 验证空字符串原样返回
func TestAesStorageCodec_Encode_空文本(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewAesStorageCodec(key)
	result := c.Encode("")
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
	result := c.Encode("hello world")
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
	encrypted := c.Encode(plaintext)
	decrypted := c.Decode(encrypted)

	if decrypted != plaintext {
		t.Errorf("解密结果 = %q, want %q", decrypted, plaintext)
	}
}

// TestAesStorageCodec_Decode_空key_passthrough 验证空 key 时不解密
func TestAesStorageCodec_Decode_空key_passthrough(t *testing.T) {
	c, _ := NewAesStorageCodec(nil)
	result := c.Decode("ciphertext")
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

	result := c.Decode("invalid_ciphertext_data")
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
	encrypted := c.Encode(multimodal)
	decrypted := c.Decode(encrypted)

	if decrypted != multimodal {
		t.Errorf("解密结果不匹配, got %q", decrypted)
	}
}

// TestAesStorageCodec_满足StorageCodec接口 验证 AesStorageCodec 满足 StorageCodec 接口
func TestAesStorageCodec_满足StorageCodec接口(t *testing.T) {
	// 验证 AesStorageCodec 满足 StorageCodec 接口
	var _ index.StorageCodec = (*AesStorageCodec)(nil)
}

// TestAesStorageCodec_Encode_每次产生不同输出 验证 GCM 随机 nonce 导致相同明文加密两次产生不同密文
// 对齐 Python: test_encode_produces_different_output
func TestAesStorageCodec_Encode_每次产生不同输出(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	plaintext := "same input text"
	encoded1 := c.Encode(plaintext)
	encoded2 := c.Encode(plaintext)

	if encoded1 == encoded2 {
		t.Error("相同明文加密两次应产生不同密文（GCM 随机 nonce）")
	}

	// 但两次密文都应能正确解密
	if c.Decode(encoded1) != plaintext {
		t.Error("第一次密文解密失败")
	}
	if c.Decode(encoded2) != plaintext {
		t.Error("第二次密文解密失败")
	}
}

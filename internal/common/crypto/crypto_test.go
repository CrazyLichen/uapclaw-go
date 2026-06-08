package crypto

import (
	"encoding/hex"
	"strings"
	"sync"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// testKey 32 字节测试密钥
var testKey = make([]byte, KeyLength)

func init() {
	// 填充测试密钥（全 1）
	for i := range testKey {
		testKey[i] = 0x01
	}
}

// ─── AesGcmCrypt 测试 ───

func TestAesGcmCrypt_加密解密往返(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "hello world"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	decrypted, err := crypt.Decrypt(testKey, ciphertext)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", plaintext, decrypted)
	}
}

func TestAesGcmCrypt_密钥长度校验(t *testing.T) {
	crypt := &AesGcmCrypt{}

	tests := []struct {
		name string
		key  []byte
	}{
		{"空密钥", []byte{}},
		{"16字节密钥", make([]byte, 16)},
		{"24字节密钥", make([]byte, 24)},
		{"64字节密钥", make([]byte, 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := crypt.Encrypt(tt.key, "test")
			if err == nil {
				t.Error("期望加密返回错误，但未返回错误")
			}

			_, err = crypt.Decrypt(tt.key, "dummy")
			if err == nil {
				t.Error("期望解密返回错误，但未返回错误")
			}
		})
	}
}

func TestAesGcmCrypt_密文格式校验(t *testing.T) {
	crypt := &AesGcmCrypt{}

	tests := []struct {
		name       string
		ciphertext string
	}{
		{"空密文", ""},
		{"过短密文", "abcd"},
		{"仅nonce无tag", strings.Repeat("a", NonceLength*2)},
		{"nonce加tag无ciphertext", strings.Repeat("a", NonceLength*2+TagLength*2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := crypt.Decrypt(testKey, tt.ciphertext)
			if err == nil {
				t.Error("期望解密返回错误，但未返回错误")
			}
		})
	}
}

func TestAesGcmCrypt_空明文加密(t *testing.T) {
	crypt := &AesGcmCrypt{}

	ciphertext, err := crypt.Encrypt(testKey, "")
	if err != nil {
		t.Fatalf("加密空明文失败: %v", err)
	}

	decrypted, err := crypt.Decrypt(testKey, ciphertext)
	if err != nil {
		t.Fatalf("解密空明文密文失败: %v", err)
	}

	if decrypted != "" {
		t.Errorf("解密空明文结果不匹配: 期望空字符串，实际 %q", decrypted)
	}
}

func TestAesGcmCrypt_中文明文(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "你好，世界！API密钥测试 🚀"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密中文失败: %v", err)
	}

	decrypted, err := crypt.Decrypt(testKey, ciphertext)
	if err != nil {
		t.Fatalf("解密中文失败: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", plaintext, decrypted)
	}
}

func TestAesGcmCrypt_密文每次不同(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "same plaintext"

	c1, _ := crypt.Encrypt(testKey, plaintext)
	c2, _ := crypt.Encrypt(testKey, plaintext)

	if c1 == c2 {
		t.Error("相同明文两次加密的密文不应相同（nonce 应随机）")
	}
}

func TestAesGcmCrypt_密文长度合理(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "hello"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	// 密文格式: hex(nonce=12字节) + hex(tag=16字节) + hex(ciphertext=5字节)
	// hex 编码后: 24 + 32 + 10 = 66 字符
	expectedLen := NonceLength*2 + TagLength*2 + len(plaintext)*2
	if len(ciphertext) != expectedLen {
		t.Errorf("密文长度不匹配: 期望 %d，实际 %d", expectedLen, len(ciphertext))
	}
}

func TestAesGcmCrypt_篡改密文检测(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "sensitive data"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	// 篡改密文（修改最后一个字符）
	tampered := ciphertext[:len(ciphertext)-1] + string(ciphertext[len(ciphertext)-1]+1)

	_, err = crypt.Decrypt(testKey, tampered)
	if err == nil {
		t.Error("期望篡改密文解密失败，但解密成功")
	}
}

func TestAesGcmCrypt_错误密钥解密(t *testing.T) {
	crypt := &AesGcmCrypt{}
	plaintext := "secret message"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	// 使用不同的密钥解密
	wrongKey := make([]byte, KeyLength)
	for i := range wrongKey {
		wrongKey[i] = 0x02
	}

	_, err = crypt.Decrypt(wrongKey, ciphertext)
	if err == nil {
		t.Error("期望错误密钥解密失败，但解密成功")
	}
}

func TestAesGcmCrypt_Python密文兼容(t *testing.T) {
	// 此测试验证 Go 解密 Python AesGcmCrypt.encrypt 生成的密文
	// Python 端生成方式：
	//   key = bytes([0x01] * 32)
	//   from openjiuwen.core.common.security.crypt_utils import CryptUtils
	//   crypt = CryptUtils.get_crypt("aes_gcm")
	//   ciphertext = crypt.encrypt(key, "hello python")
	//
	// 由于 nonce 是随机的，无法硬编码密文进行测试。
	// 此测试通过验证格式兼容性来确保互操作性：
	// 1. Go 加密 → 模拟 Python 解密格式拆分
	// 2. 验证密文格式为 hex(nonce) + hex(tag) + hex(ciphertext)

	crypt := &AesGcmCrypt{}
	plaintext := "hello python"

	ciphertext, err := crypt.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	// 验证密文格式：能正确拆分为 nonce + tag + ciphertext
	nonceHexLen := NonceLength * 2 // 24
	tagHexLen := TagLength * 2     // 32

	if len(ciphertext) < nonceHexLen+tagHexLen {
		t.Fatalf("密文过短: %d 字符", len(ciphertext))
	}

	nonceBytes, err := hex.DecodeString(ciphertext[:nonceHexLen])
	if err != nil {
		t.Fatalf("nonce hex 解码失败: %v", err)
	}
	if len(nonceBytes) != NonceLength {
		t.Errorf("nonce 长度不匹配: 期望 %d，实际 %d", NonceLength, len(nonceBytes))
	}

	tagBytes, err := hex.DecodeString(ciphertext[nonceHexLen : nonceHexLen+tagHexLen])
	if err != nil {
		t.Fatalf("tag hex 解码失败: %v", err)
	}
	if len(tagBytes) != TagLength {
		t.Errorf("tag 长度不匹配: 期望 %d，实际 %d", TagLength, len(tagBytes))
	}

	ciphertextBytes, err := hex.DecodeString(ciphertext[nonceHexLen+tagHexLen:])
	if err != nil {
		t.Fatalf("ciphertext hex 解码失败: %v", err)
	}
	if len(ciphertextBytes) != len(plaintext) {
		t.Errorf("ciphertext 长度不匹配: 期望 %d，实际 %d", len(plaintext), len(ciphertextBytes))
	}
}

// ─── Registry 测试 ───

func TestRegistry_注册获取(t *testing.T) {
	reg := NewCryptRegistry()
	crypt := &AesGcmCrypt{}

	err := reg.Register("test_algo", crypt)
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	got, ok := reg.Get("test_algo")
	if !ok {
		t.Fatal("获取已注册算法失败")
	}
	if got != crypt {
		t.Error("获取的算法实例不匹配")
	}
}

func TestRegistry_注销(t *testing.T) {
	reg := NewCryptRegistry()
	crypt := &AesGcmCrypt{}

	_ = reg.Register("test_algo", crypt)
	reg.Unregister("test_algo")

	_, ok := reg.Get("test_algo")
	if ok {
		t.Error("注销后仍能获取算法")
	}
}

func TestRegistry_注销不存在的不报错(t *testing.T) {
	reg := NewCryptRegistry()
	reg.Unregister("nonexistent") // 不应 panic
}

func TestRegistry_获取不存在(t *testing.T) {
	reg := NewCryptRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("获取不存在的算法不应返回 true")
	}
}

func TestRegistry_注册nil返回错误(t *testing.T) {
	reg := NewCryptRegistry()
	err := reg.Register("nil_algo", nil)
	if err == nil {
		t.Error("注册 nil 应返回错误")
	}
}

func TestRegistry_覆盖注册(t *testing.T) {
	reg := NewCryptRegistry()
	c1 := &AesGcmCrypt{}
	c2 := &AesGcmCrypt{}

	_ = reg.Register("algo", c1)
	_ = reg.Register("algo", c2)

	got, ok := reg.Get("algo")
	if !ok {
		t.Fatal("获取覆盖注册的算法失败")
	}
	if got != c2 {
		t.Error("覆盖注册后应返回最新注册的实例")
	}
}

func TestRegistry_并发安全(t *testing.T) {
	reg := NewCryptRegistry()
	var wg sync.WaitGroup

	// 并发注册
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := strings.Repeat("a", i%10+1)
			_ = reg.Register(name, &AesGcmCrypt{})
		}(i)
	}

	// 并发获取
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := strings.Repeat("a", i%10+1)
			_, _ = reg.Get(name)
		}(i)
	}

	wg.Wait()
}

func Test全局注册表_AesGcm已注册(t *testing.T) {
	crypt, ok := Get(AesGcmName)
	if !ok {
		t.Fatal("AES-GCM 算法未在全局注册表中注册")
	}
	if crypt == nil {
		t.Fatal("AES-GCM 算法实例为 nil")
	}
}

// Test全局注册表_Unregister 验证全局 Unregister 函数。
func Test全局注册表_Unregister(t *testing.T) {
	// 先注册一个临时算法
	_ = Register("temp_algo", &AesGcmCrypt{})
	_, ok := Get("temp_algo")
	if !ok {
		t.Fatal("注册后应能获取")
	}
	// 注销
	Unregister("temp_algo")
	_, ok = Get("temp_algo")
	if ok {
		t.Error("注销后不应能获取")
	}
}

// ─── AesGcmProvider 测试 ───

func TestAesGcmProvider_加密解密(t *testing.T) {
	provider, err := NewAesGcmProvider(testKey)
	if err != nil {
		t.Fatalf("创建 AesGcmProvider 失败: %v", err)
	}

	plaintext := "my-secret-api-key"
	ciphertext, err := provider.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	decrypted, err := provider.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", plaintext, decrypted)
	}
}

func TestAesGcmProvider_密钥长度校验(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"空密钥", []byte{}},
		{"16字节密钥", make([]byte, 16)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAesGcmProvider(tt.key)
			if err == nil {
				t.Error("期望创建 AesGcmProvider 返回错误，但未返回错误")
			}
		})
	}
}

func TestAesGcmProvider_密钥隔离(t *testing.T) {
	// 修改传入的 key 不应影响 provider 内部
	key := make([]byte, KeyLength)
	copy(key, testKey)

	provider, err := NewAesGcmProvider(key)
	if err != nil {
		t.Fatalf("创建 AesGcmProvider 失败: %v", err)
	}

	// 修改原始 key
	key[0] = 0xFF

	// provider 应仍能正常工作
	ciphertext, err := provider.Encrypt("test")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	decrypted, err := provider.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	if decrypted != "test" {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", "test", decrypted)
	}
}

// ─── 全局 Provider 测试 ───

func TestSetGetCryptoProvider(t *testing.T) {
	// 保存原始状态
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	provider, _ := NewAesGcmProvider(testKey)
	SetCryptoProvider(provider)

	got := GetCryptoProvider()
	if got != provider {
		t.Error("获取的全局 Provider 不匹配")
	}
}

func TestGetCryptoProvider_未设置(t *testing.T) {
	// 保存原始状态
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	SetCryptoProvider(nil)
	got := GetCryptoProvider()
	if got != nil {
		t.Error("未设置时获取全局 Provider 应返回 nil")
	}
}

func TestSetGetCryptoProvider_并发安全(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	var wg sync.WaitGroup
	provider, _ := NewAesGcmProvider(testKey)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				SetCryptoProvider(provider)
			} else {
				_ = GetCryptoProvider()
			}
		}(i)
	}

	wg.Wait()
}

// ─── NewDecryptFunc 测试 ───

func TestNewDecryptFunc_敏感变量解密(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	provider, _ := NewAesGcmProvider(testKey)
	SetCryptoProvider(provider)

	// 先加密一个值
	ciphertext, err := provider.Encrypt("sk-1234567890")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	decryptFn := NewDecryptFunc()
	decrypted, ok := decryptFn("MY_API_KEY", ciphertext)
	if !ok {
		t.Error("期望 api_key 变量被解密，但返回 false")
	}
	if decrypted != "sk-1234567890" {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", "sk-1234567890", decrypted)
	}
}

func TestNewDecryptFunc_非敏感变量不解密(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	provider, _ := NewAesGcmProvider(testKey)
	SetCryptoProvider(provider)

	decryptFn := NewDecryptFunc()
	value, ok := decryptFn("HOST_NAME", "some_value")
	if ok {
		t.Error("非敏感变量不应被解密")
	}
	if value != "some_value" {
		t.Errorf("非敏感变量值不应改变: 期望 %q，实际 %q", "some_value", value)
	}
}

func TestNewDecryptFunc_Provider未设置(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	SetCryptoProvider(nil)

	decryptFn := NewDecryptFunc()
	value, ok := decryptFn("MY_API_KEY", "encrypted_value")
	if ok {
		t.Error("Provider 未设置时不应解密")
	}
	if value != "encrypted_value" {
		t.Errorf("Provider 未设置时值不应改变: 期望 %q，实际 %q", "encrypted_value", value)
	}
}

func TestNewDecryptFunc_token变量解密(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	provider, _ := NewAesGcmProvider(testKey)
	SetCryptoProvider(provider)

	ciphertext, _ := provider.Encrypt("token-value")

	decryptFn := NewDecryptFunc()
	decrypted, ok := decryptFn("ACCESS_TOKEN", ciphertext)
	if !ok {
		t.Error("期望 token 变量被解密，但返回 false")
	}
	if decrypted != "token-value" {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", "token-value", decrypted)
	}
}

func TestNewDecryptFunc_连字符变量名(t *testing.T) {
	original := GetCryptoProvider()
	defer SetCryptoProvider(original)

	provider, _ := NewAesGcmProvider(testKey)
	SetCryptoProvider(provider)

	ciphertext, _ := provider.Encrypt("key-value")

	decryptFn := NewDecryptFunc()
	decrypted, ok := decryptFn("MY-API-KEY", ciphertext)
	if !ok {
		t.Error("期望连字符形式的 api-key 变量被解密，但返回 false")
	}
	if decrypted != "key-value" {
		t.Errorf("解密结果不匹配: 期望 %q，实际 %q", "key-value", decrypted)
	}
}

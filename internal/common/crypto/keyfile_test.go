package crypto

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateKeyFile_首次生成 不存在时生成 32 字节密钥并写文件（64 hex + 换行，0600）
func TestLoadOrCreateKeyFile_首次生成(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")

	key, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	if len(key) != KeyLength {
		t.Fatalf("密钥长度 = %d, 期望 %d", len(key), KeyLength)
	}

	// 文件存在且为 64 字符 hex + 换行
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取密钥文件失败: %v", err)
	}
	content := string(data)
	if len(content) != 65 || content[64] != '\n' {
		t.Fatalf("文件内容格式不符（期望 64 hex + \\n）: %q", content)
	}

	// 权限 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %o, 期望 600", info.Mode().Perm())
	}

	// 文件内容 hex 与返回 key 一致
	if content[:64] != hex.EncodeToString(key) {
		t.Error("文件 hex 内容与返回密钥不一致")
	}
}

// TestLoadOrCreateKeyFile_二次读取 已存在时读取并返回相同密钥
func TestLoadOrCreateKeyFile_二次读取(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")

	key1, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	key2, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("二次读取应返回相同密钥")
	}
}

// TestLoadOrCreateKeyFile_损坏hex 报错
func TestLoadOrCreateKeyFile_损坏hex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("zzzz\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("损坏 hex 应返回错误")
	}
}

// TestLoadOrCreateKeyFile_长度不符 报错（2 字节 hex）
func TestLoadOrCreateKeyFile_长度不符(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("aabb\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("密钥长度不符应返回错误")
	}
}

// TestLoadOrCreateKeyFile_空文件 报错
func TestLoadOrCreateKeyFile_空文件(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crypto.key")
	_ = os.WriteFile(path, []byte("\n"), 0o600)
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("空文件应返回错误")
	}
}

// TestLoadOrCreateKeyFile_写入失败 父目录不存在时生成报错
func TestLoadOrCreateKeyFile_写入失败(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noexist", "crypto.key")
	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("父目录不存在应返回写入错误")
	}
}

// TestLoadOrCreateKeyFile_路径为目录 报错
func TestLoadOrCreateKeyFile_路径为目录(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateKeyFile(dir); err == nil {
		t.Error("目录路径应返回错误")
	}
}

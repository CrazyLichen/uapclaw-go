package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keyFilePerm 密钥文件权限（仅属主可读写）
	keyFilePerm = 0o600
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadOrCreateKeyFile 读取密钥文件，不存在时生成 32 字节密钥并写入。
//
// 文件格式：64 字符 hex 文本（32 字节 AES-256 密钥）+ 换行，权限 0600。
// 首次生成使用 crypto/rand；删除文件即重新生成（旧密文自然失效，
// 解密失败由消费方逐字段跳过，对齐 Python except: pass）。
//
// 接收路径参数保持本包叶子地位（不 import workspace/config；
// 本包亦禁止导入 common/logger——logger→config→crypto 会成环），
// 路径由调用方（cmd 层）组装，日志用 stdlib log 以 [crypto] 前缀区分。
func LoadOrCreateKeyFile(path string) ([]byte, error) {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("密钥路径 %s 是目录", path)
		}
		// 文件存在：读取并解析
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取密钥文件 %s 失败: %w", path, err)
		}
		key, err := parseKeyFileContent(string(data))
		if err != nil {
			return nil, fmt.Errorf("密钥文件 %s 损坏: %w", path, err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("访问密钥文件 %s 失败: %w", path, err)
	}

	// 首次生成：crypto/rand 32 字节 → hex 写文件
	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), keyFilePerm); err != nil {
		return nil, fmt.Errorf("写入密钥文件 %s 失败: %w", path, err)
	}
	log.Printf("[crypto] 首次生成密钥文件 %s（换机/迁移请备份；删除文件即重新生成，旧密文将失效）", path)
	return key, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseKeyFileContent 解析密钥文件内容为 32 字节密钥
func parseKeyFileContent(content string) ([]byte, error) {
	trimmed := strings.TrimSpace(content)
	key, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("密钥长度 %d 字节，期望 %d", len(key), KeyLength)
	}
	return key, nil
}

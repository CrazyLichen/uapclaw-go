package config

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryEngineConfig 记忆引擎配置。
//
// 包含默认模型配置、禁用变量、加密密钥等引擎级参数。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryEngineConfig)
type MemoryEngineConfig struct {
	// DefaultModelCfg 默认模型请求配置
	DefaultModelCfg *llmschema.ModelRequestConfig
	// DefaultModelClientCfg 默认模型客户端配置
	DefaultModelClientCfg *llmschema.ModelClientConfig
	// ForbiddenVariables 禁用变量名列表（逗号分隔）
	ForbiddenVariables string
	// InputMsgMaxLen 输入消息最大长度
	InputMsgMaxLen int
	// CryptoKey AES 加密密钥（空=不加密，非空则长度必须 == AESKeyLength）
	CryptoKey []byte
	// SingleTurnHistorySummaryMaxToken 单轮历史摘要最大 token 数
	SingleTurnHistorySummaryMaxToken int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// AESKeyLength AES 密钥长度（32 字节 = AES-256）
const AESKeyLength = 32

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 确保 MemoryEngineConfig 引用的类型可用（编译时检查）
	_ *llmschema.ModelRequestConfig = nil
	_ *llmschema.ModelClientConfig  = nil
	_ *embedding.EmbeddingConfig    = nil
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMemoryEngineConfig 返回默认记忆引擎配置。
//
// 对齐 Python MemoryEngineConfig 的字段默认值。
func DefaultMemoryEngineConfig() *MemoryEngineConfig {
	return &MemoryEngineConfig{
		ForbiddenVariables:               "",
		InputMsgMaxLen:                   8192,
		CryptoKey:                        []byte{},
		SingleTurnHistorySummaryMaxToken: 128,
	}
}

// Validate 校验记忆引擎配置。
//
// 规则：
//   - CryptoKey：空 or 长度 == AESKeyLength(32)，否则返回错误
//   - SingleTurnHistorySummaryMaxToken：必须 > 0
func (c *MemoryEngineConfig) Validate() error {
	if len(c.CryptoKey) > 0 && len(c.CryptoKey) != AESKeyLength {
		return fmt.Errorf("crypto_key 长度必须为 %d 或为空，当前长度: %d", AESKeyLength, len(c.CryptoKey))
	}
	if c.SingleTurnHistorySummaryMaxToken <= 0 {
		return fmt.Errorf("single_turn_history_summary_max_token 必须 > 0，当前值: %d", c.SingleTurnHistorySummaryMaxToken)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

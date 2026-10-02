package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultMemoryEngineConfig(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	assert.Equal(t, "", cfg.ForbiddenVariables)
	assert.Equal(t, 8192, cfg.InputMsgMaxLen)
	assert.Empty(t, cfg.CryptoKey)
	assert.Equal(t, 128, cfg.SingleTurnHistorySummaryMaxToken)
	assert.Nil(t, cfg.DefaultModelCfg)
	assert.Nil(t, cfg.DefaultModelClientCfg)
}

func TestMemoryEngineConfig_Validate_正常(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	assert.NoError(t, cfg.Validate())
}

func TestMemoryEngineConfig_Validate_CryptoKey32字节(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.CryptoKey = make([]byte, 32)
	assert.NoError(t, cfg.Validate())
}

func TestMemoryEngineConfig_Validate_CryptoKey非32字节(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.CryptoKey = make([]byte, 16)
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "crypto_key")
}

func TestMemoryEngineConfig_Validate_SummaryMaxToken为零(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.SingleTurnHistorySummaryMaxToken = 0
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "single_turn_history_summary_max_token")
}

func TestMemoryEngineConfig_Validate_SummaryMaxToken为负(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.SingleTurnHistorySummaryMaxToken = -1
	err := cfg.Validate()
	assert.Error(t, err)
}

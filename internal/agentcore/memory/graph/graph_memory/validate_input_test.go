package graph_memory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestValidateAddMemoryInput_正常情况(t *testing.T) {
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "user123", nil)
	assert.NoError(t, err)
}

func TestValidateAddMemoryInput_带ContentFmtKwargs(t *testing.T) {
	kwargs := map[string]string{"source_description": "test"}
	err := ValidateAddMemoryInput(32, config.EpisodeTypeDocument, "user123", kwargs)
	assert.NoError(t, err)
}

func TestValidateAddMemoryInput_ContentFmtKwargs为空map(t *testing.T) {
	kwargs := map[string]string{}
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "user123", kwargs)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Equal(t, exception.StatusMemoryStoreValidationInvalid, baseErr.Status())
}

func TestValidateAddMemoryInput_ContentFmtKwargs有空键(t *testing.T) {
	kwargs := map[string]string{"": "value"}
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "user123", kwargs)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateAddMemoryInput_ContentFmtKwargs有空值(t *testing.T) {
	kwargs := map[string]string{"key": ""}
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "user123", kwargs)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateAddMemoryInput_无效EpisodeType(t *testing.T) {
	err := ValidateAddMemoryInput(32, config.EpisodeType(99), "user123", nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "src_type must be one of")
}

func TestValidateAddMemoryInput_所有有效EpisodeType(t *testing.T) {
	for _, et := range []config.EpisodeType{
		config.EpisodeTypeConversation,
		config.EpisodeTypeDocument,
		config.EpisodeTypeJSON,
	} {
		err := ValidateAddMemoryInput(32, et, "user123", nil)
		assert.NoError(t, err, "EpisodeType=%d should be valid", et)
	}
}

func TestValidateAddMemoryInput_userID为空字符串(t *testing.T) {
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "", nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateAddMemoryInput_userID全是空白(t *testing.T) {
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, "   ", nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateAddMemoryInput_userID超长(t *testing.T) {
	err := ValidateAddMemoryInput(10, config.EpisodeTypeConversation, "a_very_long_user_id_that_exceeds_max_length", nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "user_id must be a string of length <= 10")
}

func TestValidateAddMemoryInput_userID恰好最大长度(t *testing.T) {
	userID := strings.Repeat("a", 32)
	err := ValidateAddMemoryInput(32, config.EpisodeTypeConversation, userID, nil)
	assert.NoError(t, err)
}

func TestValidateSearchInput_正常UserID列表(t *testing.T) {
	result, err := ValidateSearchInput("test query", []string{"user123"}, []bool{true, false, true})
	assert.NoError(t, err)
	assert.Equal(t, []string{"user123"}, result)
}

func TestValidateSearchInput_多UserID(t *testing.T) {
	result, err := ValidateSearchInput("test query", []string{"user1", "user2"}, []bool{true, false, true})
	assert.NoError(t, err)
	assert.Equal(t, []string{"user1", "user2"}, result)
}

func TestValidateSearchInput_空query(t *testing.T) {
	_, err := ValidateSearchInput("", []string{"user123"}, []bool{true})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "query must be a non-empty string value")
}

func TestValidateSearchInput_空白query(t *testing.T) {
	_, err := ValidateSearchInput("   ", []string{"user123"}, []bool{true})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateSearchInput_userID列表中有空字符串(t *testing.T) {
	_, err := ValidateSearchInput("test query", []string{"user1", ""}, []bool{true})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateSearchInput_userID超长(t *testing.T) {
	longUID := strings.Repeat("a", 33)
	_, err := ValidateSearchInput("test query", []string{longUID}, []bool{true})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

func TestValidateSearchInput_userID恰好32字符(t *testing.T) {
	uid := strings.Repeat("a", 32)
	result, err := ValidateSearchInput("test query", []string{uid}, []bool{true})
	assert.NoError(t, err)
	assert.Equal(t, []string{uid}, result)
}

func TestIsValidEpisodeType(t *testing.T) {
	assert.True(t, isValidEpisodeType(config.EpisodeTypeConversation))
	assert.True(t, isValidEpisodeType(config.EpisodeTypeDocument))
	assert.True(t, isValidEpisodeType(config.EpisodeTypeJSON))
	assert.False(t, isValidEpisodeType(config.EpisodeType(99)))
}


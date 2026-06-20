package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWithEnvs_注入环境变量 测试 WithEnvs 注入 context
func TestWithEnvs_注入环境变量(t *testing.T) {
	ctx := context.Background()
	envs := map[string]any{"KEY": "value"}

	newCtx := WithEnvs(ctx, envs)

	// 验证可以从 context 中取回
	result := getEnvsFromContext(newCtx)
	assert.Equal(t, envs, result)
}

// TestWithEnvs_空map 测试注入空 map
func TestWithEnvs_空map(t *testing.T) {
	ctx := context.Background()
	envs := map[string]any{}

	newCtx := WithEnvs(ctx, envs)

	result := getEnvsFromContext(newCtx)
	assert.Equal(t, envs, result)
}

// TestGetEnvsFromContext_无注入 测试无注入时返回 nil
func TestGetEnvsFromContext_无注入(t *testing.T) {
	ctx := context.Background()

	result := getEnvsFromContext(ctx)
	assert.Nil(t, result)
}

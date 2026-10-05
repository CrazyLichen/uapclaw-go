package vector_store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCheckConfigsMatching_匹配 测试配置完全匹配
func TestCheckConfigsMatching_匹配(t *testing.T) {
	configured := map[string]any{"space": "cosine", "max_neighbors": 16}
	actual := map[string]any{"space": "cosine", "max_neighbors": 16, "ef_search": 100}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_不匹配 测试配置不匹配
func TestCheckConfigsMatching_不匹配(t *testing.T) {
	configured := map[string]any{"space": "cosine"}
	actual := map[string]any{"space": "l2"}
	err := CheckConfigsMatching(configured, actual)
	assert.Error(t, err)
}

// TestCheckConfigsMatching_数值近似匹配 测试 16.0 和 16 视为匹配
func TestCheckConfigsMatching_数值近似匹配(t *testing.T) {
	configured := map[string]any{"max_neighbors": 16.0}
	actual := map[string]any{"max_neighbors": 16}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_忽略efSearchFactor 测试 efSearchFactor 被忽略
func TestCheckConfigsMatching_忽略efSearchFactor(t *testing.T) {
	configured := map[string]any{"efSearchFactor": 100, "space": "cosine"}
	actual := map[string]any{"space": "cosine"}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_大小写不敏感 测试字符串比较大小写不敏感
func TestCheckConfigsMatching_大小写不敏感(t *testing.T) {
	configured := map[string]any{"space": "Cosine"}
	actual := map[string]any{"space": "cosine"}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_实际配置缺键 测试实际配置缺少配置的键
func TestCheckConfigsMatching_实际配置缺键(t *testing.T) {
	configured := map[string]any{"space": "cosine", "max_neighbors": 16}
	actual := map[string]any{"space": "cosine"}
	err := CheckConfigsMatching(configured, actual)
	assert.Error(t, err)
}

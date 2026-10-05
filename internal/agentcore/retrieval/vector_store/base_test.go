package vector_store

import (
	"math"
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

// TestIsNumericString 测试 isNumericString 的各种场景
func TestIsNumericString(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"12.5", true},
		{"", false},
		{"abc", false},
		{"12.3.4", false},  // 多个小数点
		{"-5", true},       // 负号开头
		{"-", false},       // 仅负号
		{"12a", false},     // 包含字母
	}
	for _, tt := range tests {
		got := isNumericString(tt.input)
		assert.Equal(t, tt.want, got, "isNumericString(%q)", tt.input)
	}
}

// TestCheckConfigsMatching_配置为空 测试空配置始终匹配
func TestCheckConfigsMatching_配置为空(t *testing.T) {
	err := CheckConfigsMatching(map[string]any{}, map[string]any{"space": "cosine"})
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

// TestNewStoreOptions 测试 NewStoreOptions 构造函数
func TestNewStoreOptions(t *testing.T) {
	// 无参数
	opts := NewStoreOptions()
	assert.Equal(t, 0, opts.BatchSize)

	// 有参数
	opts = NewStoreOptions(WithStoreBatchSize(64))
	assert.Equal(t, 64, opts.BatchSize)

	// 多个参数
	opts = NewStoreOptions(WithStoreBatchSize(32), WithStoreBatchSize(128))
	assert.Equal(t, 128, opts.BatchSize) // 最后一个生效
}

// TestWithStoreBatchSize 测试 WithStoreBatchSize 选项
func TestWithStoreBatchSize(t *testing.T) {
	opt := WithStoreBatchSize(256)
	opts := &StoreOptions{}
	opt(opts)
	assert.Equal(t, 256, opts.BatchSize)
}

// TestToFloat 测试 toFloat 类型转换
func TestToFloat(t *testing.T) {
	assert.Equal(t, 1.0, toFloat(1))
	assert.Equal(t, 2.0, toFloat(int64(2)))
	assert.Equal(t, 3.5, toFloat(float32(3.5)))
	assert.Equal(t, 4.25, toFloat(4.25))
	assert.True(t, math.IsNaN(toFloat("not a number")))
	assert.True(t, math.IsNaN(toFloat(nil)))
}

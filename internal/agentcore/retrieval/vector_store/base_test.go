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

// TestCheckConfigsMatching_边界恰超1个百分点 测试 M-47 修正后的 isclose 边界语义。
// 输入 actual=31.68 / val=32 时：diff=1.28，rel_tol*max(31.68,32)=0.32，abs_tol=1e-3，
// 故 max(0.32, 0.001)=0.32 < 1.28，Python math.isclose 判定 False，Go 应同样不匹配。
// 修正前（+ 叠加公式）1.28 <= 0.32+0.001 为 False，但
// actual=126.72/val=128 时 diff=1.28，旧公式 0.01*128+0.001=1.281 >= 1.28 误放行。
func TestCheckConfigsMatching_边界恰超1个百分点(t *testing.T) {
	configured := map[string]any{"nlist": 128}
	actual := map[string]any{"nlist": 126.72}
	err := CheckConfigsMatching(configured, actual)
	assert.Error(t, err, "diff=1.28 恰超 rel_tol*max=1.28 边界外，应判定不匹配")
}

// TestCheckConfigsMatching_边界1个百分点内匹配 测试 M-47 修正后边界内侧匹配。
// actual=127.36 / val=128：diff=0.64，rel_tol*max(127.36,128)=1.28 → 匹配。
func TestCheckConfigsMatching_边界1个百分点内匹配(t *testing.T) {
	configured := map[string]any{"nlist": 128}
	actual := map[string]any{"nlist": 127.36}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_零值absTol兜底 测试 val=0 时 abs_tol 兜底。
// actual=0.0005 / val=0：diff=0.0005，rel_tol*max=0，abs_tol=1e-3=0.001 → 0.0005 <= 0.001 匹配。
func TestCheckConfigsMatching_零值absTol兜底(t *testing.T) {
	configured := map[string]any{"param": 0}
	actual := map[string]any{"param": 0.0005}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestIsNumericString 测试 isNumericString 的各种场景
// T-24: 语义对齐 Python s.replace(".", "").isnumeric()
func TestIsNumericString(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"12.5", true},
		{"", false},
		{"abc", false},
		{"12.3.4", true}, // Python: "12.3.4".replace(".","") == "1234".isnumeric() → True
		{"-5", false},    // Python: "-5".isnumeric() == False（负数走字符串比较路径）
		{"-", false},     // 仅负号
		{"12a", false},   // 包含字母
		{"1e5", false},   // Python: "1e5".isnumeric() == False（拒绝科学计数法）
		{".", false},     // 全点串 isnumeric 空串 → False
		{"１２３", true},    // 全角数字 Python isnumeric 接受
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
// T-23: 覆盖 isNumeric 接受的全部 13 种数值类型
func TestToFloat(t *testing.T) {
	assert.Equal(t, 1.0, toFloat(1))
	assert.Equal(t, 2.0, toFloat(int64(2)))
	assert.Equal(t, 3.5, toFloat(float32(3.5)))
	assert.Equal(t, 4.25, toFloat(4.25))
	// T-23 新增分支
	assert.Equal(t, 5.0, toFloat(int8(5)))
	assert.Equal(t, 6.0, toFloat(int16(6)))
	assert.Equal(t, 7.0, toFloat(int32(7)))
	assert.Equal(t, 8.0, toFloat(uint(8)))
	assert.Equal(t, 9.0, toFloat(uint8(9)))
	assert.Equal(t, 10.0, toFloat(uint16(10)))
	assert.Equal(t, 11.0, toFloat(uint32(11)))
	assert.Equal(t, 12.0, toFloat(uint64(12)))
	assert.True(t, math.IsNaN(toFloat("not a number")))
	assert.True(t, math.IsNaN(toFloat(nil)))
}

// TestCheckConfigsMatching_uint配置 T-23 回归测试。
// uint 类型配置值原走 toFloat default 返回 NaN 被误判不匹配，修复后应通过 isclose 比较。
func TestCheckConfigsMatching_uint配置(t *testing.T) {
	configured := map[string]any{"max_neighbors": uint(16)}
	actual := map[string]any{"max_neighbors": "16"}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

// TestCheckConfigsMatching_数值不近似匹配 测试数值差距过大时不匹配
func TestCheckConfigsMatching_数值不近似匹配(t *testing.T) {
	configured := map[string]any{"max_neighbors": 16}
	actual := map[string]any{"max_neighbors": 100}
	err := CheckConfigsMatching(configured, actual)
	assert.Error(t, err)
}

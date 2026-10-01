package registry

import (
	"testing"
)

// TestPoolAccessor_接口存在 验证接口定义可编译
func TestPoolAccessor_接口存在(t *testing.T) {
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ PoolAccessor = PoolAccessor(nil)
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ PoolEntry = PoolEntry(nil)
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ PoolTeamEntry = PoolTeamEntry(nil)
}

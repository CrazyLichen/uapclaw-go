package registry

import (
	"testing"
)

// TestPoolAccessor_接口存在 验证接口定义可编译
func TestPoolAccessor_接口存在(t *testing.T) {
	var _ PoolAccessor = (PoolAccessor)(nil)
	var _ PoolEntry = (PoolEntry)(nil)
	var _ PoolTeamEntry = (PoolTeamEntry)(nil)
}

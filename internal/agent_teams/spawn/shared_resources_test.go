package spawn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
)

// TestGetSharedRuntime_返回非nil 测试 GetSharedRuntime 返回真实 TeamRuntime 单例。
func TestGetSharedRuntime_返回非nil(t *testing.T) {
	spawn.CleanupSharedResources()

	result := spawn.GetSharedRuntime()
	assert.NotNil(t, result, "GetSharedRuntime 应返回非 nil 的 TeamRuntime")

	// 二次调用返回同一实例
	result2 := spawn.GetSharedRuntime()
	assert.Same(t, result, result2, "GetSharedRuntime 应返回同一单例")
}

// TestGetSharedDB_config为nil返回nil 测试 GetSharedDB 传入 nil config 返回 nil。
func TestGetSharedDB_config为nil返回nil(t *testing.T) {
	spawn.CleanupSharedResources()

	result := spawn.GetSharedDB(nil)
	assert.Nil(t, result, "GetSharedDB 传入 nil config 应返回 nil")
}

// TestCleanupSharedResources_可重复调用 测试清理可重复调用。
func TestCleanupSharedResources_可重复调用(t *testing.T) {
	// 不应 panic
	spawn.CleanupSharedResources()
	spawn.CleanupSharedResources()
}

// TestCleanupSharedResources_重置Runtime 测试清理后 GetSharedRuntime 返回新实例。
func TestCleanupSharedResources_重置Runtime(t *testing.T) {
	spawn.CleanupSharedResources()

	first := spawn.GetSharedRuntime()
	assert.NotNil(t, first)

	spawn.CleanupSharedResources()

	second := spawn.GetSharedRuntime()
	assert.NotNil(t, second)
	assert.NotSame(t, first, second, "清理后应创建新 TeamRuntime 实例")
}

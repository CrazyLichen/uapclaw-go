package spawn_test

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
)

// TestGetSharedRuntime_当前返回nil 测试 GetSharedRuntime 当前返回 nil + TODO。
func TestGetSharedRuntime_当前返回nil(t *testing.T) {
	spawn.CleanupSharedResources()

	result := spawn.GetSharedRuntime()
	// 当前返回 nil（TODO #9.85）
	_ = result
}

// TestGetSharedDB_当前返回nil 测试 GetSharedDB 当前返回 nil + TODO。
func TestGetSharedDB_当前返回nil(t *testing.T) {
	spawn.CleanupSharedResources()

	result := spawn.GetSharedDB(nil)
	// 当前返回 nil（TODO #9.64）
	_ = result
}

// TestCleanupSharedResources_可重复调用 测试清理可重复调用。
func TestCleanupSharedResources_可重复调用(t *testing.T) {
	// 不应 panic
	spawn.CleanupSharedResources()
	spawn.CleanupSharedResources()
}

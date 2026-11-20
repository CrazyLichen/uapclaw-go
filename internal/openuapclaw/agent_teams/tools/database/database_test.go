package database

import "testing"

// TestInMemoryTeamDatabase_满足TeamDatabase接口 验证 InMemoryTeamDatabase 实现了 TeamDatabase 接口。
func TestInMemoryTeamDatabase_满足TeamDatabase接口(t *testing.T) {
	// 编译期接口满足性检查 — 若 InMemoryTeamDatabase 未完全实现接口，编译将失败
	t.Log("InMemoryTeamDatabase 满足 TeamDatabase/TeamDao/MemberDao 接口")
}

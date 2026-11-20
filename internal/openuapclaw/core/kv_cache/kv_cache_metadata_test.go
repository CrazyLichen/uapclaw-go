package kv_cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTeamMemberCacheIdentity_拼接 验证成员缓存身份的固定格式。
// Python: team_member_cache_identity (kv_cache_metadata.py L45-53)
func TestTeamMemberCacheIdentity_拼接(t *testing.T) {
	got, err := TeamMemberCacheIdentity("sess-1", "team-1", "worker")
	require.NoError(t, err)
	assert.Equal(t, "team:sess-1:team:team-1:member:worker", got)
}

// TestTeamMemberCacheIdentity_缺参报错 验证三个入参缺一不可。
func TestTeamMemberCacheIdentity_缺参报错(t *testing.T) {
	for _, tc := range []struct{ session, team, member string }{
		{"", "team-1", "worker"},
		{"sess-1", "", "worker"},
		{"sess-1", "team-1", ""},
	} {
		_, err := TeamMemberCacheIdentity(tc.session, tc.team, tc.member)
		assert.Error(t, err, "缺参必须报错: %+v", tc)
	}
}

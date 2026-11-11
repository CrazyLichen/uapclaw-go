package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestSyncSessionRequestMetadata_新建会话 验证元数据缺失时兜底创建并锁定字段。
func TestSyncSessionRequestMetadata_新建会话(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-new"
	persist := true

	locked := SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID:             sid,
		ChannelID:             "web",
		Mode:                  "agent.work.plan",
		ExplicitModeProvided:  true,
		Model:                 "m1",
		ExplicitModelProvided: true,
		ProjectDir:            "/tmp/p1",
		ProjectID:             "proj-1",
		CronID:                "cron-1",
		UserID:                "u1",
		IsChatTurn:            true,
		WorkMode:              "code",
		PersistSession:        &persist,
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, "/tmp/p1", locked)

	meta := GetSessionMetadata(sid)
	require.Equal(t, "agent.work.plan", meta["mode"])
	require.Equal(t, "m1", meta["model"])
	require.Equal(t, "/tmp/p1", meta["project_dir"])
	require.Equal(t, "proj-1", meta["project_id"])
	require.Equal(t, "cron-1", meta["cron_id"])
	require.Equal(t, "u1", meta["user_id"])
	require.Equal(t, "web", meta["channel_id"])
	require.Equal(t, true, meta["persist_session"])
	require.Equal(t, "code", meta["work_mode"])
	require.Contains(t, meta, "last_user_message_at")
	require.Contains(t, meta, "last_message_at")
}

// TestSyncSessionRequestMetadata_锁定语义 验证首次锁定字段不被后续请求改写。
func TestSyncSessionRequestMetadata_锁定语义(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-lock"
	first := SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, ChannelID: "web", ProjectDir: "/tmp/p1",
		ProjectID: "proj-1", CronID: "cron-1", UserID: "u1",
		WorkMode: "work", Mode: "agent.work.plan", ExplicitModeProvided: true,
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, "/tmp/p1", first)

	// 第二次：不同 project_dir/project_id/cron_id/user_id/work_mode 均不得覆盖
	second := SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, ChannelID: "tui", ProjectDir: "/tmp/p2",
		ProjectID: "proj-2", CronID: "cron-2", UserID: "u2",
		WorkMode: "code", Mode: "agent.work.normal", ExplicitModeProvided: true,
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, "/tmp/p1", second, "已锁定的 project_dir 优先返回")

	meta := GetSessionMetadata(sid)
	require.Equal(t, "/tmp/p1", meta["project_dir"])
	require.Equal(t, "proj-1", meta["project_id"])
	require.Equal(t, "cron-1", meta["cron_id"])
	require.Equal(t, "u1", meta["user_id"])
	require.Equal(t, "web", meta["channel_id"])
	require.Equal(t, "work", meta["work_mode"])
	require.Equal(t, "agent.work.normal", meta["mode"], "mode 是显式覆盖式")
}

// TestSyncSessionRequestMetadata_既有会话逐字段首次锁定 覆盖 else 分支的逐字段写入。
func TestSyncSessionRequestMetadata_既有会话逐字段首次锁定(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-fields"

	// 先建一个只有 session_id 的骨架（create 分支不写这些字段）
	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid})
	FlushMetadataQueue(5.0)

	stamp := 999.25
	locked := SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, ChannelID: "web", ProjectDir: "/tmp/pl",
		ProjectID: "proj-x", CronID: "cron-x", UserID: "user-x",
		WorkMode: "code", Model: "m-x", ExplicitModelProvided: true,
		IsChatTurn: true, LastUserMessageAt: &stamp,
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, "/tmp/pl", locked)

	meta := GetSessionMetadata(sid)
	require.Equal(t, "/tmp/pl", meta["project_dir"])
	require.Equal(t, "proj-x", meta["project_id"])
	require.Equal(t, "cron-x", meta["cron_id"])
	require.Equal(t, "user-x", meta["user_id"])
	require.Equal(t, "web", meta["channel_id"])
	// work_mode 在骨架创建时已按渠道默认锁定为 work，后续 code 不覆盖（首次锁定语义）
	require.Equal(t, "work", meta["work_mode"])
	require.Equal(t, "m-x", meta["model"])
	require.Equal(t, stamp, meta["last_user_message_at"])

	// 再次进入 else 分支：已锁定字段全部不覆盖（覆盖告警分支）
	locked2 := SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, ChannelID: "tui", ProjectDir: "/tmp/other",
		ProjectID: "proj-y", CronID: "cron-y", UserID: "user-y", WorkMode: "work",
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, "/tmp/pl", locked2)
	meta2 := GetSessionMetadata(sid)
	require.Equal(t, "proj-x", meta2["project_id"])
	require.Equal(t, "cron-x", meta2["cron_id"])
	require.Equal(t, "web", meta2["channel_id"])
	require.Equal(t, "user-x", meta2["user_id"])
	require.Equal(t, "work", meta2["work_mode"])
}

// TestSyncSessionRequestMetadata_非显式不覆盖mode与model 验证只读 RPC 不腐蚀已锁定值。
func TestSyncSessionRequestMetadata_非显式不覆盖mode与model(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-explicit"
	SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, Mode: "team.work.plan", ExplicitModeProvided: true,
		Model: "m1", ExplicitModelProvided: true,
	})
	FlushMetadataQueue(5.0)

	SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, Mode: "agent.work.normal", ExplicitModeProvided: false,
		Model: "m2", ExplicitModelProvided: false,
	})
	FlushMetadataQueue(5.0)

	meta := GetSessionMetadata(sid)
	require.Equal(t, "team.work.plan", meta["mode"])
	require.Equal(t, "m1", meta["model"])
}

// TestSyncSessionRequestMetadata_时间戳语义 验证 last_message_at 仅 chat 轮次刷新。
func TestSyncSessionRequestMetadata_时间戳语义(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-time"
	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid, IsChatTurn: true})
	FlushMetadataQueue(5.0)
	before := GetSessionMetadata(sid)["last_message_at"]

	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid, IsChatTurn: false})
	FlushMetadataQueue(5.0)
	require.Equal(t, before, GetSessionMetadata(sid)["last_message_at"], "非 chat 轮次不刷新 last_message_at")

	stamp := 12345.5
	SyncSessionRequestMetadata(SessionRequestMetadataSync{
		SessionID: sid, LastUserMessageAt: &stamp, IsChatTurn: false,
	})
	FlushMetadataQueue(5.0)
	require.Equal(t, stamp, GetSessionMetadata(sid)["last_user_message_at"], "last_user_message_at 覆盖式")
}

// TestSyncSessionRequestMetadata_persistSession锁定 验证创建期锁定后仅告警。
func TestSyncSessionRequestMetadata_persistSession锁定(t *testing.T) {
	setupSyncTestDir(t)
	sid := "sess-sync-persist"
	enabled := true
	disabled := false
	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid, PersistSession: &enabled})
	FlushMetadataQueue(5.0)
	require.Equal(t, true, GetSessionMetadata(sid)["persist_session"])

	// 已锁定 true，请求 false → 只告警不覆盖
	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid, PersistSession: &disabled})
	FlushMetadataQueue(5.0)
	require.Equal(t, true, GetSessionMetadata(sid)["persist_session"])

	// 历史脏值（非布尔）→ 失败关闭为 false
	stamp := GetSessionMetadata(sid)
	stamp["persist_session"] = "yes"
	WriteSessionMetadata(GetSessionsDir(), sid, stamp)
	RemoveSessionMetadataCache(sid)

	SyncSessionRequestMetadata(SessionRequestMetadataSync{SessionID: sid, PersistSession: &enabled})
	FlushMetadataQueue(5.0)
	require.Equal(t, false, GetSessionMetadata(sid)["persist_session"])
}

// TestSyncSessionRequestMetadata_空会话直接返回 验证无 session_id 不写盘。
func TestSyncSessionRequestMetadata_空会话直接返回(t *testing.T) {
	setupSyncTestDir(t)
	require.Equal(t, "", SyncSessionRequestMetadata(SessionRequestMetadataSync{ProjectDir: "/tmp/p"}))
}

// TestSyncMetadataHelpers 验证 work_mode / 渠道默认值等辅助函数。
func TestSyncMetadataHelpers(t *testing.T) {
	require.True(t, isValidWorkMode(" CODE "))
	require.False(t, isValidWorkMode("plan"))
	require.Equal(t, "code", normalizeSyncWorkMode(" Code "))
	require.Equal(t, "", normalizeSyncWorkMode("plan"))
	require.Equal(t, "code", defaultWorkModeForChannel("TUI"))
	require.Equal(t, "work", defaultWorkModeForChannel("web"))
	require.True(t, hasNonEmptyString(map[string]any{"k": " v "}, "k"))
	require.False(t, hasNonEmptyString(map[string]any{"k": "  "}, "k"))
	require.False(t, hasNonEmptyString(map[string]any{}, "k"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupSyncTestDir 把全局 sessions 目录指向测试临时目录。
func setupSyncTestDir(t *testing.T) {
	t.Helper()
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		FlushMetadataQueue(5.0)
		ClearAllSessionMetadataCache()
		pathutil.ResetCache()
	})
}

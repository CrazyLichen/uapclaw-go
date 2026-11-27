package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// newStoreTestEnv 隔离数据根：UAPCLAW_DATA_DIR 在 WorkspaceDir 槽初始化前设置，
// projects.json 与 sessions/ 都落在临时目录（对齐 Python monkeypatch 风格）。
func newStoreTestEnv(t *testing.T) string {
	t.Helper()
	// 手动建临时根（不用 t.TempDir：WorkspaceDir 槽进程级 memoize，
	// 首个测试固定后后续测试的元数据写入会落在首测目录，t.TempDir 的
	// 严格清理会被 memoize 目录的迟到写交叉击穿）
	dataDir, err := os.MkdirTemp("", "store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	agentRoot := filepath.Join(dataDir, "agent")
	agentRootOverride = agentRoot
	sessionsDir := filepath.Join(agentRoot, "sessions")
	sessionsDirOverride = sessionsDir
	t.Cleanup(func() { agentRootOverride = ""; sessionsDirOverride = "" })
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectsCache = nil
	t.Cleanup(func() { projectsCache = nil })
	return agentRoot
}

// writeProject 写入一条项目记录到 projects.json。
func writeProject(t *testing.T, rec map[string]any) {
	t.Helper()
	path := filepath.Join(agentRootOverride, "projects.json")
	raw := readDiskProjects(path)
	raw = append(raw, rec)
	if err := writeDiskProjects(path, raw); err != nil {
		t.Fatal(err)
	}
	projectsCache = nil
}

// writeSessionMeta 写入一条会话 metadata.json。
func writeSessionMeta(t *testing.T, sessionID string, meta map[string]any) {
	t.Helper()
	dir := filepath.Join(agentRootOverride, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		return
	}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkModeHelpers(t *testing.T) {
	if !IsDefaultProjectID("") || !IsDefaultProjectID("default") || !IsDefaultProjectID("default_code") {
		t.Fatal("默认项目判定失败")
	}
	if IsDefaultProjectID("proj_abc") {
		t.Fatal("真实项目不应判为默认")
	}
	if ResolveDefaultProjectID("code") != DefaultProjectIDCode || ResolveDefaultProjectID("work") != DefaultProjectIDWork {
		t.Fatal("resolve_default_project_id 失败")
	}
	if got, _ := ResolveRequestWorkMode(map[string]any{"work_mode": " code "}, "web"); got != "code" {
		t.Fatalf("strip+lower 失败: %q", got)
	}
	if _, code := ResolveRequestWorkMode(map[string]any{"work_mode": "bogus"}, "web"); code != "BAD_REQUEST" {
		t.Fatalf("非法 work_mode 应 BAD_REQUEST: %q", code)
	}
	if got, _ := ResolveRequestWorkMode(map[string]any{}, "web"); got != WorkModeWeb {
		t.Fatalf("缺省应通道默认: %q", got)
	}
}

func TestProjectStore_读取与惰性迁移(t *testing.T) {
	newStoreTestEnv(t)
	writeProject(t, map[string]any{
		"project_id": "proj_1", "name": "旧项目", "project_dir": "/tmp/p1", "hidden": false,
	})
	projects := ListProjects(true, true)
	if len(projects) != 1 {
		t.Fatalf("want 1 project got %d", len(projects))
	}
	if projects[0].WorkMode != WorkModeWeb {
		t.Fatalf("旧数据应推断 work: %q", projects[0].WorkMode)
	}
	// 惰性迁移写回磁盘
	path := filepath.Join(agentRootOverride, "projects.json")
	disk := readDiskProjects(path)
	if disk[0]["work_mode"] != WorkModeWeb {
		t.Fatalf("惰性迁移未写回: %+v", disk[0])
	}
	if GetProjectByID("proj_1", true) == nil || GetProjectByID("nope", true) != nil {
		t.Fatal("get_project_by_id 失败")
	}
}

func TestAttributeSessionProject(t *testing.T) {
	visible := map[string]bool{"proj_v": true}
	removed := map[string]bool{"proj_r": true}
	cases := []struct {
		meta map[string]any
		want string
		note string
	}{
		{map[string]any{"project_id": "proj_v"}, "proj_v", "可见"},
		{map[string]any{"project_id": "proj_r"}, "", "已移除"},
		{map[string]any{"work_mode": "code"}, DefaultProjectIDCode, "code 兜底"},
		{map[string]any{}, DefaultProjectIDWork, "无归属兜底"},
	}
	for _, c := range cases {
		if got := AttributeSessionProject(c.meta, visible, removed); got != c.want {
			t.Fatalf("%s: got %q want %q", c.note, got, c.want)
		}
	}
}

func TestLoadProjectList_统计与默认项目(t *testing.T) {
	newStoreTestEnv(t)
	// 一个可见项目 + 两个 web 会话（一个归属 proj_v，一个无归属落默认）
	writeProject(t, map[string]any{"project_id": "proj_v", "name": "可见", "hidden": false, "work_mode": "work"})
	writeSessionMeta(t, "web_s1", map[string]any{
		"session_id": "web_s1", "channel_id": "web", "title": "任务一",
		"project_id": "proj_v", "message_count": 2, "last_message_at": 100.0,
		"last_user_message_at": 90.0, "pinned": false,
	})
	writeSessionMeta(t, "web_s2", map[string]any{
		"session_id": "web_s2", "channel_id": "web", "title": "默认项目任务",
		"message_count": 1, "last_message_at": 200.0, "last_user_message_at": 180.0,
		"pinned": false,
	})
	// cron 会话与置顶会话不计入统计
	writeSessionMeta(t, "cron_x_1", map[string]any{
		"session_id": "cron_x_1", "channel_id": "web", "cron_id": "job1", "pinned": false,
	})
	writeSessionMeta(t, "web_p1", map[string]any{
		"session_id": "web_p1", "channel_id": "web", "pinned": true,
	})

	got := LoadProjectList(map[string]any{})
	if got.Error != "" {
		t.Fatalf("load_project_list err=%s", got.Error)
	}
	projects := got.Payload["projects"].([]map[string]any)
	var defProj, visProj map[string]any
	for _, p := range projects {
		if p["project_id"] == "default" {
			defProj = p
		}
		if p["project_id"] == "proj_v" {
			visProj = p
		}
	}
	if defProj == nil || visProj == nil {
		t.Fatalf("缺默认或可见项目: %+v", projects)
	}
	if defProj["session_count"] != 1 || visProj["session_count"] != 1 {
		t.Fatalf("统计错误: default=%v vis=%v（cron/置顶应排除）", defProj["session_count"], visProj["session_count"])
	}
	if visProj["last_message_at"] != 100.0 {
		t.Fatalf("last_message_at 应 100: %v", visProj["last_message_at"])
	}
}

func TestLoadProjectSessions_分页与工作模式过滤(t *testing.T) {
	newStoreTestEnv(t)
	writeSessionMeta(t, "web_a", map[string]any{
		"session_id": "web_a", "channel_id": "web", "work_mode": "work", "pinned": false,
		"last_user_message_at": 30.0,
	})
	writeSessionMeta(t, "web_b", map[string]any{
		"session_id": "web_b", "channel_id": "web", "work_mode": "work", "pinned": false,
		"last_user_message_at": 50.0,
	})
	writeSessionMeta(t, "web_c", map[string]any{
		"session_id": "web_c", "channel_id": "web", "work_mode": "design", "pinned": false,
		"last_user_message_at": 40.0,
	})
	got := LoadProjectSessions(map[string]any{"project_id": "default", "limit": float64(1), "work_mode": "work"}, "")
	if got.Error != "" {
		t.Fatalf("err=%s", got.Error)
	}
	sessions := got.Payload["sessions"].([]any)
	if len(sessions) != 1 || sessions[0].(map[string]any)["session_id"] != "web_b" {
		t.Fatalf("应只剩 web_b（design 过滤 + 按 recency 首条）: %+v", sessions)
	}
	if got.Payload["total"] != 2 {
		t.Fatalf("total 应 2: %v", got.Payload["total"])
	}
	// 缺 project_id
	if bad := LoadProjectSessions(map[string]any{}, ""); bad.Error != "project_id is required" {
		t.Fatalf("缺 project_id 应报错: %+v", bad)
	}
}

func TestLoadProjectCronSessions_名字回填(t *testing.T) {
	newStoreTestEnv(t)
	// 存量 team 会话：目录名 cron_100_job1 但元数据缺 cron_id
	writeSessionMeta(t, "cron_100_job1", map[string]any{
		"session_id": "cron_100_job1", "channel_id": "web", "pinned": false,
		"last_user_message_at": 10.0,
	})
	writeSessionMeta(t, "web_other", map[string]any{
		"session_id": "web_other", "channel_id": "web", "pinned": false,
	})
	got := LoadProjectCronSessions(map[string]any{"project_id": "default", "cron_id": "job1"}, "")
	if got.Error != "" {
		t.Fatalf("err=%s", got.Error)
	}
	sessions := got.Payload["sessions"].([]any)
	if len(sessions) != 1 || sessions[0].(map[string]any)["session_id"] != "cron_100_job1" {
		t.Fatalf("应回填并命中 cron_100_job1: %+v", sessions)
	}
	// 回填后磁盘 metadata 有 cron_id（首次锁定语义）
	meta := swarmsession.ReadSessionMetadata(sessionsDirOverride, "cron_100_job1")
	if meta["cron_id"] != "job1" {
		t.Fatalf("回填未落盘: %+v", meta)
	}
	// 第二次查询走正常路径仍命中
	got2 := LoadProjectCronSessions(map[string]any{"project_id": "default", "cron_id": "job1"}, "")
	if len(got2.Payload["sessions"].([]any)) != 1 {
		t.Fatal("二次查询应仍命中")
	}
}

func TestLoadPinnedSessions(t *testing.T) {
	newStoreTestEnv(t)
	writeSessionMeta(t, "web_pin2", map[string]any{
		"session_id": "web_pin2", "channel_id": "web", "pinned": true, "pin_order": 2.0,
	})
	writeSessionMeta(t, "web_pin1", map[string]any{
		"session_id": "web_pin1", "channel_id": "web", "pinned": true, "pin_order": 1.0,
	})
	writeSessionMeta(t, "web_plain", map[string]any{
		"session_id": "web_plain", "channel_id": "web", "pinned": false,
	})
	got := LoadPinnedSessions()
	sessions := got.Payload["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("want 2 pinned got %d", len(sessions))
	}
	if sessions[0].(map[string]any)["session_id"] != "web_pin1" {
		t.Fatalf("应按 pin_order 升序: %+v", sessions)
	}
}

func TestResolveCronBindingControl(t *testing.T) {
	newStoreTestEnv(t)
	got := ResolveCronBindingControl(map[string]any{"project_id": "default"}, "web")
	if got.Error != "" || got.Payload["project_id"] != "default" || got.Payload["work_mode"] != WorkModeWeb {
		t.Fatalf("显式默认项目直映射失败: %+v", got)
	}
	bad := ResolveCronBindingControl(map[string]any{"work_mode": "design"}, "web")
	if bad.Code != "BAD_REQUEST" {
		t.Fatalf("design 不支持 cron 应 BAD_REQUEST: %+v", bad)
	}
}

func TestCronSessionMatchesJob(t *testing.T) {
	if !CronSessionMatchesJob("cron_job1", "job1") ||
		!CronSessionMatchesJob("cron_100_job1", "job1") ||
		CronSessionMatchesJob("__cron___100_abcd", "job1") ||
		CronSessionMatchesJob("", "job1") {
		t.Fatal("cron_session_matches_job 判定失败")
	}
}

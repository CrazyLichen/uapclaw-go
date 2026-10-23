package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupInteractionsEnv 隔离 interactions 目录环境（memoryrpc 先例）
func setupInteractionsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return tmp
}

// newTestPI 构造测试用完整 PendingInteraction（15 字段全填）
func newTestPI(id string) *PendingInteraction {
	return &PendingInteraction{
		InteractionID:    id,
		Mode:             "group",
		OriginChannelID:  "feishu_enterprise:app1",
		OriginSessionID:  "sess-1",
		OriginContent:    "帮我订会议室",
		OriginSenderName: "张三",
		OriginSenderID:   "ou-zhangsan",
		Question:         "需要哪个楼层？",
		TargetUserID:     "ou-zhangsan",
		TargetUserName:   "张三",
		OriginMetadata:   map[string]any{"api_key": "sk-secret", "Token": "tok", "AUTHORIZATION": "Bearer x", "keep": "v"},
		PrincipalID:      "ou-principal",
		Status:           "pending",
		CreatedAt:        1700000000.0,
		UpdatedAt:        1700000000.0,
	}
}

// TestSave_落盘与敏感键过滤 Save 写文件内容断言（api_key/Token/AUTHORIZATION 大小写三态剔除）
func TestSave_落盘与敏感键过滤(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_sess-1_ou-zhangsan")
	pi.Save()

	// 文件应存在
	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_sess-1_ou-zhangsan.json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("save 后文件应存在: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("应为合法 JSON: %v", err)
	}

	// 敏感键剔除（小写比较三键）
	meta, _ := data["origin_metadata"].(map[string]any)
	if meta == nil {
		t.Fatalf("origin_metadata 应为 map: %v", data["origin_metadata"])
	}
	for _, k := range []string{"api_key", "Token", "AUTHORIZATION"} {
		if _, ok := meta[k]; ok {
			t.Errorf("敏感键 %q 应被剔除", k)
		}
	}
	if meta["keep"] != "v" {
		t.Errorf("非敏感键 keep 应保留, 实际 %v", meta["keep"])
	}

	// updated_at 被刷新为 now（>= 保存前时刻）
	updatedAt, _ := data["updated_at"].(float64)
	if updatedAt < float64(time.Now().Unix())-2 {
		t.Errorf("updated_at 应刷新为当前时间, 实际 %v", updatedAt)
	}

	// 15 字段全量断言
	if data["interaction_id"] != "gpq_sess-1_ou-zhangsan" || data["mode"] != "group" ||
		data["status"] != "pending" || data["question"] != "需要哪个楼层？" {
		t.Errorf("核心字段不匹配: %+v", data)
	}

	// created_at 零值时补 now（dataclass default_factory 对齐）
	pi2 := newTestPI("gpq_zero_time")
	pi2.CreatedAt = 0
	pi2.UpdatedAt = 0
	pi2.Save()
	raw2, _ := os.ReadFile(filepath.Join(pathutil.AgentInteractionsDir(), "gpq_zero_time.json"))
	var data2 map[string]any
	_ = json.Unmarshal(raw2, &data2)
	createdAt, _ := data2["created_at"].(float64)
	if createdAt < float64(time.Now().Unix())-2 {
		t.Errorf("created_at 零值应补当前时间, 实际 %v", createdAt)
	}
}

// TestSave_失败仅Warn 目录路径被同名文件占据 → MkdirAll 失败不 panic
func TestSave_失败仅Warn(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	// 用同名文件占据 interactions 目录路径
	dir := filepath.Join(tmp, "agent", "workspace", "interactions")
	_ = os.MkdirAll(filepath.Dir(dir), 0o755)
	if err := os.WriteFile(dir, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("预置失败: %v", err)
	}

	pi := newTestPI("gpq_any")
	pi.Save() // 不 panic 即通过
}

// TestRemove_存在与不存在 存在删除 + 不存在静默
func TestRemove_存在与不存在(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_rm_1")
	pi.Save()
	pi.Remove()

	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_rm_1.json")
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Errorf("remove 后文件应不存在")
	}

	// 不存在时静默（不 panic 不报错）
	pi2 := newTestPI("gpq_rm_missing")
	pi2.Remove()
}

// TestLoad_完整往返 Save→Load 15 字段逐一断言
func TestLoad_完整往返(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_round_1")
	pi.Save()

	loaded := LoadPendingInteraction("gpq_round_1")
	if loaded == nil {
		t.Fatal("load 应返回非 nil")
	}
	if loaded.InteractionID != pi.InteractionID || loaded.Mode != pi.Mode ||
		loaded.OriginChannelID != pi.OriginChannelID || loaded.OriginSessionID != pi.OriginSessionID ||
		loaded.OriginContent != pi.OriginContent || loaded.OriginSenderName != pi.OriginSenderName ||
		loaded.OriginSenderID != pi.OriginSenderID || loaded.Question != pi.Question ||
		loaded.TargetUserID != pi.TargetUserID || loaded.TargetUserName != pi.TargetUserName ||
		loaded.PrincipalID != pi.PrincipalID || loaded.Status != pi.Status ||
		loaded.CreatedAt != pi.CreatedAt {
		t.Errorf("字段往返不匹配:\n got %+v\nwant %+v", loaded, pi)
	}
	if _, ok := loaded.OriginMetadata["keep"]; !ok {
		t.Errorf("origin_metadata.keep 应保留")
	}
	// updated_at 被 Save 刷新为 now，往返后 >= 原值
	if loaded.UpdatedAt < pi.CreatedAt {
		t.Errorf("UpdatedAt 应 >= 原时间戳")
	}
}

// TestLoad_文件不存在与损坏 nil 返回 + 坏 JSON → nil
func TestLoad_文件不存在与损坏(t *testing.T) {
	setupInteractionsEnv(t)

	if got := LoadPendingInteraction("gpq_not_exist"); got != nil {
		t.Errorf("文件不存在应返回 nil, 实际 %v", got)
	}

	// 坏 JSON
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "gpq_bad.json"), []byte("{not json"), 0o644)
	if got := LoadPendingInteraction("gpq_bad"); got != nil {
		t.Errorf("坏 JSON 应返回 nil, 实际 %v", got)
	}
}

// TestLoad_未知键忽略与缺失时间补now 手写 JSON 含未知字段、缺 created_at → now 附近
func TestLoad_未知键忽略与缺失时间补now(t *testing.T) {
	setupInteractionsEnv(t)

	raw := `{
  "interaction_id": "iact_ch_1",
  "mode": "dm",
  "origin_channel_id": "feishu_enterprise:app1",
  "origin_session_id": "sess-1",
  "origin_content": "订会议室",
  "origin_sender_name": "张三",
  "origin_sender_id": "ou-zhangsan",
  "question": "确认吗？",
  "target_user_id": "ou-principal",
  "unknown_extra_key": "ignored"
}`
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "iact_ch_1.json"), []byte(raw), 0o644)

	pi := LoadPendingInteraction("iact_ch_1")
	if pi == nil {
		t.Fatal("应加载成功")
	}
	// 未知键被忽略（无报错）
	// 缺失可选字段用默认值
	if pi.TargetUserName != "" || pi.PrincipalID != "" {
		t.Errorf("缺失可选字段应为零值")
	}
	if pi.Status != StatusPending {
		t.Errorf("缺 status 应默认 pending（dataclass default），实际 %q", pi.Status)
	}
	if pi.OriginMetadata == nil || len(pi.OriginMetadata) != 0 {
		t.Errorf("缺 origin_metadata 应为空 map（dataclass default_factory=dict）")
	}
	// 缺失时间字段补当前时间（default_factory=time.time）
	if pi.CreatedAt < float64(time.Now().Unix())-2 {
		t.Errorf("缺 created_at 应补当前时间, 实际 %v", pi.CreatedAt)
	}
	if pi.UpdatedAt < float64(time.Now().Unix())-2 {
		t.Errorf("缺 updated_at 应补当前时间, 实际 %v", pi.UpdatedAt)
	}
}

// TestLoad_必填字段缺失跳过 缺 question 的 JSON → nil（Python TypeError→except 对齐）
func TestLoad_必填字段缺失跳过(t *testing.T) {
	setupInteractionsEnv(t)

	raw := `{"interaction_id": "gpq_miss_q", "mode": "group", "origin_channel_id": "ch",
"origin_session_id": "s", "origin_content": "c", "origin_sender_name": "n",
"origin_sender_id": "u"}`
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "gpq_miss_q.json"), []byte(raw), 0o644)

	if got := LoadPendingInteraction("gpq_miss_q"); got != nil {
		t.Errorf("必填字段缺失应返回 nil, 实际 %+v", got)
	}
}

// TestUnmarshalJSON_null元数据 JSON null → nil map（Python None）
func TestUnmarshalJSON_null元数据(t *testing.T) {
	var pi PendingInteraction
	raw := `{"interaction_id":"a","mode":"dm","origin_channel_id":"c","origin_session_id":"s",
"origin_content":"c","origin_sender_name":"n","origin_sender_id":"u","question":"q",
"target_user_id":"t","origin_metadata":null}`
	if err := json.Unmarshal([]byte(raw), &pi); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	if pi.OriginMetadata != nil {
		t.Errorf("JSON null 应解为 nil map")
	}
}

// TestFindGroupPending_直达加载 ID 拼接正确 + 不做 status/TTL 过滤（completed 也返回）
func TestFindGroupPending_直达加载(t *testing.T) {
	setupInteractionsEnv(t)

	// completed 状态的文件（find_group_pending 不过滤）
	pi := newTestPI("gpq_sess-9_ou-user9")
	pi.Status = StatusCompleted
	pi.CreatedAt = 1.0 // 远古时间（不过滤 TTL）
	pi.Save()

	got := FindGroupPending("sess-9", "ou-user9")
	if got == nil {
		t.Fatal("completed/远古时间也应返回（不过滤 status/TTL）")
	}
	if got.InteractionID != "gpq_sess-9_ou-user9" {
		t.Errorf("ID 拼接应 gpq_{session}_{user}")
	}

	// 不存在 → nil
	if got2 := FindGroupPending("sess-none", "ou-none"); got2 != nil {
		t.Errorf("不存在应返回 nil")
	}
}

// TestFindAllGroupPending pending+未过期收集、completed/过期排除
func TestFindAllGroupPending(t *testing.T) {
	setupInteractionsEnv(t)

	// 应收集（CreatedAt 用当前时间——newTestPI 固定 1.7e9 已超 TTL 会误判过期）
	p1 := newTestPI("gpq_s1_u1")
	p1.CreatedAt = float64(time.Now().Unix()) - 10
	p1.Save()
	// completed → 排除
	p2 := newTestPI("gpq_s2_u2")
	p2.Status = StatusCompleted
	p2.Save()
	// 过期 → 排除
	p3 := newTestPI("gpq_s3_u3")
	p3.CreatedAt = float64(time.Now().Unix()) - DefaultTTL - 100
	p3.Save()
	// iact 前缀 → 不在 gpq_ glob 内，排除
	p4 := newTestPI("iact_ch_abcd")
	p4.Save()
	// 坏 JSON → 跳过
	dir := pathutil.AgentInteractionsDir()
	_ = os.WriteFile(filepath.Join(dir, "gpq_bad.json"), []byte("{not json"), 0o644)

	all := FindAllGroupPending()
	if len(all) != 1 {
		t.Fatalf("应只收集 1 个 pending 未过期 gpq, 实际 %d: %+v", len(all), all)
	}
	if all[0].InteractionID != "gpq_s1_u1" {
		t.Errorf("应收集 gpq_s1_u1, 实际 %s", all[0].InteractionID)
	}
}

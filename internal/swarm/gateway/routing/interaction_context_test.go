package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestFindPending_五层过滤顺序 ①status ②TTL触发删除 ③principal ④target ⑤取最旧
func TestFindPending_五层过滤顺序(t *testing.T) {
	setupInteractionsEnv(t)

	channelID := "feishu_enterprise:app1"
	principal := "ou-principal"

	// ① 非 pending → 跳过
	a := newTestPI("iact_" + channelID + "_aaaa")
	a.Status = StatusCompleted
	a.PrincipalID = principal
	a.Save()

	// ② TTL 过期 → 触发 Remove + 跳过
	b := newTestPI("iact_" + channelID + "_bbbb")
	b.PrincipalID = principal
	b.CreatedAt = float64(time.Now().Unix()) - DefaultTTL - 100
	b.Save()

	// ③ principal 不匹配 → 跳过（CreatedAt 须未过期，否则在 ② 就被拦截走不到 ③）
	c := newTestPI("iact_" + channelID + "_cccc")
	c.PrincipalID = "ou-other"
	c.CreatedAt = float64(time.Now().Unix()) - 30
	c.Save()

	// ④ target 不匹配 → 跳过（同上，须未过期）
	d := newTestPI("iact_" + channelID + "_dddd")
	d.PrincipalID = principal
	d.TargetUserID = "ou-somebody-else"
	d.CreatedAt = float64(time.Now().Unix()) - 20
	d.Save()

	// ⑤ 候选 1（较新）
	e := newTestPI("iact_" + channelID + "_eeee")
	e.PrincipalID = principal
	e.TargetUserID = principal
	e.CreatedAt = float64(time.Now().Unix()) - 10
	e.Save()
	// ⑤ 候选 2（较旧 → 应命中）
	f := newTestPI("gpq_sess-1_ou-zhangsan")
	f.PrincipalID = principal
	f.TargetUserID = principal
	f.CreatedAt = float64(time.Now().Unix()) - 50
	f.Save()

	got := FindPendingInteraction(channelID, principal)
	if got == nil {
		t.Fatal("应命中候选 2（最旧）")
	}
	if got.InteractionID != "gpq_sess-1_ou-zhangsan" {
		t.Errorf("应取最旧 gpq_sess-1_ou-zhangsan, 实际 %s", got.InteractionID)
	}

	// ② 的过期文件已被顺带删除
	fpB := filepath.Join(pathutil.AgentInteractionsDir(), "iact_"+channelID+"_bbbb.json")
	if _, err := os.Stat(fpB); !os.IsNotExist(err) {
		t.Errorf("过期文件应被 Remove")
	}
}

// TestFindPending_全部不匹配 全过滤后返回 nil
func TestFindPending_全部不匹配(t *testing.T) {
	setupInteractionsEnv(t)

	// 只有 principal 不匹配的文件（CreatedAt 未过期，走 ③ 分支被跳过）
	c := newTestPI("iact_feishu_enterprise:app1_cccc")
	c.PrincipalID = "ou-other"
	c.CreatedAt = float64(time.Now().Unix()) - 30
	c.Save()

	if got := FindPendingInteraction("feishu_enterprise:app1", "ou-principal"); got != nil {
		t.Errorf("全部不匹配应返回 nil")
	}
}

// TestFindPending_尾随cleanup FindPending 尾部无条件 CleanupExpired
func TestFindPending_尾随cleanup(t *testing.T) {
	setupInteractionsEnv(t)

	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)

	// 预置坏 JSON gpq 文件（两前缀扫描 parse 失败跳过，不影响主流程）
	_ = os.WriteFile(filepath.Join(dir, "gpq_badjson.json"), []byte("{not json"), 0o644)
	// 预置缺 created_at 的完整 iact 文件 → 尾部清理当 0 → 恒过期 → 删除
	// （主扫描：UnmarshalJSON 补 now → 不会在过滤链中被判过期；尾部清理走裸 dict → 删除）
	raw := `{"interaction_id":"iact_ch_zzzz","mode":"dm","origin_channel_id":"ch",
"origin_session_id":"s","origin_content":"c","origin_sender_name":"n",
"origin_sender_id":"u","question":"q","target_user_id":"ou-principal",
"origin_metadata":{},"principal_id":"ou-principal","status":"pending"}`
	_ = os.WriteFile(filepath.Join(dir, "iact_ch_zzzz.json"), []byte(raw), 0o644)

	_ = FindPendingInteraction("ch", "ou-principal")

	// 坏 JSON 文件保留（两处解析都跳过）
	if _, err := os.Stat(filepath.Join(dir, "gpq_badjson.json")); err != nil {
		t.Errorf("坏 JSON 文件不应被删除（解析失败跳过）")
	}
	// 缺 created_at 的文件被尾部清理删除（裸 dict 语义 created_at=0 → 恒过期）
	if _, err := os.Stat(filepath.Join(dir, "iact_ch_zzzz.json")); !os.IsNotExist(err) {
		t.Errorf("缺 created_at 文件应被尾部 CleanupExpired 删除（get 默认 0 → 过期）")
	}
}

// TestCleanupExpired 过期删/未过期留/坏文件跳过不删/返回计数
func TestCleanupExpired(t *testing.T) {
	setupInteractionsEnv(t)
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)

	writeFile := func(name, content string) {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	now := time.Now().Unix()

	// 过期（数字时间戳）→ 删
	writeFile("gpq_old1.json", fmt.Sprintf(`{"created_at": %d}`, now-int64(DefaultTTL)-100))
	// 未过期 → 留
	writeFile("gpq_new1.json", fmt.Sprintf(`{"created_at": %d}`, now-10))
	// 坏 JSON → 跳过不删
	writeFile("gpq_bad.json", "{not json")
	// 字符串时间戳可解析且过期 → 删
	writeFile("gpq_str1.json", `{"created_at": "1"}`)
	// 字符串时间戳无法解析 → 跳过不删
	writeFile("gpq_str2.json", `{"created_at": "abc"}`)
	// 缺 created_at → 当 0 → 恒过期 → 删
	writeFile("gpq_missing.json", `{"interaction_id": "x"}`)
	// 非数字非字符串类型（list）→ 跳过不删
	writeFile("gpq_list.json", `{"created_at": [1, 2]}`)

	count := CleanupExpired(DefaultTTL)

	if count != 3 {
		t.Errorf("应删除 3 个（gpq_old1/gpq_str1/gpq_missing）, 实际 %d", count)
	}
	for _, gone := range []string{"gpq_old1.json", "gpq_str1.json", "gpq_missing.json"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s 应被删除", gone)
		}
	}
	for _, keep := range []string{"gpq_new1.json", "gpq_bad.json", "gpq_str2.json", "gpq_list.json"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s 应保留: %v", keep, err)
		}
	}
}

// TestBuildResumeContent_DM模板 逐字节断言中文模板（提示词一比一纪律）
func TestBuildResumeContent_DM模板(t *testing.T) {
	pi := newTestPI("iact_ch_1")
	pi.Mode = "dm"
	pi.OriginContent = "帮我订明天的会议室"
	pi.OriginSenderName = "张三"
	pi.Question = "需要哪个楼层？"
	answer := "三楼"

	want := "[任务恢复] 之前的群聊请求：帮我订明天的会议室（来自 张三）\n" +
		"你之前的追问：需要哪个楼层？\n" +
		"principal 的回答：三楼\n" +
		"请综合「原始请求」和「principal 的回答」中的所有信息继续完成任务，" +
		"原始请求中已明确提供的信息直接使用即可，不要再次追问。" +
		"并将结果回复到群聊。"

	if got := pi.BuildResumeContent(answer); got != want {
		t.Errorf("DM 模板不匹配:\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildResumeContent_群聊模板 target_user_name 回退 "用户" 分支 + 显式名分支
func TestBuildResumeContent_群聊模板(t *testing.T) {
	// 显式名分支
	pi := newTestPI("gpq_s_u")
	pi.Mode = "group"
	pi.OriginContent = "订会议室"
	pi.OriginSenderName = "张三"
	pi.Question = "几点开始？"
	pi.TargetUserName = "李四"
	answer := "14点"

	want := "[任务恢复] 你正在处理的群聊请求：订会议室（来自 张三）\n" +
		"你之前追问了 李四：几点开始？\n" +
		"李四 的回答：14点\n" +
		"请综合「原始请求」和「用户的回答」中的所有信息来完成任务。" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问或要求补充。" +
		"现在请立即执行任务（如设置提醒、创建日程等），不要只是确认或记录信息，并将执行结果回复到群聊。"

	if got := pi.BuildResumeContent(answer); got != want {
		t.Errorf("群聊模板（显式名）不匹配:\n got: %q\nwant: %q", got, want)
	}

	// 回退 "用户" 分支（TargetUserName 为空）
	pi2 := newTestPI("gpq_s_u2")
	pi2.Mode = "group"
	pi2.OriginContent = "订会议室"
	pi2.OriginSenderName = "张三"
	pi2.Question = "几点开始？"
	pi2.TargetUserName = ""

	got2 := pi2.BuildResumeContent(answer)
	if !strings.Contains(got2, "你之前追问了 用户：几点开始？") {
		t.Errorf("空名应回退 '用户':\n%q", got2)
	}
	if !strings.Contains(got2, "用户 的回答：14点") {
		t.Errorf("空名应回退 '用户'（回答段）:\n%q", got2)
	}
}

// TestRemove_目录权限错误失败仅Warn interactions 路径被同名文件占据 → Stat 失败非 NotExist 分支
func TestRemove_目录权限错误失败仅Warn(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	dir := filepath.Join(tmp, "agent", "workspace", "interactions")
	_ = os.MkdirAll(filepath.Dir(dir), 0o755)
	_ = os.WriteFile(dir, []byte("occupied"), 0o644)

	pi := newTestPI("gpq_perm")
	pi.Remove() // Stat/Remove 失败分支，不 panic 即通过
}

// TestRemove_删除失败 Stat 成功但 os.Remove 失败（非空目录占据 .json 路径 → ENOTEMPTY）
func TestRemove_删除失败(t *testing.T) {
	setupInteractionsEnv(t)
	dir := pathutil.AgentInteractionsDir()
	// 文件路径本身是非空目录：os.Stat 成功，os.Remove 报 ENOTEMPTY（root 无法绕过，POSIX 语义）
	fp := filepath.Join(dir, "gpq_notempty.json")
	_ = os.MkdirAll(fp, 0o755)
	_ = os.WriteFile(filepath.Join(fp, "child"), []byte("x"), 0o644)

	pi := newTestPI("gpq_notempty")
	pi.Remove() // os.Remove 失败 → 仅 Warn 分支，不 panic 即通过

	if _, err := os.Stat(fp); err != nil {
		t.Errorf("非空目录删除应失败仍存在: %v", err)
	}
}

// TestLoad_读文件权限失败 ReadFile 返回非 NotExist 错误（目录占据 .json 路径 → EISDIR）
func TestLoad_读文件权限失败(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	// 制造 interaction_id+".json" 同名目录 → ReadFile 返回 EISDIR（非 NotExist）
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(filepath.Join(dir, "gpq_isdir.json"), 0o755)
	_ = os.MkdirAll(filepath.Join(tmp, "agent", "workspace"), 0o755)
	_ = os.WriteFile(filepath.Join(tmp, "x"), []byte(""), 0o644)

	if got := LoadPendingInteraction("gpq_isdir"); got != nil {
		t.Errorf("读目录失败应返回 nil, 实际 %v", got)
	}
}

// TestParseInteractionFile_读失败 glob 命中但读取失败（目录占据）
func TestParseInteractionFile_读失败(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(filepath.Join(dir, "gpq_isdir2"), 0o755)
	_ = os.MkdirAll(filepath.Join(tmp, "agent", "workspace"), 0o755)

	if got := parseInteractionFile(filepath.Join(dir, "gpq_isdir2")); got != nil {
		t.Errorf("读目录失败应返回 nil")
	}
}

// TestUnmarshalJSON_非法JSON 外层类型不匹配（合法 JSON 但字段类型错）→ alias 反序列化失败
//
// 注意：纯语法错误（"{not json"）被 json 包 checkValid 预检拦截，不会进入 UnmarshalJSON；
// 用数字赋给 string 字段触发 alias 解码 error，覆盖 UnmarshalJSON 内部 L422-424 分支。
func TestUnmarshalJSON_非法JSON(t *testing.T) {
	raw := `{"interaction_id":123}` // 合法 JSON，类型不匹配
	var pi PendingInteraction
	if err := json.Unmarshal([]byte(raw), &pi); err == nil {
		t.Errorf("字段类型不匹配应返回 error")
	}
}

// TestUnmarshalJSON_metadata非对象 origin_metadata 是字符串 → error（Python 重建时类型不匹配 TypeError）
func TestUnmarshalJSON_metadata非对象(t *testing.T) {
	var pi PendingInteraction
	raw := `{"interaction_id":"a","mode":"dm","origin_channel_id":"c","origin_session_id":"s",
"origin_content":"c","origin_sender_name":"n","origin_sender_id":"u","question":"q",
"target_user_id":"t","origin_metadata":"not-an-object"}`
	if err := json.Unmarshal([]byte(raw), &pi); err == nil {
		t.Errorf("origin_metadata 非对象应返回 error（对齐 Python TypeError 跳过文件）")
	}
}

// TestIndentJSON_非JSON输入 缩进失败退回紧凑形式
func TestIndentJSON_非JSON输入(t *testing.T) {
	out := indentJSON([]byte("{not json"))
	if string(out) != "{not json" {
		t.Errorf("非 JSON 输入应原样返回, 实际 %q", out)
	}
}

// TestSave_Encode失败保险 不可编码值（channel）→ Warn 分支不 panic
func TestSave_Encode失败保险(t *testing.T) {
	setupInteractionsEnv(t)
	pi := newTestPI("gpq_enc_fail")
	pi.OriginMetadata = map[string]any{"ch": make(chan int)} // chan 不可序列化
	pi.Save()                                                // Encode 失败分支 → Warn，不 panic
}

// TestCleanupExpired_读取失败 glob 命中目录名 → ReadFile 失败跳过
func TestCleanupExpired_读取失败(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(filepath.Join(dir, "gpq_isdir3.json"), 0o755) // 目录伪装 .json
	_ = os.MkdirAll(filepath.Join(tmp, "agent", "workspace"), 0o755)

	count := CleanupExpired(DefaultTTL)
	if count != 0 {
		t.Errorf("读取失败应跳过不删, 实际删了 %d 个", count)
	}
}

package discovery

import (
	"strings"
	"testing"
)

func cards(m map[string][2]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for k, v := range m {
		out[k] = map[string]string{"name": v[0], "description": v[1]}
	}
	return out
}

// TestConsumeIncrementalSkillReminder 首次全量视为 added、二次同基线无通知
func TestConsumeIncrementalSkillReminder(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	base := cards(map[string][2]string{"a": {"Alpha", "desc a"}, "b": {"Beta", "desc b"}})
	// Python 语义：首次 consume（无 initialize）建基线、不产出通知
	_ = InitializeIncrementalSkillNoticeState("s1", base)
	// 同基线 → 无通知
	r1, err := ConsumeIncrementalSkillReminder("s1", base, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != "" {
		t.Fatalf("同基线应无通知: %q", r1)
	}
	// 新增技能 → 通知含新增项
	extended := cards(map[string][2]string{"a": {"Alpha", "desc a"}, "b": {"Beta", "desc b"}, "c": {"Gamma", "desc c"}})
	r2, _ := ConsumeIncrementalSkillReminder("s1", extended, 4000)
	if !strings.Contains(r2, "Gamma") || strings.Contains(r2, "Alpha") {
		t.Fatalf("应只通知新增: %q", r2)
	}
}

// TestConsumeIncrementalSkillReminder_会话隔离 scope 独立
func TestConsumeIncrementalSkillReminder_会话隔离(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	base := cards(map[string][2]string{"a": {"Alpha", "d"}})
	// s1 建基线；s1 二次 → 无通知；s2 无基线 → 建基线亦无通知
	if err := InitializeIncrementalSkillNoticeState("s1", base); err != nil {
		t.Fatal(err)
	}
	r1, _ := ConsumeIncrementalSkillReminder("s1", base, 4000)
	if r1 != "" {
		t.Fatalf("s1 同基线应无通知: %q", r1)
	}
	r2, _ := ConsumeIncrementalSkillReminder("s2", base, 4000)
	if r2 != "" {
		t.Fatalf("s2 首建基线无通知（Python 语义）: %q", r2)
	}
	// s1 新增后通知、s2 无感知
	extended := cards(map[string][2]string{"a": {"Alpha", "d"}, "n": {"New", "d"}})
	r3, _ := ConsumeIncrementalSkillReminder("s1", extended, 4000)
	if !strings.Contains(r3, "New") {
		t.Fatalf("s1 新增应通知: %q", r3)
	}
	// s2 基线是首建时的 base，extended 对它也是新增（独立记账语义）
	r4, _ := ConsumeIncrementalSkillReminder("s2", extended, 4000)
	if !strings.Contains(r4, "New") {
		t.Fatalf("s2 独立基线应感知自己的新增: %q", r4)
	}
}

// TestConsumeIncrementalSkillReminder_toolName 隔离 tool_id 双维
func TestConsumeIncrementalSkillReminder_toolName(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	// toolID 直接验证（consume 入口固定 skill_index）
	id1, err := toolID("s1", "skill_index")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := toolID("s1", "other_tool")
	if id1 == id2 {
		t.Fatal("不同工具名应产生不同 tool_id")
	}
	if _, err := toolID("", "x"); err == nil {
		t.Fatal("空 scope 应报错")
	}
	if _, err := toolID("x", ""); err == nil {
		t.Fatal("空 tool 应报错")
	}
}

// TestConsumeIncrementalSkillReminder_失败重试 acknowledge 语义
func TestConsumeIncrementalSkillReminder_失败重试(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	base := cards(map[string][2]string{"a": {"Alpha", strings.Repeat("d", 200)}})
	_ = InitializeIncrementalSkillNoticeState("s1", cards(nil)) // 建空基线
	// 渲染超长 → 回落计数摘要（仍算投递成功）
	r1, _ := ConsumeIncrementalSkillReminder("s1", base, 60)
	if !strings.Contains(r1, "1 new skills are available") {
		t.Fatalf("超长应回落计数摘要: %q", r1)
	}
	// 投递成功 → 基线推进 → 二次无通知
	r2, _ := ConsumeIncrementalSkillReminder("s1", base, 4000)
	if r2 != "" {
		t.Fatalf("投递成功后不应重复: %q", r2)
	}
}

// TestConsumeIncrementalSkillReminder_maxChars 校验
func TestConsumeIncrementalSkillReminder_maxChars(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	if _, err := ConsumeIncrementalSkillReminder("s", cards(nil), 0); err == nil {
		t.Fatal("maxChars<=0 应报错")
	}
}

// TestNoticeState_LRU 淘汰最旧
func TestNoticeState_LRU(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	empty := cards(nil)
	for i := 0; i < maxSessionStates+10; i++ {
		id, _ := toolID(string(rune('a'+i%26))+string(rune('a'+i/26)), SkillIndexToolName)
		noticeStateOf(id, normalizeCards(empty))
	}
	noticeStatesMu.Lock()
	size := len(noticeStates)
	noticeStatesMu.Unlock()
	if size > maxSessionStates {
		t.Fatalf("应淘汰至 %d: %d", maxSessionStates, size)
	}
}

// TestNormalizeCards 归一化
func TestNormalizeCards(t *testing.T) {
	got := normalizeCards(map[string]map[string]string{
		"a": {"name": "", "description": strings.Repeat("x", 400)},
		"":  {"name": "skip", "description": "d"},
	})
	if len(got) != 1 {
		t.Fatalf("空 worker_id 应剔除: %d", len(got))
	}
	if got["a"].name != "a" {
		t.Fatalf("name 应回落 worker_id: %q", got["a"].name)
	}
	if len([]rune(got["a"].description)) > maxDescriptionChars {
		t.Fatalf("描述应截断: %d", len([]rune(got["a"].description)))
	}
	if !strings.HasSuffix(got["a"].description, "...") {
		t.Fatalf("截断应有占位符: %q", got["a"].description[len(got["a"].description)-3:])
	}
}

// TestSafeText 空白折叠与转义
func TestSafeText(t *testing.T) {
	if safeText("a\n  b", "") != "a b" {
		t.Fatalf("折叠不符: %q", safeText("a\n  b", ""))
	}
	if safeText("", "fallback") != "fallback" {
		t.Fatal("空回落")
	}
	if safeText("<x>", "") != "&lt;x&gt;" {
		t.Fatal("转义不符")
	}
}

// TestIncrementalNoticeSession_Append 预算与放弃
func TestIncrementalNoticeSession_Append(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	sess, err := NewIncrementalNoticeSession("s1", cards(nil), 4000)
	if err != nil {
		t.Fatal(err)
	}
	// 无 added → 原样
	out, notice := sess.Append("base", cards(nil), 100)
	if out != "base" || notice != "" {
		t.Fatalf("无 added 应原样: %q %q", out, notice)
	}
	// 有 added → 拼接（构造时基线为空 → 新卡即增量；预算放宽）
	out, notice = sess.Append("base", cards(map[string][2]string{"n": {"New", "d"}}), 300)
	if !strings.Contains(out, "New") || notice == "" {
		t.Fatalf("应拼接通知: %q", out)
	}
	// 新会话重建后再来同卡 → 无通知
	sess2, _ := NewIncrementalNoticeSession("s1", cards(map[string][2]string{"n": {"New", "d"}}), 4000)
	out2, notice2 := sess2.Append("base", cards(map[string][2]string{"n": {"New", "d"}, "m": {"More", "d"}}), 1000)
	if !strings.Contains(out2, "More") || notice2 == "" {
		t.Fatalf("新卡应通知: %q", out2)
	}
}

// TestIncrementalNoticeSession_Append_预算放弃 超预算不投递
func TestIncrementalNoticeSession_Append_预算放弃(t *testing.T) {
	defer ClearIncrementalSkillNoticeStates()
	sess, err := NewIncrementalNoticeSession("sb", cards(nil), 4000)
	if err != nil {
		t.Fatal(err)
	}
	out, notice := sess.Append("base", cards(map[string][2]string{"n": {"New", "d"}}), 10)
	if out != "base" || notice != "" {
		t.Fatalf("超预算应放弃: %q %q", out, notice)
	}
	// 放弃后 added 保留：预算放宽再试 → 通知
	out2, notice2 := sess.Append("base", cards(map[string][2]string{"n": {"New", "d"}}), 1000)
	if notice2 == "" || !strings.Contains(out2, "New") {
		t.Fatalf("重试应成功: %q %q", out2, notice2)
	}
}

// TestRenderReminder 排序与回落
func TestRenderReminder(t *testing.T) {
	added := [][2]string{{"Zeta", "z"}, {"Alpha", "a"}}
	got := renderReminder(added, 4000, "skill_index")
	if !strings.Contains(got, "- Alpha: a") || strings.Index(got, "Alpha") > strings.Index(got, "Zeta") {
		t.Fatalf("应按 name.casefold 排序: %q", got)
	}
	got = renderReminder(added, 40, "skill_index")
	if !strings.Contains(got, "2 new skills") {
		t.Fatalf("超长应回落计数: %q", got)
	}
	if renderReminder(nil, 100, "x") != "" {
		t.Fatal("空 added 应空串")
	}
}

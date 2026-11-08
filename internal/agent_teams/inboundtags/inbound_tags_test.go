package agent_teams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// goldenPath 返回某个 golden 文件的绝对路径。
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", "inbound_render", name+".golden")
}

// assertGolden 逐字节比对代码输出与上游 golden。
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	want, err := os.ReadFile(goldenPath(t, name))
	if err != nil {
		t.Fatalf("读取 golden %s 失败：%v", name, err)
	}
	if got != string(want) {
		t.Errorf("golden %s 不一致\n得到: %q\n期望: %q", name, got, string(want))
	}
}

// TestRenderInbound_直发消息 验证最基本的 <team-inbound> 结构。
// Python: render_inbound (inbound_render.py L115-157)
func TestRenderInbound_直发消息(t *testing.T) {
	got := RenderInbound("你帮我看看这个接口", InboundOptions{
		Sender:    "dev-1",
		MessageID: "m1",
		MsgType:   InboundTypeDirect,
		TimeInfo:  "2026-05-28 14:30:05 +08:00",
	})
	assertGolden(t, "render_inbound_direct", got)
}

// TestRenderInbound_广播消息 验证 type="broadcast" 契约 token。
func TestRenderInbound_广播消息(t *testing.T) {
	got := RenderInbound("全体注意", InboundOptions{
		Sender: "dev-9", MessageID: "m4", MsgType: InboundTypeBroadcast,
		TimeInfo: "2026-05-28 14:32:00 +08:00",
	})
	if !strings.Contains(got, `type="broadcast"`) {
		t.Errorf("广播消息应为 type=\"broadcast\"：%q", got)
	}
}

// TestRenderInbound_带回复提示 验证 <team-note> 嵌套在 </team-inbound> 之前。
func TestRenderInbound_带回复提示(t *testing.T) {
	noteText, err := schema.ReplyHintFor("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	got := RenderInbound("你帮我看看这个接口", InboundOptions{
		Sender:    "dev-1",
		MessageID: "m1",
		MsgType:   InboundTypeDirect,
		TimeInfo:  "2026-05-28 14:30:05 +08:00",
		NoteKind:  "reply-hint",
		NoteText:  noteText,
	})
	assertGolden(t, "render_inbound_with_reply_hint", got)
}

// TestRenderInbound_人类成员通知 验证 for="controller" 与 hitt-silence note 的组合。
func TestRenderInbound_人类成员通知(t *testing.T) {
	silence, err := schema.T("hitt.silence_note")
	if err != nil {
		t.Fatal(err)
	}
	got := RenderInbound("请审批", InboundOptions{
		Sender:        "dev-2",
		MessageID:     "m2",
		MsgType:       InboundTypeBroadcast,
		TimeInfo:      "2026-05-28 14:31:00 +08:00",
		ForController: true,
		NoteKind:      "hitt-silence",
		NoteText:      silence,
	})
	assertGolden(t, "render_inbound_hitt", got)
}

// TestRenderInbound_正文转义 验证正文只转 & < >，不转引号。
func TestRenderInbound_正文转义(t *testing.T) {
	got := RenderInbound(`a < b & c > d "quote" 'single'`, InboundOptions{
		Sender: "dev-1", MessageID: "m1", MsgType: InboundTypeDirect,
		TimeInfo: "t",
	})
	want := "a &lt; b &amp; c &gt; d \"quote\" 'single'"
	if !strings.Contains(got, want) {
		t.Errorf("正文转义不符\n得到: %q\n应包含: %q", got, want)
	}
}

// TestRenderInbound_属性转义 验证属性值里的引号被转义（偏差 D-1：' → &#39;）。
func TestRenderInbound_属性转义(t *testing.T) {
	got := RenderInbound("x", InboundOptions{
		Sender: `a"b'c`, MessageID: "m1", MsgType: InboundTypeDirect,
		TimeInfo: "t",
	})
	if !strings.Contains(got, `from="a&#34;b&#39;c"`) {
		t.Errorf("属性转义不符（期望 html.EscapeString 的 &#34; 与 &#39;）：%q", got)
	}
}

// TestRenderInbound_只给NoteKind不给NoteText 验证 note 不渲染。
func TestRenderInbound_只给NoteKind不给NoteText(t *testing.T) {
	got := RenderInbound("x", InboundOptions{
		Sender: "s", MessageID: "m", MsgType: InboundTypeDirect,
		TimeInfo: "t", NoteKind: "reply-hint",
	})
	if strings.Contains(got, "<team-note") {
		t.Errorf("只有 NoteKind 时不应渲染 note：%q", got)
	}
}

// TestRenderInbound_只给NoteText不给NoteKind 验证 note 不渲染。
func TestRenderInbound_只给NoteText不给NoteKind(t *testing.T) {
	got := RenderInbound("x", InboundOptions{
		Sender: "s", MessageID: "m", MsgType: InboundTypeDirect,
		TimeInfo: "t", NoteText: "hi",
	})
	if strings.Contains(got, "<team-note") {
		t.Errorf("只有 NoteText 时不应渲染 note：%q", got)
	}
}

// TestRenderControllerInput_无messageId无time 验证控制者指令只有 from 与 type 两个属性。
func TestRenderControllerInput_无messageId无time(t *testing.T) {
	got := RenderControllerInput("转告 dev-1 已完成")
	assertGolden(t, "render_controller_input", got)
}

// TestRenderClock 验证自闭合 <team-clock now="..."/>。
func TestRenderClock(t *testing.T) {
	got := RenderClock("2026-05-28 14:35:00 +08:00")
	assertGolden(t, "render_clock", got)
}

// TestRenderEvent_基础 验证只有 kind 属性。
func TestRenderEvent_基础(t *testing.T) {
	got := RenderEvent("all-done", "任务看板上已无未完成任务", EventOptions{})
	assertGolden(t, "render_event_plain", got)
}

// TestRenderEvent_带taskId 验证 task_id 出现在 kind 之后。
func TestRenderEvent_带taskId(t *testing.T) {
	taskID := "t1"
	got := RenderEvent("task-assigned", "请处理任务 t1", EventOptions{TaskID: &taskID})
	assertGolden(t, "render_event_with_task_id", got)
}

// TestRenderEvent_taskId为空串 验证 *string 语义：空串仍渲染 task_id=""。
func TestRenderEvent_taskId为空串(t *testing.T) {
	empty := ""
	got := RenderEvent("task-assigned", "body", EventOptions{TaskID: &empty})
	if !strings.Contains(got, `task_id=""`) {
		t.Errorf("显式空串应渲染 task_id=\"\"：%q", got)
	}
}

// TestRenderEvent_taskId为nil 验证 nil 不渲染 task_id 属性。
func TestRenderEvent_taskId为nil(t *testing.T) {
	got := RenderEvent("all-done", "body", EventOptions{})
	if strings.Contains(got, "task_id") {
		t.Errorf("nil TaskID 不应渲染 task_id：%q", got)
	}
}

// TestRenderEvent_带note 验证 note 嵌套在 </team-event> 之前。
func TestRenderEvent_带note(t *testing.T) {
	got := RenderEvent("roster", "body", EventOptions{
		NoteKind: "roster-change", NoteText: "公告",
	})
	want := "<team-note kind=\"roster-change\">\n公告\n</team-note>\n</team-event>"
	if !strings.HasSuffix(got, want) {
		t.Errorf("note 应嵌套为最后一个子元素\n得到尾部: %q", got[max(0, len(got)-80):])
	}
}

// TestRenderTeamContext 验证 <team-context> 无属性。
func TestRenderTeamContext(t *testing.T) {
	got := RenderTeamContext("# 成员身份\n\n你的 member_name: dev-1")
	assertGolden(t, "render_team_context", got)
}

// TestRenderTeamContextWithIdentity_有转换 验证 <identity-conversion> 嵌套在 <identity> 内。
func TestRenderTeamContextWithIdentity_有转换(t *testing.T) {
	conv := prompts.BuildIdentityConversion("dev-0", "dev-1", "cn")
	got := RenderTeamContextWithIdentity("# 成员身份\n\n你的 member_name: dev-1", &conv, nil)
	assertGolden(t, "render_team_context_with_conversion", got)
}

// TestRenderTeamContextWithIdentity_无转换 验证只有 <identity> 与 info 兄弟。
func TestRenderTeamContextWithIdentity_无转换(t *testing.T) {
	info := "# 团队信息\n\n- team_name: t1"
	got := RenderTeamContextWithIdentity("# 成员身份\n\n你的 member_name: dev-1", nil, &info)
	assertGolden(t, "render_team_context_with_identity", got)
}

// TestRenderTeamContextWithIdentity_空identity仍渲染空元素 验证本函数**不**做空值保护。
// 上游 render_team_context_with_identity (inbound_render.py L293-300) 无条件生成
// <identity> 元素，空保护由调用方 _compose_blocks 负责（它只在 identity_body 非空
// 时才走 fork 分支）。Go 侧照抄同一分工：本函数不兜底，避免与上游字节不一致。
func TestRenderTeamContextWithIdentity_空identity仍渲染空元素(t *testing.T) {
	info := "# 团队信息\n\n- team_name: t1"
	got := RenderTeamContextWithIdentity("", nil, &info)
	if !strings.Contains(got, "<identity>\n\n</identity>") {
		t.Errorf("空 identityBody 上游仍渲染空 <identity>，本函数应逐字对齐：%q", got)
	}
	if !strings.Contains(got, info) {
		t.Errorf("应包含 info 正文：%q", got)
	}
}

// TestRenderTeamPolicy_特殊字符不转义 验证 Markdown 正文原样插入。
func TestRenderTeamPolicy_特殊字符不转义(t *testing.T) {
	got := RenderTeamPolicy("# 团队准则\n\n用 & 表示同时满足", "team_tools", "工具名不带命名空间前缀")
	assertGolden(t, "render_team_policy", got)
}

// TestRenderTeamPolicyInline 验证无属性、无 note 的内联版。
func TestRenderTeamPolicyInline(t *testing.T) {
	got := RenderTeamPolicyInline("# 团队准则\n\n- 先看板")
	assertGolden(t, "render_team_policy_inline", got)
}

// TestDropSupersededSnapshots_只留最新看板 验证同名快照只保留最后一个，其余原样。
func TestDropSupersededSnapshots_只留最新看板(t *testing.T) {
	parts := []string{
		"<team-event kind=\"roster-change\">\nA\n</team-event>",
		"<team-event kind=\"task-board\">\nB1\n</team-event>",
		"<team-clock now=\"x\"/>",
		"<team-event kind=\"task-board\">\nB2\n</team-event>",
	}
	got := DropSupersededSnapshots(parts)
	assertGolden(t, "drop_superseded_snapshots", strings.Join(got, "\n"))
}

// TestSnapshotKindOf 验证只有完整快照事件才被识别（大小写/前后缀严格）。
func TestSnapshotKindOf(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"<team-event kind=\"task-board\">\nX\n</team-event>", "task-board"},
		{"  <team-event kind=\"task-board\">\nX\n</team-event>\n", "task-board"},
		{"<team-event kind=\"roster-change\">\nX\n</team-event>", ""},
		{"<team-event kind=\"task-board\">\nX\n</team-event>\nextra", ""},
		{"<team-clock now=\"x\"/>", ""},
	}
	for _, c := range cases {
		if got := snapshotKindOf(c.text); got != c.want {
			t.Errorf("snapshotKindOf(%.40q) = %q，期望 %q", c.text, got, c.want)
		}
	}
}

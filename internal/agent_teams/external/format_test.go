package external

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ts20260528 2026-05-28 14:30:05 +08:00 的毫秒 epoch（与 gen_golden.py 一致）
const ts20260528 int64 = 1779949805000

// goldenZone golden 生成时所处的时区。
// gen_golden.py 固定 TZ=Asia/Shanghai，而渲染默认取运行环境本地时区；
// CI runner 是 UTC，不固定时区就会把 +08:00 渲染成 +00:00 导致比对失败。
var goldenZone = time.FixedZone("CST", 8*3600)

// goldenPath 返回 golden 绝对路径（与 inbound_render 共用目录）。
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "testdata", "inbound_render", name+".golden")
}

// assertGolden 逐字节比对。
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

// fakeMessage 测试用消息行，实现 MessageLike。
type fakeMessage struct {
	messageID   string
	fromMember  string
	content     string
	broadcast   bool
	timestampMs int64
}

func (f fakeMessage) MessageID() string      { return f.messageID }
func (f fakeMessage) FromMemberName() string { return f.fromMember }
func (f fakeMessage) Content() string        { return f.content }
func (f fakeMessage) Broadcast() bool        { return f.broadcast }
func (f fakeMessage) TimestampMs() int64     { return f.timestampMs }

// TestRenderMessage_直发普通成员 验证普通成员收到 reply-hint note（golden）。
func TestRenderMessage_直发普通成员(t *testing.T) {
	got, err := RenderMessage(fakeMessage{
		messageID: "m1", fromMember: "dev-1", content: "你帮我看看", timestampMs: ts20260528,
	}, RenderMessageOptions{Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "render_message_direct", got)
}

// TestRenderMessage_人类成员 验证 for="controller" + hitt-silence note（golden）。
func TestRenderMessage_人类成员(t *testing.T) {
	got, err := RenderMessage(fakeMessage{
		messageID: "m1", fromMember: "dev-1", content: "你帮我看看", timestampMs: ts20260528,
	}, RenderMessageOptions{IsHumanAgent: true, Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "render_message_human", got)
}

// TestRenderMessage_带body模板 验证用 body 替代 msg.Content 且**无** reply-hint note。
func TestRenderMessage_带body模板(t *testing.T) {
	body := "[框架模板] 请提交计划"
	got, err := RenderMessage(fakeMessage{
		messageID: "m1", fromMember: "dev-1", content: "原始内容", timestampMs: ts20260528,
	}, RenderMessageOptions{Body: &body, Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "render_message_with_body", got)
}

// TestRenderMessage_user发来 验证 note 走 reply_hint_user 专用文案（golden）。
func TestRenderMessage_user发来(t *testing.T) {
	got, err := RenderMessage(fakeMessage{
		messageID: "m3", fromMember: "user", content: "你帮我看看", timestampMs: ts20260528,
	}, RenderMessageOptions{Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "render_message_user", got)
}

// TestRenderMessage_广播 验证 type="broadcast"。
func TestRenderMessage_广播(t *testing.T) {
	got, err := RenderMessage(fakeMessage{
		messageID: "m4", fromMember: "dev-9", content: "全体注意", broadcast: true, timestampMs: ts20260528,
	}, RenderMessageOptions{Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	if !stringsContains(got, `type="broadcast"`) {
		t.Errorf("广播消息应为 type=\"broadcast\"：%q", got)
	}
}

// TestRenderMessages_批量 验证多条之间用 \n\n 连接（golden）。
func TestRenderMessages_批量(t *testing.T) {
	got, err := RenderMessages([]MessageLike{
		fakeMessage{messageID: "m1", fromMember: "dev-1", content: "你帮我看看", timestampMs: ts20260528},
		fakeMessage{messageID: "m3", fromMember: "user", content: "你帮我看看", timestampMs: ts20260528},
	}, RenderMessagesOptions{Location: goldenZone})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "render_messages_batch", got)
}

// stringsContains 避免引入 strings 导入
func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

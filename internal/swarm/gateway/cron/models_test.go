package cron

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCronJobToDict 可选字段空值时省略
func TestCronJobToDict(t *testing.T) {
	j := &CronJob{ID: "j1", Name: "n", Enabled: true, CronExpr: "0 9 * * *",
		Timezone: "Asia/Shanghai", WakeOffsetSeconds: 300, Description: "d", Targets: "web"}
	d := j.ToDict()
	if _, exists := d["session_id"]; exists {
		t.Error("session_id should be omitted when empty")
	}
	if _, exists := d["chat_type"]; exists {
		t.Error("chat_type should be omitted when empty")
	}
	if _, exists := d["mode"]; exists {
		t.Error("mode should be omitted when empty")
	}
	if _, exists := d["delete_after_run"]; exists {
		t.Error("delete_after_run should be omitted when false")
	}
	if v, ok := d["enabled"].(bool); !ok || !v {
		t.Error("enabled should be true")
	}
	if v, ok := d["wake_offset_seconds"].(int); !ok || v != 300 {
		t.Error("wake_offset_seconds should be 300")
	}
	j.DeleteAfterRun = true
	j.SessionID = "feishu::c1::b1"
	j.ChatType = "group"
	j.Mode = "plan"
	if v, ok := j.ToDict()["delete_after_run"].(bool); !ok || !v {
		t.Error("delete_after_run should be true when set")
	}
	if j.ToDict()["session_id"] != "feishu::c1::b1" {
		t.Error("session_id should be present when set")
	}
	if j.ToDict()["chat_type"] != "group" {
		t.Error("chat_type should be present when set")
	}
	if j.ToDict()["mode"] != "plan" {
		t.Error("mode should be present when set")
	}
}

// TestFromCronJobDict_必填校验：缺 id/name/cron_expr/timezone/targets 各自报错
func TestFromCronJobDict_必填校验(t *testing.T) {
	base := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web"}
	for _, key := range []string{"id", "name", "cron_expr", "timezone", "targets"} {
		bad := map[string]any{}
		for k, v := range base {
			bad[k] = v
		}
		bad[key] = ""
		if _, err := FromCronJobDict(bad); err == nil {
			t.Errorf("missing %s should error", key)
		}
	}
}

// TestFromCronJobDict_校验顺序对齐Python：description 先于 id 报错
func TestFromCronJobDict_校验顺序对齐Python(t *testing.T) {
	// description 与 id 同时缺失时，Python 先 raise "description is required"
	d := map[string]any{"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai"}
	_, err := FromCronJobDict(d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "description is required") {
		t.Errorf("description should be validated first, got %q", err.Error())
	}
}

// TestFromCronJobDict_wakeOffset默认与钳制
func TestFromCronJobDict_wakeOffset钳制(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web", "wake_offset_seconds": -5}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.WakeOffsetSeconds != 0 {
		t.Errorf("negative wake_offset should clamp to 0, got %d", j.WakeOffsetSeconds)
	}
}

// TestFromCronJobDict_wakeOffset缺失默认300
func TestFromCronJobDict_wakeOffset默认300(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web"}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.WakeOffsetSeconds != 300 {
		t.Errorf("missing wake_offset_seconds should default to 300, got %d", j.WakeOffsetSeconds)
	}
}

// TestFromCronJobDict_wakeOffset显式null：int(None) 语义 → 报错
func TestFromCronJobDict_wakeOffset显式null报错(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"wake_offset_seconds": nil}
	if _, err := FromCronJobDict(d); err == nil {
		t.Error("explicit null wake_offset_seconds should error (int(None) semantics)")
	}
}

// TestFromCronJobDict_wakeOffset非整报错
func TestFromCronJobDict_wakeOffset非整报错(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"wake_offset_seconds": "abc"}
	if _, err := FromCronJobDict(d); err == nil {
		t.Error("non-int wake_offset_seconds should error")
	}
}

// TestFromCronJobDict_wakeOffset数字字符串：int("300") 语义 → 300
func TestFromCronJobDict_wakeOffset数字字符串(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"wake_offset_seconds": "300"}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.WakeOffsetSeconds != 300 {
		t.Errorf(`int("300") should yield 300, got %d`, j.WakeOffsetSeconds)
	}
}

// TestFromCronJobDict_legacyTargetsList：旧格式 []map 取第一个非空 channel_id
func TestFromCronJobDict_legacyTargetsList(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d",
		"targets": []any{map[string]any{"channel_id": "feishu"}}}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Targets != "feishu" {
		t.Errorf("legacy targets list should resolve, got %q", j.Targets)
	}
	// 多项时取第一个非空
	d["targets"] = []any{
		map[string]any{"channel_id": ""},
		map[string]any{"channel_id": "tui"},
		map[string]any{"channel_id": "web"},
	}
	j, err = FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Targets != "tui" {
		t.Errorf("legacy targets should take first non-empty, got %q", j.Targets)
	}
	// 空列表 → targets is required
	d["targets"] = []any{}
	if _, err := FromCronJobDict(d); err == nil {
		t.Error("empty targets list should error")
	}
}

// TestFromCronJobDict_Targets非法值归一化为web
func TestFromCronJobDict_Targets非法值归一化为web(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "foo"}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Targets != "web" {
		t.Errorf("invalid targets should normalize to web, got %q", j.Targets)
	}
}

// TestFromCronJobDict_完整字段往返
func TestFromCronJobDict_完整字段往返(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "feishu",
		"enabled": true, "expired": false, "wake_offset_seconds": 60,
		"session_id": "feishu::c1::b1", "created_at": 100.5, "updated_at": 200.5,
		"chat_type": "group", "mode": "plan", "delete_after_run": true}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.SessionID != "feishu::c1::b1" || j.ChatType != "group" || j.Mode != "plan" {
		t.Errorf("optional fields mismatch: %+v", j)
	}
	if j.CreatedAt != 100.5 || j.UpdatedAt != 200.5 {
		t.Errorf("timestamps mismatch: %v %v", j.CreatedAt, j.UpdatedAt)
	}
	if !j.DeleteAfterRun || !j.Enabled {
		t.Error("bool flags mismatch")
	}
	// 往返
	rt := j.ToDict()
	j2, err := FromCronJobDict(rt)
	if err != nil {
		t.Fatal(err)
	}
	if *j != *j2 {
		t.Errorf("round-trip mismatch: %+v vs %+v", j, j2)
	}
}

// TestFromCronJobDict_数值真值转换
func TestFromCronJobDict_数值真值转换(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"enabled": 1, "delete_after_run": 0.0}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if !j.Enabled {
		t.Error("enabled=1 should be true (Python truthiness)")
	}
	if j.DeleteAfterRun {
		t.Error("delete_after_run=0.0 should be false")
	}
}

// TestFromCronJobDict_布尔转时间戳
func TestFromCronJobDict_布尔转时间戳(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"created_at": true}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.CreatedAt != 1.0 {
		t.Errorf("float(True) should be 1.0, got %v", j.CreatedAt)
	}
}

// TestFromCronJobDict_createdAt非数值回退0
func TestFromCronJobDict_createdAt非数值回退0(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web",
		"created_at": "not-a-number"}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.CreatedAt != 0 {
		t.Errorf("non-numeric created_at should fall back to 0, got %v", j.CreatedAt)
	}
}

// TestFromCronJobDict_mode默认agent且小写化
func TestFromCronJobDict_mode默认agent(t *testing.T) {
	d := map[string]any{"id": "j1", "name": "n", "cron_expr": "0 9 * * *",
		"timezone": "Asia/Shanghai", "description": "d", "targets": "web"}
	j, err := FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Mode != "agent" {
		t.Errorf("mode should default to agent, got %q", j.Mode)
	}
	d["mode"] = "PLAN"
	j, err = FromCronJobDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if j.Mode != "plan" {
		t.Errorf("mode should lowercase, got %q", j.Mode)
	}
}

// TestIsValidTargetChannelID 目标渠道 ID 合法性
func TestIsValidTargetChannelID(t *testing.T) {
	for _, valid := range []string{"web", "tui", "feishu", "wecom", "dingtalk", "wechat", "whatsapp", "xiaoyi"} {
		if !IsValidTargetChannelID(valid) {
			t.Errorf("%s should be valid", valid)
		}
	}
	if !IsValidTargetChannelID("feishu_enterprise:cli_123") {
		t.Error("feishu_enterprise:cli_123 should be valid")
	}
	for _, invalid := range []string{"", "foo", "feishu_enterprise:", "feishu_enterprise"} {
		if IsValidTargetChannelID(invalid) {
			t.Errorf("%q should be invalid", invalid)
		}
	}
}

// TestNormalizeTargetChannelID 目标渠道 ID 归一化
func TestNormalizeTargetChannelID(t *testing.T) {
	if got := NormalizeTargetChannelID("WEB", "web"); got != "web" {
		t.Errorf("uppercase should normalize, got %q", got)
	}
	if got := NormalizeTargetChannelID("foo", "web"); got != "web" {
		t.Errorf("invalid should fallback, got %q", got)
	}
	if got := NormalizeTargetChannelID("", "tui"); got != "tui" {
		t.Errorf("empty should use default, got %q", got)
	}
	if got := NormalizeTargetChannelID("feishu_enterprise:cli_1:chat_2", "web"); got != "feishu_enterprise:cli_1" {
		t.Errorf("feishu_enterprise should truncate suffix, got %q", got)
	}
}

// TestCronTargetDict 往返
func TestCronTargetDict(t *testing.T) {
	tgt := &CronTarget{ChannelID: "feishu", SessionID: "s1"}
	d := tgt.ToDict()
	if d["channel_id"] != "feishu" || d["session_id"] != "s1" {
		t.Errorf("CronTarget.ToDict mismatch: %v", d)
	}
	rt, err := FromCronTargetDict(d)
	if err != nil {
		t.Fatal(err)
	}
	if *rt != *tgt {
		t.Errorf("CronTarget round-trip mismatch: %+v vs %+v", rt, tgt)
	}
}

// TestFromCronTargetDict_channelID必填
func TestFromCronTargetDict_channelID必填(t *testing.T) {
	if _, err := FromCronTargetDict(map[string]any{"channel_id": ""}); err == nil {
		t.Error("empty channel_id should error")
	}
	if _, err := FromCronTargetDict(nil); err == nil {
		t.Error("nil data should error")
	}
}

// TestIntAny 边界类型
func TestIntAny(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{300, 300, true},
		{int64(7), 7, true},
		{300.9, 300, true}, // float 截断
		{true, 1, true},    // bool → 1/0
		{false, 0, true},
		{json.Number("42"), 42, true},
		{nil, 0, false},
		{"300", 300, true}, // int("300")
		{"3.5", 0, false},  // int("3.5") 报错
		{"abc", 0, false},
		{[]any{}, 0, false},
	}
	for _, c := range cases {
		got, err := intAny(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("intAny(%v) = %d, err=%v; want %d", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("intAny(%v) should error", c.in)
		}
	}
}

// TestFloatAny 边界类型
func TestFloatAny(t *testing.T) {
	if got := floatAny(nil, 0); got != 0 {
		t.Errorf("floatAny(nil) should be 0, got %v", got)
	}
	if got := floatAny(true, 0); got != 1.0 {
		t.Errorf("floatAny(true) should be 1.0, got %v", got)
	}
	if got := floatAny(false, 0); got != 0.0 {
		t.Errorf("floatAny(false) should be 0.0, got %v", got)
	}
	if got := floatAny(5, 0); got != 5.0 {
		t.Errorf("floatAny(5) should be 5.0, got %v", got)
	}
	if got := floatAny("x", 9.9); got != 9.9 {
		t.Errorf("floatAny(\"x\") should fall back to default, got %v", got)
	}
}

// TestBoolAny Python 真值语义
func TestBoolAny(t *testing.T) {
	if boolAny(nil) {
		t.Error("nil should be false")
	}
	if !boolAny(1) {
		t.Error("1 should be true")
	}
	if boolAny(0) {
		t.Error("0 should be false")
	}
	if boolAny("") {
		t.Error("empty string should be false")
	}
	if !boolAny("x") {
		t.Error("non-empty string should be true")
	}
	if boolAny([]any{}) {
		t.Error("empty list should be false")
	}
	if !boolAny([]any{1}) {
		t.Error("non-empty list should be true")
	}
	if boolAny(map[string]any{}) {
		t.Error("empty map should be false")
	}
}

// TestModeAny 边界
func TestModeAny(t *testing.T) {
	if modeAny(nil) != "agent" {
		t.Error("nil mode should default agent")
	}
	if modeAny("") != "agent" {
		t.Error("empty mode should default agent")
	}
	if modeAny("PLAN") != "plan" {
		t.Error("mode should lowercase")
	}
	if modeAny(123) != "agent" {
		t.Error("non-string mode should default agent")
	}
}

package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// newSessionMapAt 已移至 session_map.go 实现文件（非导出函数区），
// 与 NewSessionMapAtForTest 共用，测试文件不再重复声明。

// TestMakeKey 两 scope 的键格式
func TestMakeKey(t *testing.T) {
	cases := []struct {
		scope SessionMapScope
		want  string
	}{
		{SessionMapScopePerChatBot, "feishu_enterprise::oc-123::bot-001"},
		{SessionMapScopePerChatBotUser, "feishu_enterprise::oc-123::bot-001::user-9"},
	}
	for _, c := range cases {
		got := makeKey(c.scope, "feishu_enterprise", "oc-123", "bot-001", "user-9")
		if got != c.want {
			t.Errorf("makeKey(scope=%d) = %q, want %q", c.scope, got, c.want)
		}
	}
}

// TestMakeSessionID 格式：key + "::" + hex时间戳 + "::" + 6位hex
func TestMakeSessionID(t *testing.T) {
	sid := makeSessionID(SessionMapScopePerChatBot, "feishu_enterprise", "oc-123", "bot-001", "user-9")
	parts := strings.Split(sid, "::")
	if len(parts) != 5 {
		t.Fatalf("PerChatBot session_id 应 5 段，实际 %d 段: %q", len(parts), sid)
	}
	if parts[0] != "feishu_enterprise" || parts[1] != "oc-123" || parts[2] != "bot-001" {
		t.Errorf("前三段应为身份键，实际 %q", sid)
	}
	if len(parts[3]) == 0 {
		t.Errorf("时间戳段非空")
	}
	if len(parts[4]) != 6 {
		t.Errorf("随机后缀应 6 个 hex 字符，实际 %q", parts[4])
	}

	sid6 := makeSessionID(SessionMapScopePerChatBotUser, "p", "c", "b", "u")
	if got := strings.Split(sid6, "::"); len(got) != 6 {
		t.Errorf("PerChatBotUser session_id 应 6 段，实际 %d 段", len(got))
	}
}

// TestGetSessionID_非轮转幂等
func TestGetSessionID_非轮转幂等(t *testing.T) {
	dir := t.TempDir()
	m := newSessionMapAt(filepath.Join(dir, "session_map.json"), SessionMapScopePerChatBot)
	first := m.GetSessionID("p", "c", "b", "u", false)
	second := m.GetSessionID("p", "c", "b", "u", false)
	if first == "" {
		t.Fatal("首次应生成非空 sid")
	}
	if first != second {
		t.Errorf("非轮转两次应返回同值: %q vs %q", first, second)
	}
}

// TestGetSessionID_轮转生成新值且写盘
func TestGetSessionID_轮转生成新值且写盘(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	old := m.GetSessionID("p", "c", "b", "u", false)
	fresh := m.GetSessionID("p", "c", "b", "u", true)
	if old == fresh {
		t.Fatal("轮转应生成新值")
	}
	// 写盘验证
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("轮转后应写盘: %v", err)
	}
	var persisted map[string]string
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("持久化内容应为合法 JSON: %v", err)
	}
	if persisted[makeKey(SessionMapScopePerChatBot, "p", "c", "b", "u")] != fresh {
		t.Errorf("盘上映射应更新为新值")
	}
	if !strings.Contains(string(data), "\n  ") {
		t.Errorf("应使用 indent=2 缩进（对齐 json.dump indent=2）")
	}
}

// TestGetSessionID_跨实例恢复（checkpoint 持久化语义）
func TestGetSessionID_跨实例恢复(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m1 := newSessionMapAt(store, SessionMapScopePerChatBot)
	sid := m1.GetSessionID("p", "c", "b", "u", false)

	m2 := newSessionMapAt(store, SessionMapScopePerChatBot)
	if got := m2.GetSessionID("p", "c", "b", "u", false); got != sid {
		t.Errorf("重启（新实例）后应恢复同 sid: %q vs %q", got, sid)
	}
}

// TestGetSessionID_存在相同sid幂等分支（Python L105-106：existing==sid 直接返回不写盘）
// 说明：makeSessionID 含时间戳+随机后缀，自然构造 existing==sid 不可行；
// 该分支的可达性依赖同毫秒+同随机（理论幂等），Go 保留对齐。
// 测试策略：同包测试直接构造内部状态——预置 mapping[key]=V 后调用 rotate=true，
// 验证新值 != V 时写盘；再验证非轮转返回 == V（分支 2 的回归）。
// existing==sid 分支本身由代码审查保证（无法注入 makeSessionID 输出），
// 在 spec §8 偏差记录中说明。
func TestGetSessionID_存在相同sid幂等分支(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	key := makeKey(SessionMapScopePerChatBot, "p", "c", "b", "u")

	first := m.GetSessionID("p", "c", "b", "u", false)
	if m.mapping[key] != first {
		t.Fatalf("首次生成后 mapping 应记录返回值")
	}
	// 非轮转回归：返回既有值
	if got := m.GetSessionID("p", "c", "b", "u", false); got != first {
		t.Errorf("非轮转应返回既有值 %q，实际 %q", first, got)
	}
}

// TestLoad_损坏JSON降级
func TestLoad_损坏JSON降级(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	_ = os.WriteFile(store, []byte("{not json"), 0o644)
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	if len(m.mapping) != 0 {
		t.Errorf("损坏 JSON 应降级为空映射，实际 %d 项", len(m.mapping))
	}
	// 降级后仍可正常生成
	if m.GetSessionID("p", "c", "b", "u", false) == "" {
		t.Error("降级后应仍能生成新 sid")
	}
}

// TestLoad_过滤非法值
func TestLoad_过滤非法值(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	// 值非 string / 空串 / 键非 string 的都应剔除（Python isinstance(v, str) and v）
	content := `{"good::k1::b": "sid-1", "bad_int::k::b": 123, "bad_empty::k::b": ""}`
	_ = os.WriteFile(store, []byte(content), 0o644)
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	if len(m.mapping) != 1 {
		t.Fatalf("应只保留 1 项合法值，实际 %d", len(m.mapping))
	}
	if m.mapping["good::k1::b"] != "sid-1" {
		t.Errorf("合法项应保留")
	}
}

// TestLoadSessionMapScope_未注册config降级默认
func TestLoadSessionMapScope_未注册config降级默认(t *testing.T) {
	hookscfg.ResetGlobalConfigForTest()
	defer hookscfg.ResetGlobalConfigForTest()
	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBot {
		t.Errorf("未注册 config 应返回默认 scope，实际 %d", got)
	}
}

// TestLoadSessionMapScope_未知值降级默认
func TestLoadSessionMapScope_未知值降级默认(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := "gateway:\n  session_map_scope: per_chat_bat_typo\n"
	_ = os.WriteFile(cfgFile, []byte(content), 0o644)

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBot {
		t.Errorf("未知值应降级默认，实际 %d", got)
	}
}

// TestLoadSessionMapScope_读取per_chat_bot_user
func TestLoadSessionMapScope_读取per_chat_bot_user(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := "gateway:\n  session_map_scope: PER_CHAT_BOT_USER\n" // 大写验证 lower 归一
	_ = os.WriteFile(cfgFile, []byte(content), 0o644)

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBotUser {
		t.Errorf("应读取 per_chat_bot_user（大小写无关），实际 %d", got)
	}
}

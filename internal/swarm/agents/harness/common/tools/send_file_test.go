package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ──────────────────── 测试基建 ────────────────────

// assertFailErr 恒失败错误（downloadInfoBuilder stub 用）。
type assertFailErr struct{}

// Error 实现错误接口。
func (assertFailErr) Error() string { return "stub download info failure" }

// sendFileTestHistoryEntry history 捕获条目。
type sendFileTestHistoryEntry struct {
	sessionID, requestID, channelID, role, content, eventType string
	extra                                                     map[string]any
}

// sendFileTestHooks 测试钩子：捕获 push/history 调用。
type sendFileTestHooks struct {
	mu        sync.Mutex
	pushMsgs  []map[string]any
	histCalls []sendFileTestHistoryEntry
}

// push 捕获推送消息。
func (h *sendFileTestHooks) push(_ context.Context, msg map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pushMsgs = append(h.pushMsgs, msg)
	return nil
}

// history 捕获历史记录调用。
func (h *sendFileTestHooks) history(sessionID, requestID, channelID, role, content string,
	_ float64, eventType string, extra map[string]any, _ map[string]any, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.histCalls = append(h.histCalls, sendFileTestHistoryEntry{
		sessionID: sessionID, requestID: requestID, channelID: channelID,
		role: role, content: content, eventType: eventType, extra: extra,
	})
}

// installSendFileHooks 注入钩子并注册清理。
func installSendFileHooks(t *testing.T) *sendFileTestHooks {
	t.Helper()
	h := &sendFileTestHooks{}
	SetSendFilePushFunc(h.push)
	SetSendFileHistoryFunc(h.history)
	t.Cleanup(func() {
		SetSendFilePushFunc(nil)
		SetSendFileHistoryFunc(nil)
	})
	return h
}

// newSendFileToolkitForTest 固定密钥 + 钩子注入的 toolkit 测试构造。
// 密钥经 loader 注入（零文件系统副作用），下载信息真实生成。
func newSendFileToolkitForTest(t *testing.T) *SendFileToolkit {
	t.Helper()
	newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	return NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{Metadata: map[string]any{"user": "u1"}})
}

// ──────────────────── SendFile 核心执行测试 ────────────────────

// TestSendFile_JSON数组 解析 JSON 数组成功发送。
// Python: send_file L88-101（json.loads → list 展开）
func TestSendFile_JSON数组(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)

	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "a.csv")
	f2 := filepath.Join(tmp, "b.txt")
	os.WriteFile(f1, []byte("x"), 0o644)
	os.WriteFile(f2, []byte("y"), 0o644)

	input, _ := json.Marshal([]string{f1, f2})
	result, err := tk.SendFile(t.Context(), string(input))
	if err != nil {
		t.Fatalf("SendFile error: %v", err)
	}
	if !strings.Contains(result, "成功发送 2 个文件") {
		t.Errorf("结果串错误: %q", result)
	}

	// push 断言：路由三元组 + is_complete + metadata + payload 结构
	if len(hooks.pushMsgs) != 1 {
		t.Fatalf("push 应调 1 次, 实际 %d", len(hooks.pushMsgs))
	}
	msg := hooks.pushMsgs[0]
	if msg["request_id"] != "req-1" || msg["session_id"] != "sess-1" || msg["channel_id"] != "web" {
		t.Errorf("push 路由三元组错误: %v", msg)
	}
	if msg["is_complete"] != false {
		t.Error("is_complete 应为 false")
	}
	if msg["metadata"] == nil {
		t.Error("metadata 应带上")
	}
	payload := msg["payload"].(map[string]any)
	if payload["event_type"] != "chat.file" {
		t.Errorf("event_type: %v", payload["event_type"])
	}
	files := payload["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("files 应 2 条: %v", files)
	}
	if files[0]["download_token"] == "" || files[0]["download_url"] == "" {
		t.Error("下载信息字段缺失")
	}
	if files[0]["size"] != int64(1) {
		t.Errorf("size: %v", files[0]["size"])
	}

	// history 断言
	if len(hooks.histCalls) != 1 {
		t.Fatalf("history 应调 1 次, 实际 %d", len(hooks.histCalls))
	}
	hc := hooks.histCalls[0]
	if hc.eventType != "chat.file" || hc.role != "assistant" || hc.content != "" {
		t.Errorf("history 参数错误: %+v", hc)
	}
	if hc.sessionID != "sess-1" || hc.requestID != "req-1" || hc.channelID != "web" {
		t.Errorf("history 三元组: %+v", hc)
	}
	if _, ok := hc.extra["files"]; !ok {
		t.Error("history extra 应含 files")
	}
}

// TestSendFile_单字符串 非 JSON 路径串包裹发送。
// Python: json.loads 失败 → [原文]
func TestSendFile_单字符串(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "only.md")
	os.WriteFile(fp, []byte("# hi"), 0o644)

	result, err := tk.SendFile(t.Context(), fp)
	if err != nil || !strings.Contains(result, "成功发送 1 个文件") {
		t.Errorf("单串发送失败: %q err=%v", result, err)
	}
	if len(hooks.pushMsgs) != 1 {
		t.Error("push 应调 1 次")
	}
}

// TestSendFile_JSON单串包裹 JSON 解析出单字符串也包裹。
// Python: isinstance(parsed, str) → [parsed]
func TestSendFile_JSON单串包裹(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "j.txt")
	os.WriteFile(fp, []byte("j"), 0o644)

	tk.SendFile(t.Context(), `"`+fp+`"`) // JSON 字符串
	if len(hooks.pushMsgs) != 1 {
		t.Errorf("JSON 单串应包裹发送: %d", len(hooks.pushMsgs))
	}
}

// TestSendFile_JSON数字包裹 JSON 解析出数字 → [原文]（非 str 非 list）。
// Python: else → [abs_file_path_list]（原文包裹）
func TestSendFile_JSON数字包裹(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)

	// "123" 解析为 float64 → 非 list 非 str → 包裹原文 "123" → 不存在 → 全缺失
	result, err := tk.SendFile(t.Context(), "123")
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result, "发送文件失败：所有文件均不存在") {
		t.Errorf("数字应包裹原文后全缺失: %q", result)
	}
	if len(hooks.pushMsgs) != 0 {
		t.Error("不应 push")
	}
}

// TestSendFile_全缺失 返回中文错误串不发 push。
// Python: L115-119
func TestSendFile_全缺失(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)

	result, err := tk.SendFile(t.Context(), "/nonexistent/x.pdf")
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result, "发送文件失败：所有文件均不存在") {
		t.Errorf("错误串: %q", result)
	}
	if !strings.Contains(result, "/nonexistent/x.pdf") {
		t.Errorf("缺失清单应含路径: %q", result)
	}
	if len(hooks.pushMsgs) != 0 || len(hooks.histCalls) != 0 {
		t.Error("全缺失不应 push/history")
	}
}

// TestSendFile_部分缺失 清单拼接（JSON 数组输入）。
// Python: L194-199 结果串拼接
func TestSendFile_部分缺失(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "ok.txt")
	os.WriteFile(f1, []byte("ok"), 0o644)
	miss := filepath.Join(tmp, "miss.txt")

	// JSON 数组输入：一个存在一个缺失 → 部分 missing
	input, _ := json.Marshal([]string{f1, miss})
	result, err := tk.SendFile(t.Context(), string(input))
	if err != nil {
		t.Fatalf("SendFile error: %v", err)
	}
	if !strings.Contains(result, "成功发送 1 个文件") || !strings.Contains(result, "以下文件不存在，未发送：") {
		t.Errorf("部分缺失结果串: %q", result)
	}
	if !strings.Contains(result, "  - "+miss) {
		t.Errorf("缺失清单应含路径: %q", result)
	}
	// push 应只含存在文件
	files := hooks.pushMsgs[0]["payload"].(map[string]any)["files"].([]map[string]any)
	if len(files) != 1 || files[0]["path"] != f1 {
		t.Errorf("push files 应只含存在文件: %v", files)
	}
}

// TestSendFile_回退基础模式 下载信息生成失败时仅 path+name。
// Python: L153-164 except → 基础模式
func TestSendFile_回退基础模式(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	// downloadInfoBuilder 替换为恒失败 stub（包级变量注入点）
	origBuilder := downloadInfoBuilder
	downloadInfoBuilder = func(filePath, baseName, sessionID, userID string) (map[string]any, error) {
		return nil, assertFailErr{}
	}
	defer func() { downloadInfoBuilder = origBuilder }()

	tmp := t.TempDir()
	fp := filepath.Join(tmp, "fb.bin")
	os.WriteFile(fp, []byte("z"), 0o644)

	result, _ := tk.SendFile(t.Context(), fp)
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Errorf("回退模式仍应成功: %q", result)
	}
	files := hooks.pushMsgs[0]["payload"].(map[string]any)["files"].([]map[string]any)
	if _, has := files[0]["download_url"]; has {
		t.Error("回退模式不应有 download_url")
	}
	if files[0]["path"] != fp || files[0]["name"] != "fb.bin" {
		t.Errorf("回退字段: %v", files[0])
	}
	// history 也应带基础模式 files
	if _, ok := hooks.histCalls[0].extra["files"]; !ok {
		t.Error("回退模式 history extra 仍应含 files")
	}
}

// TestSendFile_nil注入降级 未注入时 Warn 跳过不崩溃。
func TestSendFile_nil注入降级(t *testing.T) {
	SetSendFilePushFunc(nil)
	SetSendFileHistoryFunc(nil)
	t.Cleanup(func() { SetSendFilePushFunc(nil); SetSendFileHistoryFunc(nil) })
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "n.txt")
	os.WriteFile(fp, []byte("n"), 0o644)

	result, err := tk.SendFile(t.Context(), fp)
	if err != nil || !strings.Contains(result, "成功发送 1 个文件") {
		t.Errorf("nil 注入应降级成功: %q err=%v", result, err)
	}
}

// TestSendFile_空白路径跳过 数组内空串跳过。
// Python: fp.strip() 空则 continue
func TestSendFile_空白路径跳过(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "w.txt")
	os.WriteFile(fp, []byte("w"), 0o644)

	// JSON 数组带空串与空白串
	input, _ := json.Marshal([]string{"", "   ", fp})
	result, _ := tk.SendFile(t.Context(), string(input))
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Errorf("空白应跳过: %q", result)
	}
	files := hooks.pushMsgs[0]["payload"].(map[string]any)["files"].([]map[string]any)
	if len(files) != 1 {
		t.Errorf("files 应 1 条: %v", files)
	}
}

// TestSendFile_push失败兜底 push 返回错误转"提交文件失败"。
// Python: L200-206 except Exception → 错误串
func TestSendFile_push失败兜底(t *testing.T) {
	SetSendFilePushFunc(func(_ context.Context, _ map[string]any) error {
		return fmt.Errorf("connection lost")
	})
	t.Cleanup(func() { SetSendFilePushFunc(nil) })
	tk := newSendFileToolkitForTest(t)
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "e.txt")
	os.WriteFile(fp, []byte("e"), 0o644)

	result, err := tk.SendFile(t.Context(), fp)
	if err != nil {
		t.Fatalf("SendFile 不应返回 error（Python 兜底语义）: %v", err)
	}
	if !strings.Contains(result, "提交文件失败: connection lost") {
		t.Errorf("push 失败应转中文错误串: %q", result)
	}
}

// ──────────────────── 上下文/工具卡测试 ────────────────────

// TestSendFile_UpdateRuntimeContext 三元组刷新。
// Python: update_runtime_context L57-77
func TestSendFile_UpdateRuntimeContext(t *testing.T) {
	tk := NewSendFileToolkit("r1", "s1", "web", SendFileRuntimeContext{})
	tk.UpdateRuntimeContext("r2", "s2", "feishu", SendFileRuntimeContext{Metadata: map[string]any{"k": 1}})
	if tk.requestID != "r2" || tk.sessionID != "s2" || tk.channelID != "feishu" {
		t.Errorf("三元组未刷新: %+v", tk)
	}
	if tk.requestMetadata["k"] != 1 {
		t.Error("metadata 未刷新")
	}
}

// TestSendFile_UpdateRuntimeContext并发 -race 下并发刷新。
func TestSendFile_UpdateRuntimeContext并发(t *testing.T) {
	tk := NewSendFileToolkit("r0", "s0", "web", SendFileRuntimeContext{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tk.UpdateRuntimeContext(fmt.Sprintf("r%d", n), "s", "web", SendFileRuntimeContext{})
		}(i)
	}
	wg.Wait()
}

// TestSendFile_GetTools 工具卡断言。
// Python: get_tools L208-254——description 中文逐字复制
func TestSendFile_GetTools(t *testing.T) {
	tk := NewSendFileToolkit("r", "s", "web", SendFileRuntimeContext{})
	tools := tk.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应 1 个工具: %d", len(tools))
	}
	card := tools[0].Card()
	if card.Name != "send_file_to_user" {
		t.Errorf("工具名: %q", card.Name)
	}
	if !strings.Contains(card.Description, "【文件发送工具】") {
		t.Error("description 应含中文逐字开头")
	}
	if len(card.InputParams) != 2 || card.InputParams[0].Name != "abs_file_path_list" ||
		card.InputParams[1].Name != "target_channels" {
		t.Errorf("InputParams: %+v", card.InputParams)
	}
	if !card.InputParams[0].Required {
		t.Error("abs_file_path_list 应必填")
	}
}

// TestSendFile_Invoke冒烟 走 MapFunction 路径完整执行。
func TestSendFile_Invoke冒烟(t *testing.T) {
	hooks := installSendFileHooks(t)
	newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	tk := NewSendFileToolkit("req-9", "sess-9", "web", SendFileRuntimeContext{})
	got := tk.GetTools()

	tmp := t.TempDir()
	fp := filepath.Join(tmp, "smoke.txt")
	os.WriteFile(fp, []byte("s"), 0o644)

	out, err := got[0].Invoke(t.Context(), map[string]any{"abs_file_path_list": fp})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if s, _ := out["result"].(string); !strings.Contains(s, "成功发送 1 个文件") {
		t.Errorf("Invoke result: %v", out)
	}
	if len(hooks.pushMsgs) != 1 {
		t.Error("Invoke 应触发 push")
	}
	if hooks.pushMsgs[0]["request_id"] != "req-9" {
		t.Errorf("工具实例应绑定构造时三元组: %v", hooks.pushMsgs[0])
	}
}

// TestSendFile_metadata不拷贝入参 构造后修改原 map 不影响内部。
// Python: dict(metadata) 拷贝语义
func TestSendFile_metadata不拷贝入参(t *testing.T) {
	md := map[string]any{"k": "v1"}
	tk := NewSendFileToolkit("r", "s", "web", SendFileRuntimeContext{Metadata: md})
	md["k"] = "v2"
	if tk.requestMetadata["k"] != "v1" {
		t.Error("构造时应拷贝 metadata")
	}

	md2 := map[string]any{"k": "a"}
	tk.UpdateRuntimeContext("r", "s", "web", SendFileRuntimeContext{Metadata: md2})
	md2["k"] = "b"
	if tk.requestMetadata["k"] != "a" {
		t.Error("刷新时应拷贝 metadata")
	}
}

// TestSendFile_metadata置nil UpdateRuntimeContext 传 nil 清空。
// Python: dict(metadata) if metadata else None
func TestSendFile_metadata置nil(t *testing.T) {
	tk := NewSendFileToolkit("r", "s", "web", SendFileRuntimeContext{Metadata: map[string]any{"k": 1}})
	tk.UpdateRuntimeContext("r", "s", "web", SendFileRuntimeContext{})
	if tk.requestMetadata != nil {
		t.Errorf("nil 刷新应清空 metadata: %v", tk.requestMetadata)
	}
}

// ──────────────────── 会话级去重 + 静默守卫（2026-11-16 对齐批） ────────────────────

// newDedupTestFile 独立内容文件（避免路径归一撞车）。
func newDedupTestFile(t *testing.T, name string) string {
	t.Helper()
	return writeFileSendFileTest(t, t.TempDir(), name)
}

// writeFileSendFileTest 写测试文件。
func writeFileSendFileTest(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("content-"+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSendFile_会话去重 同一 session 重复投递同路径 → 第二次跳过（Python L531-553）。
func TestSendFile_会话去重(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	fp := newDedupTestFile(t, "dup.txt")

	// 第一次：成功
	first, _ := tk.SendFile(context.Background(), fp)
	if !strings.Contains(first, "成功发送 1 个文件") {
		t.Fatalf("首次投递应成功: %q", first)
	}
	if len(hooks.pushMsgs) != 1 {
		t.Fatalf("首次应 push 1 次: %d", len(hooks.pushMsgs))
	}

	// 第二次：跳过重复（提示串逐字对齐 Python L544-546）
	second, _ := tk.SendFile(context.Background(), fp)
	if !strings.Contains(second, "文件已在本次会话发送过，跳过重复投递：") {
		t.Fatalf("重复投递应被跳过: %q", second)
	}
	if !strings.Contains(second, "  - "+fp) {
		t.Fatalf("跳过列表应含原路径: %q", second)
	}
	if len(hooks.pushMsgs) != 1 {
		t.Fatalf("第二次不应 push: %d", len(hooks.pushMsgs))
	}

	// 不同 session 不去重
	tk2 := NewSendFileToolkit("req-2", "sess-2", "web", SendFileRuntimeContext{})
	third, _ := tk2.SendFile(context.Background(), fp)
	if !strings.Contains(third, "成功发送 1 个文件") {
		t.Fatalf("不同 session 不应去重: %q", third)
	}
}

// TestSendFile_去重路径归一 归一函数行为：反斜杠转正斜杠 + 小写（Python L121-122）。
// 注：整路径大写在大小写敏感 FS 上是不同文件，故直接单测归一函数；
// 大小写不敏感生效场景为 Windows/macOS（case-insensitive FS 上的大小写差异路径）。
func TestSendFile_去重路径归一(t *testing.T) {
	got := normalizeSentFilePath(`/TMP/DIR/File.TXT`)
	want := strings.ToLower("/tmp/dir/File.txt")
	if got != want {
		t.Fatalf("归一结果: %q != %q", got, want)
	}
	if got := normalizeSentFilePath(`/TMP/DIR\File.TXT`); got != strings.ToLower("/tmp/dir/file.txt") {
		t.Fatalf("反斜杠应转正斜杠: %q", got)
	}
}

// TestClearSentFilesForSession 清理去重状态后可重发（Python L152-155）。
func TestClearSentFilesForSession(t *testing.T) {
	installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	fp := newDedupTestFile(t, "clear.txt")
	if _, err := tk.SendFile(context.Background(), fp); err != nil {
		t.Fatal(err)
	}
	ClearSentFilesForSession("sess-1")
	result, _ := tk.SendFile(context.Background(), fp)
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Fatalf("clear 后应可重发: %q", result)
	}
}

// TestSendFile_静默守卫 skills 重建/知识转 Skill 静默 Agent 禁止投递（Python L379-390）。
func TestSendFile_静默守卫(t *testing.T) {
	hooks := installSendFileHooks(t)
	fp := newDedupTestFile(t, "silent.txt")

	cases := []struct {
		name     string
		metadata map[string]any
	}{
		{"skills_rebuild_silent", map[string]any{"skills_rebuild_silent": true}},
		{"skills_create_from_knowledge_silent", map[string]any{"skills_create_from_knowledge_silent": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{Metadata: tc.metadata})
			result, _ := tk.SendFile(context.Background(), fp)
			// 提示串逐字对齐 Python L388-390
			want := "静默模式禁止 send_file_to_user；请直接用文件写入工具生成 Skill 目录，不要投递文件给用户。"
			if result != want {
				t.Fatalf("静默提示串不符:\n got=%q\nwant=%q", result, want)
			}
			if len(hooks.pushMsgs) != 0 {
				t.Fatal("静默模式不应 push")
			}
			if len(hooks.histCalls) != 0 {
				t.Fatal("静默模式不应写 history")
			}
		})
	}
}

// ──────────────────── 信封快照 + target_channels + 团队物化 + skill 包（Task 4） ────────────────────

// TestSendFile_targetChannels push metadata 合并 send_file_targets（Python L794-795）。
func TestSendFile_targetChannels(t *testing.T) {
	hooks := installSendFileHooks(t)
	tk := newSendFileToolkitForTest(t)
	fp := newDedupTestFile(t, "chan.txt")

	result, _ := tk.SendFile(context.Background(), fp, "web", "human-player-1")
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Fatalf("投递应成功: %q", result)
	}
	if len(hooks.pushMsgs) != 1 {
		t.Fatalf("应 push 1 次: %d", len(hooks.pushMsgs))
	}
	md, _ := hooks.pushMsgs[0]["metadata"].(map[string]any)
	if md == nil {
		t.Fatal("target_channels 非空时应写 metadata")
	}
	targets, _ := md["send_file_targets"].([]string)
	if len(targets) != 2 || targets[0] != "web" || targets[1] != "human-player-1" {
		t.Fatalf("send_file_targets 不符: %v", md["send_file_targets"])
	}
}

// TestSendFile_信封快照 并发换上下文不串话（Python _SendFileRuntimeEnvelope L91-103）。
func TestSendFile_信封快照(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var mu sync.Mutex
	var pushed []map[string]any
	var hist []sendFileTestHistoryEntry
	SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		entered <- struct{}{} // 通知测试：SendFile 已越过快照点
		<-release             // 阻塞 push，制造并发窗口
		mu.Lock()
		pushed = append(pushed, msg)
		mu.Unlock()
		return nil
	})
	SetSendFileHistoryFunc(func(sessionID, requestID, channelID, role, content string,
		_ float64, eventType string, extra map[string]any, _ map[string]any, _ string) {
		mu.Lock()
		hist = append(hist, sendFileTestHistoryEntry{sessionID: sessionID, requestID: requestID,
			channelID: channelID, role: role, content: content, eventType: eventType, extra: extra})
		mu.Unlock()
	})
	t.Cleanup(func() { SetSendFilePushFunc(nil); SetSendFileHistoryFunc(nil) })

	tk := NewSendFileToolkit("req-1", "sess-orig", "web", SendFileRuntimeContext{})
	fp := newDedupTestFile(t, "env.txt")
	done := make(chan string, 1)
	go func() {
		result, _ := tk.SendFile(context.Background(), fp)
		done <- result
	}()
	// SendFile 已进入 push（快照已冻结）后，另一轮请求刷新上下文
	<-entered
	tk.UpdateRuntimeContext("req-2", "sess-new", "web", SendFileRuntimeContext{})
	close(release)
	result := <-done
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Fatalf("投递应成功: %q", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pushed) != 1 {
		t.Fatalf("应 push 1 次: %d", len(pushed))
	}
	if pushed[0]["session_id"] != "sess-orig" {
		t.Fatalf("投递应使用快照时 session: %v", pushed[0]["session_id"])
	}
	if hist[0].sessionID != "sess-orig" {
		t.Fatalf("history 应使用快照时 session: %v", hist[0].sessionID)
	}
}

// TestSendFile_团队物化 team-workspace 交付物复制到项目目录（Python L277-346/L492-516）。
func TestSendFile_团队物化(t *testing.T) {
	hooks := installSendFileHooks(t)
	root := t.TempDir()
	project := t.TempDir()
	teamWS := filepath.Join(root, ".agent_teams", "t1", "team-workspace")
	src := filepath.Join(teamWS, "artifacts", "20260101", "chat-1", "outputs", "out.txt")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}

	tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{ProjectDir: project})
	result, _ := tk.SendFile(context.Background(), src)
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Fatalf("投递应成功: %q", result)
	}
	// 相对路径保留复制到项目目录
	dst := filepath.Join(project, "artifacts", "20260101", "chat-1", "outputs", "out.txt")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("应复制到项目目录: %v", err)
	}
	if string(data) != "deliverable" {
		t.Fatalf("复制内容不符: %q", string(data))
	}
	// 结果串含项目目录提示段（Python L637-640）
	if !strings.Contains(result, "最终交付文件已位于当前项目目录：") || !strings.Contains(result, "  - "+dst) {
		t.Fatalf("结果串应含物化提示: %q", result)
	}
	// push payload 路径应为物化后路径
	if len(hooks.pushMsgs) != 1 {
		t.Fatal("应 push 1 次")
	}
	payload := hooks.pushMsgs[0]["payload"].(map[string]any)
	files := payload["files"].([]map[string]any)
	if files[0]["path"] != dst {
		t.Fatalf("payload path 应为物化路径: %v", files[0]["path"])
	}
}

// TestSendFile_团队物化_无项目目录 原路径直投（Python L304-305）。
func TestSendFile_团队物化_无项目目录(t *testing.T) {
	installSendFileHooks(t)
	teamWS := filepath.Join(t.TempDir(), ".agent_teams", "t1", "team-workspace")
	if err := os.MkdirAll(teamWS, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(teamWS, "out.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{})
	result, _ := tk.SendFile(context.Background(), src)
	if !strings.Contains(result, "成功发送 1 个文件") || strings.Contains(result, "当前项目目录") {
		t.Fatalf("无 project_dir 应原路径直投: %q", result)
	}
}

// TestSendFile_团队物化_冲突拒绝 目标已存在不同内容（Python L337-339 + L503-511）。
func TestSendFile_团队物化_冲突拒绝(t *testing.T) {
	installSendFileHooks(t)
	root := t.TempDir()
	project := t.TempDir()
	teamWS := filepath.Join(root, ".agent_teams", "t1", "team-workspace")
	if err := os.MkdirAll(teamWS, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(teamWS, "out.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(project, "out.txt")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{ProjectDir: project})
	result, _ := tk.SendFile(context.Background(), src)
	// Python L509-511 错误串逐字复制
	if !strings.Contains(result, "发送文件失败：无法将团队交付文件写入当前项目目录") {
		t.Fatalf("冲突应拒绝: %q", result)
	}
}

// TestSendFile_物化projectDir回退 session metadata 回退解析（Python _resolve_project_dir L240-264）。
func TestSendFile_物化projectDir回退(t *testing.T) {
	installSendFileHooks(t)
	root := t.TempDir()
	project := t.TempDir()
	teamWS := filepath.Join(root, ".agent_teams", "t1", "team-workspace")
	if err := os.MkdirAll(teamWS, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(teamWS, "out.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := sendFileProjectDirResolver
	sendFileProjectDirResolver = func(sessionID string) string {
		if sessionID == "sess-1" {
			return project
		}
		return ""
	}
	t.Cleanup(func() { sendFileProjectDirResolver = orig })

	tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{})
	result, _ := tk.SendFile(context.Background(), src)
	if !strings.Contains(result, "成功发送 1 个文件") {
		t.Fatalf("投递应成功: %q", result)
	}
	if _, err := os.Stat(filepath.Join(project, "out.txt")); err != nil {
		t.Fatalf("回退 project_dir 应生效: %v", err)
	}
}

// TestLooksLikeSkillPackage zip 探测矩阵（Python L48-88）。
func TestLooksLikeSkillPackage(t *testing.T) {
	dir := t.TempDir()
	// .skill 扩展名
	if !LooksLikeSkillPackage(filepath.Join(dir, "pkg.skill")) {
		t.Fatal(".skill 应为 true")
	}
	// .skill.zip
	if !LooksLikeSkillPackage(filepath.Join(dir, "pkg.skill.zip")) {
		t.Fatal(".skill.zip 应为 true")
	}
	// zip 含根级 SKILL.md
	zipPath := filepath.Join(dir, "root.zip")
	writeZipWithSKILL(t, zipPath, "SKILL.md")
	if !LooksLikeSkillPackage(zipPath) {
		t.Fatal("根级 SKILL.md zip 应为 true")
	}
	// zip 含一层目录 SKILL.md
	nested := filepath.Join(dir, "nested.zip")
	writeZipWithSKILL(t, nested, "my-skill/SKILL.md")
	if !LooksLikeSkillPackage(nested) {
		t.Fatal("一层目录 SKILL.md zip 应为 true")
	}
	// 普通 zip
	plain := filepath.Join(dir, "plain.zip")
	writeZipWithSKILL(t, plain, "data/other.txt")
	if LooksLikeSkillPackage(plain) {
		t.Fatal("无 SKILL.md zip 应为 false")
	}
	// 非 zip 文件
	if LooksLikeSkillPackage(filepath.Join(dir, "doc.pdf")) {
		t.Fatal("非 zip 应为 false")
	}
	// 不存在路径
	if LooksLikeSkillPackage(filepath.Join(dir, "missing.zip")) {
		t.Fatal("不存在路径应为 false")
	}
}

// TestSendFile_isSkillPackage payload 携带探测结果（Python L702/732/758/770）。
func TestSendFile_isSkillPackage(t *testing.T) {
	hooks := installSendFileHooks(t)
	newTestManagerWithSecret(t, "0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	skillZip := filepath.Join(dir, "out.skill.zip")
	writeZipWithSKILL(t, skillZip, "SKILL.md")
	plainFile := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plainFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := NewSendFileToolkit("req-1", "sess-1", "web", SendFileRuntimeContext{})
	// JSON 数组入参（Python normalize_send_file_paths：JSON list → 候选展开）
	listInput := fmt.Sprintf(`[%q, %q]`, skillZip, plainFile)
	if _, err := tk.SendFile(context.Background(), listInput); err != nil {
		t.Fatal(err)
	}
	payload := hooks.pushMsgs[0]["payload"].(map[string]any)
	files := payload["files"].([]map[string]any)
	if files[0]["is_skill_package"] != true {
		t.Fatalf("skill 包应标记 true: %v", files[0])
	}
	if files[1]["is_skill_package"] != false {
		t.Fatalf("普通文件应标记 false: %v", files[1])
	}
}

// writeZipWithSKILL 构造含指定条目的 zip（skill 包探测测试用）。
func writeZipWithSKILL(t *testing.T, path, entry string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("---\nname: t\n---\nbody")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}


package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions/rootcontext"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// ──────────────────────────── Task 10: 纯函数段 ────────────────────────────

// TestExtractPendingJobID 对齐 _PENDING_JOB_ID_RE:73 双形状与不命中。
func TestExtractPendingJobID(t *testing.T) {
	if id := ExtractPendingJobID("Video job abc-123 submitted and still running after 120s - generation can take..."); id != "abc-123" {
		t.Fatalf("submitted 形状: %q", id)
	}
	if id := ExtractPendingJobID("Video job abc-123 is still running."); id != "abc-123" {
		t.Fatalf("is still 形状: %q", id)
	}
	if id := ExtractPendingJobID("[ERROR]: video job x ended with status failed"); id != "" {
		t.Fatalf("终态不应命中: %q", id)
	}
	if id := ExtractPendingJobID(""); id != "" {
		t.Fatalf("空串: %q", id)
	}
}

// TestIsTerminalJobResult 对齐 :92-107。
func TestIsTerminalJobResult(t *testing.T) {
	if !IsTerminalJobResult("Video generated successfully!\nSaved to: /tmp/v.mp4\n") {
		t.Fatal("成功形状应 true")
	}
	if !IsTerminalJobResult("[ERROR]: video job abc ended with status failed: no detail") {
		t.Fatal("终态失败形状应 true")
	}
	for _, transient := range []string{
		"Video job abc is still running.",
		"[ERROR]: polling video job abc failed: 500 oops",
		"[ERROR]: video generation submit returned invalid JSON",
	} {
		if IsTerminalJobResult(transient) {
			t.Fatalf("瞬态不应判终态: %q", transient)
		}
	}
}

// TestVideoGenEnabled 对齐 :217-227 环境变量门控。
func TestVideoGenEnabled(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "on", "TRUE"} {
		t.Setenv("VIDEO_GEN_ENABLED", v)
		if !VideoGenEnabled() {
			t.Fatalf("%q 应 true", v)
		}
	}
	t.Setenv("VIDEO_GEN_ENABLED", "0")
	if VideoGenEnabled() {
		t.Fatal("0 应 false")
	}
	os.Unsetenv("VIDEO_GEN_ENABLED")
	if VideoGenEnabled() {
		t.Fatal("未设应 false")
	}
}

// TestVideoGenConfigured 对齐 :240-254。
func TestVideoGenConfigured(t *testing.T) {
	os.Unsetenv("VIDEO_GEN_API_KEY")
	os.Unsetenv("VIDEO_GEN_API_BASE")
	os.Unsetenv("VIDEO_GEN_MODEL_NAME")
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
	if VideoGenConfigured() {
		t.Fatal("全缺应 false")
	}
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", "https://b")
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	if !VideoGenConfigured() {
		t.Fatal("三样齐应 true")
	}
	// vLLM-Omni keyless：只需 base
	os.Unsetenv("VIDEO_GEN_API_KEY")
	os.Unsetenv("VIDEO_GEN_MODEL_NAME")
	t.Setenv("VIDEO_GEN_PROTOCOL", "vllm-omni")
	if !VideoGenConfigured() {
		t.Fatal("keyless 只需 base 应 true")
	}
	os.Unsetenv("VIDEO_GEN_API_BASE")
	if VideoGenConfigured() {
		t.Fatal("keyless 缺 base 应 false")
	}
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
}

// TestDetectVideoGenBackend 对齐 gen_toolkits.detect_backend:236-252。
func TestDetectVideoGenBackend(t *testing.T) {
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
	cases := map[string]string{
		"https://api.minimax.io/v1":                  backendMinimax,
		"https://api.minimaxi.com/v1":                backendMinimax,
		"https://ark.cn-beijing.volces.com/api/v3":   backendModelark,
		"https://ark.example.bytepluses.com/api/v3":  backendModelark,
		"https://dashscope.aliyuncs.com/api/v1":      backendDashscope,
		"https://dashscope-intl.aliyuncs.com/api/v1": backendDashscope,
		"https://openrouter.ai/api/v1":               "",
	}
	for apiBase, want := range cases {
		if got := DetectVideoGenBackend("VIDEO_GEN_PROTOCOL", apiBase); got != want {
			t.Fatalf("%s → %q, want %q", apiBase, got, want)
		}
	}
	t.Setenv("VIDEO_GEN_PROTOCOL", "dashscope")
	if got := DetectVideoGenBackend("VIDEO_GEN_PROTOCOL", "https://openrouter.ai/api/v1"); got != backendDashscope {
		t.Fatalf("协议槽优先: %q", got)
	}
}

// TestClampDuration 对齐 :256-260 H3 下限与 4-15 域。
func TestClampDuration(t *testing.T) {
	if got := clampDuration("MiniMax-H3-Max", 3); got != 5 {
		t.Fatalf("H3 下限 = %d", got)
	}
	if got := clampDuration("other-model", 3); got != 4 {
		t.Fatalf("普通下限 = %d", got)
	}
	if got := clampDuration("other", 99); got != 15 {
		t.Fatalf("上限 = %d", got)
	}
}

// TestResolveFrameReference 对齐 :262-282 四分支。
func TestResolveFrameReference(t *testing.T) {
	if uri, errMsg := resolveFrameReference("", "first_frame_path"); uri != "" || errMsg != "" {
		t.Fatalf("空输入: %q/%q", uri, errMsg)
	}
	if uri, errMsg := resolveFrameReference("https://x/a.png", "f"); uri != "https://x/a.png" || errMsg != "" {
		t.Fatalf("URL 原样: %q/%q", uri, errMsg)
	}
	uri, errMsg := resolveFrameReference("data:image/png;base64,abc", "f")
	if uri != "data:image/png;base64,abc" || errMsg != "" {
		t.Fatalf("data URI 原样: %q/%q", uri, errMsg)
	}
	if _, errMsg = resolveFrameReference("/nonexistent/f.png", "first_frame_path"); !strings.Contains(errMsg, "is not a URL/data URI") {
		t.Fatalf("不存在文件: %q", errMsg)
	}
	p := filepath.Join(t.TempDir(), "f.jpg")
	_ = os.WriteFile(p, []byte("jpg"), 0o644)
	uri, errMsg = resolveFrameReference(p, "f")
	if errMsg != "" || !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Fatalf("本地读取: %q/%q", uri, errMsg)
	}
}

// TestResolveSavePath 对齐 :284-289。
func TestResolveSavePath(t *testing.T) {
	dir := t.TempDir()
	got := resolveSavePath(dir, "video_x.mp4")
	if got != filepath.Join(dir, "video_x.mp4") {
		t.Fatalf("save_dir: %s", got)
	}
	if got2 := resolveSavePath("", "v.mp4"); !strings.Contains(got2, "generated_videos") {
		t.Fatalf("默认工作区目录: %s", got2)
	}
}

// ──────────────────────────── Task 11: 提交轮询 ────────────────────────────

// setFastPoll 注入短轮询参数（测试不真等 120s）。
func setFastPoll(t *testing.T) {
	t.Helper()
	oldInterval, oldBudget := pollInterval, pollBudget
	pollInterval = 5 * time.Millisecond
	pollBudget = 1 // 1 秒预算 × 5ms tick → 若干次轮询
	t.Cleanup(func() { pollInterval, pollBudget = oldInterval, oldBudget })
}

// TestGenerateVideoCore_未配置与空prompt 对齐 :395-404 两个早退分支。
func TestGenerateVideoCore_未配置与空prompt(t *testing.T) {
	os.Unsetenv("VIDEO_GEN_API_KEY")
	os.Unsetenv("VIDEO_GEN_API_BASE")
	os.Unsetenv("VIDEO_GEN_MODEL_NAME")
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
	got := generateVideoCore(context.Background(), map[string]any{"prompt": "x"})
	if !strings.Contains(got, "video generation is not configured") {
		t.Fatalf("未配置: %q", got)
	}
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", "https://b")
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	if got := generateVideoCore(context.Background(), map[string]any{"prompt": "  "}); got != "[ERROR]: prompt is required." {
		t.Fatalf("空 prompt: %q", got)
	}
}

// TestGenerateVideoCore_首帧错误与referenceMode 对齐 :406-425。
func TestGenerateVideoCore_首帧错误与referenceMode(t *testing.T) {
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", "https://openrouter.ai/api/v1")
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	if got := generateVideoCore(context.Background(), map[string]any{
		"prompt": "p", "first_frame_path": "/nonexistent.png",
	}); !strings.Contains(got, "is not a URL/data URI") {
		t.Fatalf("首帧错误: %q", got)
	}
	if got := generateVideoCore(context.Background(), map[string]any{
		"prompt": "p", "reference_mode": true,
	}); got != "[ERROR]: reference_mode needs reference_image_paths or first_frame_path." {
		t.Fatalf("reference_mode: %q", got)
	}
}

// TestGenerateVideoCore_提交即完成 对齐 :470-532 完成路径（提交→终态→下载）。
func TestGenerateVideoCore_提交即完成(t *testing.T) {
	setFastPoll(t)
	saveDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/videos" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "seed-model" || body["aspect_ratio"] != "9:16" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"id":"job-ok","status":"completed"}`))
		case strings.HasPrefix(r.URL.Path, "/videos/job-ok/content"):
			_, _ = w.Write([]byte("mp4-bytes"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "seed-model")
	got := generateVideoCore(context.Background(), map[string]any{
		"prompt": "clip", "aspect_ratio": "9:16", "save_dir": saveDir,
	})
	if !strings.Contains(got, "Video generated successfully!") || !strings.Contains(got, "Saved to: ") {
		t.Fatalf("完成路径: %q", got)
	}
	if _, err := os.Stat(filepath.Join(saveDir, "video_job-ok.mp4")); err != nil {
		t.Fatalf("视频未落盘: %v", err)
	}
}

// rootCtx 构造携带 RootDecisionContext 的 ctx（D-23.8-4 回收后盯单路由直接取叶子包）。
// request_id/channel_id 固定 req-1/ch-1，与历史断言的路由字段一致。
func rootCtx(sessionID string) context.Context {
	ctx, _ := rootcontext.BindRootDecisionContext(context.Background(), rootRC(sessionID))
	return ctx
}

// rootRC 构造路由三元组 rc（request_id/channel_id 固定 req-1/ch-1，与历史断言一致）。
func rootRC(sessionID string) *rootcontext.RootDecisionContext {
	rc, err := rootcontext.NewRootDecisionContext(sessionID, "req-1", "ch-1", nil, "")
	if err != nil {
		panic(err)
	}
	return rc
}

// TestGenerateVideoCore_轮询到期pending 对齐 :497-503 窗口到期返回 pending 形状并布防盯单。
func TestGenerateVideoCore_轮询到期pending(t *testing.T) {
	setFastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 在可取消 ctx 上绑定 RootDecisionContext：watcher 随测试结束的 cancel 退出，
	// 不污染后续用例的 waitForWatchDrain。
	rc := rootRC("s")
	ctx, _ = rootcontext.BindRootDecisionContext(ctx, rc)

	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/videos" && r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"job-slow","status":"running"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"job-slow","status":"running"}`))
	}))
	defer server.Close()

	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	got := generateVideoCore(ctx, map[string]any{"prompt": "p"})
	if !strings.HasPrefix(got, "Video job job-slow submitted and still running") {
		t.Fatalf("pending 形状: %q", got)
	}
	watchedPendingJobsMu.Lock()
	if n := len(watchedPendingJobs); n != 1 {
		watchedPendingJobsMu.Unlock()
		t.Fatalf("盯单应布防一次: %d", n)
	}
	watchedPendingJobsMu.Unlock()
}

// TestGenerateVideoCore_终态失败 对齐 :505-511 失败形状。
func TestGenerateVideoCore_终态失败(t *testing.T) {
	setFastPoll(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"job-f","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"job-f","status":"expired","error":"timeout"}`))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	got := generateVideoCore(context.Background(), map[string]any{"prompt": "p"})
	if got != "[ERROR]: video job job-f ended with status expired: timeout" {
		t.Fatalf("失败形状: %q", got)
	}
}

// TestGenerateVideoCore_提交失败与坏JSON 对齐 :487-496。
func TestGenerateVideoCore_提交失败与坏JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("denied"))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	if got := generateVideoCore(context.Background(), map[string]any{"prompt": "p"}); !strings.Contains(got, "submit failed: 403") {
		t.Fatalf("提交失败: %q", got)
	}
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer badJSON.Close()
	t.Setenv("VIDEO_GEN_API_BASE", badJSON.URL)
	if got := generateVideoCore(context.Background(), map[string]any{"prompt": "p"}); !strings.Contains(got, "invalid JSON") {
		t.Fatalf("坏 JSON: %q", got)
	}
	// 提交响应无 job id（对齐 :495-497）
	noID := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer noID.Close()
	t.Setenv("VIDEO_GEN_API_BASE", noID.URL)
	if got := generateVideoCore(context.Background(), map[string]any{"prompt": "p"}); !strings.Contains(got, "no job id") {
		t.Fatalf("无 job id: %q", got)
	}
}

// ──────────────────────────── Task 12: 盯单与交付 ────────────────────────────

// setFastWatch 注入短盯单参数。
func setFastWatch(t *testing.T) {
	t.Helper()
	oldI, oldM := watchInterval, watchMax
	watchInterval = 5 * time.Millisecond
	watchMax = 60 * time.Millisecond
	t.Cleanup(func() { watchInterval, watchMax = oldI, oldM })
}

// waitForWatchDrain 等待盯单集合清空（终态交付或放弃后 discard）。
func waitForWatchDrain(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		watchedPendingJobsMu.Lock()
		n := len(watchedPendingJobs)
		watchedPendingJobsMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("盯单未在时限内结算")
}

// TestWatchUntilSettled_完成交付 chat.final append 推送（用户硬要求覆盖点）。
func TestWatchUntilSettled_完成交付(t *testing.T) {
	setFastWatch(t)
	pollInterval, pollBudget = time.Millisecond, 1
	defer func() { pollInterval, pollBudget = pollIntervalSeconds, maxPollSeconds }()
	saveDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			_, _ = w.Write([]byte("mp4"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"job-w","status":"completed"}`))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")

	// D-23.8-5 回收：完成交付走真实 SendFileToolkit → chat.file push（含 files payload）
	var pushed []map[string]any
	var pushMu sync.Mutex
	SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		pushMu.Lock()
		pushed = append(pushed, msg)
		pushMu.Unlock()
		return nil
	})
	t.Cleanup(func() { SetSendFilePushFunc(nil) })

	watchUntilSettled(rootCtx("sess-1"), "job-w", saveDir, rootRC("sess-1"))
	waitForWatchDrain(t, time.Second)
	pushMu.Lock()
	defer pushMu.Unlock()
	if len(pushed) != 1 {
		t.Fatalf("应推送一次: %d", len(pushed))
	}
	if payload, _ := pushed[0]["payload"].(map[string]any); payload["event_type"] != "chat.file" {
		t.Fatalf("应推送 chat.file: %v", payload)
	}
	msg := pushed[0]
	// D-23.8-5 回收：files payload 含视频路径与探测标记（is_skill_package=false）
	payload, _ := pushed[0]["payload"].(map[string]any)
	files, _ := payload["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("应 1 个文件: %v", files)
	}
	if !strings.HasSuffix(fmt.Sprint(files[0]["path"]), "video_job-w.mp4") {
		t.Fatalf("files path 错: %v", files[0]["path"])
	}
	if files[0]["is_skill_package"] != false {
		t.Fatalf("is_skill_package 应为 false: %v", files[0])
	}
	if msg["session_id"] != "sess-1" || msg["request_id"] != "req-1" || msg["channel_id"] != "ch-1" || msg["is_complete"] != false {
		t.Fatalf("路由字段错: %v", msg)
	}
	// 已有显式 final_mode 不覆盖（annotate 语义在 watch 路径同样生效）
	msg2Payload := swarmcommon.AnnotateChatFinal(map[string]any{"final_mode": "replace_turn"}, swarmcommon.FinalModeAppend)
	if msg2Payload["final_mode"] != "replace_turn" {
		t.Fatal("annotate 不应覆盖显式值")
	}
}

// TestWatchUntilSettled_失败交付 对齐 :198-199 失败兜底通知。
func TestWatchUntilSettled_失败交付(t *testing.T) {
	setFastWatch(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"job-x","status":"cancelled","error":"user cancel"}`))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")

	var contents []string
	SetVideoGenRuntimePush(func(msg map[string]any) bool {
		payload, _ := msg["payload"].(map[string]any)
		contents = append(contents, fmt.Sprint(payload["content"]))
		return true
	})
	t.Cleanup(func() { SetVideoGenRuntimePush(nil) })

	watchUntilSettled(rootCtx("s"), "job-x", "", rootRC("s"))
	waitForWatchDrain(t, time.Second)
	if len(contents) != 1 || !strings.Contains(contents[0], "未能成功完成") || !strings.Contains(contents[0], "cancelled") {
		t.Fatalf("失败通知: %v", contents)
	}
}

// TestWatchUntilSettled_超限放弃 对齐 :148-153 30 分钟上限（测试 60ms）。
func TestWatchUntilSettled_超限放弃(t *testing.T) {
	setFastWatch(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"job-forever","status":"running"}`))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")

	pushCount := 0
	SetVideoGenRuntimePush(func(map[string]any) bool { pushCount++; return true })
	t.Cleanup(func() { SetVideoGenRuntimePush(nil) })

	// pending 轮询会接力调度盯单（scheduleCompletionWatchIfPending），
	// 用可取消 ctx 让整条调度链随测试结束退出，不污染后续用例。
	ctx, cancel := context.WithCancel(context.Background())
	rc := rootRC("s")
	ctx, _ = rootcontext.BindRootDecisionContext(ctx, rc)
	defer cancel()
	watchUntilSettled(ctx, "job-forever", "", rc)
	waitForWatchDrain(t, time.Second)
	if pushCount != 0 {
		t.Fatalf("超限放弃不应推送: %d", pushCount)
	}
}

// TestScheduleCompletionWatchIfPending_防重复与无上下文 对齐 :109-123。
func TestScheduleCompletionWatchIfPending_防重复与无上下文(t *testing.T) {
	// 无上下文 → 不布防
	scheduleCompletionWatchIfPending(context.Background(), "Video job j1 is still running.", "")
	watchedPendingJobsMu.Lock()
	n := len(watchedPendingJobs)
	watchedPendingJobsMu.Unlock()
	if n != 0 {
		t.Fatal("无上下文不应布防")
	}
	// 有上下文布防后，同 job_id 重复调用不叠加
	// 用可取消 ctx：j2 盯单 goroutine 随测试结束退出（watchInterval 恢复后不再轮询）。
	ctx, cancel := context.WithCancel(rootCtx("s"))
	defer cancel()
	watchInterval = time.Hour // 阻止真实轮询推进
	defer func() { watchInterval = watchIntervalSeconds }()
	scheduleCompletionWatchIfPending(ctx, "Video job j2 is still running.", "")
	scheduleCompletionWatchIfPending(ctx, "Video job j2 is still running.", "")
	watchedPendingJobsMu.Lock()
	n = len(watchedPendingJobs)
	watchedPendingJobsMu.Unlock()
	if n != 1 {
		t.Fatalf("同 job 应去重: %d", n)
	}
	// 非 pending 消息不布防
	scheduleCompletionWatchIfPending(context.Background(), "[ERROR]: video job j3 ended with status failed", "")
	watchedPendingJobsMu.Lock()
	_, has := watchedPendingJobs["j3"]
	watchedPendingJobsMu.Unlock()
	if has {
		t.Fatal("终态失败不应布防")
	}
	// 清理 j2
	watchedPendingJobsMu.Lock()
	delete(watchedPendingJobs, "j2")
	watchedPendingJobsMu.Unlock()
}

// TestCheckVideoStatusImpl_状态分支 对齐 :533-596 各分支。
func TestCheckVideoStatusImpl_状态分支(t *testing.T) {
	os.Unsetenv("VIDEO_GEN_API_KEY")
	os.Unsetenv("VIDEO_GEN_API_BASE")
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
	if got := checkVideoStatusImpl(context.Background(), "j", ""); !strings.Contains(got, "video generation is not configured") {
		t.Fatalf("未配置: %q", got)
	}
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", "https://b")
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")
	if got := checkVideoStatusImpl(context.Background(), " ", ""); got != "[ERROR]: job_id is required." {
		t.Fatalf("空 job_id: %q", got)
	}

	pollInterval, pollBudget = time.Millisecond, 1
	defer func() { pollInterval, pollBudget = pollIntervalSeconds, maxPollSeconds }()
	_ = pollBudget
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			_, _ = w.Write([]byte("mp4"))
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"id":"j1","status":%q}`, r.Header.Get("X-Mock-Status"))))
	}))
	defer server.Close()
	os.Unsetenv("VIDEO_GEN_PROTOCOL")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)

	// pending → 布防盯单（需清 provider 防误布防）
	// pending 状态消息
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"j1","status":"queued"}`))
	}))
	defer statusServer.Close()
	t.Setenv("VIDEO_GEN_API_BASE", statusServer.URL)
	if got := checkVideoStatusImpl(context.Background(), "j1", ""); got != "Video job j1 is still queued." {
		t.Fatalf("pending: %q", got)
	}

	// completed → 下载
	doneServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			_, _ = w.Write([]byte("mp4"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"j1","status":"completed"}`))
	}))
	defer doneServer.Close()
	t.Setenv("VIDEO_GEN_API_BASE", doneServer.URL)
	saveDir := t.TempDir()
	got := checkVideoStatusImpl(context.Background(), "j1", saveDir)
	if !strings.Contains(got, "Video generated successfully!") {
		t.Fatalf("completed 下载: %q", got)
	}

	// 瞬态网络错误 → unconfirmed pending 形状（不误报失败）
	unreachable := "https://127.0.0.1:1"
	t.Setenv("VIDEO_GEN_API_BASE", unreachable)
	got = checkVideoStatusImpl(context.Background(), "j1", "")
	if !strings.Contains(got, "is still unconfirmed") {
		t.Fatalf("瞬态网络错误: %q", got)
	}
}

// TestWatchUntilSettled_sendFile注入 成功与失败两分支（对齐 :170-193）。
func TestWatchUntilSettled_sendFile注入(t *testing.T) {
	setFastWatch(t)
	saveDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			_, _ = w.Write([]byte("mp4"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"job-sf","status":"completed"}`))
	}))
	defer server.Close()
	t.Setenv("VIDEO_GEN_API_KEY", "k")
	t.Setenv("VIDEO_GEN_API_BASE", server.URL)
	t.Setenv("VIDEO_GEN_MODEL_NAME", "m")

	// 成功分支：真实 SendFileToolkit（D-23.8-5 已回收）——push 宿主在位 →
	// chat.file 投递成功 → 不推送兜底
	SetSendFilePushFunc(func(context.Context, map[string]any) error { return nil })
	SetSendFileHistoryFunc(func(string, string, string, string, string, float64, string, map[string]any, map[string]any, string) {})
	t.Cleanup(func() { SetSendFilePushFunc(nil); SetSendFileHistoryFunc(nil) })
	SetVideoGenRuntimePush(func(map[string]any) bool { return true })
	t.Cleanup(func() { SetVideoGenRuntimePush(nil) })
	watchUntilSettled(rootCtx("s"), "job-sf", saveDir, rootRC("s"))
	waitForWatchDrain(t, time.Second)

	// 清会话去重（新对齐语义：同 session 同路径会被去重跳过）
	ClearSentFilesForSession("s")

	// 失败分支：push 宿主报错 → send_file 兜底「提交文件失败」前缀 →
	// 回落文本通知含本地文件（Python _SEND_FILE_FAILURE_PREFIXES 分支）
	var contents []string
	SetSendFilePushFunc(func(context.Context, map[string]any) error {
		return fmt.Errorf("push host unavailable")
	})
	SetVideoGenRuntimePush(func(msg map[string]any) bool {
		payload, _ := msg["payload"].(map[string]any)
		contents = append(contents, fmt.Sprint(payload["content"]))
		return true
	})
	t.Cleanup(func() { SetVideoGenRuntimePush(nil) })
	watchUntilSettled(rootCtx("s"), "job-sf", saveDir, rootRC("s"))
	waitForWatchDrain(t, time.Second)
	if len(contents) != 1 || !strings.Contains(contents[0], "自动投递到会话失败") || !strings.Contains(contents[0], "本地文件：") {
		t.Fatalf("投递失败回落: %v", contents)
	}
}

// ──────────────────────────── Task 13: 装配门控 smoke ────────────────────────────

// TestCreateVideoGenTools_卡片与工具名 验证装配层拿到的工具卡名与多模态集合判定。
func TestCreateVideoGenTools_卡片与工具名(t *testing.T) {
	gen := CreateGenerateVideoTool()
	check := CreateCheckVideoStatusTool()
	if gen.Card().Name != "generate_video" || check.Card().Name != "check_video_status" {
		t.Fatalf("工具名错: %s/%s", gen.Card().Name, check.Card().Name)
	}
	if !IsMultimodalToolName("generate_video") || !IsMultimodalToolName("check_video_status") {
		t.Fatal("video_gen 工具应在 MULTIMODAL_TOOL_NAMES 集合内")
	}
	vu := CreateVideoUnderstandingTool()
	if vu.Card().Name != "video_understanding" || !IsMultimodalToolName("video_understanding") {
		t.Fatalf("video_understanding 卡名: %s", vu.Card().Name)
	}
}

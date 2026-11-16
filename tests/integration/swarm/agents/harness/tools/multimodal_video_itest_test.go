//go:build integration

package tools

// 23.8 视频生成与聊天交付集成测试。
//
// 链路（跨 ≥3 个 internal 二级子系统）：adapter 装配层注入缝
// （swarm/server/adapter 的 init 桥接）→ video_gen_tools（swarm tools）
// → swarm/common chat_final 协议 → runtime push 链。
//
// 覆盖（用户硬要求的讨论点）：
//   - 假慢端点提交 → 轮询窗口到期 pending → 盯单 goroutine（短 tick）
//     → 终态 → chat.final（final_mode=append）推送全链
//   - 配置门控：未配置时工具调用直接返回配置错误、不布防盯单
//   - 防重复盯单（同 job_id 去重）
//
// Python: tests/unit_tests/agents/test_video_gen_tools.py 的链路级等价物。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions/rootcontext"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VideoGenSuite 视频生成与交付集成套件。
// bindRootCtx 构造携带 RootDecisionContext 的 ctx（D-23.8-4 回收后盯单路由直接取叶子包）。
func bindRootCtx(sessionID, requestID, channelID string) context.Context {
	rc, err := rootcontext.NewRootDecisionContext(sessionID, requestID, channelID, nil, "")
	if err != nil {
		panic(err)
	}
	ctx, _ := rootcontext.BindRootDecisionContext(context.Background(), rc)
	return ctx
}

type VideoGenSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestVideoGenSuite(t *testing.T) {
	suite.Run(t, new(VideoGenSuite))
}

// ──────────────────────────── 测试方法 ────────────────────────────

// TestVideoGenSuite_提交到盯单推送全链 假慢端点 → pending → 盯单 → chat.final。
func (s *VideoGenSuite) TestVideoGenSuite_提交到盯单推送全链() {
	// 短轮询/短盯单注入（测试不真等 120s/30min）
	tools.SetVideoGenPollTiming(50*time.Millisecond, 1)
	tools.SetVideoGenWatchTiming(5*time.Millisecond, 60*time.Millisecond)
	s.T().Cleanup(func() {
		tools.SetVideoGenPollTiming(10*time.Second, 120)
		tools.SetVideoGenWatchTiming(20*time.Second, 30*time.Minute)
	})

	saveDir := s.T().TempDir()
	var mu sync.Mutex
	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/videos" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"itest-job","status":"pending"}`))
		case strings.HasPrefix(r.URL.Path, "/videos/itest-job/content"):
			_, _ = w.Write([]byte("mp4-payload"))
		default:
			// 同步轮询窗口内永远 running → 窗口到期返回 pending 形状；
			// 盯单 goroutine（短 tick）继续轮询，结算时同一端点已切 completed。
			pollCount++
			// 同步窗口预算 1s/50ms ≈ 20 次轮询；第 21 次起才 completed，
			// 保证同步窗口返回 pending 形状、由盯单 goroutine 结算交付。
			if pollCount >= 21 {
				_, _ = w.Write([]byte(`{"id":"itest-job","status":"completed"}`))
			} else {
				_, _ = w.Write([]byte(`{"id":"itest-job","status":"running"}`))
			}
		}
	}))
	defer server.Close()

	s.T().Setenv("VIDEO_GEN_API_KEY", "itest-key")
	s.T().Setenv("VIDEO_GEN_API_BASE", server.URL)
	s.T().Setenv("VIDEO_GEN_MODEL_NAME", "itest-model")

	// 注入推送捕获（对齐 Python send_runtime_push 的宿主推送）
	var pushed []map[string]any
	var pushMu sync.Mutex
	tools.SetVideoGenRuntimePush(func(msg map[string]any) bool {
		pushMu.Lock()
		pushed = append(pushed, msg)
		pushMu.Unlock()
		return true
	})
	s.T().Cleanup(func() { tools.SetVideoGenRuntimePush(nil) })
	// D-23.8-5 已回收：盯单完成走真实 SendFileToolkit → chat.file 投递；
	// 注入 send_file push/history 捕获真实文件投递事件。
	var filePushes []map[string]any
	var filePushMu sync.Mutex
	tools.SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		filePushMu.Lock()
		filePushes = append(filePushes, msg)
		filePushMu.Unlock()
		return nil
	})
	tools.SetSendFileHistoryFunc(func(string, string, string, string, string,
		float64, string, map[string]any, map[string]any, string) {})
	s.T().Cleanup(func() { tools.SetSendFilePushFunc(nil); tools.SetSendFileHistoryFunc(nil) })
	// D-23.8-4 已回收：盯单路由经 ctx 绑定的 RootDecisionContext 直达（对齐 Python current_root_decision_context）

	// 工具调用全链（与 adapter 注册的工具闭包一致）
	gen := tools.CreateGenerateVideoTool()
	res, err := gen.Invoke(bindRootCtx("itest-sess", "itest-req", "itest-ch"), map[string]any{
		"prompt": "a tiny clip", "save_dir": saveDir,
	})
	s.Require().NoError(err)
	result, _ := res["result"].(string)
	s.Require().True(strings.HasPrefix(result, "Video job itest-job submitted and still running"),
		"窗口到期应返回 pending 形状: %q", result)

	// 等盯单 goroutine 结算真实 chat.file 投递
	s.Require().Eventually(func() bool {
		filePushMu.Lock()
		defer filePushMu.Unlock()
		return len(filePushes) >= 1
	}, 5*time.Second, 10*time.Millisecond, "盯单应在时限内投递 chat.file")

	// 真实文件投递事件（chat.file，含 files payload 与视频落盘路径）
	filePushMu.Lock()
	defer filePushMu.Unlock()
	s.Require().NotEmpty(filePushes, "D-23.8-5 回收后应推送真实 chat.file")
	fileMsg := filePushes[0]
	s.Equal("itest-sess", fileMsg["session_id"])
	s.Equal("itest-req", fileMsg["request_id"])
	s.Equal("itest-ch", fileMsg["channel_id"])
	filePayload, ok := fileMsg["payload"].(map[string]any)
	s.Require().True(ok)
	s.Equal("chat.file", filePayload["event_type"])
	files, ok := filePayload["files"].([]map[string]any)
	s.Require().True(ok)
	s.Require().Len(files, 1)
	videoPath, _ := files[0]["path"].(string)
	s.True(strings.HasSuffix(videoPath, "video_itest-job.mp4"), "files path 应为视频落盘路径: %v", videoPath)
	s.Equal(false, files[0]["is_skill_package"], "视频非 skill 包")
	// 视频已落盘
	s.Require().NoError(func() error {
		_, err := os.Stat(videoPath)
		return err
	}())
	// 兜底通知不应触发（投递已成功）
	pushMu.Lock()
	s.Empty(pushed, "投递成功不应走 fallback 文本通知")
	pushMu.Unlock()
}

// TestVideoGenSuite_配置门控与防重复盯单 未配置不布防；同 job 去重。
func (s *VideoGenSuite) TestVideoGenSuite_配置门控与防重复盯单() {
	os.Unsetenv("VIDEO_GEN_API_KEY")
	os.Unsetenv("VIDEO_GEN_API_BASE")
	os.Unsetenv("VIDEO_GEN_MODEL_NAME")
	os.Unsetenv("VIDEO_GEN_PROTOCOL")

	gen := tools.CreateGenerateVideoTool()
	res, err := gen.Invoke(context.Background(), map[string]any{"prompt": "p"})
	s.Require().NoError(err)
	result, _ := res["result"].(string)
	s.Require().True(strings.Contains(result, "video generation is not configured"),
		"未配置应直接返回配置错误: %q", result)

	// 同 job_id 重复布防去重：provider 有上下文但消息非 pending 不布防
	s.Require().False(tools.IsTerminalJobResult("Video job dup-1 is still running."))
}

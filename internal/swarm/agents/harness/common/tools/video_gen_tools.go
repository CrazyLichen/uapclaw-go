package tools

// 文生图/图生视频生成工具。
//
// 对齐 Python jiuwenswarm/agents/harness/common/tools/video_gen_tools.py（609 行）。
// 生成是"提交后轮询"任务（可能耗时数分钟），不是单次请求/响应：
// generate_video 提交任务并同步轮询至多 _MAX_POLL_SECONDS（单次工具调用内短等待可接受）；
// 超时未完返回 job_id 并让 Agent 改调 check_video_status，既不无限阻塞也不谎报视频已就绪。
//
// 凭据来自独立的「视频处理」配置槽（VIDEO_GEN_API_KEY/VIDEO_GEN_API_BASE/
// VIDEO_GEN_MODEL_NAME/VIDEO_GEN_PROTOCOL）——与 video_understanding 读的
// 「视频模型」槽相互独立，两个能力可指向不同服务商。
// 无内置默认端点/模型：配置槽里有什么就用什么；api_key/api_base/model 必须齐全
// （自部署 vLLM-Omni 端点只需 API URL）。
//
// 后台盯单（GitCode #5806）：generate_video/check_video_status 自身的受限轮询窗口
// 到期后任务仍未终态时，本模块起 goroutine 继续轮询（20s 间隔、上限 30 分钟），
// 结算后把结果推进聊天（chat.final, final_mode=append）。没有它，Team/多 Agent
// 模式下提交的慢任务在该轮流结束后永远不会被再次呈现。
//
// 语言差异（偏差登记）：
//   - D-23.8-2：Python 模块级集合/任务列表（单线程）→ Go map+Mutex / goroutine
//   - D-23.8-4：Python 直接 import 的 RootDecisionContext 在 Go 因
//     rails/permissions→tools 循环依赖不可达 → 装配层经
//     SetVideoWatchContextProvider 注入（Task 13 接线）
//   - D-23.8-5：send_runtime_push/SendFileToolkit.send_file 在 Go 为注入缝
//     （SetVideoGenRuntimePush/SetVideoGenSendFileFunc），由 adapter 装配层桥接
//   - Python httpx.AsyncClient → Go net/http；asyncio.create_task → goroutine

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// pollIntervalSeconds generate_video/check_video_status 自身轮询间隔。
	// Python: _POLL_INTERVAL_SECONDS:51
	pollIntervalSeconds = 10 * time.Second
	// maxPollSeconds 单次工具调用内的轮询上限。
	// Python: _MAX_POLL_SECONDS:52
	maxPollSeconds = 120
	// videoMinSeconds 视频时长下限。
	// Python: _VIDEO_MIN_SECONDS:54
	videoMinSeconds = 4
	// videoMaxSeconds 视频时长上限。
	// Python: _VIDEO_MAX_SECONDS:55
	videoMaxSeconds = 15
	// h3MaxMinSeconds MiniMax-H3-Max 拒绝短于 5s 的片段。
	// Python: _H3_MAX_MIN_SECONDS:57
	h3MaxMinSeconds = 5
	// watchIntervalSeconds 盯单 goroutine 轮询间隔。
	// Python: _WATCH_POLL_INTERVAL_SECONDS:66
	watchIntervalSeconds = 20 * time.Second
	// watchMaxSeconds 盯单上限 30 分钟。
	// Python: _WATCH_MAX_SECONDS:67
	watchMaxSeconds = 30 * 60
	// videoGenSubmitTimeoutSeconds 提交请求超时（httpx timeout=60）。
	videoGenSubmitTimeoutSeconds = 60
	// videoGenCheckTimeoutSeconds 状态查询超时（httpx timeout=30）。
	videoGenCheckTimeoutSeconds = 30
)

// 视频生成终态集合。
// Python: _TERMINAL_STATUSES:53
var terminalStatuses = map[string]struct{}{
	"completed": {}, "failed": {}, "cancelled": {}, "expired": {},
}

// genBackend 厂商原生后端标识。
// Python: gen_toolkits.py MINIMAX/MODELARK/DASHSCOPE/VLLM_OMNI:96-99
const (
	backendMinimax   = "minimax"
	backendModelark  = "modelark"
	backendDashscope = "dashscope"
	backendVllmOmni  = "vllm-omni"
)

// nativeBackends 全部厂商原生后端。
// Python: NATIVE_BACKENDS:100
var nativeBackends = map[string]struct{}{
	backendMinimax: {}, backendModelark: {}, backendDashscope: {}, backendVllmOmni: {},
}

// keylessBackends 同时服务自部署端点的后端：无需 api_key/model。
// Python: KEYLESS_BACKENDS:102
var keylessBackends = map[string]struct{}{backendVllmOmni: {}}

// minimaxHostRe / modelarkHostRe / dashscopeHostRe 厂商 host 识别正则。
// Python: _MINIMAX_HOST:118 / _MODELARK_HOST:128 / _DASHSCOPE_HOST:135
var (
	minimaxHostRe   = regexp.MustCompile(`(?i)(^|\.)minimax(i)?\.(io|com)$`)
	modelarkHostRe  = regexp.MustCompile(`(?i)^ark\.[\w-]+\.(bytepluses\.com|volces\.com)$`)
	dashscopeHostRe = regexp.MustCompile(`(?i)^dashscope(-intl)?\.aliyuncs\.com$`)
)

// pendingJobIDRe 识别"仍在跑"消息形状并捕获 job_id。
// Python: _PENDING_JOB_ID_RE:73
var pendingJobIDRe = regexp.MustCompile(`^Video job (\S+) (?:submitted and still|is still) \w+`)

// savedToRe 提取完成消息里的本地保存路径。
// Python: _SAVED_TO_RE:74
var savedToRe = regexp.MustCompile(`Saved to: (.+?)\n`)

// terminalFailureRe 厂商上报终态失败的消息形状。
// Python: _TERMINAL_FAILURE_RE:78
var terminalFailureRe = regexp.MustCompile(`^\[ERROR\]: video job \S+ ended with status \w+`)

// sendFileFailurePrefixes SendFileToolkit.send_file 的中文失败前缀（协议字符串不本地化）。
// Python: _SEND_FILE_FAILURE_PREFIXES:159
var sendFileFailurePrefixes = []string{"发送文件失败", "提交文件失败"}

// ──────────────────────────── 全局变量 ────────────────────────────

// watchedPendingJobs 已有盯单 goroutine 的 job_id 集合（防重复盯单）。
// Python: _WATCHED_PENDING_JOBS:80（单线程集合 → map+Mutex，D-23.8-2）
var (
	watchedPendingJobs   = map[string]struct{}{}
	watchedPendingJobsMu sync.Mutex
)

// watchInterval/watchMax 测试注入点（生产值为上方常量）。
var (
	watchInterval = watchIntervalSeconds
	watchMax      = watchMaxSeconds * time.Second
	pollInterval  = pollIntervalSeconds
	pollBudget    = maxPollSeconds
)

// currentVideoWatchContextProvider 取当前请求的盯单交付路由三元组。
// D-23.8-4：由装配层注入（对齐 Python current_root_decision_context）。
var currentVideoWatchContextProvider func(ctx context.Context) *VideoWatchContext

// videoGenRuntimePush 推送缝（对齐 Python send_runtime_push）。
// D-23.8-5：由 adapter 装配层桥接 SetDefaultSendPushFunc。
var videoGenRuntimePush func(msg map[string]any) bool

// videoGenSendFile 完成视频的文件投递缝（对齐 Python SendFileToolkit.send_file）。
// D-23.8-5：Go 侧 SendFileToolkit 未移植时为 nil → 走 fallback 文本通知分支。
var videoGenSendFile func(wctx *VideoWatchContext, localPath string) string

// ──────────────────────────── 结构体 ────────────────────────────

// VideoWatchContext 盯单交付所需的最小路由上下文。
// Python: RootDecisionContext 的 request_id/session_id/channel_id 三元组（D-23.8-4）。
type VideoWatchContext struct {
	// RequestID 宿主请求标识
	RequestID string
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识
	ChannelID string
}

// videoJobContext 单个视频生成任务的共享传输上下文。
// Python: _JobContext:291-299（client/headers/job_id 三件套）。
type videoJobContext struct {
	// client 共享 HTTP 客户端
	client *http.Client
	// headers 请求头（Authorization）
	headers map[string]string
	// jobID 任务 id
	jobID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetVideoWatchContextProvider 注入盯单交付路由上下文提供者（装配层调用，D-23.8-4）。
func SetVideoWatchContextProvider(fn func(ctx context.Context) *VideoWatchContext) {
	currentVideoWatchContextProvider = fn
}

// SetVideoGenRuntimePush 注入运行时推送缝（装配层调用，D-23.8-5）。
func SetVideoGenRuntimePush(fn func(msg map[string]any) bool) {
	videoGenRuntimePush = fn
}

// SetVideoGenSendFileFunc 注入文件投递缝（装配层调用，D-23.8-5）。
func SetVideoGenSendFileFunc(fn func(wctx *VideoWatchContext, localPath string) string) {
	videoGenSendFile = fn
}

// ExtractPendingJobID 从 pending 消息提取 job_id；不匹配返回空串。
// Python: _extract_pending_job_id:87-90
func ExtractPendingJobID(message string) string {
	m := pendingJobIDRe.FindStringSubmatch(message)
	if m == nil {
		return ""
	}
	return m[1]
}

// IsTerminalJobResult 结果是否为确定性终局（成功或厂商上报终态）。
// Python: _is_terminal_job_result:92-107
// 瞬态错误（非 200、坏 JSON、下载抖动、凭据中途被清等）不是这两种形状 → 继续轮询。
func IsTerminalJobResult(result string) bool {
	return strings.HasPrefix(result, "Video generated successfully!") ||
		terminalFailureRe.MatchString(result)
}

// VideoGenEnabled 「视频处理」开关是否打开。
// Python: video_gen_enabled:217-227
// 镜像 multimodal_model_enabled 的环境变量门控：VIDEO_GEN_ENABLED 独立于配置是否
// 完整，允许保留有效凭据的同时关闭生成。
func VideoGenEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VIDEO_GEN_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// VideoGenConfigured 视频处理配置（key/base/model）是否完整。
// Python: video_gen_configured:240-249
func VideoGenConfigured() bool {
	apiKey, apiBase, model := getVideoGenAPICredentials()
	return videoGenCredentialsComplete(apiKey, apiBase, model)
}

// DetectVideoGenBackend 返回配置槽的厂商原生后端，空串=OpenRouter 式直连路径。
// Python: gen_toolkits.detect_backend:236-252
func DetectVideoGenBackend(protocolEnv string, apiBase string) string {
	protocol := strings.ToLower(strings.TrimSpace(os.Getenv(protocolEnv)))
	if _, ok := nativeBackends[protocol]; ok {
		return protocol
	}
	host := hostOf(apiBase)
	if minimaxHostRe.MatchString(host) {
		return backendMinimax
	}
	if modelarkHostRe.MatchString(host) {
		return backendModelark
	}
	if dashscopeHostRe.MatchString(host) {
		return backendDashscope
	}
	return ""
}

// CreateGenerateVideoTool 构造 generate_video 工具。
// Python: @tool(name="generate_video")（:380 平铺参数签名，供 LLM 工具调用）。
func CreateGenerateVideoTool() tool.Tool {
	description := "Generate a video clip from a text prompt (and optionally a first-frame image, or " +
		"reference images the clip must stay consistent with) using AI video generation " +
		"models. Use this tool when the user wants to create or generate a video based on " +
		"a text description. Video generation can take " +
		"several minutes; if this tool returns a job_id instead of a finished video, call " +
		"check_video_status with that job_id to keep waiting for it instead of resubmitting."
	card := tool.NewToolCard("generate_video", description, nil, nil)
	fn := func(ctx context.Context, input struct {
		Prompt              string   `json:"prompt"`
		AspectRatio         string   `json:"aspect_ratio"`
		Resolution          string   `json:"resolution"`
		DurationSeconds     int      `json:"duration_seconds"`
		FirstFramePath      string   `json:"first_frame_path"`
		GenerateAudio       bool     `json:"generate_audio"`
		SaveDir             string   `json:"save_dir"`
		ReferenceImagePaths []string `json:"reference_image_paths"`
		ReferenceMode       bool     `json:"reference_mode"`
	}, _ ...tool.ToolOption) (map[string]any, error) {
		result := generateVideoCore(ctx, map[string]any{
			"prompt":                input.Prompt,
			"aspect_ratio":          input.AspectRatio,
			"resolution":            input.Resolution,
			"duration_seconds":      input.DurationSeconds,
			"first_frame_path":      input.FirstFramePath,
			"generate_audio":        input.GenerateAudio,
			"save_dir":              input.SaveDir,
			"reference_image_paths": input.ReferenceImagePaths,
			"reference_mode":        input.ReferenceMode,
		})
		return map[string]any{"result": result}, nil
	}
	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// CreateCheckVideoStatusTool 构造 check_video_status 工具。
// Python: @tool(name="check_video_status")（:597-609）
func CreateCheckVideoStatusTool() tool.Tool {
	description := "Poll a previously submitted generate_video job by job_id and download it once " +
		"complete. Use this after generate_video returns a job_id instead of a finished video."
	card := tool.NewToolCard("check_video_status", description, nil, nil)
	fn := func(ctx context.Context, input struct {
		JobID   string `json:"job_id"`
		SaveDir string `json:"save_dir"`
	}, _ ...tool.ToolOption) (map[string]any, error) {
		result := checkVideoStatusCore(ctx, map[string]any{
			"job_id":   input.JobID,
			"save_dir": input.SaveDir,
		})
		return map[string]any{"result": result}, nil
	}
	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hostOf 取 API URL 的 host 部分。
// Python: gen_toolkits._host_of
func hostOf(apiBase string) string {
	s := strings.TrimSpace(apiBase)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// getVideoGenAPICredentials 读独立「视频处理」槽凭据；空串=未配置。
// Python: _get_video_gen_api_credentials:229-238
func getVideoGenAPICredentials() (string, string, string) {
	return strings.TrimSpace(os.Getenv("VIDEO_GEN_API_KEY")),
		strings.TrimSpace(os.Getenv("VIDEO_GEN_API_BASE")),
		strings.TrimSpace(os.Getenv("VIDEO_GEN_MODEL_NAME"))
}

// videoGenCredentialsComplete 凭据完整性（keyless 后端只需 base）。
// Python: _credentials_complete:250-254
func videoGenCredentialsComplete(apiKey, apiBase, model string) bool {
	if _, keyless := keylessBackends[DetectVideoGenBackend("VIDEO_GEN_PROTOCOL", apiBase)]; keyless {
		return apiBase != ""
	}
	return apiKey != "" && apiBase != "" && model != ""
}

// clampDuration 按模型钳制时长（H3 下限抬至 5s）。
// Python: _clamp_duration:256-260
func clampDuration(model string, durationSeconds int) int {
	var compact strings.Builder
	for _, ch := range strings.ToLower(model) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			compact.WriteRune(ch)
		}
	}
	low := videoMinSeconds
	if strings.Contains(compact.String(), "h3max") {
		low = h3MaxMinSeconds
	}
	if durationSeconds < low {
		return low
	}
	if durationSeconds > videoMaxSeconds {
		return videoMaxSeconds
	}
	return durationSeconds
}

// resolveFrameReference 首帧/参考图解析为 data URI。
// Python: _resolve_frame_reference:262-282
// 返回 (uri, err)；URL/data URI 原样；本地路径服务端读字节转 base64。
func resolveFrameReference(pathOrURL string, argName string) (string, string) {
	value := strings.TrimSpace(pathOrURL)
	if value == "" {
		return "", ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:") {
		return value, ""
	}
	expanded := expandHome(value)
	info, err := os.Stat(expanded)
	if err != nil || info.IsDir() {
		return "", fmt.Sprintf("[ERROR]: %s %q is not a URL/data URI and no such file exists.", argName, value)
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return "", fmt.Sprintf("[ERROR]: %s %q read failed: %v", argName, value, err)
	}
// Python: mimetypes.guess_type → Go 图片扩展名推断
	mime := guessImageMimeByExt(expanded)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), ""
}

// resolveSavePath 解析保存目录并确保存在。
// Python: _resolve_save_path:284-289
func resolveSavePath(saveDir string, filename string) string {
	root := saveDir
	if root == "" {
		root = filepath.Join(workspace.AgentWorkspaceDir(), "generated_videos")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return ""
	}
	return filepath.Join(root, filename)
}

// downloadVideo 下载任务产物并落盘。
// Python: _download_video:302-324
func downloadVideo(vctx *videoJobContext, apiBase string, saveDir string) string {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/videos/"+vctx.jobID+"/content?index=0", nil)
	if err != nil {
		return fmt.Sprintf("[ERROR]: downloading video job %s failed: %v", vctx.jobID, err)
	}
	for k, v := range vctx.headers {
		req.Header.Set(k, v)
	}
	resp, err := vctx.client.Do(req)
	if err != nil {
		return fmt.Sprintf("[ERROR]: downloading video job %s failed: %v", vctx.jobID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("[ERROR]: video job %s completed but downloading content failed: %d", vctx.jobID, resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("[ERROR]: downloading video job %s failed: %v", vctx.jobID, err)
	}

	filename := "video_" + vctx.jobID + ".mp4"
	target := resolveSavePath(saveDir, filename)
	if target == "" {
		return fmt.Sprintf("[ERROR]: failed to save video (%s) to %s", filename, saveDirOrWorkspace(saveDir))
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return fmt.Sprintf("[ERROR]: failed to save video (%s) to %s: %v", filename, saveDirOrWorkspace(saveDir), err)
	}
	return "Video generated successfully!\n" +
		"Saved to: " + target + "\n" +
		"(job " + vctx.jobID + " - the remote source URL expires 24h after completion, so this " +
		"local file is now the durable copy.)"
}

// saveDirOrWorkspace 错误文案里的落盘位置描述。
func saveDirOrWorkspace(saveDir string) string {
	if saveDir != "" {
		return saveDir
	}
	return "agent workspace"
}

// pollJob 轮询至终态或预算耗尽。
// Python: _poll_job:326-378
// 返回 (status, job, elapsedSeconds, pollError)；pollError 非空时调用方直接返回它。
// elapsed 以 pollInterval 步进累计（Python elapsed += _POLL_INTERVAL_SECONDS 等价）。
func pollJob(vctx *videoJobContext, pollingURL string, status string, job map[string]any) (string, map[string]any, int, string) {
	// Python: 以调用方已知状态播种，避免提交即终态时丢失 error 字段
	if job == nil {
		job = map[string]any{}
	}
	budget := time.Duration(pollBudget) * time.Second
	elapsed := time.Duration(0)
	for {
		if _, terminal := terminalStatuses[status]; terminal || elapsed >= budget {
			return status, job, int(elapsed.Seconds()), ""
		}
		time.Sleep(pollInterval)
		elapsed += pollInterval
		req, err := http.NewRequest(http.MethodGet, pollingURL, nil)
		if err != nil {
			return status, job, int(elapsed.Seconds()), fmt.Sprintf("[ERROR]: polling video job %s failed: %v", vctx.jobID, err)
		}
		for k, v := range vctx.headers {
			req.Header.Set(k, v)
		}
		resp, err := vctx.client.Do(req)
		if err != nil {
			return status, job, int(elapsed.Seconds()), fmt.Sprintf("[ERROR]: polling video job %s failed: %v", vctx.jobID, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return status, job, int(elapsed.Seconds()), fmt.Sprintf(
				"[ERROR]: polling video job %s failed: %d %s", vctx.jobID, resp.StatusCode, truncate(string(body), 200))
		}
		var polled map[string]any
		if err := json.Unmarshal(body, &polled); err != nil {
			return status, job, int(elapsed.Seconds()), fmt.Sprintf(
				"[ERROR]: polling video job %s returned invalid JSON: %v", vctx.jobID, err)
		}
		job = polled
		status = strValOrEmpty(polled["status"])
		if status == "" {
			status = "pending"
		}
	}
}

// generateVideoCore generate_video 主体。
// Python: generate_video:380-532（try/except 包裹 + 平铺参数）
func generateVideoCore(ctx context.Context, inputs map[string]any) string {
	apiKey, apiBase, model := getVideoGenAPICredentials()
	if !videoGenCredentialsComplete(apiKey, apiBase, model) {
		return "[ERROR]: video generation is not configured - set the Video processing " +
			"API key, API URL, and model name in configuration settings."
	}
	promptText := strings.TrimSpace(strValOrEmpty(inputs["prompt"]))
	if promptText == "" {
		return "[ERROR]: prompt is required."
	}

	aspectRatio := strValOrEmpty(strOrAny(inputs["aspect_ratio"], "16:9"))
	resolution := strValOrEmpty(strOrAny(inputs["resolution"], "480p"))
	durationSeconds := toIntDefault(inputs["duration_seconds"], 15)
	generateAudio, _ := inputs["generate_audio"].(bool)
	saveDir := strValOrEmpty(inputs["save_dir"])
	referenceMode, _ := inputs["reference_mode"].(bool)

	// Python: first_frame_path 解析
	frameDataURI := ""
	if fp := strValOrEmpty(inputs["first_frame_path"]); fp != "" {
		uri, errMsg := resolveFrameReference(fp, "first_frame_path")
		if errMsg != "" {
			return errMsg
		}
		frameDataURI = uri
	}
	// Python: reference_image_paths 逐条解析
	var referenceURIs []string
	if refs, ok := inputs["reference_image_paths"].([]string); ok {
		for _, ref := range refs {
			uri, errMsg := resolveFrameReference(ref, "reference_image_paths")
			if errMsg != "" {
				return errMsg
			}
			referenceURIs = append(referenceURIs, uri)
		}
	}
	if referenceMode && len(referenceURIs) == 0 && frameDataURI == "" {
		return "[ERROR]: reference_mode needs reference_image_paths or first_frame_path."
	}

	// Python: 厂商原生后端分发（gen_toolkits；Go 侧后端提交链 ⤵️ gen_toolkits.py 全量移植）
	backend := DetectVideoGenBackend("VIDEO_GEN_PROTOCOL", apiBase)
	if backend != "" {
		return "[ERROR]: video generation backend " + backend +
			" requires the gen_toolkits dispatch chain, which is not yet ported (registered as pending)."
	}
	if len(referenceURIs) > 0 || referenceMode {
		return fmt.Sprintf(
			"[ERROR]: the configured video model %q (%s) only supports text-to-video and "+
				"first-frame image-to-video, not reference images. Retry with first_frame_path only, or ask the "+
				"user to switch Settings > Agent (Video generation) to a provider that supports reference-to-video: "+
				"MiniMax (MiniMax-H3), BytePlus ModelArk / Volcengine (Seedance 2.x), Alibaba DashScope "+
				"(wan3.0-video / wan3.0-video-prime) or a self-deployed vLLM-Omni server.", model, apiBase)
	}

	// Python: body 组装（frame_images 首帧块）
	body := map[string]any{
		"model":          model,
		"prompt":         promptText,
		"aspect_ratio":   aspectRatio,
		"resolution":     resolution,
		"duration":       durationSeconds,
		"generate_audio": generateAudio,
	}
	if frameDataURI != "" {
		body["frame_images"] = []map[string]any{
			{"type": "image_url", "image_url": map[string]any{"url": frameDataURI}, "frame_type": "first_frame"},
		}
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	logger.Info(logComponent).
		Str("model", model).
		Str("api_base", apiBase).
		Str("aspect_ratio", aspectRatio).
		Str("resolution", resolution).
		Int("duration", durationSeconds).
		Msg("[generate_video] using model")

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Sprintf("[ERROR]: video generation request failed: %v", err)
	}
	client := newVideoGenClient(videoGenSubmitTimeoutSeconds)

	// Python: 提交
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/videos", bytesReader(bodyBytes))
	if err != nil {
		return fmt.Sprintf("[ERROR]: video generation request failed: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	submit, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("[ERROR]: video generation request failed: %v", err)
	}
	submitBody, _ := io.ReadAll(submit.Body)
	_ = submit.Body.Close()
	if submit.StatusCode != http.StatusOK && submit.StatusCode != http.StatusCreated && submit.StatusCode != http.StatusAccepted {
		return fmt.Sprintf("[ERROR]: video generation submit failed: %d %s", submit.StatusCode, truncate(string(submitBody), 200))
	}
	var job map[string]any
	if err := json.Unmarshal(submitBody, &job); err != nil {
		return fmt.Sprintf("[ERROR]: video generation submit returned invalid JSON: %v", err)
	}
	jobID := strValOrEmpty(job["id"])
	if jobID == "" {
		return fmt.Sprintf("[ERROR]: video generation submit returned no job id: %s", truncate(string(submitBody), 200))
	}
	pollingURL := strValOrEmpty(job["polling_url"])
	if pollingURL == "" {
		pollingURL = apiBase + "/videos/" + jobID
	}
	status := strValOrEmpty(job["status"])
	if status == "" {
		status = "pending"
	}
	vctx := &videoJobContext{client: client, headers: headers, jobID: jobID}

	// Python: 轮询至终态或预算耗尽
	status, job, elapsed, pollError := pollJob(vctx, pollingURL, status, job)
	if pollError != "" {
		return pollError
	}
	if _, terminal := terminalStatuses[status]; !terminal {
		pendingMessage := fmt.Sprintf(
			"Video job %s submitted and still %s after %ds - generation can "+
				"take several minutes. Call check_video_status with job_id=%s to check progress "+
				"and download it once ready.", jobID, status, elapsed, jobID)
		scheduleCompletionWatchIfPending(ctx, pendingMessage, saveDir)
		return pendingMessage
	}
	if status != "completed" {
		errDetail := "no error detail provided"
		if e, ok := job["error"].(string); ok && e != "" {
			errDetail = e
		}
		return fmt.Sprintf("[ERROR]: video job %s ended with status %s: %s", jobID, status, errDetail)
	}
	return downloadVideo(vctx, apiBase, saveDir)
}

// checkVideoStatusImpl check_video_status 实现。
// Python: _check_video_status_impl:533-596
func checkVideoStatusImpl(ctx context.Context, jobID string, saveDir string) string {
	apiKey, apiBase, _ := getVideoGenAPICredentials()
	backend := DetectVideoGenBackend("VIDEO_GEN_PROTOCOL", apiBase)
	if apiBase == "" || (apiKey == "" && !isKeylessBackend(backend)) {
		return "[ERROR]: video generation is not configured - set the Video processing " +
			"API key, API URL, and model name in configuration settings."
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return "[ERROR]: job_id is required."
	}
	if backend != "" {
		return "[ERROR]: video generation backend " + backend +
			" requires the gen_toolkits dispatch chain, which is not yet ported (registered as pending)."
	}

	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	client := newVideoGenClient(videoGenCheckTimeoutSeconds)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/videos/"+jobID, nil)
	if err != nil {
		return fmt.Sprintf("[ERROR]: checking video job %s failed: %v", jobID, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	poll, err := client.Do(req)
	if err != nil {
		// Python: 瞬态网络错误 → "still unconfirmed" pending 形状（盯单继续轮询，
		// 不把它当终态误报失败）。
		pendingMessage := fmt.Sprintf(
			"Video job %s is still unconfirmed - the provider could not be reached (%v). "+
				"Call check_video_status again to keep waiting (do not use shell sleep).", jobID, err)
		scheduleCompletionWatchIfPending(ctx, pendingMessage, saveDir)
		return pendingMessage
	}
	body, _ := io.ReadAll(poll.Body)
	_ = poll.Body.Close()
	if poll.StatusCode != http.StatusOK {
		return fmt.Sprintf("[ERROR]: checking video job %s failed: %d %s", jobID, poll.StatusCode, truncate(string(body), 200))
	}
	var job map[string]any
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Sprintf("[ERROR]: checking video job %s returned invalid JSON: %v", jobID, err)
	}
	status := strValOrEmpty(job["status"])
	if status == "" {
		status = "unknown"
	}
	if status == "completed" {
		return downloadVideo(&videoJobContext{client: client, headers: headers, jobID: jobID}, apiBase, saveDir)
	}
	if _, terminal := terminalStatuses[status]; terminal {
		errDetail := "no error detail provided"
		if e, ok := job["error"].(string); ok && e != "" {
			errDetail = e
		}
		return fmt.Sprintf("[ERROR]: video job %s ended with status %s: %s", jobID, status, errDetail)
	}
	pendingMessage := fmt.Sprintf("Video job %s is still %s.", jobID, status)
	scheduleCompletionWatchIfPending(ctx, pendingMessage, saveDir)
	return pendingMessage
}

// checkVideoStatusCore check_video_status 工具主体。
// Python: check_video_status:597-609
func checkVideoStatusCore(ctx context.Context, inputs map[string]any) string {
	return checkVideoStatusImpl(ctx, strValOrEmpty(inputs["job_id"]), strValOrEmpty(inputs["save_dir"]))
}

// isKeylessBackend 后端是否免密钥。
func isKeylessBackend(backend string) bool {
	_, ok := keylessBackends[backend]
	return ok
}

// newVideoGenClient 构建带超时的 HTTP 客户端（verify 走 getRequestsVerify）。
func newVideoGenClient(timeoutSeconds int) *http.Client {
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	if !getRequestsVerify() {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return client
}

// guessImageMimeByExt 首帧/参考图 mime 推断（image/ 前缀外的回落由调用方处理）。
func guessImageMimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// bytesReader 字节切片转 Reader。
func bytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }

// scheduleCompletionWatchIfPending 首次出现"仍在跑"消息时布防后台盯单。
// Python: _schedule_completion_watch_if_pending:109-123
// goroutine 启动位置与 Python asyncio.create_task 调用点一一对应（用户已确认）。
func scheduleCompletionWatchIfPending(ctx context.Context, result string, saveDir string) {
	jobID := ExtractPendingJobID(result)
	if jobID == "" {
		return
	}
	watchedPendingJobsMu.Lock()
	if _, dup := watchedPendingJobs[jobID]; dup {
		watchedPendingJobsMu.Unlock()
		return
	}
	wctx := (*VideoWatchContext)(nil)
	if currentVideoWatchContextProvider != nil {
		wctx = currentVideoWatchContextProvider(ctx)
	}
	if wctx == nil {
		// Python: 无会话/请求可交付（如 CLI/离线调用）——调用方应自行轮询
		// check_video_status；不布防。
		watchedPendingJobsMu.Unlock()
		return
	}
	watchedPendingJobs[jobID] = struct{}{}
	watchedPendingJobsMu.Unlock()
	go watchUntilSettled(ctx, jobID, saveDir, wctx)
}

// watchUntilSettled 后台盯单：轮询至终态交付或超限放弃。
// Python: _watch_until_settled:125-157
func watchUntilSettled(ctx context.Context, jobID string, saveDir string, wctx *VideoWatchContext) {
	defer func() {
		// Python: finally: _WATCHED_PENDING_JOBS.discard(job_id)
		watchedPendingJobsMu.Lock()
		delete(watchedPendingJobs, jobID)
		watchedPendingJobsMu.Unlock()
	}()
	defer func() {
		// Python: except Exception: logger.exception(...)
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("job_id", jobID).
				Str("error", fmt.Sprint(r)).
				Msg("[generate_video] background completion watch failed")
		}
	}()
	elapsed := time.Duration(0)
	for elapsed < watchMax {
		select {
		case <-ctx.Done():
			return
		case <-time.After(watchInterval):
		}
		elapsed += watchInterval
		result := checkVideoStatusImpl(ctx, jobID, saveDir)
		if IsTerminalJobResult(result) {
			deliverWatchResult(ctx, jobID, result, wctx)
			return
		}
		if ExtractPendingJobID(result) == "" {
			// 既非终态也非可识别的 pending 形状——瞬态/中间错误，继续等待不误报。
			logger.Warn(logComponent).
				Str("job_id", jobID).
				Str("result", truncate(result, 200)).
				Msg("[generate_video] non-terminal error while watching job, retrying")
		}
	}
	logger.Warn(logComponent).
		Str("job_id", jobID).
		Int("elapsed_seconds", int(watchMax.Seconds())).
		Msg("[generate_video] gave up watching job for completion")
}

// deliverWatchResult 把已结算任务的结果推进其来源聊天。
// Python: _deliver_watch_result:162-199
func deliverWatchResult(ctx context.Context, jobID string, result string, wctx *VideoWatchContext) {
	// Python: saved_to = _SAVED_TO_RE.search(result + "\n")
	savedTo := savedToRe.FindStringSubmatch(result + "\n")
	if savedTo != nil {
		localPath := strings.TrimSpace(savedTo[1])
		if videoGenSendFile != nil {
			delivery := videoGenSendFile(wctx, localPath)
			if hasSendFileFailurePrefix(delivery) {
				// 视频本身没问题，只是投递失败：不谎报成功，也不丢视频——
				// 回落文本通知并说明本地文件位置。
				logger.Warn(logComponent).
					Str("job_id", jobID).
					Str("session_id", wctx.SessionID).
					Str("delivery", truncate(delivery, 200)).
					Msg("[generate_video] send_file_to_user failed delivering completed job")
				pushFallbackNotice(ctx, jobID, wctx,
					fmt.Sprintf("视频已生成，但自动投递到会话失败：%s\n本地文件：%s", delivery, localPath))
				return
			}
			logger.Info(logComponent).
				Str("job_id", jobID).
				Str("session_id", wctx.SessionID).
				Str("delivery", truncate(delivery, 200)).
				Msg("[generate_video] delivered completed job")
			return
		}
		// D-23.8-5：Go 侧 SendFileToolkit 未移植 → 与投递失败同路的文本通知。
		pushFallbackNotice(ctx, jobID, wctx,
			fmt.Sprintf("视频已生成：\n本地文件：%s", localPath))
		return
	}
	pushFallbackNotice(ctx, jobID, wctx, fmt.Sprintf("视频生成任务 %s 未能成功完成：%s", jobID, result))
}

// pushFallbackNotice 经 runtime push 推送兜底文本通知（chat.final, append）。
// Python: _push_fallback_notice:200-215
func pushFallbackNotice(_ context.Context, jobID string, wctx *VideoWatchContext, content string) {
	if videoGenRuntimePush == nil {
		logger.Warn(logComponent).
			Str("job_id", jobID).
			Str("session_id", wctx.SessionID).
			Msg("[generate_video] no active push host to deliver job result")
		return
	}
	payload := map[string]any{"content": content}
	pushed := videoGenRuntimePush(map[string]any{
		"request_id":  wctx.RequestID,
		"channel_id":  wctx.ChannelID,
		"session_id":  wctx.SessionID,
		"payload":     swarmcommon.AnnotateChatFinal(payload, swarmcommon.FinalModeAppend),
		"is_complete": false,
	})
	if !pushed {
		logger.Warn(logComponent).
			Str("job_id", jobID).
			Str("session_id", wctx.SessionID).
			Msg("[generate_video] no active push host to deliver job result")
	}
}

// hasSendFileFailurePrefix 判断投递结果是否为失败文案。
// Python: delivery.startswith(_SEND_FILE_FAILURE_PREFIXES)
func hasSendFileFailurePrefix(delivery string) bool {
	for _, prefix := range sendFileFailurePrefixes {
		if strings.HasPrefix(delivery, prefix) {
			return true
		}
	}
	return false
}

// Package harness 内图像输入模态探测：判定模型能否接收原生图片输入。
//
// Python: openjiuwen/harness/image_modality_probe.py（一文件一文件移植）
package harness

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// probeCacheKey 探测结论缓存键 (api_base, model_name)。
// Python: image_modality_probe.py:160-168 probe_cache_key 返回的 tuple
type probeCacheKey struct {
	// APIBase 模型客户端 API 基础 URL
	APIBase string
	// ModelName 面向 provider 的模型名
	ModelName string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// imageInputScanMaxDepth 异常错误值递归收集的最大深度。
	// Python: _IMAGE_INPUT_SCAN_MAX_DEPTH = 8
	imageInputScanMaxDepth = 8
	// imageModalityProbeTimeout 单次探测请求超时。
	// Python: _IMAGE_MODALITY_PROBE_TIMEOUT_SECONDS = 5.0
	imageModalityProbeTimeout = 5 * time.Second
	// imageModalityProbeMaxTokens 探测回答的 token 预算。
	// 只需要模型说出一个颜色，但保留少量余量避免"前面几个词被截断"被
	// 永久缓存为 image-blind；截断答案另行按"未知"处理（见 interpretProbeResponse）。
	// Python: _IMAGE_MODALITY_PROBE_MAX_TOKENS = 64
	imageModalityProbeMaxTokens = 64
	// lengthFinishReason 表示回答命中 token 预算的 finish_reason。
	// Python: _LENGTH_FINISH_REASON = "length"
	lengthFinishReason = "length"
	// probeRouterScope 探测专用路由作用域：探测失败不冷却正式部署。
	// Python: _PROBE_ROUTER_SCOPE = "image_modality_probe"
	probeRouterScope = "image_modality_probe"
)

// imageInputUnsupportedErrorCodes 确定性拒绝图片输入的错误码（归一化后精确/后缀匹配）。
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_CODES:39-46（6 个）
var imageInputUnsupportedErrorCodes = [...]string{
	"invalid_image_input",
	"image_input_unsupported",
	"unsupported_content_type",
	"unsupported_image",
	"unsupported_image_input",
	"unsupported_message_content_type",
}

// imageInputUnsupportedErrorPatterns 拒绝图片输入的错误文案片段（子串匹配）。
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_PATTERNS:47-64（16 条）
var imageInputUnsupportedErrorPatterns = [...]string{
	"no endpoints found that support image input",
	"does not accept images",
	"does not support image",
	"doesn't accept images",
	"doesn't support image",
	"do not support image",
	"image input is not supported",
	"image input not supported",
	"image_url is not supported",
	"images are not supported",
	"multimodal input is not supported",
	"not a multimodal model",
	"not support image input",
	"unknown variant",
	"unsupported image",
	"vision is not supported",
}

// thinkingDisabledExtraBody 各厂商思考关闭开关合并进一个 body。
// 宽松网关丢弃不认识的键；严格网关拒绝整个请求——这正是 runProbe
// 去掉开关重试一次的原因。
// Python: _THINKING_DISABLED_EXTRA_BODY:30-34
var thinkingDisabledExtraBody = map[string]any{
	"thinking":         map[string]any{"type": "disabled"},
	"enable_thinking":  false,
	"reasoning":        map[string]any{"enabled": false},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// dummyImageB64 现场生成的 32x32 纯红 PNG（base64）。
// Python: DUMMY_IMAGE_B64（_make_red_png_b64:67-83）
var dummyImageB64 = makeRedPngB64()

// probeResults 探测结论缓存：(api_base, model_name) → bool，进程生命周期。
// Python: _probe_results:37
var probeResults sync.Map // probeCacheKey → bool

// probeInFlight 防重复调度：在途探测任务集合。
// Python: _probe_tasks:38（asyncio.Task → goroutine 无句柄需求，只占位防重）
var probeInFlight sync.Map // probeCacheKey → struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeRedPngB64 生成一张 32x32 纯红 PNG 并返回 base64 编码。
// Python: _make_red_png_b64:67-83（手写 IHDR/IDAT/IEND，无图片库依赖）
func MakeRedPngB64() string {
	return makeRedPngB64()
}

// ProbeCacheKeyOf 返回模型的探测缓存键；无法解析（模型名缺失）时返回 nil。
// Python: probe_cache_key:160-168
func ProbeCacheKeyOf(model *llm.Model) *probeCacheKey {
	return probeCacheKeyOf(model)
}

// GetCachedImageSupport 返回模型已缓存的探测结论；未探测过返回 nil。
// Python: get_cached_image_support:171-176
func GetCachedImageSupport(model *llm.Model) *bool {
	key := probeCacheKeyOf(model)
	if key == nil {
		return nil
	}
	return cachedImageSupport(*key)
}

// SetCachedImageSupport 预填缓存：模型条目显式声明 supports_vision 时免探测。
// Python: set_cached_image_support:179-189
func SetCachedImageSupport(key probeCacheKey, supported bool) {
	probeResults.Store(key, supported)
}

// ResetImageSupportCache 清空缓存结论与在途探测标记。供测试与重配置使用。
// Python: reset_image_support_cache:192-197
func ResetImageSupportCache() {
	probeResults.Range(func(k, _ any) bool {
		probeResults.Delete(k)
		return true
	})
	probeInFlight.Range(func(k, _ any) bool {
		probeInFlight.Delete(k)
		return true
	})
}

// ScheduleImageSupportProbe 在后台探测模型，每个 (api_base, model_name) 至多一次。
// 调用方不需要等结果：结论落地前 read_file 保持元数据模式，后续同端点模型的
// Agent 构建时从缓存取结论。
// Python: schedule_image_support_probe:200-231（asyncio task → goroutine）
func ScheduleImageSupportProbe(model *llm.Model) {
	key := probeCacheKeyOf(model)
	if key == nil {
		return
	}
	if _, done := probeResults.Load(*key); done {
		return
	}
	if _, running := probeInFlight.LoadOrStore(*key, struct{}{}); running {
		return
	}
	go func() {
		defer probeInFlight.Delete(*key)
		probeAndCache(model, *key)
	}()
}

// ProbeImageSupport 同步探测模型是否接受原生图片输入（复用缓存）。
// 返回：true=说出了颜色；false=未说出颜色或确定性拒绝；nil=结论未知
// （超时、鉴权/限流/5xx 等调用失败、模型什么都没说），不缓存。
// Python: probe_image_support:422-439
func ProbeImageSupport(ctx context.Context, model *llm.Model) *bool {
	key := probeCacheKeyOf(model)
	if key != nil {
		if cached := cachedImageSupport(*key); cached != nil {
			return cached
		}
	}
	supported := runProbe(ctx, model)
	if supported != nil && key != nil {
		probeResults.Store(*key, *supported)
	}
	return supported
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeRedPngB64 手写 32x32 纯红 PNG 的字节并 base64 编码。
// Python: _make_red_png_b64:67-83（struct+zlib → encoding/binary+compress/zlib）
func makeRedPngB64() string {
	const width, height = 32, 32

	// Python: _chunk(tag, data) = len + tag + data + crc32(tag+data)
	chunk := func(tag string, data []byte) []byte {
		out := make([]byte, 0, 12+len(data))
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
		out = append(out, lenBuf[:]...)
		out = append(out, tag...)
		out = append(out, data...)
		var crcBuf [4]byte
		binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(append([]byte(tag), data...)))
		out = append(out, crcBuf[:]...)
		return out
	}

	// 红色行：滤波字节 0x00 + width * (ff 00 00)
	redRow := make([]byte, 0, 1+width*3)
	redRow = append(redRow, 0x00)
	for i := 0; i < width; i++ {
		redRow = append(redRow, 0xff, 0x00, 0x00)
	}

	// IHDR: width, height, bit depth 8, color type 2 (truecolor), 其余 0
	ihdrData := make([]byte, 0, 13)
	var dims [8]byte
	binary.BigEndian.PutUint32(dims[:4], width)
	binary.BigEndian.PutUint32(dims[4:], height)
	ihdrData = append(ihdrData, dims[:]...)
	ihdrData = append(ihdrData, 8, 2, 0, 0, 0)

	rawIDAT, err := zlibCompressBytes(redRow)
	if err != nil {
		// zlib 压缩内存字节不会失败；保底返回空串避免携带半成品
		return ""
	}

	png := make([]byte, 0, 8+len(ihdrData)+len(rawIDAT)+24)
	png = append(png, "\x89PNG\r\n\x1a\n"...)
	png = append(png, chunk("IHDR", ihdrData)...)
	png = append(png, chunk("IDAT", rawIDAT)...)
	png = append(png, chunk("IEND", nil)...)
	return base64.StdEncoding.EncodeToString(png)
}

// probeCacheKeyOf 从 Model 提取 (api_base, model_name)；模型名缺失返回 nil。
// Python: probe_cache_key:160-168
func probeCacheKeyOf(model *llm.Model) *probeCacheKey {
	if model == nil || model.ModelConfig == nil {
		return nil
	}
	modelName := strings.TrimSpace(model.ModelConfig.ModelName)
	if modelName == "" {
		return nil
	}
	apiBase := ""
	if model.ClientConfig != nil {
		apiBase = strings.TrimSpace(model.ClientConfig.APIBase)
	}
	return &probeCacheKey{APIBase: apiBase, ModelName: modelName}
}

// cachedImageSupport 读缓存；未命中返回 nil。
func cachedImageSupport(key probeCacheKey) *bool {
	if v, ok := probeResults.Load(key); ok {
		b := v.(bool)
		return &b
	}
	return nil
}

// probeAndCache 跑一次探测并把结论性结果写入缓存；探测失败仅记日志不缓存。
// Python: _probe_and_cache:234-251
func probeAndCache(model *llm.Model, key probeCacheKey) {
	supported := runProbe(context.Background(), model)
	if supported == nil {
		return
	}
	probeResults.Store(key, *supported)
	logger.Info(logComponent).
		Str("api_base", key.APIBase).
		Str("model", key.ModelName).
		Bool("supported", *supported).
		Msg("[ImageModalityProbe] image modality probed")
}

// invokeProbe 发送探测请求，可选携带思考关闭开关。
// Python: _invoke_probe:254-282（asyncio.wait_for → context.WithTimeout；
// LlmObservationSuppression → call_scope 观测抑制）
func invokeProbe(ctx context.Context, model *llm.Model, extraBody map[string]any) (*llmschema.AssistantMessage, error) {
	opts := []model_clients.InvokeOption{
		model_clients.WithInvokeMaxTokens(imageModalityProbeMaxTokens),
		model_clients.WithInvokeTemperature(0),
	}
	if extraBody != nil {
		opts = append(opts, model_clients.WithInvokeExtra(maps.Clone(extraBody)))
	}
	probeCtx, cancel := context.WithTimeout(ctx, imageModalityProbeTimeout)
	defer cancel()

	// Python: with LlmObservationSuppression(): 探测请求不出现在用户可见轨迹
	probeCtx, restoreSuppression := llm.SuppressLLMObservation(probeCtx)
	defer restoreSuppression()

	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
		llmschema.ContentPart{
			Type: "image_url",
			ImageURL: &llmschema.ImageURL{
				URL: "data:image/png;base64," + dummyImageB64,
			},
		},
		llmschema.ContentPart{Type: "text", Text: "What color is this image? Reply with one word."},
	))
	return model.Invoke(probeCtx, model_clients.NewMessagesParam(msg), opts...)
}

// isolatedProbeModel 构造探测用模型复印件：模型组路由状态独立。
// 探测常在会话首条消息前运行；若共享正式路由，探测大概率命中的首位置失败
// 会把该 deployment 冷却掉，第一条正式消息被悄悄改道且无迹可寻。
// 非模型组模型无共享路由状态，原样返回。
// Python: _isolated_probe_model:288-321
func isolatedProbeModel(model *llm.Model) *llm.Model {
	if model == nil || model.ClientConfig == nil || model.ClientConfig.IntelliRouter == nil {
		return model
	}
	// Python: client_config.model_copy(deep=True, update={"intelli_router":
	//   router_config.model_copy(update={"router_scope": _PROBE_ROUTER_SCOPE})})
	scoped := *model.ClientConfig
	router := *model.ClientConfig.IntelliRouter
	scope := probeRouterScope
	router.RouterScope = &scope
	scoped.IntelliRouter = &router
	scopedCopy, err := llm.NewModel(&scoped, model.ModelConfig, llm.WithClient(model.GetClient()))
	if err != nil {
		// 隔离绝不能弄坏探测本身：失败回退共享路由
		logger.Debug(logComponent).
			Err(err).
			Msg("[ImageModalityProbe] falling back to the shared router")
		return model
	}
	return scopedCopy
}

// runProbe 发送探测请求并解释结果。
// Python: _run_probe:323-375
func runProbe(ctx context.Context, model *llm.Model) *bool {
	model = isolatedProbeModel(model)
	response, err := invokeProbe(ctx, model, thinkingDisabledExtraBody)
	if err != nil {
		if isContextDeadline(err) {
			// Python: asyncio.TimeoutError → None（未知不缓存）
			logger.Warn(logComponent).
				Float64("timeout_seconds", imageModalityProbeTimeout.Seconds()).
				Msg("[ImageModalityProbe] image modality probe timed out; leaving image support undetermined")
			return nil
		}
		if isImageModalityRejection(err) {
			logger.Info(logComponent).
				Err(err).
				Msg("[ImageModalityProbe] model rejected image input; treating read_file image multimodal as unsupported")
			return ptrBool(false)
		}
		// 严格网关会拒绝思考关闭开关本身：去掉开关重试一次再放弃。
		// Python: :343-373
		logger.Debug(logComponent).
			Err(err).
			Msg("[ImageModalityProbe] probe with reasoning disabled failed, retrying without vendor switches")
		response, err = invokeProbe(ctx, model, nil)
		if err != nil {
			if isContextDeadline(err) {
				logger.Warn(logComponent).
					Float64("timeout_seconds", imageModalityProbeTimeout.Seconds()).
					Msg("[ImageModalityProbe] image modality probe timed out; leaving image support undetermined")
				return nil
			}
			if isImageModalityRejection(err) {
				logger.Info(logComponent).
					Err(err).
					Msg("[ImageModalityProbe] model rejected image input; treating read_file image multimodal as unsupported")
				return ptrBool(false)
			}
			logger.Warn(logComponent).
				Err(err).
				Msg("[ImageModalityProbe] image modality probe call failed")
			return nil
		}
	}
	return interpretProbeResponse(response)
}

// interpretProbeResponse 把探测响应映射为结论；说不出结论时返回 nil。
// 结论会缓存到进程结束，因此不够真实的回答必须保持未知：把截断/空回答
// 缓存成 false 会让一个完全正常的模型永久 image-blind。
// Python: _interpret_probe_response:378-419
func interpretProbeResponse(response *llmschema.AssistantMessage) *bool {
	if response == nil {
		logger.Warn(logComponent).
			Msg("[ImageModalityProbe] image modality probe returned no content; leaving image support undetermined")
		return nil
	}
	content := response.GetContent().Text()
	reasoningContent := response.ReasoningContent
	var parts []string
	if content != "" {
		parts = append(parts, content)
	}
	if reasoningContent != "" {
		parts = append(parts, reasoningContent)
	}
	responseText := strings.Join(parts, "\n")
	if strings.Contains(strings.ToLower(responseText), "red") {
		return ptrBool(true)
	}

	if strings.TrimSpace(responseText) == "" {
		// 思考型模型把（很小的）预算全花在思考上，对图片支持一无所知
		logger.Warn(logComponent).
			Msg("[ImageModalityProbe] image modality probe returned no content; leaving image support undetermined")
		return nil
	}

	if response.FinishReason == lengthFinishReason {
		// 答到一半被截断：下一个词很可能就是颜色
		logger.Warn(logComponent).
			Int("max_tokens", imageModalityProbeMaxTokens).
			Msg("[ImageModalityProbe] image modality probe answer hit the token budget before naming a color; leaving image support undetermined")
		return nil
	}

	return ptrBool(false)
}

// isImageModalityRejection 判定 err 是否为对图片的确定性客户端侧拒绝。
// 沿 Unwrap 链收集错误文本（含 exception 的 Code/Name/Message），先按
// 归一化错误码精确/后缀匹配，再按文案子串匹配。
// Python: is_image_modality_rejection:135-157
func isImageModalityRejection(err error) bool {
	values := extractExceptionErrorValues(err)
	lowered := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			lowered = append(lowered, strings.ToLower(v))
		}
	}
	// Python: 归一化 "-"/" " → "_" 后与错误码精确或后缀匹配
	for _, value := range lowered {
		normalized := strings.ReplaceAll(strings.ReplaceAll(value, "-", "_"), " ", "_")
		for _, code := range imageInputUnsupportedErrorCodes {
			if normalized == code || strings.HasSuffix(normalized, "_"+code) {
				return true
			}
		}
	}
	text := strings.Join(lowered, "\n")
	for _, pattern := range imageInputUnsupportedErrorPatterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// extractExceptionErrorValues 收集错误链上所有可读文本。
// Python: _extract_exception_error_values:109-132 + _iter_exception_error_values:86-132
// （getattr(code/status_code/message/body/response) → Go: BaseError 的
// Code()/Name()/Message() 与 params/details；深度上限 imageInputScanMaxDepth）
func extractExceptionErrorValues(err error) []string {
	values := []string{}
	seen := map[error]struct{}{}
	current := err
	depth := 0
	for current != nil && depth <= imageInputScanMaxDepth {
		if _, dup := seen[current]; dup {
			break
		}
		seen[current] = struct{}{}
		values = append(values, current.Error())
		if be, ok := current.(*exception.BaseError); ok {
			values = append(values, fmt.Sprintf("%d", be.Code()), be.Message())
			if be.Details() != nil {
				values = append(values, fmt.Sprintf("%v", be.Details()))
			}
			for _, v := range be.Params() {
				values = append(values, fmt.Sprintf("%v", v))
			}
		}
		current = errors.Unwrap(current)
		depth++
	}
	return values
}

// isContextDeadline 判定 err 是否为探测超时（对应 Python asyncio.TimeoutError）。
func isContextDeadline(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// ptrBool 返回 bool 指针。
func ptrBool(b bool) *bool { return &b }

// zlibCompressHelper zlib 压缩助手。
func zlibCompressBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

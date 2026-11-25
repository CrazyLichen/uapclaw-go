package prompt

// 多模态图片输入适配：附件提取、归一化、data URI 读取与消息定位。
//
// Python: jiuwenswarm/agents/harness/common/prompt/user_prompt_builder.py 的图片函数段
// （23.8 批2；Python 单文件中的图片函数聚为一个 Go 文件，结构映射见 doc.go）。
// 语言差异：Python duck-typed dict 块 → Go 强类型 schema.ContentPart /
// schema.MessageContent（字段一一对应：{"type":"image_url","image_url":{"url":…}}）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/turnctx"
)

// ──────────────────────────── 常量 ────────────────────────────

// D-23.8-1：Python contextvars「隐形袋子」→ Go request context 携带，
// 每次 invoke 一个新 ctx，作用域即生命周期（Python set→reset 区间的等价物）。
// ImageContentOmitted 图片内容全量剥离后留下的占位文本。
// Python: IMAGE_CONTENT_OMITTED:15-18（协议字符串，不本地化）
const ImageContentOmitted = "[Image content omitted from chat-model context. Use the original image " +
	"path or a vision tool when image analysis is required.]"

// imageContentTypes 视为图片块的 content part 类型集合。
// Python: _IMAGE_CONTENT_TYPES:20
var imageContentTypes = map[string]struct{}{
	"image":       {},
	"image_url":   {},
	"input_image": {},
}

// visionInlineImageMimeTypes 允许内联进模型输入的图片 mime 白名单。
// Python: _VISION_INLINE_IMAGE_MIME_TYPES:21-26
var visionInlineImageMimeTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

// maxVisionInlineImageBytes 单张内联图片字节上限。
// Python: _MAX_VISION_INLINE_IMAGE_BYTES:27
const maxVisionInlineImageBytes = 10 * 1024 * 1024

// ──────────────────────────── 结构体 ────────────────────────────

// MultimodalImageFile 归一化后的图片附件记录。
// Python: _normalize_image_file 输出 dict（type/filename/path/mime_type/size_bytes）。
type MultimodalImageFile struct {
	// Type 固定为 "image"
	Type string `json:"type"`
	// Filename 文件名（展示用）
	Filename string `json:"filename"`
	// Path 服务器磁盘上的绝对路径
	Path string `json:"path"`
	// MimeType 归一化后的小写 mime
	MimeType string `json:"mime_type"`
	// SizeBytes 字节数（可选，Python 仅在原始条目携带 int 时透传）
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// MultimodalImageFilesFromContext 从 turn ctx 读取本轮图片附件列表；未设置返回 nil。
// Python: current_multimodal_image_files
// 读取 core 侧 harness.TurnMultimodalImageFilesFromContext 的 raw 清单
// （消费侧 runSingleRoundInvoke 写入；D-23.8-1 修订：ctx 载体随 SendInput
// 队列在消费侧重建，见 harness/turn_multimodal_ctx.go 包注释）。
func MultimodalImageFilesFromContext(ctx context.Context) []MultimodalImageFile {
	if ctx == nil {
		return nil
	}
	raw := turnctx.TurnMultimodalImageFilesFromContext(ctx)
	if len(raw) == 0 {
		return nil
	}
	out := make([]MultimodalImageFile, 0, len(raw))
	for _, m := range raw {
		f := MultimodalImageFile{
			Type:     stringValue(m["type"]),
			Filename: stringValue(m["filename"]),
			Path:     stringValue(m["path"]),
			MimeType: stringValue(m["mime_type"]),
		}
		if v, ok := m["size_bytes"].(int64); ok {
			f.SizeBytes = v
		} else if v, ok := m["size_bytes"].(int); ok {
			f.SizeBytes = int64(v)
		} else if v, ok := m["size_bytes"].(float64); ok {
			f.SizeBytes = int64(v)
		}
		out = append(out, f)
	}
	return out
}

// stringValue 取 map 的字符串值（nil 安全）。
func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareMultimodalImageMessages 把图片文件注入消息列表最后一条 user 消息。
// Python: prepare_multimodal_image_messages:191-215
// 返回更新后的消息列表与注入的图片块数；无图/找不到 user 消息/全部读取失败时原样返回。
func PrepareMultimodalImageMessages(messages []llmschema.BaseMessage, imageFiles []MultimodalImageFile) ([]llmschema.BaseMessage, int) {
	images := NormalizeImageFiles(imageFiles)
	if len(images) == 0 {
		return messages, 0
	}
	targetIndex := LatestUserMessageIndex(messages)
	if targetIndex < 0 {
		return messages, 0
	}

	message := messages[targetIndex]
	updatedContent, injected := appendImageFilesToContent(message.GetContent(), images)
	if injected == 0 {
		return messages, 0
	}

	updatedMessages := make([]llmschema.BaseMessage, len(messages))
	copy(updatedMessages, messages)
	// 副本替换槽位：原消息（含历史持有的同一指针）不受投影影响
	updatedMessages[targetIndex] = copyMessageWithContent(messages[targetIndex], updatedContent)
	return updatedMessages, injected
}

// StripImageContentBlocks 从消息内容中剥离图片块。
// Python: strip_image_content_blocks:218-243
// 返回净化后的内容与剥除数；无图片块时内容原样返回计数 0。
// Python 纯字符串 content 对应 Go 的 IsText 内容（无 parts，无法含图片块）。
func StripImageContentBlocks(content llmschema.MessageContent) (llmschema.MessageContent, int) {
	parts := content.Parts()
	if len(parts) == 0 {
		return content, 0
	}

	keptParts := make([]llmschema.ContentPart, 0, len(parts))
	removed := 0
	for _, part := range parts {
		if IsImageContentBlock(part) {
			removed++
			continue
		}
		keptParts = append(keptParts, part)
	}
	if removed == 0 {
		return content, 0
	}
	if len(keptParts) == 0 {
		return llmschema.NewTextContent(ImageContentOmitted), removed
	}

	// Python: 全部剩余块都能取文本时合并为单段文本；任一块无文本则保留原 parts。
	textParts := make([]string, 0, len(keptParts))
	for _, part := range keptParts {
		text, ok := textFromContentPart(part)
		if !ok {
			return llmschema.NewMultiModalContent(keptParts...), removed
		}
		if text != "" {
			textParts = append(textParts, text)
		}
	}
	joined := strings.TrimSpace(strings.Join(textParts, "\n"))
	if joined == "" {
		return llmschema.NewTextContent(ImageContentOmitted), removed
	}
	return llmschema.NewTextContent(joined), removed
}

// StripImageContentFromModelContext 遍历消息列表剥除图片块，返回剥除总数。
// Python: strip_image_content_from_model_context:245-256
func StripImageContentFromModelContext(messages []llmschema.BaseMessage) int {
	removedTotal := 0
	for _, message := range messages {
		sanitized, removed := StripImageContentBlocks(message.GetContent())
		if removed == 0 {
			continue
		}
		message.SetContent(sanitized)
		removedTotal += removed
	}
	return removedTotal
}

// NormalizeImageFiles 归一化图片文件列表（导出供窗口装配复用）。
// Python: _normalize_image_files:308-321
func NormalizeImageFiles(imageFiles []MultimodalImageFile) []MultimodalImageFile {
	normalized := make([]MultimodalImageFile, 0, len(imageFiles))
	seenPaths := make(map[string]struct{})
	for _, item := range imageFiles {
		norm := normalizeImageFileStruct(item)
		if norm == nil {
			continue
		}
		if _, dup := seenPaths[norm.Path]; dup {
			continue
		}
		seenPaths[norm.Path] = struct{}{}
		normalized = append(normalized, *norm)
	}
	return normalized
}

// IsImageContentBlock 判断 content part 是否为图片块。
// Python: is_image_content_block:35-42
// Python 的 `“image_url” in part or “image” in part` 对应 Go 的
// ImageURL / InputAudio 等 image 形字段非 nil。
func IsImageContentBlock(part llmschema.ContentPart) bool {
	if _, ok := imageContentTypes[strings.ToLower(strings.TrimSpace(part.Type))]; ok {
		return true
	}
	return part.ImageURL != nil
}

// ExtractMultimodalImageFiles 从请求参数中提取归一化图片文件记录。
// Python: extract_multimodal_image_files:44-73
// 候选来源：params["media_items"] 列表 + params["files"]["uploaded_images"] 列表；
// 逐条归一化后按 path 去重。
func ExtractMultimodalImageFiles(params map[string]any) []MultimodalImageFile {
	if params == nil {
		return nil
	}

	var candidates []any
	// Python: raw_media_items = params.get("media_items")
	if rawMediaItems, ok := params["media_items"].([]any); ok {
		candidates = append(candidates, rawMediaItems...)
	}
	// Python: files = params.get("files")；files["uploaded_images"]
	if files, ok := params["files"].(map[string]any); ok {
		if uploadedImages, ok := files["uploaded_images"].([]any); ok {
			candidates = append(candidates, uploadedImages...)
		}
	}

	normalized := make([]MultimodalImageFile, 0, len(candidates))
	seenPaths := make(map[string]struct{})
	for _, item := range candidates {
		image, ok := item.(map[string]any)
		if !ok {
			continue
		}
		norm := normalizeImageFile(image)
		if norm == nil {
			continue
		}
		if _, dup := seenPaths[norm.Path]; dup {
			continue
		}
		seenPaths[norm.Path] = struct{}{}
		normalized = append(normalized, *norm)
	}
	return normalized
}

// LatestUserMessageIndex 倒序定位最后一条 user 消息的下标；找不到返回 -1。
// Python: _latest_user_message_index:353-358 + _is_user_message:360-368
// Python 按 role 字符串或类名 UserMessage 判定，Go 统一按 RoleType == RoleTypeUser。
func LatestUserMessageIndex(messages []llmschema.BaseMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].GetRole() == llmschema.RoleTypeUser {
			return i
		}
	}
	return -1
}

// ImageDataURIFromPath 读取图片文件并编码为 data URI；失败/超限/超白名单返回空串。
// Python: _image_data_uri_from_path:433-447
func ImageDataURIFromPath(pathText string, mimeType string) string {
	path := strings.TrimSpace(pathText)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	if _, ok := visionInlineImageMimeTypes[mimeType]; !ok {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) == 0 || len(data) > maxVisionInlineImageBytes {
		return ""
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// multimodalImageWindowMutator 多模态图片窗口投影（闭包捕获图片清单）。
// Python: multimodal_image_window_mutator 闭包 + _MULTIMODAL_IMAGE_WINDOW_MUTATOR_ATTR
// 标记（user_prompt_builder.py:28/291）——Go 以私有接口实现类型承载同一标记，
// IsMultimodalImageMutator 用接口断言识别（替换式安装的旧项识别）。
// 作用层级：ContextWindow 级（对 ContextMessages 应用注入，历史不改），
// 经 context 的持久投影链消费（session_model_context.go 窗口投影段）。
type multimodalImageWindowMutator struct {
	images []MultimodalImageFile
}

// MutateWindow 实现 ceiface.WindowMutator：向最后一条 user 消息注入图片块。
func (m multimodalImageWindowMutator) MutateWindow(window *ceiface.ContextWindow) *ceiface.ContextWindow {
	if window == nil {
		return nil
	}
	updated, _ := PrepareMultimodalImageMessages(window.ContextMessages, m.images)
	next := *window
	next.ContextMessages = updated
	return &next
}

// IsMultimodalImageMutator 判断投影是否为多模态图片投影（替换式安装的旧项识别）。
// Python: getattr(mutator, _MULTIMODAL_IMAGE_WINDOW_MUTATOR_ATTR, False)（:291）。
func IsMultimodalImageMutator(m ceiface.WindowMutator) bool {
	_, ok := m.(multimodalImageWindowMutator)
	return ok
}

// EnsureMultimodalImageWindowMutator 把多模态图片投影替换式安装进 context 的
// 持久投影链：先清全部带标记旧项，图片非空时追加新投影。
// Python: ensure_multimodal_image_window_mutator:277-294——
// mutators[:] = [非图片 mutator]；if images: mutators.append(闭包)。
// context 须实现 ceiface.WindowMutatorsRegistrar（SessionModelContext 实现）；
// 未实现或空图列表返回 false（对齐 Python getattr 失败/不安装）。
func EnsureMultimodalImageWindowMutator(context ceiface.ModelContext, imageFiles []MultimodalImageFile) bool {
	registrar, ok := context.(ceiface.WindowMutatorsRegistrar)
	if !ok {
		return false
	}
	images := NormalizeImageFiles(imageFiles)
	// Python: 先清旧图片 mutator（:288-292）
	kept := make([]ceiface.WindowMutator, 0, len(registrar.WindowMutators()))
	for _, m := range registrar.WindowMutators() {
		if !IsMultimodalImageMutator(m) {
			kept = append(kept, m)
		}
	}
	if len(images) == 0 {
		registrar.SetWindowMutators(kept)
		return false
	}
	// Python: mutators.append(mutator)，闭包对窗口应用图片注入
	kept = append(kept, multimodalImageWindowMutator{images: images})
	registrar.SetWindowMutators(kept)
	return true
}

// ImageInputStatusMessage 用同一段图片输入能力描述服务提示词与降级通知。
// Python: image_input_status_message:75-83
func ImageInputStatusMessage(status string, modelLabel string) string {
	model := "当前模型" + modelLabel
	switch status {
	case "disabled":
		return model + "已在配置中关闭原生图片输入。"
	case "unsupported":
		return model + "的当前接口经检测不支持原生图片输入。"
	default:
		return "尚未确认" + model + "的接口是否支持原生图片输入。"
	}
}

// imageToolHint 按场景生成图片工具提示。
// Python: _image_tool_hint:85-111
func imageToolHint(imageInputStatus string, visionToolAvailable bool, steerImageTool string) string {
	if steerImageTool != "" {
		invocation := "visual_question_answering(image_path_or_url=mediaPath, question=question)"
		if steerImageTool == "read_file" {
			invocation = "read_file(file_path=mediaPath)"
		}
		return "This is supplemental input for the current task. The steering queue carries text only; " +
			"the uploaded images have not yet been read. For every entry in mediaItems, call " +
			invocation + " using that entry's mediaPath before describing or comparing the images. " +
			"If reading fails or returns only metadata, explain the failure; do not guess image contents."
	}
	var toolHint string
	if visionToolAvailable {
		toolHint = "请调用已配置的图片理解工具；" +
			"优先使用 image_reading(local_url=mediaPath, prompt=question)，" +
			"或使用 visual_question_answering(image_path_or_url=mediaPath, question=question)。"
	} else {
		toolHint = "没有配置可用的视觉模型工具。" +
			"请根据上述状态向用户说明本次无法直接读图的原因，不要猜测图片内容。" +
			"能力未确认不等于模型不支持读图。"
	}
	return ImageInputStatusMessage(imageInputStatus, "") + "本次图片未作为原生图片输入发送给主模型。" + toolHint
}

// PrepareImageToolInputs 为普通聊天与纯文本 steer 序列化图片工具上下文。
// Python: prepare_image_tool_inputs:113-176
// inputs 的 "_multimodal_image_files" 键在注入后被删除（对齐 :168）。
func PrepareImageToolInputs(params map[string]any, inputs map[string]any, enableReadImageMultimodal bool, visionToolAvailable bool, imageInputStatus string, steerImageTool string) map[string]any {
	// Python: if enable_read_image_multimodal and steer_image_tool is None: return inputs
	if enableReadImageMultimodal && steerImageTool == "" {
		return inputs
	}
	query, ok := inputs["query"].(string)
	if !ok || (steerImageTool == "" && strings.Contains(query, "jiuwenswarm_image_tool_context")) {
		return inputs
	}

	// Python: image_files = inputs.get("_multimodal_image_files")；缺失时从 params 再提取
	var imageFiles []MultimodalImageFile
	if raw, ok := inputs["_multimodal_image_files"].([]MultimodalImageFile); ok && len(raw) > 0 {
		imageFiles = raw
	} else {
		imageFiles = ExtractMultimodalImageFiles(params)
	}
	if len(imageFiles) == 0 {
		return inputs
	}

	// Python: params.get("query") → params.get("content")；空则 steer 场景补默认句
	rawQuestion := stripAnyGo(params["query"])
	if rawQuestion == "" {
		rawQuestion = stripAnyGo(params["content"])
	}
	question := rawQuestion
	if steerImageTool != "" && question == "" {
		question = "Read the attached images and use them to continue the current task."
	}
	firstPath := imageFiles[0].Path
	if strings.TrimSpace(firstPath) == "" {
		return inputs
	}

	// Python: media_items 逐条 {type, filename, mediaPath, mimeType}
	mediaItems := make([]map[string]any, 0, len(imageFiles))
	for _, imageFile := range imageFiles {
		path := strings.TrimSpace(imageFile.Path)
		if path == "" {
			continue
		}
		mediaItems = append(mediaItems, map[string]any{
			"type":      "image",
			"filename":  strOrAnyGo(imageFile.Filename, filepath.Base(path)),
			"mediaPath": path,
			"mimeType":  imageFile.MimeType,
		})
	}
	if len(mediaItems) == 0 {
		return inputs
	}

	toolContext := map[string]any{
		"marker":           "jiuwenswarm_image_tool_context",
		"mediaPath":        firstPath,
		"mediaItems":       mediaItems,
		"question":         question,
		"imageInputStatus": imageInputStatus,
		"toolHint":         imageToolHint(imageInputStatus, visionToolAvailable, steerImageTool),
	}
	updated := make(map[string]any, len(inputs)+1)
	for k, v := range inputs {
		updated[k] = v
	}
	// Python: updated.pop("_multimodal_image_files", None)
	delete(updated, "_multimodal_image_files")
	data, _ := json.Marshal(toolContext)
	updated["query"] = query + "\n\n图片附件上下文（供 ReAct 选择图片理解工具使用）：\n" + string(data)
	return updated
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeImageFileStruct 对结构体形态的图片记录做与 map 形态相同的归一化校验。
// Python: _normalize_image_file:323-351（结构体版；mime 白名单与 filename 兜底一致）。
func normalizeImageFileStruct(item MultimodalImageFile) *MultimodalImageFile {
	path := strings.TrimSpace(item.Path)
	if path == "" {
		return nil
	}
	mimeType := strings.ToLower(strings.TrimSpace(item.MimeType))
	if _, ok := visionInlineImageMimeTypes[mimeType]; !ok {
		return nil
	}
	filename := strings.TrimSpace(item.Filename)
	if filename == "" {
		filename = filepath.Base(path)
	}
	return &MultimodalImageFile{
		Type:      "image",
		Filename:  filename,
		Path:      path,
		MimeType:  mimeType,
		SizeBytes: item.SizeBytes,
	}
}

// appendImageFilesToContent 把图片 data URI 追加为 content 的 image_url 块。
// Python: _append_image_files_to_content:371-387
// 首块是 {"query":…,"file":[…]} JSON 文本块，其后跟注入成功的图片块。
func appendImageFilesToContent(content llmschema.MessageContent, imageFiles []MultimodalImageFile) (llmschema.MessageContent, int) {
	var imageURLParts []llmschema.ContentPart
	var injectedFiles []MultimodalImageFile
	for _, imageFile := range imageFiles {
		dataURI := ImageDataURIFromPath(imageFile.Path, imageFile.MimeType)
		part, ok := appendImageURLPart(dataURI)
		if !ok {
			continue
		}
		imageURLParts = append(imageURLParts, part)
		injectedFiles = append(injectedFiles, imageFile)
	}
	if len(injectedFiles) == 0 {
		return content, 0
	}

	parts := make([]llmschema.ContentPart, 0, len(imageURLParts)+1)
	parts = append(parts, llmschema.ContentPart{
		Type: "text",
		Text: buildQueryFileText(content, injectedFiles),
	})
	parts = append(parts, imageURLParts...)
	return llmschema.NewMultiModalContent(parts...), len(injectedFiles)
}

// buildQueryFileText 序列化 {query, file} JSON 文本块。
// Python: _build_query_file_text:389-408
func buildQueryFileText(content llmschema.MessageContent, imageFiles []MultimodalImageFile) string {
	fileRecords := make([]map[string]any, 0, len(imageFiles))
	for _, imageFile := range imageFiles {
		fileRecords = append(fileRecords, map[string]any{
			"filename":  imageFile.Filename,
			"path":      imageFile.Path,
			"mime_type": imageFile.MimeType,
		})
	}
	payload := map[string]any{
		"query": contentToQueryText(content),
		"file":  fileRecords,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

// contentToQueryText 取 content 的纯文本形态。
// Python: _content_to_query_text:410-423
func contentToQueryText(content llmschema.MessageContent) string {
	if content.IsText() {
		return content.Text()
	}
	texts := make([]string, 0, len(content.Parts()))
	for _, part := range content.Parts() {
		if text, ok := textFromContentPart(part); ok && text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// textFromContentPart 取 content part 的文本；非文本块返回 ("" , false)。
// Python: _text_from_content_part:425-431
func textFromContentPart(part llmschema.ContentPart) (string, bool) {
	if part.Type == "text" {
		return part.Text, true
	}
	return "", false
}

// appendImageURLPart 构造 image_url 块。
// Python: _append_image_url_part:449-454
func appendImageURLPart(dataURI string) (llmschema.ContentPart, bool) {
	if !strings.HasPrefix(dataURI, "data:image/") {
		return llmschema.ContentPart{}, false
	}
	return llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: dataURI}}, true
}

// copyMessageWithContent 用新 content 替换消息内容（就地 SetContent）。
// Python: _copy_message_with_content:456-464
// copyMessageWithContent 构造携带新内容的消息副本（不改原消息）。
// Python 消息为 dict，注入是「新列表 + 改写副本」；Go 侧 BaseMessage 为指针，
// 直接 SetContent 会穿透投影改写历史——违反窗口投影「不得修改入参」契约
// （window.go 约定 / context.py:737 投影语义）。目标恒为 user 消息
// （PrepareMultimodalImageMessages 只定位 LatestUserMessageIndex），
// 无 tool_call 等派生字段需要保留，role + metadata 透传即完整。
func copyMessageWithContent(message llmschema.BaseMessage, content llmschema.MessageContent) llmschema.BaseMessage {
	opts := make([]llmschema.MessageOption, 0, 1)
	if meta := message.GetMetadata(); len(meta) > 0 {
		opts = append(opts, llmschema.WithMetadata(meta))
	}
	cloned := llmschema.NewDefaultMessage(message.GetRole(), "", opts...)
	cloned.SetContent(content)
	return cloned
}

// ──────────────────────────── 归一化辅助（同文件前段） ────────────────────────────

// normalizeImageFile 归一化单个图片附件条目；非法条目返回 nil。
// Python: _normalize_image_file:323-351
// 校验顺序对齐：type 过滤 → path 非空 → mime 补全与白名单 → filename 兜底 → size_bytes 透传。
func normalizeImageFile(item map[string]any) *MultimodalImageFile {
	// Python: item_type is not None and item_type != "image" → 拒绝
	if itemType, ok := item["type"]; ok && itemType != nil {
		if s := stripAnyGo(itemType); s != "image" {
			return nil
		}
	}

	path := stripAnyGo(item["path"])
	if path == "" {
		return nil
	}

	// Python: mime_type = item.get("mime_type") or item.get("mimeType")；缺失按扩展名推断
	mimeType := stripAnyGo(strOrAnyGo(item["mime_type"], item["mimeType"]))
	if mimeType == "" {
		mimeType = guessMimeTypeByExtension(path)
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if _, ok := visionInlineImageMimeTypes[mimeType]; !ok {
		return nil
	}

	filename := stripAnyGo(strOrAnyGo(item["filename"], filepath.Base(path)))
	if filename == "" {
		filename = filepath.Base(path)
	}

	image := &MultimodalImageFile{
		Type:     "image",
		Filename: filename,
		Path:     path,
		MimeType: mimeType,
	}
	// Python: size_bytes 仅在 isinstance(int) 时透传
	if sizeBytes, ok := item["size_bytes"].(int64); ok {
		image.SizeBytes = sizeBytes
	}
	return image
}

// guessMimeTypeByExtension 按扩展名推断 mime 类型。
// Python: mimetypes.guess_type(path)[0]
func guessMimeTypeByExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

// stripAnyGo 等价 Python str(x or "").strip()（None/空 → ""）。
func stripAnyGo(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(s)
	default:
		return strings.TrimSpace(stringifyAnyGo(v))
	}
}

// strOrAnyGo 第一个 strip 后非空的值。
func strOrAnyGo(v1, v2 any) any {
	if s := stripAnyGo(v1); s != "" {
		return v1
	}
	return v2
}

// stringifyAnyGo 非 string 的 any 转字符串（对齐 Python str(x)）。
func stringifyAnyGo(v any) string {
	return fmt.Sprintf("%v", v)
}

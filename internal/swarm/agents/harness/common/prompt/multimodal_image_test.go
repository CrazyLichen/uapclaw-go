package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── IsImageContentBlock ────────────────────────────

// TestIsImageContentBlock 对齐 Python is_image_content_block:35-42。
func TestIsImageContentBlock(t *testing.T) {
	cases := []struct {
		part  llmschema.ContentPart
		expect bool
	}{
		{llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,x"}}, true},
		{llmschema.ContentPart{Type: "image"}, true},
		{llmschema.ContentPart{Type: "input_image"}, true},
		{llmschema.ContentPart{Type: "IMAGE_URL"}, true},
		{llmschema.ContentPart{Type: "text", Text: "hello"}, false},
		{llmschema.ContentPart{Type: ""}, false},
	}
	for i, c := range cases {
		if got := IsImageContentBlock(c.part); got != c.expect {
			t.Fatalf("case %d: got %v", i, got)
		}
	}
}

// ──────────────────────────── ExtractMultimodalImageFiles ────────────────────────────

// TestExtractMultimodalImageFiles_mediaItems与uploadedImages 对齐 :44-73 双来源。
func TestExtractMultimodalImageFiles_mediaItems与uploadedImages(t *testing.T) {
	params := map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "path": "/tmp/a.png", "mime_type": "image/png", "filename": "a.png"},
		},
		"files": map[string]any{
			"uploaded_images": []any{
				map[string]any{"path": "/tmp/b.jpg", "mime_type": "image/jpeg"},
			},
		},
	}
	files := ExtractMultimodalImageFiles(params)
	if len(files) != 2 {
		t.Fatalf("期望 2 条，得 %d: %+v", len(files), files)
	}
	if files[0].Type != "image" || files[0].Path != "/tmp/a.png" || files[0].Filename != "a.png" {
		t.Fatalf("media_items 归一化错: %+v", files[0])
	}
	if files[1].Filename != "b.jpg" {
		t.Fatalf("filename 兜底取 base: %+v", files[1])
	}
}

// TestExtractMultimodalImageFiles_过滤与去重 对齐 :323-351 归一化规则与 :63-70 去重。
func TestExtractMultimodalImageFiles_过滤与去重(t *testing.T) {
	params := map[string]any{
		"media_items": []any{
			"not-a-map",                                                             // 非 dict → 丢
			map[string]any{"type": "video", "path": "/tmp/v.mp4"},                   // type 非 image → 丢
			map[string]any{"path": ""},                                              // 无 path → 丢
			map[string]any{"path": "/tmp/t.txt"},                                    // mime 不在白名单 → 丢
			map[string]any{"path": "/tmp/d.webp"},                                   // mime 缺失按扩展名补 webp
			map[string]any{"path": "/tmp/a.png", "mime_type": "image/png"},          // 第一份
			map[string]any{"path": "/tmp/a.png", "mime_type": "image/png", "size_bytes": int64(3)}, // 重复 path → 丢
		},
	}
	files := ExtractMultimodalImageFiles(params)
	if len(files) != 2 {
		t.Fatalf("期望 2 条（d.webp + a.png），得 %d: %+v", len(files), files)
	}
	if files[0].MimeType != "image/webp" {
		t.Fatalf("扩展名推断错: %v", files[0].MimeType)
	}
	if files[1].SizeBytes != 0 {
		t.Fatalf("重复条目不应进入: %+v", files[1])
	}
	// params 非 dict / nil → 空
	if got := ExtractMultimodalImageFiles(nil); len(got) != 0 {
		t.Fatal("nil 应返回空")
	}
}

// TestExtractMultimodalImageFiles_sizeBytes透传 对齐 :349-351 仅 int 透传。
func TestExtractMultimodalImageFiles_sizeBytes透传(t *testing.T) {
	params := map[string]any{"media_items": []any{
		map[string]any{"path": "/tmp/a.png", "mime_type": "image/png", "size_bytes": int64(123)},
	}}
	files := ExtractMultimodalImageFiles(params)
	if files[0].SizeBytes != 123 {
		t.Fatalf("size_bytes = %d", files[0].SizeBytes)
	}
}

// ──────────────────────────── LatestUserMessageIndex ────────────────────────────

// TestLatestUserMessageIndex 对齐 :353-368。
func TestLatestUserMessageIndex(t *testing.T) {
	mk := func(role llmschema.RoleType) llmschema.BaseMessage {
		return llmschema.NewDefaultMessage(role, "x")
	}
	msgs := []llmschema.BaseMessage{
		mk(llmschema.RoleTypeSystem), mk(llmschema.RoleTypeUser), mk(llmschema.RoleTypeAssistant), mk(llmschema.RoleTypeUser),
	}
	if got := LatestUserMessageIndex(msgs); got != 3 {
		t.Fatalf("index = %d", got)
	}
	if got := LatestUserMessageIndex(nil); got != -1 {
		t.Fatalf("空应 -1: %d", got)
	}
	if got := LatestUserMessageIndex([]llmschema.BaseMessage{mk(llmschema.RoleTypeSystem)}); got != -1 {
		t.Fatalf("无 user 应 -1: %d", got)
	}
}

// ──────────────────────────── ImageDataURIFromPath ────────────────────────────

// TestImageDataURIFromPath_正常编码 对齐 :433-447 成功路径。
func TestImageDataURIFromPath_正常编码(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := ImageDataURIFromPath(p, "image/png")
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("URI 前缀错: %q", uri[:min(40, len(uri))])
	}
	// base64 内容与文件字节一致由 encoding/base64 保证，此处断言非空正文
	if len(uri) <= len("data:image/png;base64,") {
		t.Fatal("正文为空")
	}
}

// TestImageDataURIFromPath_失败路径 对齐 :433-447 四个空串分支。
func TestImageDataURIFromPath_失败路径(t *testing.T) {
	if got := ImageDataURIFromPath("/nonexistent/x.png", "image/png"); got != "" {
		t.Fatalf("文件不存在应空串: %q", got)
	}
	p := filepath.Join(t.TempDir(), "x.png")
	_ = os.WriteFile(p, []byte("data"), 0o644)
	if got := ImageDataURIFromPath(p, "text/plain"); got != "" {
		t.Fatalf("mime 超白名单应空串: %q", got)
	}
	if got := ImageDataURIFromPath("", "image/png"); got != "" {
		t.Fatalf("空路径应空串: %q", got)
	}
	// 空文件 → 空串
	empty := filepath.Join(t.TempDir(), "e.png")
	_ = os.WriteFile(empty, nil, 0o644)
	if got := ImageDataURIFromPath(empty, "image/png"); got != "" {
		t.Fatalf("空文件应空串: %q", got)
	}
}

// ──────────────────────────── 注入（prepare_multimodal_image_messages） ────────────────────────────

// mkImageFile 构造测试图片记录，写真实文件供 data URI 读取。
func mkImageFile(t *testing.T, name string) MultimodalImageFile {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("img-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	mime := "image/png"
	if strings.HasSuffix(name, ".jpg") {
		mime = "image/jpeg"
	}
	return MultimodalImageFile{Type: "image", Filename: name, Path: p, MimeType: mime}
}

// TestPrepareMultimodalImageMessages_注入最后一条user消息 对齐 :191-215 与 :371-387。
func TestPrepareMultimodalImageMessages_注入最后一条user消息(t *testing.T) {
	img := mkImageFile(t, "shot.png")
	msgs := []llmschema.BaseMessage{
		llmschema.NewDefaultMessage(llmschema.RoleTypeSystem, "sys"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "earlier"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "look at this"),
	}
	updated, injected := PrepareMultimodalImageMessages(msgs, []MultimodalImageFile{img})
	if injected != 1 {
		t.Fatalf("injected = %d", injected)
	}
	// 最后一条 user 被改
	last := updated[2].GetContent()
	parts := last.Parts()
	if len(parts) != 2 {
		t.Fatalf("parts = %d", len(parts))
	}
	if parts[0].Type != "text" {
		t.Fatalf("首块应为文本: %+v", parts[0])
	}
	// {query, file} JSON 结构
	var payload struct {
		Query string `json:"query"`
		File  []struct {
			Filename string `json:"filename"`
			Path     string `json:"path"`
			MimeType string `json:"mime_type"`
		} `json:"file"`
	}
	if err := json.Unmarshal([]byte(parts[0].Text), &payload); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if payload.Query != "look at this" {
		t.Fatalf("query = %q", payload.Query)
	}
	if len(payload.File) != 1 || payload.File[0].Filename != "shot.png" || payload.File[0].MimeType != "image/png" {
		t.Fatalf("file 记录错: %+v", payload.File)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil ||
		!strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image_url 块错: %+v", parts[1])
	}
	// 前面的消息不受影响
	if updated[1].GetContent().Text() != "earlier" || updated[1].GetContent().IsText() != true {
		t.Fatal("非目标消息被改动")
	}
}

// TestPrepareMultimodalImageMessages_无图与无user 对齐 :194-198/:202-205。
func TestPrepareMultimodalImageMessages_无图与无user(t *testing.T) {
	msgs := []llmschema.BaseMessage{llmschema.NewDefaultMessage(llmschema.RoleTypeSystem, "s")}
	updated, injected := PrepareMultimodalImageMessages(msgs, nil)
	if injected != 0 || len(updated) != 1 {
		t.Fatalf("无图应原样: injected=%d", injected)
	}
	if _, injected = PrepareMultimodalImageMessages(msgs, []MultimodalImageFile{{Path: "/nonexistent.png", MimeType: "image/png"}}); injected != 0 {
		t.Fatalf("找不到 user 消息应原样: %d", injected)
	}
}

// TestPrepareMultimodalImageMessages_全部读取失败不注入 对齐 :456-466 injected==0。
func TestPrepareMultimodalImageMessages_全部读取失败不注入(t *testing.T) {
	msgs := []llmschema.BaseMessage{llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "q")}
	updated, injected := PrepareMultimodalImageMessages(msgs, []MultimodalImageFile{
		{Type: "image", Filename: "x.png", Path: "/nonexistent/x.png", MimeType: "image/png"},
	})
	if injected != 0 {
		t.Fatalf("injected = %d", injected)
	}
	if !updated[0].GetContent().IsText() || updated[0].GetContent().Text() != "q" {
		t.Fatal("原消息应未被改动")
	}
}

// ──────────────────────────── 剥离（strip_image_content_blocks） ────────────────────────────

// TestStripImageContentBlocks_混合内容 对齐 :218-243 三分支。
func TestStripImageContentBlocks_混合内容(t *testing.T) {
	mixed := llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "text", Text: "a"},
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,x"}},
		llmschema.ContentPart{Type: "text", Text: "b"},
	)
	sanitized, removed := StripImageContentBlocks(mixed)
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	if !sanitized.IsText() || sanitized.Text() != "a\nb" {
		t.Fatalf("合并文本错: %q", sanitized.Text())
	}

	// 全剥光 → IMAGE_CONTENT_OMITTED
	onlyImg := llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "x"}},
	)
	sanitized, removed = StripImageContentBlocks(onlyImg)
	if removed != 1 || !sanitized.IsText() || sanitized.Text() != ImageContentOmitted {
		t.Fatalf("全剥光分支错: removed=%d text=%q", removed, sanitized.Text())
	}

	// 剩余块含非文本 → 保留原 parts
	nonText := llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "input_audio", InputAudio: &llmschema.InputAudio{Data: "d", Format: "wav"}},
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "x"}},
	)
	sanitized, _ = StripImageContentBlocks(nonText)
	if len(sanitized.Parts()) != 1 {
		t.Fatalf("非文本块保留失败: %d", len(sanitized.Parts()))
	}

	// 无图片块 → 原样计数 0
	plain := llmschema.NewTextContent("plain")
	sanitized, removed = StripImageContentBlocks(plain)
	if removed != 0 || sanitized.Text() != "plain" {
		t.Fatalf("无图块应原样: removed=%d", removed)
	}
}

// TestStripImageContentFromModelContext 对齐 :245-256 遍历计数与就地更新。
func TestStripImageContentFromModelContext(t *testing.T) {
	msgs := []llmschema.BaseMessage{
		llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "text only"),
		func() llmschema.BaseMessage { m := llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, ""); m.SetContent(llmschema.NewMultiModalContent(llmschema.ContentPart{Type: "text", Text: "with"}, llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "x"}})); return m }(),
	}
	total := StripImageContentFromModelContext(msgs)
	if total != 1 {
		t.Fatalf("total = %d", total)
	}
	if msgs[1].GetContent().Text() != "with" {
		t.Fatalf("就地更新失败: %q", msgs[1].GetContent().Text())
	}
}

// ──────────────────────────── EnsureMultimodalImageWindowMutator（D-23.8-3） ────────────────────────────

// TestEnsureMultimodalImageWindowMutator 对齐 :277-305。
func TestEnsureMultimodalImageWindowMutator(t *testing.T) {
	if mutator, ok := EnsureMultimodalImageWindowMutator(nil); ok || mutator != nil {
		t.Fatal("空图列表应 (nil,false)")
	}
	img := mkImageFile(t, "w.png")
	mutator, ok := EnsureMultimodalImageWindowMutator([]MultimodalImageFile{img})
	if !ok {
		t.Fatal("有图应返回闭包")
	}
	msgs := []llmschema.BaseMessage{llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "q")}
	updated := mutator(msgs)
	if updated[0].GetContent().IsText() {
		t.Fatal("闭包执行后应注入图片块")
	}
	// 重复调用返回各自独立闭包（Python 替换式由 rail 持有层保证）
	_, ok = EnsureMultimodalImageWindowMutator([]MultimodalImageFile{img})
	if !ok {
		t.Fatal("重复构造应成功")
	}
}

// ──────────────────────────── prepare_image_tool_inputs 段 ────────────────────────────

// TestImageInputStatusMessage 对齐 :75-83 三态。
func TestImageInputStatusMessage(t *testing.T) {
	if msg := ImageInputStatusMessage("disabled", "A"); !strings.Contains(msg, "A已在配置中关闭原生图片输入") {
		t.Fatalf("disabled: %q", msg)
	}
	if msg := ImageInputStatusMessage("unsupported", ""); !strings.Contains(msg, "经检测不支持") {
		t.Fatalf("unsupported: %q", msg)
	}
	if msg := ImageInputStatusMessage("unknown", ""); !strings.Contains(msg, "尚未确认") {
		t.Fatalf("unknown: %q", msg)
	}
}

// TestImageToolHint_不可见 对齐 :85-111 steer 与 vision 两分支。
func TestImageToolHint_分支(t *testing.T) {
	hint := imageToolHint("unknown", true, "")
	if !strings.Contains(hint, "image_reading") || !strings.Contains(hint, "未作为原生图片输入") {
		t.Fatalf("vision 可用分支: %q", hint)
	}
	hint2 := imageToolHint("disabled", false, "")
	if !strings.Contains(hint2, "没有配置可用的视觉模型工具") {
		t.Fatalf("vision 不可用分支: %q", hint2)
	}
	hint3 := imageToolHint("unknown", true, "read_file")
	if !strings.Contains(hint3, "read_file(file_path=mediaPath)") {
		t.Fatalf("steer read_file 分支: %q", hint3)
	}
	hint4 := imageToolHint("unknown", true, "other")
	if !strings.Contains(hint4, "visual_question_answering(image_path_or_url=mediaPath") {
		t.Fatalf("steer 默认分支: %q", hint4)
	}
}

// TestPrepareImageToolInputs_原生输入开启直接返回 对齐 :116-118。
func TestPrepareImageToolInputs_原生输入开启直接返回(t *testing.T) {
	inputs := map[string]any{"query": "q"}
	out := PrepareImageToolInputs(map[string]any{}, inputs, true, true, "unknown", "")
	if len(out) != 1 || out["query"] != "q" {
		t.Fatalf("应原样返回: %v", out)
	}
}

// TestPrepareImageToolInputs_marker已存在跳过 对齐 :120-124。
func TestPrepareImageToolInputs_marker已存在跳过(t *testing.T) {
	img := promptImageFile(t)
	inputs := map[string]any{"query": "already jiuwenswarm_image_tool_context", "_multimodal_image_files": []MultimodalImageFile{img}}
	out := PrepareImageToolInputs(map[string]any{}, inputs, false, true, "unknown", "")
	if out["query"] != inputs["query"] {
		t.Fatalf("marker 已存在且非 steer 应原样: %q", out["query"])
	}
}

// TestPrepareImageToolInputs_注入JSON上下文 对齐 :126-176 主路径。
func TestPrepareImageToolInputs_注入JSON上下文(t *testing.T) {
	img := promptImageFile(t)
	inputs := map[string]any{
		"query":                   "看看这张图",
		"_multimodal_image_files": []MultimodalImageFile{img},
	}
	out := PrepareImageToolInputs(map[string]any{"query": "看看这张图"}, inputs, false, true, "unknown", "")
	q, _ := out["query"].(string)
	if !strings.Contains(q, "图片附件上下文") {
		t.Fatalf("未注入: %q", q)
	}
	if _, still := out["_multimodal_image_files"]; still {
		t.Fatal("_multimodal_image_files 应被删除")
	}
	// JSON 块含全键
	idx := strings.Index(q, "{")
	var payload map[string]any
	if err := json.Unmarshal([]byte(q[idx:]), &payload); err != nil {
		t.Fatalf("JSON 解析: %v", err)
	}
	for _, key := range []string{"marker", "mediaPath", "mediaItems", "question", "imageInputStatus", "toolHint"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("缺键 %s: %v", key, payload)
		}
	}
	if payload["marker"] != "jiuwenswarm_image_tool_context" || payload["question"] != "看看这张图" {
		t.Fatalf("marker/question 错: %v", payload)
	}
}

// TestPrepareImageToolInputs_steer补默认question 对齐 :139-141。
func TestPrepareImageToolInputs_steer补默认question(t *testing.T) {
	img := promptImageFile(t)
	inputs := map[string]any{"query": "s", "_multimodal_image_files": []MultimodalImageFile{img}}
	out := PrepareImageToolInputs(map[string]any{}, inputs, false, true, "unknown", "read_file")
	q, _ := out["query"].(string)
	if !strings.Contains(q, "Read the attached images and use them to continue the current task.") {
		t.Fatalf("steer 默认 question 未补: %q", q)
	}
}

// TestPrepareImageToolInputs_无图返回原样 对齐 :131-134。
func TestPrepareImageToolInputs_无图返回原样(t *testing.T) {
	inputs := map[string]any{"query": "q"}
	out := PrepareImageToolInputs(map[string]any{}, inputs, false, true, "unknown", "")
	if out["query"] != "q" || len(out) != 1 {
		t.Fatalf("无图应原样: %v", out)
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

func promptImageFile(t *testing.T) MultimodalImageFile {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.png")
	if err := os.WriteFile(p, []byte("px"), 0o644); err != nil {
		t.Fatal(err)
	}
	return MultimodalImageFile{Type: "image", Filename: "p.png", Path: p, MimeType: "image/png"}
}

func mkImageParamsForPrompt(t *testing.T) map[string]any {
	t.Helper()
	img := promptImageFile(t)
	return map[string]any{"media_items": []any{map[string]any{
		"type": "image", "path": img.Path, "mime_type": img.MimeType, "filename": img.Filename,
	}}}
}

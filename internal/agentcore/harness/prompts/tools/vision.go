package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ImageOCRMetadataProvider image_ocr 工具元数据提供者
type ImageOCRMetadataProvider struct{}

// VisualQuestionAnsweringMetadataProvider visual_question_answering 工具元数据提供者
type VisualQuestionAnsweringMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// imageOCRDescription image_ocr 工具双语描述
var imageOCRDescription = map[string]string{
	"cn": "读取图片中的可见文本，适合 OCR、票据文本提取和截图文字识别。",
	"en": "Extract visible text from an image for OCR, screenshot text recognition, and document snippets.",
}

// visualQuestionAnsweringDescription visual_question_answering 工具双语描述
var visualQuestionAnsweringDescription = map[string]string{
	"cn": "理解图片内容并回答问题，可选先做 OCR 再结合识别到的文字回答。",
	"en": "Understand an image and answer questions, optionally grounding the answer with OCR first.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetImageOCRMetadataProviderInputParams 构建 image_ocr 工具的参数 Schema
func GetImageOCRMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"image_path_or_url": {"cn": "本地图片路径或公网 http(s) 图片 URL", "en": "Local image path or public http(s) image URL"},
		"prompt":            {"cn": "可选，自定义 OCR 提示词", "en": "Optional custom OCR prompt"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_path_or_url": map[string]any{"type": "string", "description": d("image_path_or_url")},
			"prompt":            map[string]any{"type": "string", "description": d("prompt")},
		},
		"required": []any{"image_path_or_url"},
	}
}

// GetVisualQuestionAnsweringMetadataProviderInputParams 构建 visual_question_answering 工具的参数 Schema
func GetVisualQuestionAnsweringMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"image_path_or_url": {"cn": "本地图片路径或公网 http(s) 图片 URL", "en": "Local image path or public http(s) image URL"},
		"question":          {"cn": "要询问图片的问题", "en": "Question to ask about the image"},
		"include_ocr":       {"cn": "是否先执行 OCR 并把结果拼接进问答提示词，默认 true", "en": "Whether to run OCR first and inject the result into the VQA prompt, default true"},
		"ocr_prompt":        {"cn": "可选，自定义 OCR 提示词，仅在 include_ocr 为 true 时使用", "en": "Optional custom OCR prompt used only when include_ocr is true"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_path_or_url": map[string]any{"type": "string", "description": d("image_path_or_url")},
			"question":          map[string]any{"type": "string", "description": d("question")},
			"include_ocr":       map[string]any{"type": "boolean", "description": d("include_ocr")},
			"ocr_prompt":        map[string]any{"type": "string", "description": d("ocr_prompt")},
		},
		"required": []any{"image_path_or_url", "question"},
	}
}

// GetName 返回工具名称
func (p *ImageOCRMetadataProvider) GetName() string { return "image_ocr" }

// GetDescription 返回工具描述
func (p *ImageOCRMetadataProvider) GetDescription(language string) string {
	if d, ok := imageOCRDescription[language]; ok {
		return d
	}
	return imageOCRDescription["cn"]
}

// GetInputParams 返回工具输入参数
func (p *ImageOCRMetadataProvider) GetInputParams(language string) map[string]any {
	return GetImageOCRMetadataProviderInputParams(language)
}

// GetName 返回工具名称
func (p *VisualQuestionAnsweringMetadataProvider) GetName() string {
	return "visual_question_answering"
}

// GetDescription 返回工具描述
func (p *VisualQuestionAnsweringMetadataProvider) GetDescription(language string) string {
	if d, ok := visualQuestionAnsweringDescription[language]; ok {
		return d
	}
	return visualQuestionAnsweringDescription["cn"]
}

// GetInputParams 返回工具输入参数
func (p *VisualQuestionAnsweringMetadataProvider) GetInputParams(language string) map[string]any {
	return GetVisualQuestionAnsweringMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&ImageOCRMetadataProvider{})
	RegisterToolProvider(&VisualQuestionAnsweringMetadataProvider{})
}

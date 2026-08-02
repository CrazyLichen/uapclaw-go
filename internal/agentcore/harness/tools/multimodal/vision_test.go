package multimodal

import (
	"context"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── ImageOCRTool 测试 ────────────────────────────

func TestNewImageOCRTool_Invoke成功(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "OCR text from image"}},
	}
	config := newTestVisionConfig()

	ocrTool := NewImageOCRTool(mockClient, config, "cn", "test-agent")

	// 使用 tool.Invoke 调用
	result, err := ocrTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
	})
	if err != nil {
		t.Fatalf("ImageOCRTool Invoke 返回错误: %v", err)
	}
	if result["text"] != "OCR text from image" {
		t.Errorf("text = %v, 期望 'OCR text from image'", result["text"])
	}
	if result["model"] != "gpt-4o" {
		t.Errorf("model = %v, 期望 'gpt-4o'", result["model"])
	}
}

func TestNewImageOCRTool_Invoke失败(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("vision model error"))},
		},
	}
	config := newTestVisionConfig()

	ocrTool := NewImageOCRTool(mockClient, config, "cn", "test-agent")

	_, err := ocrTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
	})
	if err == nil {
		t.Error("ImageOCRTool 失败场景应返回错误")
	}
	// 框架 Invoke 会将 fn 返回的错误包装为 StatusToolLocalFunctionExecutionError
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	// 验证错误消息包含原始错误信息
	if !strings.Contains(baseErr.Error(), "vision model error") {
		t.Errorf("错误消息应包含原始错误 'vision model error', got %q", baseErr.Error())
	}
}

func TestNewImageOCRTool_Sandbox路径(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "should not reach"}},
	}
	config := newTestVisionConfig()

	ocrTool := NewImageOCRTool(mockClient, config, "cn", "test-agent")

	_, err := ocrTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "/home/user/sandbox/image.png",
	})
	if err == nil {
		t.Error("sandbox 路径应返回错误")
	}
}

func TestNewImageOCRTool_自定义提示词(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "custom OCR result"}},
	}
	config := newTestVisionConfig()

	ocrTool := NewImageOCRTool(mockClient, config, "cn", "test-agent")

	result, err := ocrTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
		"prompt":            "Extract all numbers from this image",
	})
	if err != nil {
		t.Fatalf("ImageOCRTool 自定义提示词返回错误: %v", err)
	}
	if result["text"] != "custom OCR result" {
		t.Errorf("text = %v, 期望 'custom OCR result'", result["text"])
	}
}

// ──────────────────────────── VQATool 测试 ────────────────────────────

func TestNewVisualQuestionAnsweringTool_Invoke成功_不含OCR(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "The image shows a cat"}},
	}
	config := newTestVisionConfig()

	vqaTool := NewVisualQuestionAnsweringTool(mockClient, config, "cn", "test-agent")

	result, err := vqaTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
		"question":          "What is in this image?",
		"include_ocr":       false,
	})
	if err != nil {
		t.Fatalf("VQATool Invoke 返回错误: %v", err)
	}
	if result["answer"] != "The image shows a cat" {
		t.Errorf("answer = %v, 期望 'The image shows a cat'", result["answer"])
	}
	if result["ocr_text"] != "" {
		t.Errorf("ocr_text 应为空字符串 (include_ocr=false), got %v", result["ocr_text"])
	}
}

func TestNewVisualQuestionAnsweringTool_Invoke成功_含OCR(t *testing.T) {
	// 第一次 OCR 调用，第二次 VQA 调用
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{text: "Hello World"}, // OCR
			{text: "The text says Hello World"}, // VQA
		},
	}
	config := newTestVisionConfig()

	vqaTool := NewVisualQuestionAnsweringTool(mockClient, config, "cn", "test-agent")

	result, err := vqaTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
		"question":          "What does the text say?",
		"include_ocr":       true,
	})
	if err != nil {
		t.Fatalf("VQATool Invoke 含OCR 返回错误: %v", err)
	}
	if result["answer"] != "The text says Hello World" {
		t.Errorf("answer = %v, 期望 'The text says Hello World'", result["answer"])
	}
	if result["ocr_text"] != "Hello World" {
		t.Errorf("ocr_text = %v, 期望 'Hello World'", result["ocr_text"])
	}
	if mockClient.callCount != 2 {
		t.Errorf("callCount = %d, 期望 2 (OCR + VQA)", mockClient.callCount)
	}
}

func TestNewVisualQuestionAnsweringTool_Invoke失败(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("model error"))},
		},
	}
	config := newTestVisionConfig()

	vqaTool := NewVisualQuestionAnsweringTool(mockClient, config, "cn", "test-agent")

	_, err := vqaTool.Invoke(context.Background(), map[string]any{
		"image_path_or_url": "https://example.com/image.png",
		"question":          "What?",
	})
	if err == nil {
		t.Error("VQATool 失败场景应返回错误")
	}
}

// ──────────────────────────── CreateVisionTools 测试 ────────────────────────────

func TestCreateVisionTools(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "result"}},
	}
	config := newTestVisionConfig()

	toolList := CreateVisionTools(mockClient, config, "cn", "test-agent")
	if len(toolList) != 2 {
		t.Fatalf("CreateVisionTools 应返回 2 个工具, got %d", len(toolList))
	}

	// 验证每个工具都有 ToolCard
	for i, tl := range toolList {
		card := tl.Card()
		if card == nil {
			t.Errorf("tool[%d] Card 不应为 nil", i)
		}
	}
}

// ──────────────────────────── 工具接口合规性测试 ────────────────────────────

func TestImageOCRTool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	config := newTestVisionConfig()
	ocrTool := NewImageOCRTool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = ocrTool
}

func TestVQATool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	config := newTestVisionConfig()
	vqaTool := NewVisualQuestionAnsweringTool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = vqaTool
}

// ──────────────────────────── ocrTextOrNone 测试 ────────────────────────────

func TestOcrTextOrNone_有文本(t *testing.T) {
	result := ocrTextOrNone("OCR text here")
	if result != "OCR text here" {
		t.Errorf("result = %q, 期望 %q", result, "OCR text here")
	}
}

func TestOcrTextOrNone_空文本(t *testing.T) {
	result := ocrTextOrNone("")
	if result != "No OCR used" {
		t.Errorf("result = %q, 期望 %q", result, "No OCR used")
	}
}

// ──────────────────────────── BaseModelClient mock 接口合规性测试 ────────────────────────────

func TestMockVisionClient_ImplementsBaseModelClient(t *testing.T) {
	var _ modelclients.BaseModelClient = (*mockVisionClient)(nil)
}

// ──────────────────────────── 常量测试 ────────────────────────────

func TestDefaultOCRPrompt_非空(t *testing.T) {
	if defaultOCRPrompt == "" {
		t.Error("defaultOCRPrompt 不应为空")
	}
	if !strings.Contains(defaultOCRPrompt, "OCR") {
		t.Error("defaultOCRPrompt 应包含 'OCR'")
	}
}

func TestDefaultVQAPromptTemplate_非空(t *testing.T) {
	if defaultVQAPromptTemplate == "" {
		t.Error("defaultVQAPromptTemplate 不应为空")
	}
}

func TestSandboxPathMarker(t *testing.T) {
	if sandboxPathMarker != "home/user" {
		t.Errorf("sandboxPathMarker = %q, 期望 %q", sandboxPathMarker, "home/user")
	}
}

// ──────────────────────────── NewAssistantMessage 测试 ────────────────────────────

func TestNewAssistantMessage_空内容(t *testing.T) {
	msg := llmschema.NewAssistantMessage("")
	if msg.GetContent().Text() != "" {
		t.Error("空内容 AssistantMessage 应返回空文本")
	}
}

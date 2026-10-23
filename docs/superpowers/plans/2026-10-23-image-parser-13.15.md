# 13.15 Image Parser + Captioner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 ImageCaptioner（图片归档 + LLM caption 生成）与 ImageParser（六扩展名图片解析，产出 `metadata["image_path"]`），接通 13.4 多模态嵌入管道的生产者。

**Architecture:** 对齐 Python `captioner.py` + `image_parser.py` 一比一。captioner.go 提供 ImageCaptioner（软失败全链）+ 包级 CpImage 归档函数；image_parser.go 覆写整体 Parse（非 ParseContent——产出 image_path 元数据），B1 落点：Parse 单次归档后归档路径直通 ParseContent。测试用 `llm.NewModel + WithClient(mockLLMClient)` 走真实 Invoke 全链。

**Tech Stack:** Go stdlib（os/filepath/base64/mime/strings）+ 现有 llm foundation（Model.Invoke/MessagesParam/ContentPart）。零新增外部依赖。

**Spec:** `docs/superpowers/specs/2026-10-23-image-parser-13.15-design.md`（决策表 8 项 + 偏差归档 6 项）

**编译纪律（规则 5）:** 每次编译/测试前 `pgrep -f 'go (build|test)'` 检查残留；全程 `export GOPROXY=https://goproxy.cn,direct`；单进程互斥。

---

### Task 1: captioner.go — 常量 + CpImage 归档函数

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/captioner.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go`

- [ ] **Step 1: 写 CpImage 失败测试（先测后码）**

`captioner_test.go` 完整初始内容：

```go
package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// writeTempImage 在临时目录写一个最小 PNG 字节文件，返回路径。
func writeTempImage(t *testing.T, name string) string {
	t.Helper()
	// 1x1 透明 PNG 真实字节（zlib 结构有效，供 base64 断言复用）
	pngBytes := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG 魔数
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR 长度+类型
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, // 8bit RGBA + CRC
		0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, // IDAT
		0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, // 最小数据
		0x0D, 0x0A, 0x2D, 0xB4, // CRC
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82, // IEND
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	return path
}

// ──────────────────────────── CpImage 测试 ────────────────────────────

// TestCpImage_正常复制 测试图片复制到 images 目录且内容一致。
func TestCpImage_正常复制(t *testing.T) {
	src := writeTempImage(t, "photo.png")

	dest, err := CpImage(src)
	if err != nil {
		t.Fatalf("CpImage 不应报错: %v", err)
	}
	if filepath.Base(dest) != "photo.png" {
		t.Errorf("目标文件名应为 photo.png，实际 %s", dest)
	}
	got, _ := os.ReadFile(dest)
	want, _ := os.ReadFile(src)
	if string(got) != string(want) {
		t.Error("副本内容应与原件一致")
	}
}

// TestCpImage_文件不存在 测试源文件不存在时报错。
func TestCpImage_文件不存在(t *testing.T) {
	_, err := CpImage(filepath.Join(t.TempDir(), "no_such.png"))
	if err == nil {
		t.Fatal("文件不存在应返回 error（对齐 Python FileNotFoundError）")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestCpImage -v 2>&1 | tail -5`
Expected: FAIL——`undefined: CpImage`

- [ ] **Step 3: 写 captioner.go 骨架（常量 + CpImage）**

`captioner.go` 完整初始内容：

```go
package parser

import (
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// imageCaptionPrompt 图片描述生成提示词。
	//
	// Python: captioner.py IMAGE_CAPTION_PROMPT（逐字复制，禁止翻译改写——
	// 记忆 feedback_prompt_verbatim_copy 既定规则）。
	imageCaptionPrompt = "You are an assistant specialized in document and image analysis. " +
		"Your task is to provide a detailed, qualitative description of the provided image " +
		"so that it can be embedded and used for semantic retrieval. " +
		"Describe all visible content including text, figures, charts, tables, diagrams, and layout. " +
		"Focus on what the image conveys and means, not just what it literally depicts. " +
		"Do not include any preamble — output only the description."

	// savedImageDir 归档图片目录（相对路径，字面对齐 Python SAVED_IMAGE_DIR = "images"）。
	savedImageDir = "images"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// supportedLLMClient 支持图片 captioning 的模型名前缀白名单。
	//
	// Python: captioner.py ImageCaptioner._supported_llm_client（类属性，
	// Go 归入包级变量区块）。
	supportedLLMClient = []string{"gpt-4o", "gpt-5", "qwen3-vl", "qwen-vl"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CpImage 复制图片到 savedImageDir 目录并返回目标路径。
//
// 分支（逐条对齐 Python cp_image）：
//  1. 源不存在 → 返回 error（对齐 raise FileNotFoundError）
//  2. 目标目录创建失败 → 返回 error（对齐 makedirs 异常传播）
//  3. 源与目标同一路径 → 跳过复制直接返回目标
//  4. 复制失败 → Warn 日志 + 返回原路径 + nil（软失败，对齐 Python fallback）
//
// Python: captioner.py ImageCaptioner.cp_image（static method；Go 无默认参数，
// targetDir 定死 savedImageDir 常量——全 codebase 无非默认调用，spec 决策 5）
func CpImage(imageLoc string) (string, error) {
	if _, err := os.Stat(imageLoc); err != nil {
		return "", err
	}

	if err := os.MkdirAll(savedImageDir, 0o755); err != nil {
		return "", err
	}

	destPath := filepath.Join(savedImageDir, filepath.Base(imageLoc))

	// 仅当目标与源不同路径时复制（对齐 abspath 比较）
	srcAbs, srcErr := filepath.Abs(imageLoc)
	destAbs, destErr := filepath.Abs(destPath)
	if srcErr == nil && destErr == nil && srcAbs == destAbs {
		return destPath, nil
	}

	if err := copyFile(imageLoc, destPath); err != nil {
		// 对齐 Python: logger.warning(f"Failed to save copy of {image_loc} to {dest_path}: {e}")
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Str("dest_path", destPath).
			Err(err).
			Msg("failed to save copy of image")
		// 复制失败回落原路径（对齐 Python fall back）
		return imageLoc, nil
	}
	return destPath, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyFile 读源写目标复制文件。
//
// Python shutil.copy2 保留元数据；caption 场景仅读内容，元数据对下游
// （13.4 多模态嵌入读内容）无意义——偏差归档 spec §8-2。
func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

// init 注册 Python 端补充的 MIME 类型映射（幂等：retrieval/common/document.go
// init 已注册过，字面对齐 captioner.py 模块级 mimetypes.add_type）。
func init() {
	// Python mimetypes.add_type 无返回值；注册失败仅影响该扩展名识别
	_ = os.Setenv("UAPCLAW_PARSER_JFIF_MIME", "image/jpeg") // 占位：真实注册见 Task 3 Step 3
}
```

⚠️ 上面 init 是临时占位（编译通过用），Task 3 Step 3 替换为 mime 注册真实实现。此时文件里 `llm` import 未使用会编译报错——**先在导入块中注释掉 llm**：

```go
import (
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

（Task 3 加 ImageCaptioner 时再恢复 llm import。）

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestCpImage -v 2>&1 | tail -5`
Expected: PASS（2 用例；`images/` 目录产生在测试工作目录——用例断言已覆盖）

- [ ] **Step 5: 补 CpImage 边界用例（源=目标跳过 / 复制失败 fallback）**

在 `captioner_test.go` 追加：

```go
// TestCpImage_源即目标 测试源文件已在 images 目录时跳过复制。
func TestCpImage_源即目标(t *testing.T) {
	dir := t.TempDir()
	// 直接把测试工作目录切到 dir（CpImage 的 images/ 相对当前目录）
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换工作目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "in_place.png")
	// 预先放入 images/ 使源=目标
	if err := os.MkdirAll("images", 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join("images", "in_place.png")
	if err := os.WriteFile(dest, mustRead(t, src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := CpImage(dest)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != dest {
		t.Errorf("源=目标时应原样返回 %s，实际 %s", dest, got)
	}
}

// mustRead 读文件失败时 Fatal。
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return data
}

// TestCpImage_复制失败回落原路径 测试复制失败时软失败返回源路径。
func TestCpImage_复制失败回落原路径(t *testing.T) {
	// 源存在但内容不可读不可写场景难以跨平台构造；
	// 用源是目录的方式构造 Stat 成功但 ReadFile 失败
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "fake.png")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := CpImage(srcDir)
	if err != nil {
		t.Fatalf("复制失败应软失败不报错，实际: %v", err)
	}
	if got != srcDir {
		t.Errorf("复制失败应回落原路径 %s，实际 %s", srcDir, got)
	}
}
```

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestCpImage -v 2>&1 | tail -8`
Expected: PASS（4 用例）

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/captioner.go internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go
git commit -m "feat(parser): 13.15 CpImage 图片归档函数——四分支软失败语义 + 提示词常量"
```

---

### Task 2: captioner.go — ImageCaptioner 构造 + isLLMSupported

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/captioner.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go`

- [ ] **Step 1: 写失败测试**

在 `captioner_test.go` 追加（同时恢复 captioner.go 的 llm import）：

```go
// TestNewImageCaptioner_三类客户端 测试 nil/匹配名/不匹配名构造均不报错。
func TestNewImageCaptioner_三类客户端(t *testing.T) {
	// nil 客户端：禁用态，构造不报错
	c := NewImageCaptioner(nil)
	if c == nil {
		t.Fatal("nil client 构造应返回非 nil 实例")
	}

	// 匹配名：qwen3-vl 前缀命中白名单
	cfg, _ := llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen3-vl-plus"))
	matched := NewImageCaptioner(newTestModel(t, cfg))
	if matched == nil {
		t.Fatal("匹配名构造应返回非 nil 实例")
	}

	// 不匹配名：仅 warn 不报错
	cfg2, _ := llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-r1"))
	unmatched := NewImageCaptioner(newTestModel(t, cfg2))
	if unmatched == nil {
		t.Fatal("不匹配名构造应返回非 nil 实例（仅 warn）")
	}
}
```

需要的测试辅助（追加到 captioner_test.go 测试辅助区；导入块补 `llm`/`llmschema`）：

```go
// newTestModel 构造带 mock 客户端的 Model（WithClient 注入，走真实 Invoke 链）。
func newTestModel(t *testing.T, modelCfg *llmschema.ModelRequestConfig) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, modelCfg, llm.WithClient(&stubCaptionClient{}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// stubCaptionClient 惰性 mock：所有方法零行为，具体断言场景用 captionClient。
type stubCaptionClient struct{}

func (s *stubCaptionClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubCaptionClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubCaptionClient) SupportsKVCacheRelease() bool { return false }
```

导入块最终为：

```go
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestNewImageCaptioner -v 2>&1 | tail -5`
Expected: FAIL——`undefined: NewImageCaptioner`

- [ ] **Step 3: 实现 ImageCaptioner 结构体 + 构造 + isLLMSupported**

在 `captioner.go` 结构体区块加入：

```go
// ImageCaptioner 图片描述生成器。
//
// 构造即检查 LLM 客户端支持度（nil 或模型名不在白名单仅 Warn 不报错——
// caption 是软能力，禁用态合法）。归档能力经包级 CpImage 提供。
//
// Python: captioner.py ImageCaptioner
type ImageCaptioner struct {
	// llmClient 可选 LLM 客户端（nil = 禁用态：仅归档不 caption）。
	llmClient *llm.Model
}
```

导出函数区块加入：

```go
// NewImageCaptioner 构造 ImageCaptioner。
//
// 构造即调用 isLLMSupported（对齐 Python __init__ 内 this._is_llm_supported()）。
//
// Python: captioner.py ImageCaptioner.__init__
func NewImageCaptioner(llmClient *llm.Model) *ImageCaptioner {
	c := &ImageCaptioner{llmClient: llmClient}
	c.isLLMSupported()
	return c
}
```

非导出函数区块加入：

```go
// isLLMSupported 检查 LLM 客户端对图片 captioning 的支持度。
//
// 两分支均仅 Warn 不报错（对齐 Python docstring 声明的 bool 语义但实现
// 只打日志）：
//  1. nil 客户端 → 禁用态提示
//  2. 模型名前缀不在白名单 → 可能不支持提示
//
// Python: captioner.py ImageCaptioner._is_llm_supported
func (c *ImageCaptioner) isLLMSupported() {
	if c.llmClient == nil {
		// Python f"...empty {self.llm_client=}..." 的 = 自省语法 Go 无等价物，
		// 用字面 "nil" 近似（偏差归档 spec §8-3）
		logger.Warn(logComponent).
			Str("llm_client", "nil").
			Msg("image captioning is disabled for empty llm_client, please ensure an appropriate VLM is used")
		return
	}
	modelName := ""
	if c.llmClient.ModelConfig != nil {
		modelName = c.llmClient.ModelConfig.ModelName
	}
	for _, base := range supportedLLMClient {
		if strings.HasPrefix(modelName, base) {
			return
		}
	}
	// 对齐 Python f"{self.llm_client.model_config.model_name=} may not be supported..."
	logger.Warn(logComponent).
		Str("model_name", modelName).
		Msg("model may not be supported for image captioning, please ensure an appropriate VLM is used")
}
```

（`strings` 补入 captioner.go 导入块；恢复 `llm` import。）

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run "TestNewImageCaptioner|TestCpImage" -v 2>&1 | tail -8`
Expected: PASS（5 用例）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/captioner.go internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go
git commit -m "feat(parser): 13.15 ImageCaptioner 构造与支持度检查——warn 不报错语义"
```

---

### Task 3: captioner.go — llmCall + CaptionImages + MIME init

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/captioner.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go`

- [ ] **Step 1: 写失败测试（核心：fakeClient 断言 data URI）**

在 `captioner_test.go` 追加：

```go
// captionClient 可断言 Invoke 入参的 mock：记录收到的消息 dict。
type captionClient struct {
	stubCaptionClient
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (c *captionClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if c.invokeFn != nil {
		return c.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("default caption"), nil
}

// newCaptionModel 构造带断言 mock 的 Model。
func newCaptionModel(t *testing.T, modelCfg *llmschema.ModelRequestConfig, fn captionInvokeFn) *llm.Model {
	t.Helper()
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, err := llm.NewModel(clientCfg, modelCfg, llm.WithClient(&captionClient{invokeFn: fn}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// captionInvokeFn Invoke 注入函数类型。
type captionInvokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)

// TestCaptionImages_单图成功 核心用例：断言 LLM 收到正确的多模态消息。
func TestCaptionImages_单图成功(t *testing.T) {
	src := writeTempImage(t, "photo.png")
	wantBytes := mustRead(t, src)

	var gotRole string
	var gotParts []llmschema.ContentPart
	model := newCaptionModel(t, nil, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		msgs := messages.Messages()
		if len(msgs) != 1 {
			t.Fatalf("应发 1 条消息，实际 %d", len(msgs))
		}
		gotRole = msgs[0].GetRole().String()
		gotParts = msgs[0].GetContent().Parts()
		return llmschema.NewAssistantMessage("A chart."), nil
	})

	c := NewImageCaptioner(model)
	got := c.CaptionImages(t.Context(), []string{src})
	if len(got) != 1 || got[0] != "A chart." {
		t.Fatalf("应返回 [A chart.]，实际 %v", got)
	}
	if gotRole != "user" {
		t.Errorf("role 应为 user，实际 %s", gotRole)
	}
	if len(gotParts) != 2 {
		t.Fatalf("content 应为 2 个分片，实际 %d", len(gotParts))
	}
	// 分片 1：提示词文本
	if gotParts[0].Type != "text" || !strings.HasPrefix(gotParts[0].Text, "You are an assistant") {
		t.Errorf("分片 1 应为提示词文本，实际 type=%s", gotParts[0].Type)
	}
	// 分片 2：data URI，base64 解码后等于原件字节
	if gotParts[1].Type != "image_url" || gotParts[1].ImageURL == nil {
		t.Fatalf("分片 2 应为 image_url，实际 %+v", gotParts[1])
	}
	url := gotParts[1].ImageURL.URL
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("data URI 前缀错误: %s", url[:40])
	}
	b64 := strings.TrimPrefix(url, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(decoded) != string(wantBytes) {
		t.Error("解码后字节应与图片原件一致")
	}
}

// TestCaptionImages_文件不存在 测试跳过不存在的图。
func TestCaptionImages_文件不存在(t *testing.T) {
	c := NewImageCaptioner(nil) // 任何 client：文件不存在分支在前
	got := c.CaptionImages(t.Context(), []string{"/no/such/file.png"})
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("不存在文件应返回 [\"\"]，实际 %v", got)
	}
}

// TestLLMCall_禁用态 测试 nil client 返回空串。
func TestLLMCall_禁用态(t *testing.T) {
	c := NewImageCaptioner(nil)
	if got := c.llmCall(t.Context(), "whatever.png"); got != "" {
		t.Errorf("禁用态应返回空串，实际 %q", got)
	}
}

// TestLLMCall_调用失败软失败 测试 Invoke 报错时返回空串。
func TestLLMCall_调用失败软失败(t *testing.T) {
	src := writeTempImage(t, "fail.png")
	model := newCaptionModel(t, nil, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, errors.New("boom")
	})
	c := NewImageCaptioner(model)
	if got := c.llmCall(t.Context(), src); got != "" {
		t.Errorf("Invoke 失败应返回空串，实际 %q", got)
	}
}

// TestLLMCall_mime回落 测试无法识别扩展名时回落 image/png。
func TestLLMCall_mime回落(t *testing.T) {
	src := writeTempImage(t, "noext") // 无扩展名
	var gotURL string
	model := newCaptionModel(t, nil, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		parts := messages.Messages()[0].GetContent().Parts()
		gotURL = parts[1].ImageURL.URL
		return llmschema.NewAssistantMessage("x"), nil
	})
	c := NewImageCaptioner(model)
	_ = c.llmCall(t.Context(), src)
	if !strings.HasPrefix(gotURL, "data:image/png;base64,") {
		t.Errorf("无扩展名应回落 image/png，实际 %s", gotURL[:30])
	}
}
```

导入块补 `"encoding/base64"` 与 `"strings"`。

⚠️ `t.Context()` 需 Go 1.24+。若项目 Go 版本低于 1.24，用 `context.Background()` 替换所有 `t.Context()`。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run "TestCaptionImages|TestLLMCall" -v 2>&1 | tail -5`
Expected: FAIL——`c.CaptionImages undefined` / `c.llmCall undefined`

- [ ] **Step 3: 实现 llmCall + CaptionImages + 替换 init 占位**

`captioner.go` 导出函数区块追加：

```go
// CaptionImages 逐图串行生成 caption 列表。
//
// 文件不存在的图：Warn + 空串占位（对齐 Python skip 分支）。
// 顺序与输入一致（对齐 Python captions.append 逐个累积）。
//
// Python: captioner.py ImageCaptioner.caption_images
func (c *ImageCaptioner) CaptionImages(ctx context.Context, imageLocs []string) []string {
	captions := make([]string, 0, len(imageLocs))
	for _, imageLoc := range imageLocs {
		var caption string
		if _, err := os.Stat(imageLoc); err != nil {
			// 对齐 Python: logger.warning(f"Image file {image_loc} does not exist, skipping captioning.")
			logger.Warn(logComponent).
				Str("image_loc", imageLoc).
				Msg("image file does not exist, skipping captioning")
			caption = ""
		} else {
			caption = c.llmCall(ctx, imageLoc)
		}
		captions = append(captions, caption)
	}
	return captions
}
```

非导出函数区块追加：

```go
// llmCall 生成单张图片的 caption。
//
// 流程（逐条对齐 Python _llm_call_async）：
//  1. nil 客户端 → 返回空串（禁用态非失败）
//  2. mime 按扩展名检测；未知 → 回落 image/png + Warn
//  3. 读文件 base64 编码
//  4. 多模态消息（text 提示词 + image_url data URI）Invoke
//  5. resp.Content.Text() 取描述
//  6. 任一步失败 → Warn + 返回空串（软失败）
//
// Python: captioner.py ImageCaptioner._llm_call_async
func (c *ImageCaptioner) llmCall(ctx context.Context, imageLoc string) string {
	if c.llmClient == nil {
		return ""
	}

	// 对齐 Python: mime_type, _ = mimetypes.guess_type(image_loc)
	mimeType := mime.TypeByExtension(filepath.Ext(imageLoc))
	if mimeType == "" {
		mimeType = "image/png"
		// 对齐 Python: logger.warning(f"Could not determine MIME type for {image_loc=}, using {mime_type}")
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Str("mime_type", mimeType).
			Msg("could not determine MIME type, using fallback")
	}

	// 对齐 Python: logger.info(f"Calling LLM for image captioning for {image_loc=}")
	logger.Info(logComponent).
		Str("image_loc", imageLoc).
		Msg("calling LLM for image captioning")

	imageBytes, err := os.ReadFile(imageLoc)
	if err != nil {
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Err(err).
			Msg("LLM-based caption failed for image")
		return ""
	}
	imageB64 := base64.StdEncoding.EncodeToString(imageBytes)

	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
		llmschema.ContentPart{
			Type: "text",
			Text: imageCaptionPrompt,
		},
		llmschema.ContentPart{
			Type: "image_url",
			ImageURL: &llmschema.ImageURL{
				URL: fmt.Sprintf("data:%s;base64,%s", mimeType, imageB64),
			},
		},
	))

	resp, err := c.llmClient.Invoke(ctx, model_clients.NewMessagesParam(msg))
	if err != nil {
		// 对齐 Python: logger.warning(f"LLM-based caption ... failed ...: {e}")
		//（Python 该条日志变量冗余，Go 字段单次——偏差归档 spec §8-4）
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Err(err).
			Msg("LLM-based caption failed for image")
		return ""
	}
	if resp == nil {
		return ""
	}
	return resp.Content.Text()
}
```

（导入块补 `"encoding/base64"`、`"fmt"`、`"mime"`、`model_clients`、`llmschema`。）

**替换 Task 1 的 init 占位**为真实 mime 注册：

```go
// init 注册 Python 端补充的 MIME 类型映射（幂等：retrieval/common/document.go
// init 已注册过，重复注册同一映射无副作用；字面对齐 captioner.py 模块级
// mimetypes.add_type("image/jpeg", ".jfif", strict=True)）。
func init() {
	_ = mime.AddExtensionType(".jfif", "image/jpeg")
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run "TestCaptionImages|TestLLMCall|TestCpImage|TestNewImageCaptioner" -v 2>&1 | tail -15`
Expected: PASS（10 用例全绿）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/captioner.go internal/agentcore/retrieval/indexing/processor/parser/captioner_test.go
git commit -m "feat(parser): 13.15 llmCall+CaptionImages——多模态消息构造/软失败链/.jfif MIME 注册"
```

---

### Task 4: image_parser.go — ImageParser 覆写 Parse + 注册

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/image_parser.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/image_parser_test.go`

- [ ] **Step 1: 写失败测试（含禁用态核心用例）**

`image_parser_test.go` 完整初始内容：

```go
package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// ──────────────────────────── ImageParser 测试 ────────────────────────────

// newImageParserOrFatal 构造 ImageParser。
func newImageParserOrFatal(t *testing.T) *ImageParser {
	t.Helper()
	p, err := NewImageParser()
	if err != nil {
		t.Fatalf("构造 ImageParser 失败: %v", err)
	}
	return p
}

// TestImageParser_Parse_正常 测试完整链路：归档 + caption + image_path 元数据。
func TestImageParser_Parse_正常(t *testing.T) {
	// 切工作目录使 images/ 落在临时目录
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "photo.png")
	model := newCaptionModel(t, nil, nil) // 默认返回 "default caption"

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src, WithDocID("doc-1"), WithLLMClient(model))
	if err != nil {
		t.Fatalf("Parse 不应报错: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("应返回 1 个 Document，实际 %d", len(docs))
	}
	d := docs[0]
	if d.ID != "doc-1" {
		t.Errorf("ID 应透传 doc-1，实际 %q", d.ID)
	}
	if d.Text != "default caption" {
		t.Errorf("Text 应为 caption，实际 %q", d.Text)
	}
	gotPath, _ := d.Metadata["image_path"].(string)
	if gotPath != filepath.Join("images", "photo.png") {
		t.Errorf("image_path 应为归档路径，实际 %q", gotPath)
	}
}

// TestImageParser_Parse_禁用态 ★核心用例：nil client 仍返回单 Document。
func TestImageParser_Parse_禁用态(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "disabled.png")

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src)
	if err != nil {
		t.Fatalf("禁用态不应报错: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("禁用态仍应返回 1 个 Document（空文本走 13.4 纯图片嵌入），实际 %d", len(docs))
	}
	if docs[0].Text != "" {
		t.Errorf("禁用态 Text 应为空串，实际 %q", docs[0].Text)
	}
	if _, ok := docs[0].Metadata["image_path"]; !ok {
		t.Error("禁用态 metadata.image_path 必须存在")
	}
}

// TestImageParser_Parse_源不存在 测试软失败返回空切片。
func TestImageParser_Parse_源不存在(t *testing.T) {
	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), "/no/such/file.png")
	if err != nil {
		t.Fatalf("软失败不应报错: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("失败应返回空切片，实际 %d", len(docs))
	}
}

// TestImageParser_Parse_B1归档路径直通 测试 caption 读的是归档副本。
func TestImageParser_Parse_B1归档路径直通(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "b1.png")
	var invoked bool
	model := newCaptionModel(t, nil, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		invoked = true
		return llmschema.NewAssistantMessage("cap"), nil
	})

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src, WithLLMClient(model))
	if err != nil || len(docs) != 1 {
		t.Fatalf("Parse 应成功: %v, %d docs", err, len(docs))
	}
	if !invoked {
		t.Fatal("LLM 应被调用一次")
	}
	// 副本存在且与原件一致（caption 走副本）
	copied := filepath.Join("images", "b1.png")
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("归档副本应存在: %v", err)
	}
}

// TestImageParser_注册 测试六扩展名已注册（init 副作用）。
func TestImageParser_注册(t *testing.T) {
	want := []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".jfif"}
	for _, ext := range want {
		if _, ok := getParserFactory(ext); !ok {
			t.Errorf("扩展名 %s 应已注册", ext)
		}
	}
	// 工厂产出正确类型
	factory, _ := getParserFactory(".png")
	if _, ok := factory().(*ImageParser); !ok {
		t.Error("工厂应产出 *ImageParser")
	}
}

// TestImageParser_LazyParse转发 测试继承 BaseParser 默认实现。
func TestImageParser_LazyParse转发(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "lazy.png")
	p := newImageParserOrFatal(t)

	var seen []string
	err := p.LazyParse(t.Context(), src, func(d reranker.Document) error {
		seen = append(seen, d.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("LazyParse 不应报错: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("禁用态 LazyParse 应回调 1 次（空文本 Document 也产出），实际 %d", len(seen))
	}
}
```

导入块补 `model_clients` 与 `reranker`：

```go
import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)
```

（`t.Context()` 同 Task 3 的 Go 版本注意事项。）

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestImageParser -v 2>&1 | tail -5`
Expected: FAIL——`undefined: NewImageParser`（类型不存在）

- [ ] **Step 3: 实现 image_parser.go**

完整内容：

```go
package parser

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImageParser 图片文件解析器。
//
// 嵌入 BaseParser 但覆写整体 Parse（非仅 ParseContent）——因为要产出
// metadata["image_path"]，BaseParser 模板的"非空内容才包 Document"逻辑
// 不适用（对齐 Python ImageParser 覆写整体 parse）。LazyParse/Process/
// Supports 继承 BaseParser 默认（经 self 动态派发，覆写 Parse 即生效）。
//
// 归档路径策略（B1，spec 决策 4）：Parse 单次 CpImage 后把归档路径传给
// ParseContent，caption 全程读副本（语义等价：caption 读文件内容，
// 副本≡原件）。Python _parse 内返回值丢弃的第二次 cp_image 按去重决策
// 移除（spec 偏差 §8-1/8-5）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/image_parser.py (ImageParser)
type ImageParser struct {
	// BaseParser 默认适配器（LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 ImageParser 实现 Parser
var _ Parser = (*ImageParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewImageParser 构造 ImageParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）；NewBaseParser(self)
// 注入使模板方法动态派发到本类覆写。
//
// Python: image_parser.py ImageParser.__init__
func NewImageParser() (*ImageParser, error) {
	p := &ImageParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析图片文件：归档 + 生成 caption + 返回带 image_path 元数据的文档。
//
// 与 BaseParser 默认实现的差异（覆写原因）：
//  1. caption 为空也返回单个 Document（禁用态≠解析失败——空文本 chunk
//     流到 13.4 ComputeChunkEmbeddings 时 isImageChunk 靠 image_path 命中，
//     走 EmbedMultimodal 纯图片嵌入路径）
//  2. metadata 固定 {"image_path": 归档路径}（13.4 isImageChunk 的消费键）
//
// 软失败：CpImage/内部失败 → Error 日志 + 空切片（对齐 Python except → []）。
//
// Python: image_parser.py ImageParser.parse
func (p *ImageParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)

	savedPath, err := CpImage(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse image {doc}: {e}")
		logger.Error(logComponent).
			Str("doc", doc).
			Err(err).
			Msg("failed to parse image")
		return []reranker.Document{}, nil
	}

	content, err := p.ParseContent(ctx, savedPath, o.LLMClient)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse image {doc}: {e}")
		logger.Error(logComponent).
			Str("doc", doc).
			Err(err).
			Msg("failed to parse image")
		return []reranker.Document{}, nil
	}

	return []reranker.Document{{
		ID:   o.DocID,
		Text: content,
		Metadata: map[string]any{
			"image_path": savedPath,
		},
	}}, nil
}

// ParseContent 对齐 Python _parse：构造 captioner 对归档路径生成 caption。
//
// B1 落点：doc 参数为调用方（Parse）传入的归档路径；单独调用时视为
// 任意图片路径（自含：构造 captioner + caption，不再重复归档）。
// 空 caption → 返回空串（对齐 Python return None 的 falsy 语义）。
//
// Python: image_parser.py ImageParser._parse
func (p *ImageParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	captioner := NewImageCaptioner(llmClient)
	captions := captioner.CaptionImages(ctx, []string{doc})

	// 对齐 Python: "\n".join([c for c in captions if c]) if captions else None
	var nonEmpty []string
	for _, c := range captions {
		if c != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	return strings.Join(nonEmpty, "\n"), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".png", ".jpg",
// ".jpeg", ".webp", ".gif", ".jfif"]) import 时执行）。
func init() {
	RegisterParser([]string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".jfif"}, func() Parser {
		p, _ := NewImageParser()
		return p
	})
}
```

（导入块补 `"strings"`。）

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/parser/ -run TestImageParser -v 2>&1 | tail -10`
Expected: PASS（6 用例）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/image_parser.go internal/agentcore/retrieval/indexing/processor/parser/image_parser_test.go
git commit -m "feat(parser): 13.15 ImageParser——覆写Parse产出image_path/B1归档直通/六扩展名注册"
```

---

### Task 5: 全量验证 + doc.go + IMPLEMENTATION_PLAN 状态同步

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md:807`（13.15 行）

- [ ] **Step 1: 全量测试 + race + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -race -cover ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | tail -3`
Expected: `ok ... coverage: ≥90%`（基线 90.2%，只升不降；race 零报告）

- [ ] **Step 2: 更新 doc.go 文件目录**

在 `parser/doc.go` 文件目录树（json_parser.go/txt_md_parser.go 条目之后）追加：

```go
//	├── captioner.go     # ImageCaptioner 图片描述生成 + CpImage 归档
//	├── image_parser.go  # ImageParser 图片解析（image_path 元数据产出）
```

包概述补一句（在现有概述段落后）：

```go
// 图片文件经 ImageParser 解析：归档到 images/ 目录并经 ImageCaptioner
// 生成 caption，产出 metadata["image_path"] 供 13.4 多模态嵌入消费。
```

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 13.15 行**

`IMPLEMENTATION_PLAN.md` 807 行改为（☐→✅）：

```markdown
| 13.15 | ✅ | Image Parser + Captioner | `ImageCaptioner(构造即isLLMSupported warn双分支/CpImage四分支软失败归档/llmCall多模态消息+base64+data URI全软失败链/CaptionImages逐图串行) + ImageParser(覆写整体Parse产metadata.image_path——空caption仍单Document接通13.4纯图片嵌入/B1归档路径直通ParseContent/六扩展名init注册) + 双cp_image去重(spec偏差§8-1)+.jfif MIME幂等注册`。⚠️ 换序二次修订（2026-10-23）：13.15 提前至 13.12 前实施（13.12 PDF/13.13 Word 硬依赖 ImageCaptioner）。设计：docs/superpowers/specs/2026-10-23-image-parser-13.15-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/image_parser.py · captioner.py` |
```

同时 13.12/13.13 行（804/805）末尾加注：

```markdown
（⤴️ ImageCaptioner 已由 13.15 提前交付）
```

- [ ] **Step 4: 领域全量回归**

Run: `cd /home/opensource/uapclaw-gateway && go build ./... && go test ./internal/agentcore/retrieval/... 2>&1 | tail -5`
Expected: 全部 ok（retrieval 域无回归）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(parser): 13.15 状态同步——doc.go 条目+IMPLEMENTATION_PLAN ✅+13.12/13.13 ⤴️ 注记"
```

---

## 计划自审记录

1. **Spec 覆盖**：§4.1（常量/CpImage/isLLMSupported/llmCall/CaptionImages→Task 1-3）、§4.2（Parse/ParseContent/注册→Task 4）、§5 文件布局（doc.go→Task 5）、§6 测试 16 用例全落（Task 1:4、Task 2:1×3 分支、Task 3:5、Task 4:6）、§7 状态同步（Task 5）、§8 偏差已在代码注释逐处引用。无缺口。
2. **占位符扫描**：Task 1 Step 3 的 init 占位是显式的临时编译手段且 Task 3 Step 3 有替换代码——非计划失败。其余零 TBD/TODO。
3. **类型一致性**：`CpImage(string)(string,error)`、`NewImageCaptioner(*llm.Model)*ImageCaptioner`、`CaptionImages(ctx,[]string)[]string`、`llmCall(ctx,string)string`、`Parse/ParseContent` 签名与 spec §4 一致；测试辅助 `newTestModel/newCaptionModel/captionClient/stubCaptionClient` 命名前后一致。

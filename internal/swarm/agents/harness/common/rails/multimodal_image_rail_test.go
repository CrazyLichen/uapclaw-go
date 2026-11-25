package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/turnctx"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
)

// ──────────────────────────── 构造与元信息 ────────────────────────────

// TestMultimodalImageRail_Priority 对齐 Python priority = 75。
func TestMultimodalImageRail_Priority(t *testing.T) {
	rail := NewMultimodalImageRail(nil)
	if rail.Priority() != 75 {
		t.Fatalf("priority = %d", rail.Priority())
	}
}

// TestMultimodalImageRail_GetCallbacks_注册BeforeModelCall 验证回调映射含 before_model_call。
func TestMultimodalImageRail_GetCallbacks_注册BeforeModelCall(t *testing.T) {
	rail := NewMultimodalImageRail(nil)
	callbacks := rail.GetCallbacks()
	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	if !ok || fn == nil {
		t.Fatal("before_model_call 回调未注册（嵌入虚方法陷阱防护）")
	}
}

// ──────────────────────────── 开关判定 ────────────────────────────

// TestMultimodalImageRail_显式开关覆盖 对齐 :33-38 构造参数优先。
func TestMultimodalImageRail_显式开关覆盖(t *testing.T) {
	enabled := true
	rail := NewMultimodalImageRail(&enabled)
	if !rail.readImageMultimodalEnabled() {
		t.Fatal("显式 true 应生效")
	}
	disabled := false
	rail2 := NewMultimodalImageRail(&disabled)
	if rail2.readImageMultimodalEnabled() {
		t.Fatal("显式 false 应生效")
	}
}

// ──────────────────────────── BeforeModelCall 双路径 ────────────────────────────

// TestMultimodalImageRail_带图安装mutator 对齐 :40-52 installed 分支与替换式重建。
// 安装目标是 context 持久投影链（user_prompt_builder.py:288-292），不再由 rail 自持。
func TestMultimodalImageRail_带图安装mutator(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	img := writeTempImage(t, "a.png")
	ctx := turnctx.WithTurnMultimodalImageFiles(context.Background(), []map[string]any{
		{"type": "image", "path": img.Path, "filename": img.Filename, "mime_type": img.MimeType},
	})
	mc := newMutatorStubModelContext()

	rail.BeforeModelCall(ctx, newCBWithModelContext(t, mc))
	if got := mc.WindowMutators(); len(got) != 1 {
		t.Fatalf("带图轮应安装 1 个 mutator，got %d", len(got))
	}

	// 第二轮换图：替换式重建（清旧标记项再挂新，Python ensure :288-292）
	img2 := writeTempImage(t, "b.png")
	ctx2 := turnctx.WithTurnMultimodalImageFiles(ctx, []map[string]any{
		{"type": "image", "path": img2.Path, "filename": img2.Filename, "mime_type": img2.MimeType},
	})
	rail.BeforeModelCall(ctx2, newCBWithModelContext(t, mc))
	if got := mc.WindowMutators(); len(got) != 1 {
		t.Fatalf("第二轮应保持替换式单 mutator，got %d", len(got))
	}
}

// TestMultimodalImageRail_无图不安装 对齐 :44-47 installed=false 分支（清标记残留）。
func TestMultimodalImageRail_无图不安装(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	mc := newMutatorStubModelContext()
	rail.BeforeModelCall(context.Background(), newCBWithModelContext(t, mc))
	if got := mc.WindowMutators(); len(got) != 0 {
		t.Fatalf("无图轮不应安装 mutator，got %d", len(got))
	}
}

// TestMultimodalImageRail_关闭路径剥离 对齐 :54-59 removed 分支。
func TestMultimodalImageRail_关闭路径剥离(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(false))
	mc := newMemModelContextForImage()
	rail.BeforeModelCall(context.Background(), newCBWithModelContext(t, mc))
	msgs, _ := mc.GetMessages(0, true)
	text := msgs[0].GetContent().Text()
	if text == "" || len(msgs[0].GetContent().Parts()) != 0 {
		t.Fatalf("图片块应被剥离: %q parts=%d", text, len(msgs[0].GetContent().Parts()))
	}
}

// ──────────────────────────── Uninit ────────────────────────────

// TestMultimodalImageRail_Uninit 与载体解耦 安装目标在 context 投影链，
// rail 反向清理只解绑 agent 引用，不触碰 context（Python 同构：mutators 属 context）。
func TestMultimodalImageRail_Uninit与载体解耦(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	img := writeTempImage(t, "c.png")
	mc := newMutatorStubModelContext()
	rail.BeforeModelCall(turnctx.WithTurnMultimodalImageFiles(context.Background(), []map[string]any{
		{"type": "image", "path": img.Path, "filename": img.Filename, "mime_type": img.MimeType},
	}), newCBWithModelContext(t, mc))
	rail.Uninit(nil)
	if len(mc.WindowMutators()) == 0 {
		t.Fatal("mutator 归 context 所有，Uninit 不应清除")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

func ptr(b bool) *bool { return &b }

func writeTempImage(t *testing.T, name string) prompt.MultimodalImageFile {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	return prompt.MultimodalImageFile{Type: "image", Filename: name, Path: p, MimeType: "image/png"}
}

// imageStubModelContext 只覆盖 GetMessages/SetMessages，其余嵌入接口零值。
type imageStubModelContext struct {
	ceiface.ModelContext
	messages []llmschema.BaseMessage
}

func (m *imageStubModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return m.messages, nil
}

func (m *imageStubModelContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	m.messages = messages
}

// mutatorStubModelContext 在消息 stub 之上实现持久投影链载体。
type mutatorStubModelContext struct {
	*imageStubModelContext
	mutators []ceiface.WindowMutator
}

func (m *mutatorStubModelContext) WindowMutators() []ceiface.WindowMutator { return m.mutators }

func (m *mutatorStubModelContext) SetWindowMutators(ms []ceiface.WindowMutator) { m.mutators = ms }

func newMutatorStubModelContext() *mutatorStubModelContext {
	return &mutatorStubModelContext{imageStubModelContext: newMemModelContextForImage()}
}

// newCBWithModelContext 构造携带 ModelContext 的回调上下文。
func newCBWithModelContext(_ *testing.T, mc ceiface.ModelContext) *agentinterfaces.AgentCallbackContext {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetModelContext(mc)
	return cbc
}

// newMemModelContextForImage 构造带一张混合图片块消息的 stub。
func newMemModelContextForImage() *imageStubModelContext {
	msg := llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "")
	msg.SetContent(llmschema.NewMultiModalContent(
		llmschema.ContentPart{Type: "text", Text: "before"},
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,x"}},
	))
	return &imageStubModelContext{messages: []llmschema.BaseMessage{msg}}
}

// TestMultimodalImageRail_InitUninit 补齐 Init/Uninit 覆盖（15.20 遗留）。
func TestMultimodalImageRail_InitUninit(t *testing.T) {
	rail := &MultimodalImageRail{}
	if err := rail.Init(context.Background(), nil); err != nil {
		t.Fatalf("Init 应无错: %v", err)
	}
	if err := rail.Uninit(nil); err != nil {
		t.Fatalf("Uninit 应无错: %v", err)
	}
}

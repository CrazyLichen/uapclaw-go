package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
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
func TestMultimodalImageRail_带图安装mutator(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	img := writeTempImage(t, "a.png")
	ctx := prompt.WithMultimodalImageFiles(context.Background(), []prompt.MultimodalImageFile{img})

	rail.BeforeModelCall(ctx, &agentinterfaces.AgentCallbackContext{})
	first := rail.ImageMutator()
	if first == nil {
		t.Fatal("带图轮应安装 mutator")
	}

	// 第二轮换图：闭包被替换（单槽覆盖 = Python 清旧再挂新）
	img2 := writeTempImage(t, "b.png")
	ctx2 := prompt.WithMultimodalImageFiles(ctx, []prompt.MultimodalImageFile{img2})
	rail.BeforeModelCall(ctx2, nil)
	if rail.ImageMutator() == nil {
		t.Fatal("第二轮应保持安装态")
	}
	_ = first
}

// TestMultimodalImageRail_无图不安装 对齐 :44-47 installed=false 分支。
func TestMultimodalImageRail_无图不安装(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	rail.BeforeModelCall(context.Background(), &agentinterfaces.AgentCallbackContext{})
	if rail.ImageMutator() != nil {
		t.Fatal("无图轮不应安装 mutator")
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

// TestMultimodalImageRail_Uninit清mutator 验证反向清理。
func TestMultimodalImageRail_Uninit清mutator(t *testing.T) {
	rail := NewMultimodalImageRail(ptr(true))
	img := writeTempImage(t, "c.png")
	rail.BeforeModelCall(prompt.WithMultimodalImageFiles(context.Background(), []prompt.MultimodalImageFile{img}), &agentinterfaces.AgentCallbackContext{})
	rail.Uninit(nil)
	if rail.ImageMutator() != nil {
		t.Fatal("Uninit 后 mutator 应清空")
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

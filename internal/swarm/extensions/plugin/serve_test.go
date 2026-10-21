package plugin

import (
	"context"
	"testing"
)

// fakeExtension 用于测试的扩展实现
type fakeExtension struct {
	decls []Declaration
}

func (f *fakeExtension) Register(ctx context.Context) ([]Declaration, error) {
	return f.decls, nil
}

func (f *fakeExtension) Shutdown(ctx context.Context) error { return nil }

func (f *fakeExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	if ctxData == nil {
		ctxData = map[string]any{}
	}
	ctxData["touched"] = true
	return ctxData, nil
}

// TestDeclaration_MetadataDefaults 测试 Declaration 构造零值
func TestDeclaration_MetadataDefaults(t *testing.T) {
	d := Declaration{}
	if d.ProvidesTransport {
		t.Error("ProvidesTransport 零值应为 false")
	}
	if len(d.EventSubs) != 0 {
		t.Errorf("EventSubs 零值应为空, 实际 %d", len(d.EventSubs))
	}
}

// TestEventSubscription_Fields 测试事件订阅结构
func TestEventSubscription_Fields(t *testing.T) {
	sub := EventSubscription{Event: "gateway:before_chat_request", Priority: 100}
	if sub.Event != "gateway:before_chat_request" || sub.Priority != 100 {
		t.Errorf("EventSubscription 字段不符: %+v", sub)
	}
}

// TestFakeExtension_接口满足 验证 fakeExtension 满足 ExtensionImpl 接口
func TestFakeExtension_接口满足(t *testing.T) {
	var _ ExtensionImpl = (*fakeExtension)(nil)
}

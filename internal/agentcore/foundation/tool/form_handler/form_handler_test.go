package form_handler

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"
)

// ──────────────────────────── DefaultFormHandler 测试 ────────────────────────────

// TestDefaultFormHandler_Handle 测试写入普通字符串值
func TestDefaultFormHandler_Handle(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	handler := DefaultFormHandler{}

	err := handler.Handle(context.Background(), writer, "name", "Alice")
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	writer.Close()

	if !strings.Contains(buf.String(), "name") {
		t.Error("输出应包含字段名 name")
	}
	if !strings.Contains(buf.String(), "Alice") {
		t.Error("输出应包含字段值 Alice")
	}
}

// TestDefaultFormHandler_Handle_Nil值跳过 测试 nil 值不写入字段
func TestDefaultFormHandler_Handle_Nil值跳过(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	handler := DefaultFormHandler{}

	err := handler.Handle(context.Background(), writer, "nil_field", nil)
	if err != nil {
		t.Fatalf("Handle nil 不应返回错误: %v", err)
	}
	writer.Close()

	// multipart.Writer 写了 boundary，但不应该有 nil_field 内容
	if strings.Contains(buf.String(), "nil_field") {
		t.Error("nil 值不应写入字段")
	}
}

// TestDefaultFormHandler_Handle_各种类型 测试 int/float/bool/slice 等类型
func TestDefaultFormHandler_Handle_各种类型(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		contains string
	}{
		{"整数", 42, "42"},
		{"浮点数", 3.14, "3.14"},
		{"布尔值", true, "true"},
		{"切片", []int{1, 2, 3}, "[1 2 3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			handler := DefaultFormHandler{}

			err := handler.Handle(context.Background(), writer, "field", tt.value)
			if err != nil {
				t.Fatalf("Handle 失败: %v", err)
			}
			writer.Close()

			if !strings.Contains(buf.String(), tt.contains) {
				t.Errorf("输出应包含 %q，实际: %s", tt.contains, buf.String())
			}
		})
	}
}

// ──────────────────────────── FormHandlerManager 测试 ────────────────────────────

// testHandler 测试用自定义处理器
type testHandler struct {
	called bool
}

func (h *testHandler) Handle(_ context.Context, writer *multipart.Writer, formName string, value any) error {
	h.called = true
	return writer.WriteField(formName, "custom:"+fmt.Sprintf("%v", value))
}

// TestFormHandlerManager_Register 测试注册自定义处理器
func TestFormHandlerManager_Register(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	handler := &testHandler{}
	mgr.Register("custom", handler)

	got := mgr.GetHandler("custom")
	if got != handler {
		t.Error("GetHandler 应返回注册的自定义处理器")
	}
}

// TestFormHandlerManager_Register_空类型忽略 测试空 handlerType 忽略
func TestFormHandlerManager_Register_空类型忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.Register("", &testHandler{})

	// 空 handlerType 不应注册
	if len(mgr.handlerMap) != 0 {
		t.Error("空 handlerType 不应注册到 handlerMap")
	}
}

// TestFormHandlerManager_Register_Nil处理器忽略 测试 nil handler 忽略
func TestFormHandlerManager_Register_Nil处理器忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.Register("nil_type", nil)

	got := mgr.GetHandler("nil_type")
	// 应返回默认处理器而非 nil
	if got == nil {
		t.Error("GetHandler 不应返回 nil")
	}
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("未注册类型应返回 DefaultFormHandler")
	}
}

// TestFormHandlerManager_RegisterDefaultHandler 测试注册默认处理器
func TestFormHandlerManager_RegisterDefaultHandler(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	custom := &testHandler{}
	mgr.RegisterDefaultHandler(custom)

	got := mgr.GetHandler("unknown_type")
	if got != custom {
		t.Error("注册默认处理器后，GetHandler 未注册类型应返回新默认处理器")
	}
}

// TestFormHandlerManager_RegisterDefaultHandler_Nil忽略 测试 nil 默认处理器忽略
func TestFormHandlerManager_RegisterDefaultHandler_Nil忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.RegisterDefaultHandler(nil)

	// 默认处理器不应被替换为 nil
	got := mgr.GetHandler("unknown_type")
	if got == nil {
		t.Error("默认处理器不应为 nil")
	}
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("nil 注册不应替换默认处理器")
	}
}

// TestFormHandlerManager_GetHandler_未注册返回默认 测试未注册类型返回默认处理器
func TestFormHandlerManager_GetHandler_未注册返回默认(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	got := mgr.GetHandler("nonexistent")
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("未注册类型应返回 DefaultFormHandler")
	}
}

// TestFormHandlerManager_GetHandler_Default类型 测试 "default" 类型返回 DefaultFormHandler
func TestFormHandlerManager_GetHandler_Default类型(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.handlerMap["default"] = DefaultFormHandler{}

	got := mgr.GetHandler("default")
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("\"default\" 类型应返回 DefaultFormHandler")
	}
}

// TestGetFormHandlerManager_单例 测试多次调用返回同一实例
func TestGetFormHandlerManager_单例(t *testing.T) {
	// 注意：由于 sync.Once 的特性，此测试验证全局单侧行为
	// 在同一进程内多次调用应返回同一实例
	mgr1 := GetFormHandlerManager()
	mgr2 := GetFormHandlerManager()
	if mgr1 != mgr2 {
		t.Error("GetFormHandlerManager 应返回同一实例")
	}
}

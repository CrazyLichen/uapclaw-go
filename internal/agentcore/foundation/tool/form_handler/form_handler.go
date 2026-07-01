package form_handler

import (
	"context"
	"fmt"
	"mime/multipart"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FormHandler 表单数据处理器接口。
//
// 实现此接口可自定义不同类型表单字段的处理方式（如文件上传、二进制数据等）。
// 默认实现为 DefaultFormHandler，将值转为字符串写入 multipart Writer。
//
// 对应 Python: openjiuwen/core/foundation/tool/form_handler/form_handler_manager.py (FormHandler)
type FormHandler interface {
	// Handle 处理表单数据，将 formName=value 写入 multipart Writer。
	//
	// 参数：
	//   - ctx: 上下文，预留取消和超时传播
	//   - writer: multipart Writer，由调用方创建和管理生命周期
	//   - formName: 表单字段名
	//   - value: 表单字段值
	Handle(ctx context.Context, writer *multipart.Writer, formName string, value any) error
}

// DefaultFormHandler 默认表单处理器，将值转为字符串写入 multipart Writer。
//
// 跳过 nil 值，将其余值通过 fmt.Sprintf 转为字符串后
// 调用 writer.WriteField 写入。
//
// 对应 Python: DefaultFormHandler
type DefaultFormHandler struct{}

// FormHandlerManager 表单处理器注册表，单例模式。
//
// 维护 handlerType → FormHandler 的映射，支持注册自定义处理器和默认处理器。
// 获取处理器时，若指定类型未注册则返回默认处理器。
//
// 对应 Python: FormHandlerManager（Singleton 元类）
type FormHandlerManager struct {
	mu             sync.RWMutex
	handlerMap     map[string]FormHandler
	defaultHandler FormHandler
}

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	handlerManagerOnce sync.Once
	handlerManagerInst *FormHandlerManager
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetFormHandlerManager 获取全局 FormHandlerManager 单例。
//
// 首次调用时初始化注册表，注册 DefaultFormHandler 作为默认处理器，
// 同时将 "default" 类型也映射到 DefaultFormHandler。
func GetFormHandlerManager() *FormHandlerManager {
	handlerManagerOnce.Do(func() {
		handlerManagerInst = &FormHandlerManager{
			handlerMap:     make(map[string]FormHandler),
			defaultHandler: DefaultFormHandler{},
		}
		handlerManagerInst.handlerMap["default"] = DefaultFormHandler{}
	})
	return handlerManagerInst
}

// Register 注册表单处理器。
//
// handlerType 为处理器类型标识（对应 schema 中的 form_handler_type 字段），
// handler 为处理器实例。若 handlerType 无效或 handler 为 nil，记录错误日志并忽略。
//
// 对应 Python: FormHandlerManager.register()
func (m *FormHandlerManager) Register(handlerType string, handler FormHandler) {
	if handlerType == "" {
		logger.Error(logger.ComponentAgentCore).
			Str("handler_type", handlerType).
			Msg("注册处理器失败，handler_type 无效")
		return
	}
	if handler == nil {
		logger.Error(logger.ComponentAgentCore).
			Str("handler_type", handlerType).
			Msg("注册处理器失败，handler 为 nil")
		return
	}
	m.mu.Lock()
	m.handlerMap[handlerType] = handler
	m.mu.Unlock()
	logger.Info(logger.ComponentAgentCore).
		Str("handler_type", handlerType).
		Str("handler", fmt.Sprintf("%T", handler)).
		Msg("注册处理器成功")
}

// RegisterDefaultHandler 注册默认表单处理器。
//
// 若 handler 为 nil，记录错误日志并忽略。
//
// 对应 Python: FormHandlerManager.register_default_handler()
func (m *FormHandlerManager) RegisterDefaultHandler(handler FormHandler) {
	if handler == nil {
		logger.Error(logger.ComponentAgentCore).
			Msg("注册默认处理器失败，handler 为 nil")
		return
	}
	m.mu.Lock()
	m.defaultHandler = handler
	m.mu.Unlock()
	logger.Info(logger.ComponentAgentCore).
		Str("handler", fmt.Sprintf("%T", handler)).
		Msg("注册默认处理器成功")
}

// GetHandler 获取表单处理器。
//
// 若 handlerType 未注册，返回默认处理器。
//
// 对应 Python: FormHandlerManager.get_handler()
func (m *FormHandlerManager) GetHandler(handlerType string) FormHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.handlerMap[handlerType]; ok {
		return h
	}
	return m.defaultHandler
}

// Handle 默认表单处理器实现，将值转为字符串写入 multipart Writer。
//
// 跳过 nil 值，将其余值通过 fmt.Sprintf 转为字符串后写入。
//
// 对应 Python: DefaultFormHandler.handle()
func (DefaultFormHandler) Handle(_ context.Context, writer *multipart.Writer, formName string, value any) error {
	if value == nil {
		return nil
	}
	return writer.WriteField(formName, fmt.Sprintf("%v", value))
}

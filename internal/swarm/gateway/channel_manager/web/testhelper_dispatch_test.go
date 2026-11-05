package web

import (
	"fmt"
)

// newTestChannel 构造一个不装配任何依赖的测试通道。
//
// handler 需要 channel 才能回帧（对齐上游 handler 闭包捕获 channel），
// 因此测试里所有 handler 构造都传入本函数的返回值。
func newTestChannel() *WebChannel {
	return NewWebChannel(WebChannelConfig{}, nil, nil)
}

// wsCallError 帧级失败的错误载体（只用于测试断言）。
type wsCallError struct {
	// msg 帧内 error 文本
	msg string
	// code 帧内 code
	code string
}

// Error 实现 error 接口。
func (e *wsCallError) Error() string { return e.msg }

// Code 返回帧级错误码。
func (e *wsCallError) Code() string { return e.code }

// callHandler 直接执行一个 handler，把帧级结果折算成 (payload, error)。
//
// handler 现在自行写帧（对齐上游 send_response），测试用 frameCollector 接住；
// 失败帧转成 *wsCallError 以携带 code。
func callHandler(handler methodHandler, params map[string]any, sessionID string) (map[string]any, error) {
	collector := &frameCollector{}
	handler(collector, "test-req", params, sessionID)
	if len(collector.frames) == 0 {
		return map[string]any{}, fmt.Errorf("handler 未写任何帧")
	}
	frame := collector.frames[len(collector.frames)-1]
	payload, _ := frame["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	if ok, _ := frame["ok"].(bool); ok {
		return payload, nil
	}
	errMsg, _ := frame["error"].(string)
	code, _ := frame["code"].(string)
	return payload, &wsCallError{msg: errMsg, code: code}
}

// errorCode 取出帧级错误码；非本包错误返回空串。
func errorCode(err error) string {
	if ce, ok := err.(*wsCallError); ok {
		return ce.Code()
	}
	return ""
}

// dispatch 测试助手：调用已注册方法，取出响应的 payload 与错误。
//
// 生产路径由 WebChannel.handleWebSocket 查找 handler 并写回连接；
// 测试里需要一个"调用即拿结果"的入口，故在此提供等价封装。
func dispatch(wc *WebChannel, method string, params map[string]any, sessionID string) (map[string]any, error) {
	wc.methodsMu.RLock()
	handler, ok := wc.methodHandlers[method]
	wc.methodsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("方法 %q 未找到", method)
	}
	return callHandler(handler, params, sessionID)
}

package web

import (
	"fmt"
)

// dispatch 测试助手：调用已注册方法，取出响应的 payload 与错误。
//
// 生产路径由 WebChannel.handleWebSocket 用 frameCollector 收集帧后写回连接；
// 测试里需要一个"调用即拿结果"的入口，故在此提供等价封装。
func dispatch(wc *WebChannel, method string, params map[string]any, sessionID string) (map[string]any, error) {
	collector := &frameCollector{}
	if !wc.invokeMethod(collector, method, "test-req", params, sessionID) {
		return nil, fmt.Errorf("方法 %q 未找到", method)
	}
	if len(collector.frames) == 0 {
		return nil, fmt.Errorf("方法 %q 未写任何帧", method)
	}
	frame := collector.frames[len(collector.frames)-1]
	payload, _ := frame["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	if ok, _ := frame["ok"].(bool); !ok {
		errMsg, _ := frame["error"].(string)
		return payload, fmt.Errorf("%s", errMsg)
	}
	return payload, nil
}

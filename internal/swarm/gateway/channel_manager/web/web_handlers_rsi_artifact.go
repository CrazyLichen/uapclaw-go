package web

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// rsi.artifact 三代理：把 web 通道的 rsi.artifact.* 一元请求经 E2A 原样转发给
// AgentServer（server 侧 handler 已在领域十七批次就绪，rsi_handlers.go）。
// Python: app_web_handlers.py:6492-6494（三个都是 _HarnessReq 薄代理转发）。
// Python 参考注册：channel.register_method("rsi.artifact.download", ...) 等。
func registerRSIArtifactProxies(wc *WebChannel, ac *routing.AgentClient) {
	wc.registerMethod("rsi.artifact.download", sessionProxyForward(wc, ac, "rsi.artifact.download"), false)
	wc.registerMethod("rsi.artifact.files.list", sessionProxyForward(wc, ac, "rsi.artifact.files.list"), false)
	wc.registerMethod("rsi.artifact.files.get", sessionProxyForward(wc, ac, "rsi.artifact.files.get"), false)
}

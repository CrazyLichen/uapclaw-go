// Package intellirouter IntelliRouter 模型客户端 —— 包装 23.4a 移植的
// PyPI intelli-router（ReliableRouter），提供 API 池化、智能路由、自动重试、
// 流式与多模态生成（图片/语音/视频，DashScope 直调）。
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/model_clients/intelli_router_model_client.py
// （1415 行单文件；Go 按 Python 行段拆分如下，每文件头标注来源行段）。
//
// 已知偏差（总设计 2026-11-12-domain23.4 D-3/D-4/D-5）：
//   - D-3：observability metrics/web_dashboard/logging_hook 不移植（无消费方）
//   - D-4：asyncio loop 绑定防御不移植（net/http.Client 并发安全）
//   - D-5：_RoutedModelRouterMixin 动态类继承 → 构造期函数指针注入
//
// 文件目录：
//
//	intellirouter/
//	├── doc.go           # 包文档
//	├── client.go        # 主客户端：Invoke/Stream/转换/init 注册（py:434-441, 641-825, 907-1067, 1069-1093）
//	├── client_config.go # IntelliRouterClientConfig 与 FromModelClientConfig（py:223-281, 497-560 组装）
//	├── router_cache.go  # 路由缓存/D-5 钩子注入/通告日志（py:282-302, 460-495, 497-611, 661-700, 157-197）
//	├── routed_model.go  # 路由模型占位符回填与适配器包装（py:56-128）
//	├── event_bridge.go  # _CoreRoutingEventBridge 事件桥（py:338-432）
//	└── dashscope_gen.go# 多模态生成三件套（py:1095-1415）
//
// 核心类型/接口索引：
//   - IntelliRouterModelClient：实现 model_clients.BaseModelClient
//   - IntelliRouterClientConfig / FromModelClientConfig：配置提取
//   - CoreRoutingEventBridge：observability.EventHandler 实现
//   - RoutedModelLabel / HasRoutedModelPlaceholder / FillRoutedModel：占位符契约
package intellirouter

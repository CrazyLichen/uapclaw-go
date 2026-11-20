// Package intellirouter 是 PyPI 包 intelli-router 0.1.0b1 的 Go 移植（内部包，非上游仓库源码）。
//
// 上游为 Python 外部可选依赖，本包按裁剪清单移植其路由器核心，供
// model_clients/intellirouter（23.4b）封装为 BaseModelClient。升级基线：
// pip download intelli-router==0.1.0b1 解包后的 wheel 源码，按 diff 追。
//
// 依赖方向：本包只依赖自身子包（core/strategy/provider/health/observability），
// 是纯叶包；23.4b 的 model_clients/intellirouter 与 routing/errors 是唯一
// 预期消费方（State/Strategy 为导出契约）。
//
// 已知偏差（spec docs/superpowers/specs/2026-11-12-domain23.4-model-group-routing-design.md §9）：
//
//	D-1 strategy 只移植 ordered_failover/tag_filtered/simple_shuffle
//	D-2 provider 不移植 bedrock/gemini（无 Go 客户端且不在路由映射表）；
//	    deepseek/siliconflow/inference-affinity 按 wheel registry.py:40-42
//	    各自独立移植（比原计划的 openai 基座合并更忠实）
//	D-3 observability metrics/web_dashboard/logging_hook、cache 不移植
//	D-4 asyncio loop 绑定防御不移植（Go net/http.Client 并发安全）
//	语言差异（永久）：utils/exceptions.py 的 _rebuild_exception 深拷贝/
//	pickle 机制无对应物，不移植；reliable_router.py 内联的 dict 取值
//	表达式收敛为 reliable_router.go 尾部的包级助手（表达式级工具，
//	非步骤拆分）。
//
// 文件目录：
//
//	intellirouter/
//	├── doc.go                              # 包文档
//	├── types.go                            # ← types.py 消息/用量/多模态响应类型
//	├── errors.go                           # ← utils/exceptions.py 异常层级与状态码映射
//	├── media.go                            # ← utils/media.py data URI 解析（anthropic adapter 消费）
//	├── core/
//	│   ├── deployment.go                  # ← core/deployment.py Deployment + 状态枚举 + 校验
//	│   ├── context.go                     # ← core/context.py RoutingContext
//	│   └── state.go                       # ← core/state.py 冷却状态机（退避公式 py:216-220）
//	├── strategy/
//	│   ├── base_strategy.go               # ← strategy/base_strategy.py RoutingStrategy 接口
//	│   ├── ordered_failover.go            # ← strategy/ordered_failover.py
//	│   ├── simple_shuffle.go              # ← strategy/simple_shuffle.py
//	│   └── tag_filtered.go                # ← strategy/tag_filtered.py
//	├── provider/
//	│   ├── base_provider.go               # ← provider/base_provider.py adapter 协议
//	│   ├── openai_provider.go             # ← provider/openai_provider.py（兼 dashscope/zhipu 注册名）
//	│   ├── anthropic_provider.go          # ← provider/anthropic_provider.py
//	│   ├── deepseek_provider.go           # ← provider/deepseek_provider.py
//	│   ├── siliconflow_provider.go        # ← provider/siliconflow_provider.py
//	│   ├── inference_affinity_provider.go # ← provider/inference_affinity_provider.py
//	│   └── registry.go                    # ← provider/registry.py
//	├── health/
//	│   └── checker.go                     # ← health/checker.py
//	├── observability/
//	│   ├── events.go                      # ← observability/events.py 事件类型与 RoutingEvent
//	│   ├── bus.go                         # ← observability/bus.py EventBus
//	│   └── handler.go                     # ← observability/handler.py EventHandler 协议
//	└── router/
//	    ├── base_router.go                 # ← router/base_router.py
//	    └── reliable_router.go             # ← router/reliable_router.py（含内联助手）
//
// 对应 Python 代码：PyPI intelli-router 0.1.0b1（wheel 源码）
package intellirouter

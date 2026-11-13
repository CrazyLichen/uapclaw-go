// Package intellirouter 是 PyPI 包 intelli-router 0.1.0b1 的 Go 移植（内部包，非上游仓库源码）。
//
// 上游为 Python 外部可选依赖，本包按裁剪清单移植其路由器核心，供
// model_clients/intellirouter（23.4b）封装为 BaseModelClient。升级基线：
// pip download intelli-router==0.1.0b1 解包后的 wheel 源码，按 diff 追。
//
// 已知偏差（spec docs/superpowers/specs/2026-11-12-domain23.4-model-group-routing-design.md §9）：
//
//	D-1 strategy 只移植 ordered_failover/tag_filtered/simple_shuffle
//	D-2 provider 只移植 openai 兼容基座 + anthropic（bedrock/gemini 不移植）
//	D-3 observability metrics/web_dashboard/logging_hook、cache 不移植
//	D-4 asyncio loop 绑定防御不移植（Go net/http.Client 并发安全）
//	语言差异（永久）：utils/exceptions.py 的 _rebuild_exception 深拷贝/
//	pickle 机制无对应物，不移植。
//
// 对应 Python 代码：PyPI intelli-router 0.1.0b1（wheel 源码）
package intellirouter

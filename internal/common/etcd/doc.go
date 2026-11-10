// Package etcd 提供最小的 etcd v3 gRPC-gateway JSON 客户端。
//
// 与上游一致，走 etcd 的 **HTTP/JSON 网关**（`/v3/kv/*`、`/v3/watch`）而非 gRPC：
// 请求与响应体均为 JSON，key/value 以 base64 传输。HTTP 层使用标准库
// net/http（等价于上游的 httpx，不引入第三方客户端）。
//
// 共用基础设施：cron 存储与 AgentOS 配置下发都用它。
//
// 文件目录：
//
//	etcd/
//	├── doc.go       # 包文档
//	├── client.go    # EtcdJsonClient（Range/Put/Delete/CAS/WatchPrefix）+ 多端点轮询
//	└── kv.go        # EtcdKv / EtcdRangeResult / base64 / PrefixRangeEnd / 响应解析
//
// 对应 Python 代码：jiuwenswarm/common/etcd/client.py（305 行）
package etcd

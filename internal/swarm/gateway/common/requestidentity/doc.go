// Package requestidentity 提供传输层 Web 请求身份助手（路由身份的写入与读取收口）。
//
// 分层约定（对齐 Python gateway/common/request_identity.py 模块文档）：
//
//  1. Wire（浏览器 → Gateway，非权威）
//     - WS query：user_id / group_id / bot_id / gateway_id
//     - HTTP 头：X-User-Id / X-Group-Id / X-Bot-Id / X-Gateway-Id
//
//  2. 进站后权威落点
//     - metadata["user_id"]（顶层）—— 谁在说话
//     - metadata["routing"] = {group_id, bot_id, gateway_id} —— 路由维
//
//     Gateway 入口用 NormalizeRoutingIdentity + ApplyRoutingMetadata 写一次；
//     下游用 WebRoutingIdentity 一次读全（含顶层 user_id）。
//
//  3. 不要：把身份写入业务 params；不要把 group_id/bot_id/gateway_id 摊到
//     metadata 顶层；不要把 user_id 放进 routing。
//
// 已知偏差：Python 真相源 openjiuwen_protocol.identity（wheel 依赖）本地不可读，
// 本实现按 request_identity.py 模块文档的语义实现（归一化=去空白+丢弃空值）；
// 回收条件=获取该 wheel 源码后逐函数对拍。
//
// 文件目录：
//
//	requestidentity/
//	├── doc.go           # 包文档
//	└── requestidentity.go  # 归一化/写入/读取/合并四函数
//
// 对应 Python 代码：gateway/common/request_identity.py（re-export openjiuwen_protocol.identity）
package requestidentity

// RoutingMetadataKey metadata 中路由维键名。
// Python: openjiuwen_protocol.identity ROUTING_METADATA_KEY。
const RoutingMetadataKey = "routing"

// UserIDField 顶层用户身份字段名。
// Python: openjiuwen_protocol.identity USER_ID_FIELD。
const UserIDField = "user_id"

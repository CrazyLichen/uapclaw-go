// Package dmessage_queue 提供分布式消息队列的消息类型与 wire 序列化器。
// 本包承载分布式 Runner 跨进程通信的消息载体：请求消息（INPUT/STOP）与
// 响应消息（OUTPUT），以及把消息编解码为 Pulsar 可传输字节的序列化器。
// 序列化格式与 Python 侧一一对应，依赖 __class__ 标签做类型还原，
// 是 Go Runner 与 Python 客户端互通的前提。
// 文件目录：
//
//	dmessage_queue/
//	├── doc.go                 # 包文档
//	├── message.go             # DMessageType（分布式消息类型）/ResultType（结果类型） 枚举 + DmqRequestMessage（请求消息）/DmqResponseMessage（响应消息）
//	└── message_serializer.go  # SerializeMessage（序列化）/DeserializeMessage（反序列化）/MessageToMap（消息转 map）/serializePayload（payload 递归）
//
// 对应 Python 代码：
//
//	openjiuwen/core/runner/drunner/dmessage_queue/message.py              （DMessageType/ResultType/DmqRequestMessage/DmqResponseMessage）
//	openjiuwen/core/runner/drunner/dmessage_queue/message_serializer.py   （serialize_message/deserialize_message）
package dmessage_queue

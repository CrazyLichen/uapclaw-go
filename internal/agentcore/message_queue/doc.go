// Package message_queue 提供 Pulsar 消息队列后端实现。
// 本包是分布式 Runner 的传输管道：按 topic 订阅、生产消息、producer LRU 缓存、
// 消费循环与生命周期管理。自身不含业务语义，消息字段由上层适配器解读。
// 通过包内窄接口（pulsarClientAPI/pulsarConsumerAPI/pulsarProducerAPI）
// 隔离 apache/pulsar-client-go，使 LRU、生命周期、消费循环、错误包装可用
// fake 覆盖，真实连接测试走 //go:build integration。
// 文件目录：
//
//	message_queue/
//	├── doc.go        # 包文档
//	├── pulsar_api.go # pulsarClientAPI（客户端窄接口）/pulsarConsumerAPI（消费者窄接口）/pulsarProducerAPI（生产者窄接口）/pulsarMessageAPI（消息窄接口） 接口 + sdkClient/sdkConsumer/sdkProducer/sdkMessage 适配器
//	└── pulsar.go     # MessageQueuePulsar（Pulsar消息队列）/PulsarSubscription（Pulsar订阅）
//
// 对应 Python 代码：openjiuwen/extensions/message_queue/message_queue_pulsar.py
package message_queue

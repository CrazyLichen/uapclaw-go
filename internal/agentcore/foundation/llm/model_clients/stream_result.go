package model_clients

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamResult 流式调用结果，包含流式数据 channel 和最终合并消息。
//
// 对应 Python: AsyncIterator[AssistantMessageChunk] + final_message 追踪
//
// 使用方式：
//
//	result, _ := client.Stream(ctx, messages, opts...)
//	for chunk := range result.Chunks {
//	    // 处理每个流式消息块
//	}
//	// 流结束后获取最终合并的完整消息
//	final := result.Final()
type StreamResult struct {
	// Chunks 流式消息块 channel，关闭表示流结束。
	Chunks <-chan *llmschema.AssistantMessageChunk
	// finalMessage 流结束后设置的最终合并消息
	finalMessage *llmschema.AssistantMessageChunk
	// done 流完成信号 channel
	done chan struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamResult 创建 StreamResult。
//
// chunks 为流式消息块的推送 channel（只读）。
// 内部启动 goroutine 消费 Chunks 并合并，Final() 返回合并结果。
func NewStreamResult(chunks <-chan *llmschema.AssistantMessageChunk) *StreamResult {
	r := &StreamResult{
		Chunks: chunks,
		done:   make(chan struct{}),
	}

	// 后台 goroutine：消费所有 chunk，逐步合并，流结束后设置 finalMessage
	go func() {
		defer close(r.done)

		var accumulated *llmschema.AssistantMessageChunk
		for chunk := range chunks {
			if accumulated == nil {
				accumulated = chunk
			} else {
				accumulated = accumulated.Merge(chunk)
			}
		}
		r.finalMessage = accumulated
	}()

	return r
}

// Final 等待流结束并返回最终合并的消息。
//
// 阻塞直到所有 chunk 已消费完毕，返回通过 Merge 合并的完整 AssistantMessageChunk。
// 与 Python OpenAIModelClient.stream() 中 final_message 追踪对齐。
//
// 多次调用安全：第二次及之后直接返回缓存的结果。
func (r *StreamResult) Final() *llmschema.AssistantMessageChunk {
	<-r.done
	return r.finalMessage
}

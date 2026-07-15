package callback

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// CallbackChain 顺序回调执行链，支持回滚、重试和错误处理。
//
// 对应 Python: openjiuwen/core/runner/callback/chain.py (CallbackChain)
type CallbackChain struct {
	// Name 链标识
	Name string
	// callbacks 回调列表（按优先级降序排列）
	callbacks []*CallbackInfo[ChainCallbackFunc]
	// rollbackHandlers 回滚处理器（以 CallbackInfo 指针为键）
	rollbackHandlers map[*CallbackInfo[ChainCallbackFunc]]ChainRollbackHandler
	// errorHandlers 错误处理器（以 CallbackInfo 指针为键）
	errorHandlers map[*CallbackInfo[ChainCallbackFunc]]ChainErrorHandler
	// mu 并发读写锁
	mu sync.RWMutex
}

// ChainCallbackFunc 链回调函数类型。
//
// 对应 Python: Callable (chain 中使用的 async callback)
type ChainCallbackFunc func(ctx context.Context, cctx *ChainContext) (any, error)

// ChainRollbackHandler 回滚处理器类型。
//
// 对应 Python: Callable (rollback handler)
type ChainRollbackHandler func(ctx context.Context, cctx *ChainContext) error

// ChainErrorHandler 错误处理器类型。
//
// 对应 Python: Callable (error handler)，返回 ChainAction 决定后续动作
type ChainErrorHandler func(ctx context.Context, cctx *ChainContext, err error) (ChainAction, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCallbackChain 创建回调执行链。
//
// 对应 Python: CallbackChain.__init__(name)
func NewCallbackChain(name string) *CallbackChain {
	return &CallbackChain{
		Name:             name,
		callbacks:        make([]*CallbackInfo[ChainCallbackFunc], 0),
		rollbackHandlers: make(map[*CallbackInfo[ChainCallbackFunc]]ChainRollbackHandler),
		errorHandlers:    make(map[*CallbackInfo[ChainCallbackFunc]]ChainErrorHandler),
	}
}

// Add 添加回调到链中，维护优先级排序。
//
// 对应 Python: CallbackChain.add(callback_info, rollback_handler, error_handler)
func (c *CallbackChain) Add(
	info *CallbackInfo[ChainCallbackFunc],
	rollbackHandler ChainRollbackHandler,
	errorHandler ChainErrorHandler,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.callbacks = append(c.callbacks, info)
	sortCallbacks(c.callbacks)

	if rollbackHandler != nil {
		c.rollbackHandlers[info] = rollbackHandler
	}
	if errorHandler != nil {
		c.errorHandlers[info] = errorHandler
	}
}

// Remove 移除回调及关联的 handler。
//
// 对应 Python: CallbackChain.remove(callback)
// 参数 info 为要移除的 CallbackInfo 指针。
func (c *CallbackChain) Remove(info *CallbackInfo[ChainCallbackFunc]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	filtered := make([]*CallbackInfo[ChainCallbackFunc], 0, len(c.callbacks))
	for _, ci := range c.callbacks {
		if ci != info {
			filtered = append(filtered, ci)
		}
	}
	c.callbacks = filtered

	delete(c.rollbackHandlers, info)
	delete(c.errorHandlers, info)
}

// Execute 核心执行方法：按优先级顺序执行回调链。
//
// 对应 Python: CallbackChain.execute(context)
//
// 执行流程：
//  1. 按 callbacks 列表顺序执行（已按优先级排好）
//  2. 上一个回调的结果追加到 ChainContext.Results，作为下一个回调的输入
//  3. 支持 ChainAction 控制（Continue/Break/Retry/Rollback）
//  4. 超时控制：如果 CallbackInfo.Timeout > 0，使用 context.WithTimeout
//  5. 重试逻辑：失败时按 MaxRetries + RetryDelay 重试
//  6. 错误处理：如果回调配了 errorHandler，调用 errorHandler 决定后续动作
//  7. 最终失败时触发回滚
func (c *CallbackChain) Execute(ctx context.Context, cctx *ChainContext) *ChainResult {
	c.mu.RLock()
	callbacks := make([]*CallbackInfo[ChainCallbackFunc], len(c.callbacks))
	copy(callbacks, c.callbacks)
	c.mu.RUnlock()

	executedInfos := make([]*CallbackInfo[ChainCallbackFunc], 0)

	for i, info := range callbacks {
		if !info.Enabled {
			continue
		}

		cctx.CurrentIndex = i
		callback := info.Callback

		// 重试循环
		for attempt := 0; attempt <= info.MaxRetries; attempt++ {
			// 超时控制
			execCtx := ctx
			var cancel context.CancelFunc
			if info.Timeout > 0 {
				execCtx, cancel = context.WithTimeout(ctx, time.Duration(info.Timeout*float64(time.Second)))
			}

			result, err := callback(execCtx, cctx)

			if cancel != nil {
				cancel()
			}

			// 超时错误处理
			if err != nil {
				if execCtx.Err() == context.DeadlineExceeded {
					logger.Error(logger.ComponentAgentCore).
						Str("callback_name", "callback").
						Msg("回调执行超时")

					if attempt < info.MaxRetries {
						time.Sleep(time.Duration(info.RetryDelay * float64(time.Second)))
						continue
					}
					c.rollback(execCtx, cctx, executedInfos)
					return &ChainResult{
						Action:  ChainActionRollback,
						Context: cctx,
						Error:   err,
					}
				}

				// 尝试错误处理器
				c.mu.RLock()
				errorHandler, hasHandler := c.errorHandlers[info]
				c.mu.RUnlock()

				if hasHandler {
					action, handlerErr := errorHandler(execCtx, cctx, err)
					if handlerErr == nil {
						switch action {
						case ChainActionContinue:
							cctx.Results = append(cctx.Results, result)
							executedInfos = append(executedInfos, info)
						case ChainActionBreak:
							cctx.Results = append(cctx.Results, result)
							return &ChainResult{
								Action:  ChainActionBreak,
								Result:  result,
								Context: cctx,
							}
						case ChainActionRetry:
							if attempt < info.MaxRetries {
								time.Sleep(time.Duration(info.RetryDelay * float64(time.Second)))
								continue
							}
						case ChainActionRollback:
							c.rollback(execCtx, cctx, executedInfos)
							return &ChainResult{
								Action:  ChainActionRollback,
								Context: cctx,
								Error:   err,
							}
						}
						break
					}
					logger.Error(logger.ComponentAgentCore).
						Err(handlerErr).
						Msg("错误处理器执行失败")
				}

				// 重试
				if attempt < info.MaxRetries {
					logger.Info(logger.ComponentAgentCore).
						Str("callback_name", "callback").
						Int("attempt", attempt+1).
						Msg("重试回调")
					time.Sleep(time.Duration(info.RetryDelay * float64(time.Second)))
					continue
				}

				// 最终失败，触发回滚
				c.rollback(execCtx, cctx, executedInfos)
				return &ChainResult{
					Action:  ChainActionRollback,
					Context: cctx,
					Error:   err,
				}
			}

			// 成功执行，处理结果
			if chainResult, ok := result.(*ChainResult); ok {
				switch chainResult.Action {
				case ChainActionBreak:
					cctx.Results = append(cctx.Results, chainResult.Result)
					return &ChainResult{
						Action:  ChainActionBreak,
						Result:  chainResult.Result,
						Context: cctx,
					}
				case ChainActionRetry:
					if attempt < info.MaxRetries {
						time.Sleep(time.Duration(info.RetryDelay * float64(time.Second)))
						continue
					}
					// 重试次数耗尽，继续视为成功
					cctx.Results = append(cctx.Results, chainResult.Result)
				case ChainActionRollback:
					c.rollback(execCtx, cctx, executedInfos)
					return &ChainResult{
						Action:  ChainActionRollback,
						Context: cctx,
						Error:   chainResult.Error,
					}
				default:
					cctx.Results = append(cctx.Results, chainResult.Result)
				}
			} else {
				cctx.Results = append(cctx.Results, result)
			}

			executedInfos = append(executedInfos, info)

			// 只执行一次的回调
			if info.Once {
				info.Enabled = false
			}

			break // 成功，退出重试循环
		}
	}

	cctx.IsCompleted = true
	return &ChainResult{
		Action:  ChainActionContinue,
		Result:  cctx.GetLastResult(),
		Context: cctx,
	}
}

// Rollback 逆序执行已执行回调的 rollbackHandlers。
//
// 对应 Python: CallbackChain._rollback(executed_callbacks, context)
func (c *CallbackChain) Rollback(ctx context.Context, cctx *ChainContext, executedInfos []*CallbackInfo[ChainCallbackFunc]) {
	c.rollback(ctx, cctx, executedInfos)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rollback 逆序执行已执行回调的回滚处理器。
//
// 对应 Python: CallbackChain._rollback(executed_callbacks, context)
func (c *CallbackChain) rollback(ctx context.Context, cctx *ChainContext, executedInfos []*CallbackInfo[ChainCallbackFunc]) {
	cctx.IsRolledBack = true

	c.mu.RLock()
	defer c.mu.RUnlock()

	for i := len(executedInfos) - 1; i >= 0; i-- {
		info := executedInfos[i]
		if handler, ok := c.rollbackHandlers[info]; ok {
			if err := handler(ctx, cctx); err != nil {
				logger.Error(logger.ComponentAgentCore).
					Err(err).
					Msg("回滚处理器执行失败")
			}
		}
	}
}

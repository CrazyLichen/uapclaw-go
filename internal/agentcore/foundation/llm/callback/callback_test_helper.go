package callback

// GetCallbacksForTest 返回指定事件的回调列表，仅供测试使用。
func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []CallbackFunc {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.callbacks[event]
}

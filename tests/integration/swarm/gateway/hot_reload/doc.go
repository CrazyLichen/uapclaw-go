//go:build integration

// Package hot_reload_test 提供「界面保存配置 → 热重载 → 主循环/压缩链路跟随」的集成测试。
//
// 覆盖链路：
//
//	web.Dispatcher(config.save_all) → ApplyConfigPayload 写 config.yaml
//	→ NotifyConfigSavedOnce → onConfigSaved 回调（等价 gateway 的 agent.reload_config）
//	→ DeepAdapter.ReloadAgentConfig → createModel 重建模型缓存
//	→ DeepAgent.ConfigureDeepConfig → hotReconfigure → hotReloadModel
//	→ ensureInitialized 重建 ContextProcessorRail → RoundLevelCompressor 重建
//	→ 下一轮 ProcessMessageStreamImpl 实际下发的模型
//
// 断言手段：模型名由工厂注入的录制客户端记录，主循环走 Stream、压缩走 Invoke，
// 因此「哪条链路用了哪个模型」可以从调用记录直接读出，而不是靠读私有字段推测。
//
// 文件目录：
//
//	hot_reload/
//	├── doc.go            # 包文档
//	└── hot_reload_test.go # HotReloadSuite 界面切换模型与 token 上限后的跟随性
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/web/app_web_handlers.py
// （_config_save_all / _notify_config_saved_once）与
// jiuwenswarm/server/runtime/agent_adapter/interface_deep.py（reload_agent_config）。
package hot_reload_test

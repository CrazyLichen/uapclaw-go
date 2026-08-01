// 本文件对应 Python jiuwenswarm/extensions/callback_compat.py。
//
// Python 的 callback_compat.py 是为兼容 openjiuwen <0.1.9 的
// AsyncCallbackFramework.unregister_sync API 缺口而创建的。
//
// Go 项目中已有 callback.CallbackFramework 提供完整的
// OnCustom/TriggerCustom/OffCustom 方法，对应 Python 的
// register_sync/trigger/unregister_sync API，无需额外兼容层。
//
// 此文件标注 ⤴️ 10.5.9 已覆盖，不需要 Go 实现。
package extensions

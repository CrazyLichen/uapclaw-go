package adapter

// CodeAdapter 设备卸载：code 通道无 External Harness 绑定面，no-op。
// （接口收敛要求 AgentAdapter 全实现者补齐；语义对齐 Python——
// code 适配器无 _external_harness_binding 属性，unload 为属性探测 no-op。）
func (c *CodeAdapter) UnloadEquipmentIfLoaded(kind, packageID string) error {
	return nil
}

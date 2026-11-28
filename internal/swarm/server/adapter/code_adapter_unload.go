package adapter

// UnloadEquipmentIfLoaded（CodeAdapter）：Python JiuwenSwarmCodeAdapter 继承
// JiuWenSwarmDeepAdapter（interface_code.py:535），unload_equipment_if_loaded
// 定义在基类（interface_deep.py:3051）且未重写——code 会话与 deep 会话共享
// 同一卸载流程。Go 侧 CodeAdapter 组合内嵌 deep，委托即继承语义。
func (c *CodeAdapter) UnloadEquipmentIfLoaded(kind, packageID string) error {
	return c.deep.UnloadEquipmentIfLoaded(kind, packageID)
}

package pregel

import (
	"encoding/json"
	"errors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingNode 未完成节点的持久化记录。
// 物理归属偏差 D6：Python 定义于 store/base.py:26-29，task.py 以函数内延迟导入
// 破环（task.py:23/37）；Go 无延迟导入，本类型定义迁入 pregel 包、store 直接引用
// （不留别名，2026-11-19 用户拍板）。本类型自身零 pregel 依赖，迁移安全。
type PendingNode struct {
	// NodeName 节点名
	NodeName string `json:"node_name"`
	// Status 任务状态（取值对齐 TaskStatusInterrupt/TaskStatusError 字面量）
	Status string `json:"status"`
	// Exception 累积的异常列表（Python 默认 None，Go nil 等价；JSON 往返经 string 适配，偏差 D-8'）
	Exception []error `json:"exception,omitempty"`
}

// pendingNodeJSON PendingNode 的 JSON 形态（Exception 经 string 适配，偏差 D-8'）
type pendingNodeJSON struct {
	NodeName  string   `json:"node_name"`
	Status    string   `json:"status"`
	Exception []string `json:"exception"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MarshalJSON Exception []error → []string（Go error 接口 JSON 序列化静默丢内容，D-8）
func (p PendingNode) MarshalJSON() ([]byte, error) {
	aux := pendingNodeJSON{NodeName: p.NodeName, Status: p.Status}
	for _, e := range p.Exception {
		if e != nil {
			aux.Exception = append(aux.Exception, e.Error())
		}
	}
	return json.Marshal(aux)
}

// UnmarshalJSON []string → errors.New 重建；null → nil（对齐 Python exception 默认 None）
func (p *PendingNode) UnmarshalJSON(data []byte) error {
	var aux pendingNodeJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.NodeName, p.Status = aux.NodeName, aux.Status
	if aux.Exception == nil {
		p.Exception = nil
		return nil
	}
	p.Exception = make([]error, len(aux.Exception))
	for i, s := range aux.Exception {
		p.Exception[i] = errors.New(s)
	}
	return nil
}

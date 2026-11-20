package core

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// RoutingContext 路由上下文 - 请求级别。← core/context.py:16 RoutingContext
type RoutingContext struct {
	// Model 请求模型
	Model string
	// Messages 消息列表
	Messages []map[string]any
	// Kwargs 其他参数
	Kwargs map[string]any

	// SelectedDeployment 选中的部署 ← py:29
	SelectedDeployment *Deployment
	// AttemptedDeployments 尝试过的部署 ID ← py:30
	AttemptedDeployments []string

	// StartTime 开始时间（秒，对齐 time.time()）← py:33
	StartTime float64
	// EndTime 结束时间 ← py:34
	EndTime *float64
	// Response 结果 ← py:37
	Response any
	// Error 错误 ← py:38
	Error error
	// RequestTags 请求标签 ← py:40
	RequestTags []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRoutingContext 构造路由上下文。← py:16-40（start_time = time.time()）
func NewRoutingContext(model string, messages []map[string]any, kwargs map[string]any) *RoutingContext {
	if kwargs == nil {
		// ← py:29 default_factory=dict
		kwargs = map[string]any{}
	}
	return &RoutingContext{
		Model:    model,
		Messages: messages,
		Kwargs:   kwargs,
		// ← py:33 default_factory=time.time（now 注入便于测试，等价实现）
		StartTime: float64(time.Now().UnixMilli()) / 1000,
	}
}

// MarkAttempt 标记尝试过的部署。← context.py:50 mark_attempt
func (c *RoutingContext) MarkAttempt(deployment *Deployment) {
	c.AttemptedDeployments = append(c.AttemptedDeployments, deployment.ID)
}

// SetSuccess 标记成功。← context.py:54 set_success
func (c *RoutingContext) SetSuccess(deployment *Deployment, response any) {
	c.SelectedDeployment = deployment
	c.Response = response
	c.EndTime = ptrTime()
}

// SetFailure 标记失败。← context.py:60 set_failure
func (c *RoutingContext) SetFailure(err error) {
	c.Error = err
	c.EndTime = ptrTime()
}

// Latency 获取请求延迟。← context.py:64 latency property
func (c *RoutingContext) Latency() float64 {
	if c.EndTime == nil {
		return 0.0
	}
	return *c.EndTime - c.StartTime
}

// ToLogDict 转换为日志字典。← context.py:70 to_log_dict
func (c *RoutingContext) ToLogDict() map[string]any {
	var selectedID any
	if c.SelectedDeployment != nil {
		selectedID = c.SelectedDeployment.ID
	}
	return map[string]any{
		"model":               c.Model,
		"selected_deployment": selectedID,
		"latency":             c.Latency(),
		"attempted":           c.AttemptedDeployments,
		"success":             c.Error == nil,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ptrTime 当前时间（秒，对齐 time.time() 精度）。
func ptrTime() *float64 {
	t := float64(time.Now().UnixMilli()) / 1000
	return &t
}

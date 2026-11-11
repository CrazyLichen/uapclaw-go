package adapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// processCliRunOptions 根 adapter 暂存、child 消费的一次性执行选项。
// Python: interface_deep.py:12436 self._process_cli_run_options = RunContext(...)
type processCliRunOptions struct {
	// maxTurns 本次运行的迭代上限
	maxTurns *int
	// hostTools 宿主工具（⤵️ 20.6）
	hostTools []any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigureProcessCliRun 把一次性的 Process CLI 限制与宿主工具只应用到本 Agent。
// Python: interface_deep.py:12421-12470 configure_process_cli_run
//
// Runtime 在启动 rails 与 Agent 白名单就位之后、首次模型调用之前调用它。
// 不改动任何持久化 Agent 定义或其他渠道配置。
func (d *DeepAdapter) ConfigureProcessCliRun(ctx context.Context, maxTurns *int, hostTools []any) error {
	return d.configureProcessCliRun(ctx, maxTurns, hostTools)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// configureProcessCliRun ConfigureProcessCliRun 的实现（内部调用）。
func (d *DeepAdapter) configureProcessCliRun(ctx context.Context, maxTurns *int, hostTools []any) error {
	// 步骤 1: 渠道守卫
	// Python: :12430 if self._channel_id != "process_cli": raise ValueError
	if d.channelID != "process_cli" {
		return errors.New("process CLI run options require a Process CLI Agent")
	}
	// 步骤 2: 根是 router → 暂存选项，等 child 消费
	// Python: :12432-12437
	if !d.isSessionScoped {
		d.processCliRunOptions = &processCliRunOptions{maxTurns: maxTurns, hostTools: hostTools}
		return nil
	}
	// 步骤 3: child 直接应用
	// Python: :12438-12439 if self._instance is None: raise
	if d.instanceSnapshot() == nil {
		return errors.New("process CLI session Agent is not ready")
	}
	// 步骤 4: max_turns → 改 react config + 注册 rail
	// Python: :12440-12453
	if maxTurns != nil {
		if err := d.applyMaxTurns(ctx, *maxTurns); err != nil {
			return err
		}
	}
	// 步骤 5: host_tools → add_ability（重名报错）
	// Python: :12454-12460
	// ⤵️ 回填 20.6（客户端工具暂停-回填）
	if len(hostTools) > 0 {
		return errors.New("host tools are not implemented yet (see 20.6)")
	}
	return nil
}

// applyMaxTurns 把 react 配置的 max_iterations 限死为 limit+1 并注册迭代上限 rail。
// Python: interface_deep.py:12444-12453
func (d *DeepAdapter) applyMaxTurns(ctx context.Context, maxTurns int) error {
	instance := d.instanceSnapshot()
	if instance == nil {
		return errors.New("process CLI session Agent is not ready")
	}
	// Python: :12443-12445 react = self._instance.react_agent; if react is None: raise
	react := instance.ReactAgent()
	if react == nil {
		return errors.New("Agent model loop is unavailable")
	}
	// Python: :12446 config = react.config.model_copy()
	// Q5 核实：Go 的 ReActAgent.Config() 返回内部指针（react_prompt.go:120 `return a.config`），
	// 且 ReActAgentConfig 无 Clone 方法，故此处手写拷贝。
	// pydantic 的 model_copy() 默认 shallow，故 Go 用 `*src` 一层拷贝语义等价。
	cfg, ok := react.Config().(*saconfig.ReActAgentConfig)
	if !ok {
		return fmt.Errorf("unexpected react config type %T", react.Config())
	}
	copied := *cfg

	// Python: :12447-12448
	//   existing = config.max_iterations
	//   iteration_limit = min(existing, max_turns) if existing else max_turns
	existing := copied.MaxIterations
	limit := maxTurns
	if existing > 0 && existing < maxTurns {
		limit = existing
	}
	// Python: :12449-12451 多一轮给「恢复的最终工具」报告上限用，那一轮由 rail 阻止调模型
	copied.MaxIterations = limit + 1
	// Python: :12452 react.configure(config)
	if err := react.Configure(ctx, &copied); err != nil {
		return err
	}
	// Python: :12453 await self._instance.register_rail(ProcessCliIterationLimitRail(iteration_limit))
	// Go 的 AddRail 是同步排队，rails 在下一轮装配时注册生效
	instance.AddRail(harnessrails.NewProcessCliIterationLimitRail(limit))
	return nil
}

// takeProcessCliRunOptions 取出一次性的 run 选项并立即清空。
// Python: interface_deep.py:18989-18992（取用前先置 None，失败运行不泄漏回调）
func (d *DeepAdapter) takeProcessCliRunOptions() *processCliRunOptions {
	opts := d.processCliRunOptions
	d.processCliRunOptions = nil
	return opts
}

// instanceSnapshot 线程安全地取当前 DeepAgent 实例。
// d.instance 由 CreateInstance 写入，必须先取 rootInstanceMu
// （同 deep_adapter_sessions.go:787 的先例，race detector 已实证）。
func (d *DeepAdapter) instanceSnapshot() *harness.DeepAgent {
	d.rootInstanceMu.Lock()
	defer d.rootInstanceMu.Unlock()
	return d.instance
}

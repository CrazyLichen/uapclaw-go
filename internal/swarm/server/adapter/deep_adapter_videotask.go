package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/videotask"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// bindVoiceTaskExecution 受管语音任务绑定（进入 invoke 前调用，返回 unbind 供 defer）。
// 对齐 interface_deep.py:18128-18131 / :18918-18921 的 `async with bind_task_execution(...)`：
// 仅 channel_id==video_tool 的请求生效；rail 只装一次；绑定失败报错中止本轮。
func (d *DeepAdapter) bindVoiceTaskExecution(ctx context.Context, req *schema.AgentRequest,
	inputs map[string]any,
) (func(), error) {
	if req == nil || req.ChannelID != "video_tool" {
		return nil, nil
	}
	params := map[string]any{}
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}
	sessionID := ""
	if req.SessionID != nil {
		sessionID = *req.SessionID
	}
	userID := ""
	if req.UserID != nil {
		userID = *req.UserID
	}
	// 非受管证据直接短路（对齐 execution.py:49-53 的 managed 判定）
	if _, hasBinding := params["managed_task_binding"]; !hasBinding && !strings.HasPrefix(sessionID, "managed-task-") {
		return nil, nil
	}
	// rail 只装一次（对齐 :68-71 install_voice_task_rail）
	if _, err := videotask.InstallTaskRail(
		d.instance,
		func(rail *videotask.VoiceAgentTaskRail) error {
			return d.instance.RegisterRail(ctx, rail)
		},
		func(rail *videotask.VoiceAgentTaskRail) error {
			return d.instance.UnregisterRail(ctx, rail)
		},
		false,
	); err != nil {
		return nil, fmt.Errorf("语音任务 rail 注册失败: %w", err)
	}
	binding, unbind, err := videotask.BindTaskExecution(params, req.RequestID, sessionID, userID,
		&voiceTaskHarness{d: d}, inputs)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, nil
	}
	return unbind, nil
}

// voiceTaskHarness 受管执行的句柄源（偏差 O-14 装配侧实现）。
// 当前 Go 侧无调度器级 task 对象可精确抓取，保守返回无活跃轮次——
// 结算走 settlement_unobserved 语义（不误报、重启标记 unknown），与 Python
// 「绝不从终态推断」的守恒方向一致。精确句柄抓取随调度器改造另行回填。
type voiceTaskHarness struct {
	d *DeepAdapter
}

// CurrentExecutionDone 当前轮次完成信号（暂无精确来源 → nil）。
func (h *voiceTaskHarness) CurrentExecutionDone() <-chan struct{} { return nil }

// HasActiveRound 活跃轮次判定（保守 false）。
func (h *voiceTaskHarness) HasActiveRound() bool { return false }

// 编译期校验：videotask rail 满足 harness rail 接口。
var _ agentinterfaces.AgentRail = (*videotask.VoiceAgentTaskRail)(nil)

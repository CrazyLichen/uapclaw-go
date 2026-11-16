// send_file 执行授权消费接线（D-x3 注入缝）。
// Python send_file_to_user.py 经 lazy import 调用 generated_artifact_delivery；
// Go tools→permissions 编译期成环（permissions 导入 tools），由本文件在包初始化期
// 反向注入消费函数，依赖方向 permissions→tools 合法。

package permissions

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// init 注入 send_file 授权消费与路径/目标归一函数。
// Python: send_file_to_user.py L659-680 lazy import（normalize_send_file_paths /
// normalize_send_file_target_channels / consume_send_file_execution_grant）。
func init() {
	tools.SetSendFileGrantConsumer(func(ctx context.Context, requestedPaths, targetChannels []string) ([]string, error) {
		// Python: consume_send_file_execution_grant(requested_paths=, target_channels=)
		items, err := ConsumeSendFileExecutionGrant(ctx, requestedPaths, targetChannels)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.ResolvedPath)
		}
		return out, nil
	})
}

// Go 版 lazy import 注入（D-15.16-e20）：tools 包消费的 6+2 个纯函数翻译
// 符号在此回填（Python 由函数内 lazy import 运行期解析，见
// tools/external_seam.go 的依赖方向说明）。

package external

import "github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"

func init() {
	tools.SetExternalSeams(tools.ExternalSeams{
		CLIAgentForProvider:              CLIAgentForProvider,
		ExternalCliAgentSpecFromTemplate: ExternalCliAgentSpecFromTemplate,
		MergeExternalCliConfig:           MergeExternalCliConfig,
		ModelApiProtocol:                 ModelApiProtocol,
		ProviderFilterForCLI:             ProviderFilterForCLI,
		TakesAuthFallback:                TakesAuthFallback,
		IsKnownBackend:                   IsKnownBackend,
		AvailableBackends:                AvailableBackends,
	})
}

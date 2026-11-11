package main

import (
	"os"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli"
)

// Execute 构建并执行 process-cli，返回进程退出码。
//
// 单独拆出便于测试：main() 含 os.Exit 无法在测试中调用（规则 4）。
func Execute() int {
	return processcli.RunMain(os.Args[1:])
}

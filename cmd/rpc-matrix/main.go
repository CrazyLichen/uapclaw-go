package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// main 扫描三方 RPC 注册点，输出矩阵与下游缺口清单，或在 -verify 模式下判定门禁。
func main() {
	var (
		repo        = flag.String("repo", ".", "本仓根目录")
		upstream    = flag.String("upstream", "/home/opensource/jiuwenswarm-develop", "上游 Python 仓库根目录")
		out         = flag.String("out", "docs/sync/web-rpc-matrix.md", "矩阵输出路径")
		gapsOut     = flag.String("gaps-out", "docs/sync/web-rpc-downstream-gaps.md", "下游缺口清单输出路径")
		verify      = flag.Bool("verify", false, "门禁模式：本层承诺范围（local/agent:ok）存在 ❌ 即失败")
		verifyAll   = flag.Bool("verify-all", false, "严格门禁：任何 ❌ 都失败（下游缺口清零后启用）")
		frontDir    = flag.String("frontend", "internal/swarm/channels/web/frontend", "前端源码目录（相对 repo）")
		goDir       = flag.String("go", "internal/swarm", "本仓扫描目录（相对 repo）")
		upstreamDir = flag.String("upstream-dir", "gateway/channel_manager/web", "上游扫描子目录（相对 upstream，只扫 Web 通道）")
	)
	flag.Parse()

	frontendSites, err := scanFrontend(filepath.Join(*repo, *frontDir))
	if err != nil {
		fail("扫描前端", err)
	}
	localSites, err := scanGo(filepath.Join(*repo, *goDir))
	if err != nil {
		fail("扫描本仓", err)
	}

	upstreamRoot := filepath.Join(*upstream, *upstreamDir)
	var upstreamSites []rpcSite
	var src *upstreamSource
	if _, statErr := os.Stat(upstreamRoot); statErr != nil {
		if *verify || *verifyAll {
			fail("上游目录不可用，无法校验矩阵", statErr)
		}
		fmt.Fprintf(os.Stderr, "警告：上游目录不可用，矩阵仅含前端与本仓两侧: %v\n", statErr)
	} else {
		src, err = loadUpstreamSource(upstreamRoot)
		if err != nil {
			fail("索引上游函数体", err)
		}
		upstreamSites, err = scanUpstream(upstreamRoot, detectRegistrars(src))
		if err != nil {
			fail("扫描上游", err)
		}
	}

	caps, err := loadGoCapabilities(*repo)
	if err != nil {
		fail("读取本仓下游能力", err)
	}

	rows := buildMatrix(groupByMethod(frontendSites), groupByMethod(localSites), groupByMethod(upstreamSites))
	if src != nil {
		configProxy, cfgErr := scanConfigProxyMethods(upstreamRoot)
		if cfgErr != nil {
			fail("扫描 config 代理注册", cfgErr)
		}
		attachDownstream(rows, collectDownstream(upstreamHandlers(upstreamSites), configProxy, src, caps))
		httpSites, httpErr := scanUpstreamHTTP(upstreamRoot)
		if httpErr != nil {
			fail("扫描上游 HTTP 路由", httpErr)
		}
		annotateHTTP(rows, groupByMethod(httpSites))
	}

	if err := writeOutputFile(*out, render(rows)); err != nil {
		fail("写矩阵", err)
	}
	if err := writeOutputFile(*gapsOut, renderGaps(rows)); err != nil {
		fail("写缺口清单", err)
	}

	miss := missing(rows)
	counts := countByDownstream(rows)
	fmt.Printf("矩阵：%s（共 %d 个方法，缺 %d 个）\n", *out, len(rows), len(miss))
	fmt.Printf("缺口下游分布：local %d / agent:ok %d / agent:const-only %d / agent:missing %d / dep %d\n",
		counts[downLocal], counts[downAgentOK], counts[downAgentConst], counts[downAgentMissing], counts[downDep])
	fmt.Printf("缺口清单：%s\n", *gapsOut)

	pending := miss
	if !*verifyAll {
		pending = inScope(miss)
	}
	if (*verify || *verifyAll) && len(pending) > 0 {
		fmt.Fprintf(os.Stderr, "存在 %d 个本仓缺失且属于本期承诺范围的方法：\n", len(pending))
		for _, r := range pending {
			fmt.Fprintf(os.Stderr, "  ❌ %s（下游 %s）\n", r.Method, r.Down.Status)
		}
		os.Exit(1)
	}
}

// inScope 过滤出本期承诺范围的缺口（网关本地可做，或下游已就绪）。
//
// agent:const-only / agent:missing / dep 属下游缺口，登记在
// docs/sync/web-rpc-downstream-gaps.md，不计入本层门禁。
func inScope(rows []matrixRow) []matrixRow {
	var out []matrixRow
	for _, r := range rows {
		if r.Down.Status == downLocal || r.Down.Status == downAgentOK {
			out = append(out, r)
		}
	}
	return out
}

// writeFile 写文件并按需建目录。
func writeOutputFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// fail 打印错误并退出。
func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s失败: %v\n", action, err)
	os.Exit(2)
}

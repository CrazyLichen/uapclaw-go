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
		verify      = flag.Bool("verify", false, "门禁模式：存在 ❌ 或缺上游目录即失败")
		frontDir    = flag.String("frontend", "internal/swarm/channels/web/frontend", "前端源码目录（相对 repo）")
		goDir       = flag.String("go", "internal/swarm", "本仓扫描目录（相对 repo）")
		upstreamDir = flag.String("upstream-dir", "gateway", "上游扫描子目录（相对 upstream）")
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
		if *verify {
			fail("上游目录不可用，无法校验矩阵", statErr)
		}
		fmt.Fprintf(os.Stderr, "警告：上游目录不可用，矩阵仅含前端与本仓两侧: %v\n", statErr)
	} else {
		upstreamSites, err = scanUpstream(upstreamRoot)
		if err != nil {
			fail("扫描上游", err)
		}
		src, err = loadUpstreamSource(upstreamRoot)
		if err != nil {
			fail("索引上游函数体", err)
		}
	}

	caps, err := loadGoCapabilities(*repo)
	if err != nil {
		fail("读取本仓下游能力", err)
	}

	rows := buildMatrix(groupByMethod(frontendSites), groupByMethod(localSites), groupByMethod(upstreamSites))
	if src != nil {
		attachDownstream(rows, collectDownstream(upstreamHandlers(upstreamSites), src, caps))
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

	if *verify && len(miss) > 0 {
		fmt.Fprintf(os.Stderr, "存在 %d 个上游已注册但本仓缺失的方法：\n", len(miss))
		for _, r := range miss {
			fmt.Fprintf(os.Stderr, "  ❌ %s（下游 %s）\n", r.Method, r.Down.Status)
		}
		os.Exit(1)
	}
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

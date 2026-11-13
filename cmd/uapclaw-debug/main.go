//go:build !test

package main

import "os"

// main 调试 CLI 入口：直接调 harness/cli 的命令树与 REPL。
func main() {
	os.Exit(Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

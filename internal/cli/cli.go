// Package cli 实现 clowder 命令行入口与子命令分发。
package cli

import (
	"fmt"
)

const usage = `clowder — LLM 聚合代理

用法:
  clowder start                    启动代理（后台）
  clowder stop                     停止
  clowder status                   状态一览
  clowder register <provider> [N]  手动注册 N 个 key（默认 1）
  clowder keys [provider]          列出 key
  clowder providers                列出 provider 及状态
  clowder reload                   热加载配置 + 重新扫描 provider
  clowder serve                    前台运行（start 的实际工作进程）
`

// Run 分发子命令。
func Run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "start":
		return cmdStart(rest)
	case "stop":
		return cmdStop(rest)
	case "status":
		return cmdStatus(rest)
	case "register":
		return cmdRegister(rest)
	case "keys":
		return cmdKeys(rest)
	case "providers":
		return cmdProviders(rest)
	case "reload":
		return cmdReload(rest)
	case "serve":
		return cmdServe(rest)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: clowder help)", cmd)
	}
}

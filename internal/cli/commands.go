package cli

import "fmt"

// 各子命令的实现随里程碑逐步填充；未完成的先返回明确错误。

var errNotImplemented = fmt.Errorf("not implemented yet")

func cmdStart(args []string) error     { return errNotImplemented }
func cmdStop(args []string) error      { return errNotImplemented }
func cmdStatus(args []string) error    { return errNotImplemented }
func cmdRegister(args []string) error  { return errNotImplemented }
func cmdKeys(args []string) error      { return errNotImplemented }
func cmdProviders(args []string) error { return errNotImplemented }
func cmdReload(args []string) error    { return errNotImplemented }
func cmdServe(args []string) error     { return errNotImplemented }

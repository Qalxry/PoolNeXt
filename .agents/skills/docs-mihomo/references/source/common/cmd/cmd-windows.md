# Source Evidence: common/cmd/cmd_windows.go

> Source file: `common/cmd/cmd_windows.go`
> Reason: implementation candidate

```go
//go:build windows

package cmd

import (
	"os/exec"
	"syscall"
)

func prepareBackgroundCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
```

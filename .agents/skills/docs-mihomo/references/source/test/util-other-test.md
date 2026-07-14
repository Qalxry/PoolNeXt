# Source Evidence: test/util_other_test.go

> Source file: `test/util_other_test.go`
> Reason: test code

```go
//go:build !darwin

package main

import (
	"errors"
	"net/netip"
)

func defaultRouteIP() (netip.Addr, error) {
	return netip.Addr{}, errors.New("not supported")
}
```

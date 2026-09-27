# vendored copy of purego v0.11.0
[![Go Reference](https://pkg.go.dev/badge/github.com/malivvan/appkit/pure?GOOS=darwin.svg)](https://pkg.go.dev/github.com/malivvan/appkit/pure?GOOS=darwin)

A library for calling C functions from Go without Cgo.

> This is beta software so expect bugs and potentially API breaking changes
> but each release will be tagged to avoid breaking people's code.
> Bug reports are encouraged.

## Motivation

The [Ebitengine](https://github.com/hajimehoshi/ebiten) game engine was ported to use only Go on Windows. This enabled
cross-compiling to Windows from any other operating system simply by setting `GOOS=windows`. The pure project was
born to bring that same vision to the other platforms supported by Ebitengine.

## Benefits

- **Simple Cross-Compilation**: No C means you can build for other platforms easily without a C compiler.
- **Faster Compilation**: Efficiently cache your entirely Go builds.
- **Smaller Binaries**: Using Cgo generates a C wrapper function for each C function called. Purego doesn't!
- **Dynamic Linking**: Load symbols at runtime and use it as a plugin system.
- **Foreign Function Interface**: Call into other languages that are compiled into shared objects.
- **Cgo Fallback**: Works even with CGO_ENABLED=1 so incremental porting is possible. 
This also means unsupported GOARCHs (freebsd/riscv64, linux/mips, etc.) will still work
except for float arguments and return values.

## Supported Platforms

### Tier 1

Tier 1 platforms are the primary targets officially supported by PureGo. When a new version of PureGo is released, any critical bugs found on Tier 1 platforms are treated as release blockers. The release will be postponed until such issues are resolved.

- **Linux**: amd64, arm64
- **macOS**: amd64, arm64
- **Windows**: amd64<sup>1</sup>, arm64<sup>1</sup>

### Tier 2

Tier 2 platforms are supported by PureGo on a best-effort basis. Critical bugs on Tier 2 platforms do not block new PureGo releases. However, fixes contributed by external contributors are very welcome and encouraged.

- **FreeBSD**: amd64<sup>2</sup>, arm64<sup>2</sup>
- **Linux**: 386<sup>2</sup>, arm<sup>2</sup>, loong64<sup>1</sup>, ppc64le<sup>1</sup>, riscv64<sup>2</sup>, s390x<sup>2,3</sup>
- **NetBSD**: amd64<sup>2</sup>, arm64<sup>2</sup>
- **Windows**: 386<sup>2,4</sup>, arm<sup>2,4,5</sup>

**Android and iOS are not supported** by this vendored copy: their code paths,
build tags and the iOS `CGO_ENABLED=0` guard were removed, so `GOOS=android`
and `GOOS=ios` no longer build.

#### Support Notes

1. These architectures support passing structs by value as arguments and return values when calling C functions, but not in callbacks created with `NewCallback`
2. These architectures do not support passing structs by value as arguments or return values
3. These architectures require CGO_ENABLED=1 to compile in versions before Go 1.27, but will be supported without Cgo in Go 1.27 and later
4. These architectures only support `SyscallN` and `NewCallback`
5. These architectures are no longer supported as of Go 1.26

## Example

The example below only showcases pure use for macOS and Linux. The other platforms require special handling which can
be seen in the complete example at [examples/libc](https://github.com/malivvan/appkit/pure/tree/main/examples/libc) which supports FreeBSD and Windows.

```go
package main

import (
	"fmt"
	"runtime"

	"github.com/malivvan/appkit/pure"
)

func getSystemLibrary() string {
	switch runtime.GOOS {
	case "darwin":
		return "/usr/lib/libSystem.B.dylib"
	case "linux":
		return "libc.so.6"
	default:
		panic(fmt.Errorf("GOOS=%s is not supported", runtime.GOOS))
	}
}

func main() {
	libc, err := pure.Dlopen(getSystemLibrary(), pure.RTLD_NOW|pure.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	var puts func(string)
	pure.RegisterLibFunc(&puts, libc, "puts")
	puts("Calling C from Go without Cgo!")
}
```

Then to run: `CGO_ENABLED=0 go run main.go`

## Questions

If you have questions about how to incorporate pure in your project or want to discuss
how it works join the [Discord](https://discord.gg/HzGZVD6BkY)!

### External Code

Purego uses code that originates from the Go runtime. These files are under the BSD-3
License that can be found [in the Go Source](https://github.com/golang/go/blob/master/LICENSE).
This is a list of the copied files:

* `abi_*.h` from package `runtime/cgo`
* `wincallback.go` from package `runtime`
* `internal/fakecgo/abi_*.h` from package `runtime/cgo`
* `internal/fakecgo/asm_GOARCH.s` from package `runtime/cgo`
* `internal/fakecgo/callbacks.go` from package `runtime/cgo`
* `internal/fakecgo/iscgo.go` from package `runtime/cgo`
* `internal/fakecgo/setenv.go` from package `runtime/cgo`
* `internal/fakecgo/freebsd.go` from package `runtime/cgo`
* `internal/fakecgo/netbsd.go` from package `runtime/cgo`
* `internal/fakecgo/linux.go` from package `runtime/cgo`

The `internal/fakecgo/go_GOOS.go` files were modified from `runtime/cgo/gcc_GOOS_GOARCH.go`.
The `internal/fakecgo/linux.go` file is a combination of `runtime/cgo/linux.go` and `runtime/cgo/linux_syscall.c`.

The files `abi_*.h` and `internal/fakecgo/abi_*.h` are the same because Bazel does not support cross-package use of
`#include` so we need each one once per package. (cf. [issue](https://github.com/bazelbuild/rules_go/issues/3636))

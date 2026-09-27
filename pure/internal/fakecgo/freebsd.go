// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build freebsd && !cgo

package fakecgo

import _ "unsafe" // for go:linkname

// Supply environ and __progname: FreeBSD's libc.so references them, and they
// are normally defined by crt1.o, which we do not link.
//
// Upstream purego additionally forces these two symbols into the dynamic
// symbol table with //go:cgo_export_dynamic. The compiler accepts that
// directive only in cgo-generated code (or with -std), and requiring a
// -gcflags override to build FreeBSD contradicted this vendored copy's "no
// extra flags" rule, so the directive is dropped here: the symbols are still
// declared, but they are no longer dynamically exported. FreeBSD therefore
// stays a compile-only target - a binary that dlopens libc may fail to
// resolve libc's references at runtime (see the package README).

//go:linkname _environ environ
//go:linkname _progname __progname

var _environ uintptr
var _progname uintptr

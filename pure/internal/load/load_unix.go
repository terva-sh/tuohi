// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2024 The Ebitengine Authors

//go:build darwin || freebsd || linux || netbsd

package load

import "github.com/malivvan/appkit/pure"

func OpenLibrary(name string) (uintptr, error) {
	return pure.Dlopen(name, pure.RTLD_NOW|pure.RTLD_GLOBAL)
}

func CloseLibrary(handle uintptr) error {
	return pure.Dlclose(handle)
}

func OpenSymbol(lib uintptr, name string) (uintptr, error) {
	return pure.Dlsym(lib, name)
}

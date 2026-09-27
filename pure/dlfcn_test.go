// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2023 The Ebitengine Authors

//go:build darwin || freebsd || linux || netbsd

package pure_test

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/malivvan/appkit/pure"
)

func TestSimpleDlsym(t *testing.T) {
	if _, err := pure.Dlsym(pure.RTLD_DEFAULT, "dlsym"); err != nil {
		t.Errorf("Dlsym with RTLD_DEFAULT failed: %v", err)
	}
}

func TestNestedDlopenCall(t *testing.T) {
	libFileName := filepath.Join(t.TempDir(), "libdlnested.so")
	t.Logf("Build %v", libFileName)

	if err := buildSharedLib(t, "CXX", libFileName, filepath.Join("testdata", "libdlnested", "nested_test.cpp")); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(libFileName)

	lib, err := pure.Dlopen(libFileName, pure.RTLD_NOW|pure.RTLD_GLOBAL)
	if err != nil {
		t.Fatalf("Dlopen(%q) failed: %v", libFileName, err)
	}

	pure.Dlclose(lib)
}

func TestSyscallN(t *testing.T) {
	var dlsym uintptr
	var err error
	if dlsym, err = pure.Dlsym(pure.RTLD_DEFAULT, "dlsym"); err != nil {
		t.Errorf("Dlsym with RTLD_DEFAULT failed: %v", err)
	}
	r1, _, err2 := pure.SyscallN(dlsym, pure.RTLD_DEFAULT, uintptr(unsafe.Pointer(&[]byte("dlsym\x00")[0])))
	if dlsym != r1 {
		t.Fatalf("SyscallN didn't return the same result as pure.Dlsym: %d", err2)
	}
}

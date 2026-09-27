// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

//go:build !cgo && !faketime

package pure

// if there is no Cgo we must link to each of the functions from dlfcn.h
// then the functions are called inside dlfcn_stubs.s

//go:cgo_import_dynamic pure_dlopen dlopen "libdl.so.2"
//go:cgo_import_dynamic pure_dlsym dlsym "libdl.so.2"
//go:cgo_import_dynamic pure_dlerror dlerror "libdl.so.2"
//go:cgo_import_dynamic pure_dlclose dlclose "libdl.so.2"

// on amd64 we don't need the following line - on 386 we do...
// anyway - with those lines the output is better (but doesn't matter) - without it on amd64 we get multiple DT_NEEDED with "libc.so.6" etc

//go:cgo_import_dynamic _ _ "libdl.so.2"

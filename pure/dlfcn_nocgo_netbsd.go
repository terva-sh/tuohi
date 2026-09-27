// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Ebitengine Authors

//go:build !cgo

package pure

//go:cgo_import_dynamic pure_dlopen dlopen "libc.so"
//go:cgo_import_dynamic pure_dlsym dlsym "libc.so"
//go:cgo_import_dynamic pure_dlerror dlerror "libc.so"
//go:cgo_import_dynamic pure_dlclose dlclose "libc.so"

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

//go:build !cgo

package pure

//go:cgo_import_dynamic pure_dlopen dlopen "libc.so.7"
//go:cgo_import_dynamic pure_dlsym dlsym "libc.so.7"
//go:cgo_import_dynamic pure_dlerror dlerror "libc.so.7"
//go:cgo_import_dynamic pure_dlclose dlclose "libc.so.7"

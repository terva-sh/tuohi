// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2022 The Ebitengine Authors

//go:build go1.27 && !cgo

#include "textflag.h"

TEXT _cgo_pure_setegid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setegid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_seteuid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_seteuid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setgid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setgid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setregid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setregid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setresgid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setresgid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setresuid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setresuid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setreuid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setreuid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setuid_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setuid_call(SB), R1
	MOVD (R1), R1
	BR   R1

TEXT _cgo_pure_setgroups_trampoline(SB), NOSPLIT|NOFRAME, $0
	MOVD ·x_cgo_pure_setgroups_call(SB), R1
	MOVD (R1), R1
	BR   R1

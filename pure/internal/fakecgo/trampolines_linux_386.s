// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

//go:build !cgo && linux

#include "textflag.h"

TEXT _cgo_pure_setegid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setegid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_seteuid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_seteuid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setgid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setgid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setregid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setregid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setresgid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setresgid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setresuid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setresuid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setreuid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setreuid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setuid_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setuid_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

TEXT _cgo_pure_setgroups_trampoline(SB), NOSPLIT, $4-0
	MOVL 8(SP), AX
	MOVL AX, 0(SP)
	MOVL ·x_cgo_pure_setgroups_call(SB), CX
	MOVL (CX), CX
	CALL CX
	RET

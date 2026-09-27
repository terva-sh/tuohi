//go:build windows && 386

package appkit

import (
	"unsafe"

	"github.com/malivvan/appkit/pure"
)

// putBounds (386): the 32-bit Windows (MS x86 __stdcall) convention passes a
// plain, naturally-aligned 16-byte aggregate such as RECT BY VALUE on the
// stack as four 4-byte words (unlike x64/amd64, which passes it by hidden
// reference, and AAPCS64, which packs it into registers). Purego pushes each
// uintptr argument as one 4-byte stack slot, so passing the four RECT fields
// as separate words reproduces the by-value RECT exactly. (Only over-aligned
// aggregates are marshalled indirectly on x86; a RECT of four LONGs is not.)
func (i *controller) putBounds(r rect) {
	pure.SyscallN(i.vtbl.PutBounds,
		uintptr(unsafe.Pointer(i)),
		uintptr(uint32(r.Left)),
		uintptr(uint32(r.Top)),
		uintptr(uint32(r.Right)),
		uintptr(uint32(r.Bottom)))
}

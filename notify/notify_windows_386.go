//go:build windows && 386

package notify

// sizeof(NOTIFYICONDATAW) on 32-bit Windows: the three pointer-size fields
// (hWnd, hIcon, hBalloonIcon) are 4 bytes each, so the struct is 20 bytes
// smaller than the 64-bit 976-byte size.
const notifyIconDataSize = 956

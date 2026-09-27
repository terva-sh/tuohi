//go:build windows && (amd64 || arm64)

package notify

// sizeof(NOTIFYICONDATAW) on 64-bit Windows (LLP64: pointers are 8 bytes).
const notifyIconDataSize = 976

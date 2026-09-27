// macOS-only pieces of the view/window API surface: the cursor request
// parsed from the window edge-hover script message (internalWindowCursor).
// Only the darwin engine reacts to that message (setEdgeCursor), so the
// declarations live here instead of in the platform-neutral view.go.
package tuohi

import "encoding/json"

// cursorRequestParams carries the resize edge an edge-hover message reports
// (see internalWindowCursor). Edge is the tracker's lowercase corner/edge
// name ("nw"/"n"/.../"se") or empty when the pointer left an edge band.
type cursorRequestParams struct {
	Edge string `json:"edge"`
}

func parseCursorRequest(params json.RawMessage) cursorRequestParams {
	var arr []json.RawMessage
	if err := json.Unmarshal(params, &arr); err != nil || len(arr) == 0 {
		return cursorRequestParams{}
	}
	var p cursorRequestParams
	_ = json.Unmarshal(arr[0], &p)
	return p
}

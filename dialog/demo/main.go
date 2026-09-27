// Command demo exercises the dialog subpackage end to end: it opens each of
// the four native panels - open, multi-select open, save-as and choose
// directory - one after another and prints what the user picked.
//
// The panels are real platform UI (AppKit, Win32/COM, GTK) shown on the
// calling (main) thread, so the demo needs a desktop session; on a headless
// or otherwise panel-less machine each call simply returns nil and the demo
// reports it. Cancel a panel to move to the next one; cancel them all to
// finish.
//
// Run it from the repository root:
//
//	go run ./dialog/demo
package main

import (
	"fmt"

	"github.com/terva-sh/tuohi/dialog"
)

func main() {
	// Open: a single file, narrowed to text-ish files through named filter
	// groups where the platform supports per-filter choices.
	chosen, err := dialog.Open(dialog.Options{
		Type:    dialog.TypeOpen,
		Title:   "Open a text file",
		Filters: []dialog.FileFilter{{Name: "Text files", Extensions: []string{"txt", "md", "log"}}},
	})
	if err != nil {
		fmt.Println("open:", err)
	} else {
		fmt.Printf("open       -> %v\n", chosen)
	}

	// Multiple-open: any number of files, no type restriction.
	paths, err := dialog.Open(dialog.Options{Type: dialog.TypeOpenMultiple, Title: "Open several files"})
	if err != nil {
		fmt.Println("open multi:", err)
	} else {
		fmt.Printf("open multi -> %v\n", paths)
	}

	// Save: pick where to write a new file, with a suggested name.
	saveTo, err := dialog.Open(dialog.Options{
		Type:     dialog.TypeSave,
		Title:    "Save the report",
		Filename: "report.txt",
	})
	if err != nil {
		fmt.Println("save:", err)
	} else {
		fmt.Printf("save       -> %v\n", saveTo)
	}

	// Directory: choose a folder.
	dir, err := dialog.Open(dialog.Options{Type: dialog.TypeDirectory, Title: "Choose an output directory"})
	if err != nil {
		fmt.Println("directory:", err)
	} else {
		fmt.Printf("directory  -> %v\n", dir)
	}

	fmt.Println("done")
}

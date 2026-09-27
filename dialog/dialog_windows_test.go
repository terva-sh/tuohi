package dialog

import (
	"testing"
)

// TestFileDialogComPlumbing exercises the Common Item Dialog COM plumbing
// the panels rest on: a FileOpenDialog is created via CoCreateInstance, its
// options round-trip through SetOptions/GetOptions, and a title plus both
// the flat and the named file-type filters are applied - all without calling
// Show (which is modal and needs user input). The full dialog is exercised
// manually via demos/dialog.
func TestFileDialogComPlumbing(t *testing.T) {
	if err := ensureInit(); err != nil {
		t.Skipf("COM unavailable: %v", err)
	}
	coInitializeEx(0, coinitApartmentThreaded) // ensure STA on the test thread

	var pdlg uintptr
	hr := coCreateInstance(&clsidFileOpenDialog, 0, clsctxInprocServer, &iidIFileOpenDialog, &pdlg)
	if hr < 0 || pdlg == 0 {
		t.Fatalf("CoCreateInstance failed: 0x%08X", uint32(hr))
	}
	dlg := (*fileDialog)(ptr(pdlg))
	defer dlg.Release()

	dlg.SetOptions(dlg.GetOptions() | fosForceFilesystem | fosPickFolders | fosAllowMultiSelect)
	got := dlg.GetOptions()
	if got&fosPickFolders == 0 || got&fosAllowMultiSelect == 0 {
		t.Fatalf("options roundtrip lost bits: 0x%08X", got)
	}

	dlg.SetTitle(utf16Ptr("Pick"))
	if keep := applyFileTypes(dlg, []string{"png", ".jpg"}); keep == nil {
		t.Fatal("applyFileTypes returned no spec for png/.jpg")
	}
	if keep := applyFileFilters(dlg, []FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}}); keep == nil {
		t.Fatal("applyFileFilters returned no spec for a named filter")
	}
}

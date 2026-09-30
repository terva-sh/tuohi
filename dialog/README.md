# dialog

Native open/save/choose-directory panels through a single entry point, cgo-free.

```go
paths, err := dialog.Open(dialog.Options{
	Type:  dialog.TypeOpen,
	Title: "Open scene",
})
if err != nil {
	// unknown dialog type
}
if len(paths) == 0 {
	// user cancelled (or the panel could not be shown)
}
```

`Options.Type` selects the kind of panel: open a single file, open several
files, save-as, or choose a directory. Cancelling - or a platform that cannot
show the panel (for example a Linux process with no display) - returns a
nil/empty slice with a nil error; the error is non-nil only for an
unrecognized `Type`.

## API

| Func | Description |
| --- | --- |
| `Open(opts Options) ([]string, error)` | Present the panel selected by `opts.Type` and return the chosen path(s); `nil`/empty when cancelled or unavailable, `error` only for an unknown `Type`. |

| `Type` constant | Panel |
| --- | --- |
| `TypeOpen` (zero value) | Open a single file |
| `TypeOpenMultiple` | Open several files at once |
| `TypeSave` | Save-as (nothing is written; only a path is picked) |
| `TypeDirectory` | Choose a directory |

`Options` holds the `Type`, the prompt `Title`, the initial `Directory`, the
suggested `Filename` (save-as only), and the type restriction as either the
flat `Extensions` list or the named `Filters` list:

- `Options.Extensions` is a flat extension restriction; `Options.Filters` is
  the richer, named form (shown as separate choices in the panel's type
  dropdown where the platform supports it) and takes precedence. On macOS both
  flatten to `NSOpenPanel`/`NSSavePanel` allowed file types; Windows maps
  filters to `COMDLG_FILTERSPEC` lines and GTK to one `GtkFileFilter` per
  filter.

The single-selection panels (`TypeOpen`, `TypeSave`, `TypeDirectory`) return
at most one path. No native type ever crosses the API boundary - just strings.

The tuohi `View` type exposes the same panels as one `Dialog` method -
a thin wrapper that dispatches onto the UI thread and blocks the caller until
the panel is dismissed.

## Threading

The panels are platform UI and **must be called on the main thread**. This
package deliberately does not impose a threading model; the caller arranges to
be on the main thread. For an Ebitengine app:

```go
var paths []string
ebiten.RunOnMainThread(func() {
	paths, _ = dialog.Open(dialog.Options{Type: dialog.TypeOpen, Extensions: []string{"afoil"}})
})
```

## Platforms

| OS | Backend | Status |
| --- | --- | --- |
| macOS | `NSOpenPanel` / `NSSavePanel` via the objc runtime (`TypeDirectory` is `NSOpenPanel` in directory mode) | supported |
| Windows | Common Item Dialog (`IFileOpenDialog` / `IFileSaveDialog`, COM via pure) | supported (build-tested) |
| Unix (Linux, FreeBSD, NetBSD) | `GtkFileChooserNative` (GTK3 or GTK4, chosen at runtime) | supported on Linux; FreeBSD/NetBSD compile only |

### Unix notes

- **GTK stack selection.** Loading GTK3 and GTK4 into one process corrupts the
  GObject type system, so the package first probes (`RTLD_NOLOAD`, loads
  nothing) for a GTK the process **already** has - a webview host app that
  runs on GTK - and joins it. Only when neither is mapped does it load one
  fresh: GTK3 first, then GTK4.
- **Modal loop.** `gtk_native_dialog_run` was removed in GTK4, so the modal is
  driven manually (`set_modal` + `show` + the `response` signal + main-loop
  iteration) - the same mechanism `gtk_native_dialog_run` used internally,
  valid on both GTK3 and GTK4.
- **Headless.** With no display `gtk_init_check` fails and every panel returns
  `nil` instead of crashing; the package's GTK smoke test skips itself the
  same way (and runs for real under xvfb in CI).

## Demo

A runnable example in [`examples/dialog`](../examples/dialog/) walks through all four `Type` values -
open, multi-select, save-as and choose-directory - printing each result:

```bash
cd examples && go run ./dialog
```

Run it on a desktop session; on a headless machine every panel returns `nil`
and the demo reports that.

## Conventions

Part of the tuohi module. Like the sibling subpackages (`tray`, `notify`),
this README is the consumer guide - how to use the package. Implementation
detail (COM plumbing, GTK stack probing, objc marshaling) lives in the doc
comments of the `dialog*.go` source files, not in markdown.

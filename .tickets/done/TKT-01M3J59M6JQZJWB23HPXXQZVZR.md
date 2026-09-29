---
schema: 4
id: TKT-01M3J59M6JQZJWB23HPXXQZVZR
title: Report missing WebKitGTK symbols as an error, not a panic
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
  - area/ffi
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T22:31:02Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

On Unix, a missing required symbol panics inside `ensureInit`'s `sync.Once` (`pure.RegisterLibFunc` panics, `pure/func.go:34-40`, called from `lib_unix.go:326` onward). It should come back as the error `ensureInit` already returns. `sync.Once` treats a panicking call as done, so a caller that recovers carries on with nil func vars. That is how a nil `gtkWindowResize` became a fatal crash in the tests.

### Fix

Resolve each symbol with `Dlsym` and collect the missing ones into one error naming the library and symbols, or recover inside the `Once` and set `initErr`. The error should say which WebKitGTK version is needed.

## Acceptance criteria

- [x] A missing required symbol makes ensureInit return an error naming the library and symbol
- [x] No panic escapes ensureInit's sync.Once

## Implementation plan

Chosen: both of the fixes the ticket offers.

1. Move `ensureInit`'s body into `initEngine() (err error)`. `ensureInit` becomes `initOnce.Do(func() { initErr = initEngine() })`.
2. Bind every C function through `symbols.need`, which resolves it with `Dlsym` and records a missing one instead of panicking. `openFirst` remembers each handle's soname, so the error can name the library. After the last bind, `syms.err()` returns one error naming every missing function, plus the versions tuohi needs.
3. `defer recoverInit(&err)` turns any other panic into the returned error.
4. Apply the same loader to the per-view scheme registration, and a recover guard to the dialog package's own `ensureInit`, which has the same flaw.
5. Measure the version floor from the introspection data, not from memory. Document it in the README.

Rejected:
- **Recover alone.** It reports only the first missing symbol, as a panic string.
- **Dlsym checks beside RegisterLibFunc.** That resolves every symbol twice, and can still panic between the two.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T21:32:00Z

### The version floor is measured, not remembered

A script read the `version` (since) attribute of every function `ensureInit` requires, from `/usr/share/gir-1.0/` on Debian 13: WebKit2-4.1, WebKit-6.0, Gtk-3.0, Gdk-3.0, Gtk-4.0, and GObject-2.0. The newest in each set the floor:

- **WebKitGTK 2.40, on both stacks.** Set by `webkit_response_policy_decision_is_main_frame_main_resource`, and on 6.0 also by `webkit_user_content_manager_register_script_message_handler`. The WebKitGTK reference confirms "Available since: 2.40".
- **GTK 4.12.** Set by `gtk_css_provider_load_from_string`.
- **GTK 3.20.** Set by `gdk_seat_get_pointer` and `gdk_display_get_default_seat`.

Optional functions resolved through `Dlsym` are excluded: `webkit_web_view_evaluate_javascript`, the permission-request types, `set_background_color`, and `JSConfigureSignalForGC`.

### Verified

- `TestSymbolsNeed` uses libc as the stand-in library. A present function (`strlen`) is bound and callable. An absent one is left nil and named as "tuohi_no_such_function in libc.so.6", with the version floor, in the error. The error does not name the present function.
- `TestRecoverInit` checks that a panic becomes the error, and that a clean return stays nil.
- `just ci` and `just test-gui` pass on both stacks, and golangci-lint reports 0 issues for linux, freebsd, and netbsd.

A real WebKitGTK older than 2.40 was not run. None is installed, and the unit test covers the mechanism.

## Summary

Landed in PR #36 (merge d142204).

- ensureInit binds every WebKitGTK and GTK symbol through symbols.need. A missing symbol makes it return an error naming the library, the symbol and the minimum versions. A recover guard keeps any panic inside the sync.Once from escaping.
- TestSymbolsNeed and TestRecoverInit cover it.
- GitHub main stayed green in every run since, most recently run 36639214688.

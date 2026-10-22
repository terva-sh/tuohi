package updater

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type zipEntry struct {
	name, data string
	mode       os.FileMode
}

func makeBundleZIP(t *testing.T, entries []zipEntry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.zip")
	file, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		mode := entry.mode
		if mode == 0 {
			mode = 0644
		}
		h.SetMode(mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractBundlePreservesExecutableAndFrameworkLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges are not guaranteed on Windows")
	}
	archive := makeBundleZIP(t, []zipEntry{
		{"Desktop.app/Contents/MacOS/Desktop", "executable", 0755},
		{"Desktop.app/Contents/Frameworks/F.framework/Versions/A/F", "framework", 0755},
		{"Desktop.app/Contents/Frameworks/F.framework/Versions/Current", "A", os.ModeSymlink | 0777},
		{"Desktop.app/Contents/Frameworks/F.framework/F", "Versions/Current/F", os.ModeSymlink | 0777},
		{"Desktop.app/Contents/Info.plist", "plist", 0644},
	})
	dst := filepath.Join(t.TempDir(), "candidate.app")
	if err := extractBundle(context.Background(), archive, dst); err != nil {
		t.Fatal(err)
	}
	i, err := os.Stat(filepath.Join(dst, "Contents/MacOS/Desktop"))
	if err != nil || i.Mode().Perm() != 0755 {
		t.Fatal("executable mode", i, err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "Contents/Frameworks/F.framework/F"))
	if err != nil || string(b) != "framework" {
		t.Fatal("framework symlink", string(b), err)
	}
	first, err := treeDigest(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dst, "Contents/Info.plist"), []byte("changed"), 0644)
	second, err := treeDigest(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("bundle resource change not detected")
	}
}

func TestRejectUnsafeBundleArchives(t *testing.T) {
	tests := map[string][]zipEntry{
		"traversal":                         {{"Desktop.app/../outside", "x", 0644}},
		"absolute":                          {{"/Desktop.app/file", "x", 0644}},
		"windows-separator":                 {{"Desktop.app/Contents\\outside", "x", 0644}},
		"alternate-stream":                  {{"Desktop.app/file:stream", "x", 0644}},
		"multiple-roots":                    {{"Desktop.app/file", "x", 0644}, {"Other.app/file", "y", 0644}},
		"duplicate":                         {{"Desktop.app/file", "x", 0644}, {"Desktop.app/file", "y", 0644}},
		"case-collision":                    {{"Desktop.app/File", "x", 0644}, {"Desktop.app/file", "y", 0644}},
		"implicit-directory-case-collision": {{"Desktop.app/Dir/a", "x", 0644}, {"Desktop.app/dir/b", "y", 0644}},
		"normalization":                     {{"Desktop.app/e\u0301", "x", 0644}},
		"link-parent":                       {{"Desktop.app/dir", "target", os.ModeSymlink | 0777}, {"Desktop.app/dir/file", "x", 0644}},
		"file-parent":                       {{"Desktop.app/dir", "x", 0644}, {"Desktop.app/dir/file", "x", 0644}},
		"root-file":                         {{"Desktop.app", "x", 0644}},
		"non-app":                           {{"file", "x", 0644}},
		"path-depth":                        {{"Desktop.app/" + strings.Repeat("dir/", 64) + "file", "x", 0644}},
		"absolute-link":                     {{"Desktop.app/link", "/etc/passwd", os.ModeSymlink | 0777}},
		"escaping-link":                     {{"Desktop.app/link", "../outside", os.ModeSymlink | 0777}},
		"link-cycle":                        {{"Desktop.app/a", "b", os.ModeSymlink | 0777}, {"Desktop.app/b", "a", os.ModeSymlink | 0777}},
		"dangling-link":                     {{"Desktop.app/a", "missing", os.ModeSymlink | 0777}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			archive := makeBundleZIP(t, entries)
			dst := filepath.Join(t.TempDir(), "candidate.app")
			if err := extractBundle(context.Background(), archive, dst); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatal("partial extraction retained", err)
			}
		})
	}
}

func TestBundleResourceBoundsAndCancellation(t *testing.T) {
	for _, scenario := range []string{"size", "count", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			entries := []zipEntry{{"Desktop.app/file", "x", 0644}}
			if scenario == "count" {
				entries = make([]zipEntry, bundleEntryLimit+1)
				for i := range entries {
					entries[i] = zipEntry{name: "Desktop.app/" + strings.Repeat("x", i%100) + "file", data: "x"}
				}
			}
			archive := makeBundleZIP(t, entries)
			if scenario == "size" {
				b, err := os.ReadFile(archive)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i+46 < len(b); i++ {
					if string(b[i:i+4]) == "PK\x01\x02" {
						binary.LittleEndian.PutUint32(b[i+24:i+28], uint32(bundleExpandedLimit+1))
						break
					}
				}
				if err = os.WriteFile(archive, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			dst := filepath.Join(t.TempDir(), "candidate.app")
			err := extractBundle(ctx, archive, dst)
			if err == nil {
				t.Fatal("bundle resource limit ignored")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err = os.Stat(dst); !os.IsNotExist(err) {
				t.Fatal("partial extraction retained", err)
			}
		})
	}
}

func TestBundleModeExplicitlyUnavailableElsewhere(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("native bundle implementation")
	}
	f := newFixture(t)
	cfg := f.c.cfg
	cfg.Kind = AppBundle
	if _, err := New(cfg); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestBundleResourceMutationAbortsBeforeReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Desktop.app")
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	archive := makeBundleZIP(t, []zipEntry{{"Desktop.app/Contents/MacOS/Desktop", "executable", 0755}, {"Desktop.app/Contents/Resources/room.html", "old", 0644}})
	if err := extractBundle(context.Background(), archive, target); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(work, "candidate.app")
	if err := extractBundle(context.Background(), archive, candidate); err != nil {
		t.Fatal(err)
	}
	old, err := treeDigest(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	next, err := treeDigest(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(candidate, "Contents/Resources/room.html"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	p := installPlan{Options: RestartOptions{Mode: BundleInstall, Target: target}, Work: work, OldDigest: old, NewDigest: next}
	starts := 0
	state, err := install(p, func(string, string) error { t.Fatal("moved tampered bundle"); return nil }, func(installPlan) error { starts++; return nil })
	if !errors.Is(err, ErrInvalid) || state != "rolled-back" || starts != 1 {
		t.Fatal(state, err, starts)
	}
	current, err := treeDigest(context.Background(), target)
	if err != nil || current != old {
		t.Fatal("old bundle changed", err)
	}
}

func TestRejectBundleStageOverlapIncludingDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Desktop.app")
	stage := filepath.Join(bundle, "Contents", "Cache", "stage")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{bundle, stage}, {bundle, bundle}, {stage, bundle}} {
		if err := checkBundleStage(pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("overlapping clone accepted", pair, err)
		}
	}
	safe := t.TempDir()
	if err := checkBundleStage(bundle, safe); err != nil {
		t.Fatal("safe external stage rejected", err)
	}
	if err := checkBundleStage(bundle, filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing stage accepted")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{bundle, file}, {file, safe}} {
		if err := checkBundleStage(pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("non-directory accepted", pair, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(bundle, alias); err != nil {
		t.Fatal(err)
	}
	if err := checkBundleStage(bundle, filepath.Join(alias, "Contents", "Cache", "stage")); !errors.Is(err, ErrInvalid) {
		t.Fatal("symlink overlap accepted", err)
	}
}

// Executes on a case-insensitive volume, including the usual macOS volume.
// Creating the alias as a symlink would miss the original bug: EvalSymlinks
// resolves symlinks but preserves case aliases in existing directory names.
func TestRejectBundleStageCaseAliases(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Desktop.app")
	stage := filepath.Join(bundle, "Contents", "stage")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "desktop.app")
	info, err := os.Stat(alias)
	if os.IsNotExist(err) {
		t.Skip("volume uses case-sensitive directory names")
	}
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(bundle)
	if err != nil || !os.SameFile(original, info) {
		t.Fatal("case alias does not identify original bundle", err)
	}
	aliasedStage := filepath.Join(alias, "Contents", "stage")
	for _, pair := range [][2]string{{bundle, aliasedStage}, {aliasedStage, bundle}, {bundle, alias}} {
		if err := checkBundleStage(pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("case-aliased overlap accepted", pair, err)
		}
	}
}

func TestBundleStageAllowsCaseDistinctSiblings(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Desktop.app")
	stage := filepath.Join(root, "desktop.app")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0700); os.IsExist(err) {
		t.Skip("volume uses case-insensitive directory names")
	} else if err != nil {
		t.Fatal(err)
	}
	if err := checkBundleStage(bundle, stage); err != nil {
		t.Fatal("distinct filesystem directories rejected", err)
	}
}

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

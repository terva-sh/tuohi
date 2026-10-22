package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const bundleExpandedLimit = 1 << 30
const bundleEntryLimit = 20000

// Resolve aliases before testing overlap: a clone destination inside its source
// can recursively copy itself and mutate the installed bundle before shutdown.
func checkBundleStage(target, stage string) error {
	a, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	b, err := filepath.EvalSymlinks(stage)
	if err != nil {
		return err
	}
	a, err = filepath.Abs(a)
	if err != nil {
		return err
	}
	b, err = filepath.Abs(b)
	if err != nil {
		return err
	}
	if inside(a, b) || inside(b, a) {
		return fmt.Errorf("%w: bundle and staging directory overlap", ErrInvalid)
	}
	return nil
}

// extractBundle creates one app root at dst. Symlinks are created only after
// regular files, and no archive member may be nested below a symlink. Framework
// links may point through other links only when their final target stays inside
// the same app. Archive paths use a portable, normalized subset of ZIP names.
func extractBundle(ctx context.Context, archive, dst string) (err error) {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%w: bundle ZIP", ErrInvalid)
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > bundleEntryLimit {
		return fmt.Errorf("%w: bundle entry count", ErrInvalid)
	}
	root := ""
	seen := map[string]os.FileMode{}
	spelling := map[string]string{}
	var expanded uint64
	fold := cases.Fold()
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\\:\x00") || !norm.NFC.IsNormalString(name) || path.IsAbs(name) || path.Clean(name) != name {
			return fmt.Errorf("%w: archive path", ErrInvalid)
		}
		parts := strings.Split(name, "/")
		if len(parts) > 64 {
			return fmt.Errorf("%w: archive path depth", ErrInvalid)
		}
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			key := fold.String(prefix)
			if prior, ok := spelling[key]; ok && prior != prefix {
				return fmt.Errorf("%w: archive path case collision", ErrInvalid)
			}
			spelling[key] = prefix
		}
		if !strings.HasSuffix(parts[0], ".app") || parts[0] == ".app" {
			return fmt.Errorf("%w: app root", ErrInvalid)
		}
		if root == "" {
			root = parts[0]
		}
		if root != parts[0] {
			return fmt.Errorf("%w: multiple app roots", ErrInvalid)
		}
		mode := f.Mode()
		if !mode.IsRegular() && !mode.IsDir() && mode&os.ModeSymlink == 0 {
			return fmt.Errorf("%w: archive file type", ErrInvalid)
		}
		if len(parts) == 1 && !mode.IsDir() {
			return fmt.Errorf("%w: app root type", ErrInvalid)
		}
		key := fold.String(name)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate archive path", ErrInvalid)
		}
		seen[key] = mode
		if f.UncompressedSize64 > bundleExpandedLimit-expanded {
			return fmt.Errorf("%w: expanded bundle size", ErrInvalid)
		}
		expanded += f.UncompressedSize64
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if mode, ok := seen[parent]; ok && !mode.IsDir() {
				return fmt.Errorf("%w: archive ancestor is not a directory", ErrInvalid)
			}
		}
	}
	if err = os.Mkdir(dst, 0700); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dst)
		}
	}()
	var links []*zip.File
	for _, f := range z.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimSuffix(f.Name, "/"), root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if f.Mode()&os.ModeSymlink != 0 {
			links = append(links, f)
			continue
		}
		if f.Mode().IsDir() {
			if err = os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		src, e := f.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, f.Mode().Perm()&0777)
		if e != nil {
			_ = src.Close()
			return e
		}
		n, e := io.Copy(out, io.LimitReader(&contextReader{ctx: ctx, r: src}, int64(f.UncompressedSize64)+1))
		closeErr := out.Close()
		_ = src.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if uint64(n) != f.UncompressedSize64 {
			return ErrInvalid
		}
		if err = os.Chmod(target, f.Mode().Perm()&0777); err != nil {
			return err
		}
	}
	for _, f := range links {
		src, e := f.Open()
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: src}, 4097))
		_ = src.Close()
		if e != nil {
			return e
		}
		link := string(data)
		rel := strings.TrimPrefix(f.Name, root+"/")
		if len(data) == 0 || len(data) > 4096 || path.IsAbs(link) || strings.ContainsAny(link, "\\:\x00") {
			return fmt.Errorf("%w: symlink target", ErrInvalid)
		}
		resolved := path.Clean(path.Join(path.Dir(rel), link))
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return fmt.Errorf("%w: escaping symlink", ErrInvalid)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err = os.Symlink(filepath.FromSlash(link), target); err != nil {
			return err
		}
	}
	for _, f := range links {
		target := filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(f.Name, root+"/")))
		resolved, e := filepath.EvalSymlinks(target)
		if e != nil {
			return fmt.Errorf("%w: unresolved symlink", ErrInvalid)
		}
		if !inside(dst, resolved) {
			return fmt.Errorf("%w: escaping symlink chain", ErrInvalid)
		}
	}
	if err = os.Chmod(dst, 0755); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	ok = true
	return nil
}

func inside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func treeDigest(ctx context.Context, root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", ErrInvalid
	}
	h := sha256.New()
	err = filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		mode := info.Mode()
		value := ""
		switch {
		case mode.IsRegular():
			value, err = fileDigest(ctx, name)
		case mode.IsDir():
		case mode&os.ModeSymlink != 0:
			value, err = os.Readlink(name)
			if err != nil {
				return err
			}
			resolved, e := filepath.EvalSymlinks(name)
			if e != nil || !inside(root, resolved) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(h, "%d:%s:%o:%d:%s\n", len(rel), rel, uint32(mode), len(value), value)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func installDigest(ctx context.Context, mode InstallMode, target string) (string, error) {
	if mode == BundleInstall {
		return treeDigest(ctx, target)
	}
	return fileDigest(ctx, target)
}

func candidatePath(p installPlan) string {
	if p.Options.Mode == BundleInstall {
		return filepath.Join(p.Work, "candidate.app")
	}
	return filepath.Join(p.Work, "candidate")
}
func backupPath(p installPlan) string {
	if p.Options.Mode == BundleInstall {
		return filepath.Join(p.Work, "backup.app")
	}
	return filepath.Join(p.Work, "backup")
}

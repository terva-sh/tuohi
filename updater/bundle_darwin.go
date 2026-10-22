package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ditto preserves the complete signed bundle layout, including native metadata
// and executable-relative loader paths. Copying just the Mach-O is insufficient
// for consumers that link bundled frameworks or dylibs.
func cloneBundle(ctx context.Context, src, dst string) (err error) {
	if _, err = os.Lstat(dst); !os.IsNotExist(err) {
		return ErrBusy
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dst)
		}
	}()
	if err = exec.CommandContext(ctx, "/usr/bin/ditto", src, dst).Run(); err != nil {
		return fmt.Errorf("tuohi: updater: copy helper bundle: %w", err)
	}
	complete = true
	return nil
}

func bundleExecutable(ctx context.Context, root, application string) (string, error) {
	i, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !i.IsDir() || filepath.Ext(root) != ".app" {
		return "", ErrUnsupported
	}
	plist := filepath.Join(root, "Contents", "Info.plist")
	i, err = regularFile(plist)
	if err != nil {
		return "", err
	}
	if i.Size() > manifestLimit {
		return "", ErrInvalid
	}
	field := func(key string) (string, error) {
		b, e := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist).Output()
		if e != nil {
			return "", fmt.Errorf("%w: bundle property %s", ErrInvalid, key)
		}
		return strings.TrimSpace(string(b)), nil
	}
	id, err := field("CFBundleIdentifier")
	if err != nil {
		return "", err
	}
	if id != application {
		return "", fmt.Errorf("%w: bundle identifier", ErrInvalid)
	}
	name, err := field("CFBundleExecutable")
	if err != nil {
		return "", err
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
		return "", ErrInvalid
	}
	executable := filepath.Join(root, "Contents", "MacOS", name)
	i, err = regularFile(executable)
	if err != nil {
		return "", err
	}
	if i.Mode().Perm()&0111 == 0 {
		return "", ErrInvalid
	}
	return executable, nil
}

func signatureTeam(ctx context.Context, root string) (string, error) {
	if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", root).Run(); err != nil {
		return "", fmt.Errorf("%w: bundle code signature", ErrInvalid)
	}
	b, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--display", "--verbose=4", root).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: bundle signer", ErrInvalid)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if team, ok := strings.CutPrefix(line, "TeamIdentifier="); ok && team != "" && team != "not set" && team != "-" {
			return team, nil
		}
	}
	return "", fmt.Errorf("%w: a signed team identity is required", ErrUnsupported)
}

func validateBundle(ctx context.Context, old, next, application string) (string, error) {
	oldExe, err := bundleExecutable(ctx, old, application)
	if err != nil {
		return "", err
	}
	nextExe, err := bundleExecutable(ctx, next, application)
	if err != nil {
		return "", err
	}
	if filepath.Base(oldExe) != filepath.Base(nextExe) {
		return "", fmt.Errorf("%w: changed bundle executable name", ErrUnsupported)
	}
	oldTeam, err := signatureTeam(ctx, old)
	if err != nil {
		return "", err
	}
	nextTeam, err := signatureTeam(ctx, next)
	if err != nil {
		return "", err
	}
	if oldTeam != nextTeam {
		return "", fmt.Errorf("%w: changed signing team", ErrInvalid)
	}
	return oldExe, nil
}

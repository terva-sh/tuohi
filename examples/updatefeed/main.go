// Command updatefeed signs release metadata offline. It publishes nothing.
// The key file must be supplied by the publisher's secret-management system,
// outside source control. See updater/README.md for the release procedure.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/terva-sh/tuohi/updater"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	manifest := flag.String("manifest", "", "unsigned release manifest JSON file")
	keyFile := flag.String("key-file", "", "private offline file containing a base64 Ed25519 seed")
	id := flag.String("key-id", "", "pre-pinned publisher key ID")
	flag.Parse()
	if *manifest == "" || *keyFile == "" || *id == "" {
		return errors.New("manifest, key-file and key-id are required")
	}
	payload, err := readLimited(*manifest, 1<<20)
	if err != nil {
		return errors.New("cannot read bounded manifest")
	}
	encoded, err := readLimited(*keyFile, 4096)
	if err != nil {
		return errors.New("cannot read signing key file")
	}
	defer clear(encoded)
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return errors.New("invalid signing seed encoding")
	}
	defer clear(seed)
	envelope, err := signManifest(payload, *id, seed)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(envelope, '\n'))
	return err
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("file exceeds limit")
	}
	return b, err
}

func signManifest(input []byte, id string, seed []byte) ([]byte, error) {
	if len(seed) != ed25519.SeedSize || id == "" {
		return nil, errors.New("a key ID and 32-byte Ed25519 seed are required")
	}
	var m updater.Manifest
	d := json.NewDecoder(bytes.NewReader(input))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return nil, errors.New("invalid manifest")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing manifest content")
	}
	if m.Schema != 1 || m.Application == "" || m.Channel == "" || m.Version == "" || m.Sequence == 0 || !m.Expires.After(time.Now()) || len(m.Assets) == 0 {
		return nil, errors.New("incomplete or expired manifest")
	}
	// Re-encode the public Manifest type to exact protocol field names. The
	// signature covers these bytes, rather than the pipeline's input formatting.
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	defer clear(key)
	signature := ed25519.Sign(key, append([]byte("tuohi-update-manifest-v1\x00"), payload...))
	return json.Marshal(updater.Envelope{KeyID: id, Payload: payload, Signature: signature})
}

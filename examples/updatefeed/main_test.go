package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/terva-sh/tuohi/updater"
)

func TestSignedFeedRoundTripThroughPublicConsumerAPI(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("a release payload")
	h := sha256.Sum256(data)
	m := updater.Manifest{Schema: 1, Application: "test.producer", Channel: "stable", Version: "v2", Sequence: 2, Expires: time.Now().Add(time.Hour), Assets: []updater.Asset{{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: updater.Executable, URL: "https://updates.invalid/asset", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}}}
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signManifest(payload, "publisher", key.Seed())
	if err != nil {
		t.Fatal(err)
	}
	c, err := updater.New(updater.Config{Application: m.Application, Channel: m.Channel, CurrentSequence: 1, FeedURL: "https://updates.invalid/feed", Keys: map[string]ed25519.PublicKey{"publisher": pub}, StageDir: t.TempDir(), Fetch: func(ctx context.Context, url string) (io.ReadCloser, error) {
		b := data
		if url == "https://updates.invalid/feed" {
			b = signed
		}
		return io.NopCloser(bytes.NewReader(b)), ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Sequence() != 2 {
		t.Fatal("wrong signed sequence")
	}
	s, err := r.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err = signManifest(payload, "publisher", []byte("not a seed")); err == nil {
		t.Fatal("accepted malformed seed")
	}
}

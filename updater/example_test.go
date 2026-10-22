package updater_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"strings"

	"github.com/terva-sh/tuohi/updater"
)

func ExampleNew() {
	// A real application pins its release publisher's public key at build time.
	_, err := updater.New(updater.Config{
		Application: "com.example.desktop", Channel: "stable", CurrentSequence: 42,
		FeedURL: "https://updates.example/stable.json",
		Keys:    map[string]ed25519.PublicKey{"publisher-2026": make(ed25519.PublicKey, ed25519.PublicKeySize)},
		Fetch: func(ctx context.Context, url string) (io.ReadCloser, error) {
			// Supply the application's bounded, context-aware HTTPS transport.
			return io.NopCloser(strings.NewReader("")), ctx.Err()
		},
	})
	fmt.Println(err)
	// Output: <nil>
}

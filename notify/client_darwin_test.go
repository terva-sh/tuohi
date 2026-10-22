package notify

import (
	"context"
	"errors"
	"testing"
)

func TestManagedUnbundledProcessDoesNotAskForCenter(t *testing.T) {
	if err := ensureInit(); err != nil {
		t.Fatal(err)
	}
	available := false
	notificationPool(func() { available = notificationBundleAvailable() })
	if available {
		t.Skip("test executable is running inside an application bundle")
	}
	c, err := NewClient(context.Background(), Config{})
	if c != nil {
		_ = c.Close()
		t.Fatal("unbundled process got a managed client")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbundled constructor = %v", err)
	}
}

func TestManagedBundleChecks(t *testing.T) {
	for _, tt := range []struct {
		root, id, kind, executable string
		want                       bool
	}{
		{"/Applications/Test.app", "com.example.test", "APPL", "/Applications/Test.app/Contents/MacOS/test", true},
		{"/Applications/Test.app", "", "APPL", "/Applications/Test.app/Contents/MacOS/test", false},
		{"/Applications/Test.app", "com.example.test", "BNDL", "/Applications/Test.app/Contents/MacOS/test", false},
		{"/Applications/Test.app", "com.example.test", "APPL", "/outside/test", false},
		{"/Applications/Test", "com.example.test", "APPL", "/Applications/Test/Contents/MacOS/test", false},
	} {
		if got := validNotificationBundle(tt.root, tt.id, tt.kind, tt.executable); got != tt.want {
			t.Errorf("bundle validation = %v, want %v", got, tt.want)
		}
	}
}

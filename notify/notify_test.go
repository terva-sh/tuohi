package notify

import (
	"strings"
	"testing"
)

// TestErrSentinels pins the sentinel contracts of the package.
func TestErrSentinels(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported must be non-nil")
	}
	if ErrUnavailable == nil {
		t.Fatal("ErrUnavailable must be non-nil")
	}
}

// TestUrgencyConstants pins the Urgency values and their normalized levels.
func TestUrgencyConstants(t *testing.T) {
	if UrgencyLow >= UrgencyNormal || UrgencyNormal >= UrgencyCritical {
		t.Fatalf("Urgency constants must be ordered low < normal < critical, got %d < %d < %d", UrgencyLow, UrgencyNormal, UrgencyCritical)
	}
	if UrgencyLow == 0 || UrgencyNormal == 0 || UrgencyCritical == 0 {
		t.Fatal("Urgency constants must be non-zero so the zero value can mean 'unset'")
	}
	for u, want := range map[Urgency]int{
		UrgencyLow:      0,
		UrgencyNormal:   1,
		UrgencyCritical: 2,
	} {
		if got := u.level(); got != want {
			t.Errorf("Urgency(%d).level() = %d, want %d", u, got, want)
		}
	}
}

// TestZeroValueOptionsIsNormalUrgency pins the default: an Options{} (and any
// unknown Urgency value) must behave as UrgencyNormal so plain Show calls are
// unchanged.
func TestZeroValueOptionsIsNormalUrgency(t *testing.T) {
	if got := (Options{}).Urgency.level(); got != 1 {
		t.Errorf("zero Options urgency level = %d, want 1 (normal)", got)
	}
	if got := Urgency(0).level(); got != 1 {
		t.Errorf("Urgency(0).level() = %d, want 1 (normal)", got)
	}
	if got := Urgency(99).level(); got != 1 {
		t.Errorf("unknown Urgency(99).level() = %d, want 1 (normal)", got)
	}
}

// TestBeepDefaults pins the exported tone defaults.
func TestBeepDefaults(t *testing.T) {
	if DefaultFreq <= 0 {
		t.Errorf("DefaultFreq = %v, want > 0", DefaultFreq)
	}
	if DefaultDuration <= 0 {
		t.Errorf("DefaultDuration = %d, want > 0", DefaultDuration)
	}
}

// TestOptionsValidation covers the option rules that run before any platform
// code: Icon and IconData are mutually exclusive, and IconData must be
// non-empty when set.
func TestOptionsValidation(t *testing.T) {
	ok := []Options{
		{},                                   // zero value: plain notification
		{Icon: "icon.png"},                   // file path or stock name
		{IconData: []byte("png-bytes")},      // raw PNG
		{Urgency: UrgencyCritical},           // urgency alone
		{Icon: "i.png", Urgency: UrgencyLow}, // combined
	}
	for _, o := range ok {
		if err := o.validate(); err != nil {
			t.Errorf("Options%+v.validate() = %v, want nil", o, err)
		}
	}
	bad := []Options{
		{Icon: "icon.png", IconData: []byte("png-bytes")}, // both set
		{IconData: []byte{}},                              // non-nil but empty
	}
	for _, o := range bad {
		if err := o.validate(); err == nil {
			t.Errorf("Options%+v.validate() = nil, want error", o)
		}
	}
	if err := (Options{Icon: "i.png", IconData: []byte("p")}).validate(); !strings.Contains(err.Error(), "at most one") {
		t.Errorf("conflict error = %v, want mention of 'at most one'", err)
	}
	if err := (Options{IconData: []byte{}}).validate(); !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty IconData error = %v, want mention of 'empty'", err)
	}
}

// TestShowOptsRejectsEarly proves the validation happens before any platform
// notification code runs, so the rejected calls fail identically on every
// GOOS even in an environment with no notification service.
func TestShowOptsRejectsEarly(t *testing.T) {
	if err := ShowOpts("test", "t", "m", Options{Icon: "x.png", IconData: []byte("p")}); err == nil {
		t.Fatal("ShowOpts with Icon and IconData both set must fail before reaching the platform")
	}
	if err := ShowOpts("test", "t", "m", Options{IconData: []byte{}}); err == nil {
		t.Fatal("ShowOpts with empty IconData must fail before reaching the platform")
	}
	if err := Alert("test", "t", "m", Options{Icon: "x.png", IconData: []byte("p")}); err == nil {
		t.Fatal("Alert with Icon and IconData both set must fail before reaching the platform")
	}
}

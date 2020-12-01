package config

import (
	"testing"
	"time"
)

func TestGetTimeout(t *testing.T) {
	config := NewGitOpsConfig()
	got, err := config.GetTimeout()
	if err != nil {
		t.Fatal(err)
	}

	want := 2 * time.Minute

	if got != want {
		t.Fatalf("timout mismatch: got %v, want %v", got, want)
	}
}

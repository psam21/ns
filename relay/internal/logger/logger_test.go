package logger

import (
	"context"
	"testing"
)

func TestBuildEncoderRejectsUnknownFormat(t *testing.T) {
	if _, err := buildEncoder("unknown"); err == nil {
		t.Fatal("buildEncoder accepted an unknown format")
	}
	for _, format := range []string{"json", "console"} {
		if encoder, err := buildEncoder(format); err != nil || encoder == nil {
			t.Fatalf("buildEncoder(%q) = (%v, %v)", format, encoder, err)
		}
	}
}

func TestFromContextReturnsNopWhenInactive(t *testing.T) {
	oldActive := active
	oldRoot := root
	active = false
	root = nil
	t.Cleanup(func() {
		active = oldActive
		root = oldRoot
	})
	if FromContext(context.Background()) == nil {
		t.Fatal("FromContext returned nil logger")
	}
}

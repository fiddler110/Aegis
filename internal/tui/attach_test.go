package tui

import (
	"path/filepath"
	"testing"
)

func TestExtractImageRefsBareAndQuoted(t *testing.T) {
	wd := t.TempDir()
	text := `look at @image:shot.png and @image:"my pics/screen shot.jpg" please`
	clean, images := extractImageRefs(text, wd)
	if clean != "look at and please" {
		t.Fatalf("unexpected clean text %q", clean)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if want := filepath.Join(wd, "shot.png"); images[0].Path != want {
		t.Errorf("image 0: got %q, want %q", images[0].Path, want)
	}
	if want := filepath.Join(wd, "my pics", "screen shot.jpg"); images[1].Path != want {
		t.Errorf("image 1: got %q, want %q", images[1].Path, want)
	}
}

func TestExtractImageRefsNoTokensLeavesTextUntouched(t *testing.T) {
	text := "a multiline\n\nmessage with  spacing"
	clean, images := extractImageRefs(text, "")
	if clean != text || images != nil {
		t.Fatalf("expected pass-through, got %q / %v", clean, images)
	}
}

func TestExtractImageRefsPreservesNewlines(t *testing.T) {
	clean, images := extractImageRefs("line one @image:a.png\nline two", "")
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if clean != "line one\nline two" {
		t.Fatalf("expected newline preserved, got %q", clean)
	}
}

func TestLooksLikeImagePath(t *testing.T) {
	cases := map[string]bool{
		"shot.PNG":             true,
		`C:\Users\me\pic.jpeg`: true,
		"dir/photo.webp":       true,
		"notes.txt":            false,
		"multi\nline.png":      false,
		"":                     false,
		"no extension":         false,
	}
	for in, want := range cases {
		if got := looksLikeImagePath(in); got != want {
			t.Errorf("looksLikeImagePath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAttachTokenForQuotesSpaces(t *testing.T) {
	if got := attachTokenFor("a b.png"); got != `@image:"a b.png" ` {
		t.Fatalf("got %q", got)
	}
	if got := attachTokenFor("ab.png"); got != "@image:ab.png " {
		t.Fatalf("got %q", got)
	}
}

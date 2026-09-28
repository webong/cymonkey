package bookmarklet

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, source := range []string{
		"document.title = 'Hello';",
		"// a line comment\nwindow.text = '你好 + café %20 # & </script>'; // trailing comment",
		"return 'do not replace document';",
		strings.Repeat("a", MaxSourceBytes),
	} {
		encoded, err := Encode(source)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(encoded, " \r\n\t#<>") {
			t.Fatalf("URL contains unescaped content: %.80s", encoded)
		}
		decoded, err := Decode(encoded)
		if err != nil || decoded != source {
			t.Fatalf("round trip: %q, %v", decoded, err)
		}
	}
}

func TestImportDoesNotFormDecodeOrDoubleDecode(t *testing.T) {
	got, err := Decode(" JavaScript:window.n=1+2;window.s='%2520'; ")
	if err != nil || got != "window.n=1+2;window.s='%20';" {
		t.Fatalf("got %q: %v", got, err)
	}
	got, err = Decode("javascript:window.remainder=5%2;")
	if err != nil || got != "window.remainder=5%2;" {
		t.Fatalf("lost modulo operator: %q: %v", got, err)
	}
	for _, input := range []string{"https://example.com", "javascript:", "javascript:%00", "javascript:%FF"} {
		if _, err := Decode(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestRejectInvalidSource(t *testing.T) {
	for _, input := range []string{"", " \n", "javascript:alert(1)", "a\x00", "\xff", strings.Repeat("a", MaxSourceBytes+1)} {
		if _, err := Encode(input); err == nil {
			t.Fatalf("accepted %.50q", input)
		}
	}
}

func TestInstallationPageEscapesUntrustedMarkup(t *testing.T) {
	page, err := InstallPage(`</title><script>alert(1)</script>`, `window.x = '</pre><script>alert(2)</script>';`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "<script>") || !strings.Contains(page, `href="javascript:`) || !strings.Contains(page, "&lt;/pre&gt;") {
		t.Fatal("installation page must escape labels/source and retain its explicit bookmark link")
	}
	if _, err := InstallPage("", "1"); err == nil {
		t.Fatal("accepted empty name")
	}
}

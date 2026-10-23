package decoder

import (
	"encoding/base64"
	"strings"
	"testing"
)

const sampleLink = "https://urldefense.com/v3/__https://track.editorialmanager.com/CL0/https:*2F*2Fwww.editorialmanager.com*2Fexample*2Fl.asp*3Fi=1234567*26l=EXAMPLE/1/example-message-id/example-tracking-signature=123__;JSUlJSUl!!EXAMPLE-ORGANIZATION!EXAMPLE-RECIPIENT-TOKEN$"

func v3(target, replacements string) string {
	return "https://urldefense.com/v3/__" + target + "__;" + base64.RawURLEncoding.EncodeToString([]byte(replacements)) + "!!opaque$"
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name, input, want string
		layers            int
	}{
		{"sample Editorial Manager link", sampleLink, "https://www.editorialmanager.com/example/l.asp?i=1234567&l=EXAMPLE", 2},
		{"already direct preserves escapes", "https://example.org/a%2Fb?x=a%26b&token=A+B%2BC#part", "https://example.org/a%2Fb?x=a%26b&token=A+B%2BC#part", 0},
		{"query keys are not HTML entities", "https://example.org/?x=1&copy=2&not=3", "https://example.org/?x=1&copy=2&not=3", 0},
		{"clipboard whitespace and angles", " \n<https://example.org/?a=1&b=2>\r\n", "https://example.org/?a=1&b=2", 0},
		{"markdown", "[Open link](https://example.org/?a=1&b=2)", "https://example.org/?a=1&b=2", 0},
		{"HTML escaped query", "https://example.org/?a=1&amp;b=2", "https://example.org/?a=1&b=2", 0},
		{"markdown protected link", "[Destination](" + sampleLink + ")", "https://www.editorialmanager.com/example/l.asp?i=1234567&l=EXAMPLE", 2},
		{"v3 no replacements", v3("https://example.org/path", ""), "https://example.org/path", 1},
		{"v3 single replacement", v3("https://example.org/?q=a*test", "+"), "https://example.org/?q=a+test", 1},
		{"v3 preserved final escapes", v3("https://example.org/?x=a*26b&y=*2F", "%%"), "https://example.org/?x=a%26b&y=%2F", 1},
		{"v3 run and percent before stars", v3("https:%2F%2Fexample.org/**B?x=*26", "abc%"), "https://example.org/abc?x=%26", 1},
		{"v3 Unicode run", v3("https://example.org/**A?q=*", "αβ✓"), "https://example.org/αβ?q=✓", 1},
		{"v3 URL-safe base64", v3("https://example.org/?q=*", "࿿"), "https://example.org/?q=࿿", 1},
		{"v1", "https://urldefense.proofpoint.com/v1/url?u=https%3A%2F%2Fexample.org%2Fa%253Fb%3Fx%3DA+B%252BC&k=tracking", "https://example.org/a%3Fb?x=A+B%2BC", 1},
		{"v1 unescaped query", "https://urldefense.proofpoint.com/v1/url?u=https://example.org/?a=1&b=2&k=tracking", "https://example.org/?a=1&b=2", 1},
		{"v2", "https://urldefense.proofpoint.com/v2/url?u=https-3A__example.org_a-252Fb-3Fx-3DA-2BB-252BC-26y-3D2&d=tracking", "https://example.org/a%2Fb?x=A+B%2BC&y=2", 1},
		{"Editorial Manager preserves query escapes", "https://track.editorialmanager.com/CL0/https:%2F%2Fexample.org%2Farticle%2F1%2Fpart%3Ftoken=A%252FB%26q=x%2526y/1/message/signature", "https://example.org/article/1/part?token=A%2FB&q=x%26y", 1},
		{"Editorial Manager raw URL", "https://track.editorialmanager.com/CL0/https://example.org/a%2Fb?q=x%26y/1/message/signature", "https://example.org/a%2Fb?q=x%26y", 1},
		{"unknown tracking stays intact", "https://tracker.example/?url=https%3A%2F%2Fexample.org", "https://tracker.example/?url=https%3A%2F%2Fexample.org", 0},
		{"lookalike Proofpoint stays intact", "https://urldefense.com.example.org/v3/__https://example.org__;!!tracking$", "https://urldefense.com.example.org/v3/__https://example.org__;!!tracking$", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decode(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.URL != tc.want {
				t.Errorf("URL = %q; want %q", got.URL, tc.want)
			}
			if len(got.Layers) != tc.layers {
				t.Errorf("Layers = %v; want %d wrappers", got.Layers, tc.layers)
			}
		})
	}
}

func TestInvalidInput(t *testing.T) {
	inputs := []string{
		"", "   ", "example.org", "javascript:alert(1)", "file:///tmp/test", "https://",
		"https://example.org\nhttps://second.example.org", "https://example.org/a b",
		"https://example.org/%ZZ", "https://urldefense.com/v9/test",
		"https://urldefense.com/v3/__https://example.org",
		"https://urldefense.com/v3/__https://example.org/*__;?!!tracking$",
		v3("https://example.org/*", ""),
		v3("https://example.org/**", "abc"),
		v3("https://example.org/**!", "abc"),
		v3("https://example.org/**C", "abc"),
		v3("https://example.org/", "extra"),
		v3("javascript:alert(1)", ""),
		"https://urldefense.proofpoint.com/v2/url?d=tracking",
		"https://urldefense.proofpoint.com/v1/url?k=tracking",
		"https://track.editorialmanager.com/CL0/not-a-url/1/message/signature",
		"https://track.editorialmanager.com/CL0/https:%2F%2Fexample.org/missing-tail",
		strings.Repeat("x", maxInputBytes+1),
	}
	for _, input := range inputs {
		if got, err := Decode(input); err == nil {
			t.Errorf("Decode(%q) returned %#v without an error", input, got)
		}
	}
}

func TestNestedProofpoint(t *testing.T) {
	// The outer wrapper percent-escapes delimiter-sensitive inner bytes.
	inner := v3("https://example.org/?q=a*test", "+")
	escaped := strings.NewReplacer("%", "%25", ";", "%3B", "*", "%2A").Replace(inner)
	outer := v3(escaped, "")
	// Encode the inner star through an outer substitution, as Proofpoint does.
	outer = strings.Replace(outer, "%2A", "*2A", 1)
	outer = strings.Replace(outer, ";!!opaque$", ";JQ!!opaque$", 1)
	got, err := Decode(outer)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://example.org/?q=a+test" || len(got.Layers) != 2 {
		t.Fatalf("Unexpected nested result: %#v", got)
	}
}

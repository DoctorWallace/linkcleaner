// Package decoder recovers embedded URLs without opening them or making network
// requests. It preserves the target URL's query string and percent escapes.
package decoder

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Result contains the recovered URL and the wrappers removed, in input order.
// An empty Layers slice means no supported wrapper was found.
type Result struct {
	URL    string
	Layers []string
}

const (
	maxInputBytes = 100000
	maxLayers     = 12
	runAlphabet   = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

var (
	v3Pattern = regexp.MustCompile(`(?is)^https?://[^/]+/v3/__(.+?)__;([^!]*)!`)
	v1Value   = regexp.MustCompile(`(?:^|&)u=(.+?)(?:&k=|$)`)
	markdown  = regexp.MustCompile(`(?s)^\[[^\]]*\]\(\s*(.*?)\s*\)$`)
	// Requiring a semicolon avoids changing legitimate keys such as "&copy=1".
	entities = regexp.MustCompile(`&(?:amp|lt|gt|quot|apos|#[0-9]+|#[xX][0-9A-Fa-f]+);`)
	// Editorial Manager's CL0 envelope ends in counter/message-id/signature.
	// A greedy target allows an original URL to contain its own "/1/" segments.
	editorialPattern = regexp.MustCompile(`(?is)^https?://[^/]+/CL0/(.+)/[0-9]+/[^/?#]+/[^/?#]+(?:[?#].*)?$`)
)

// Decode removes known wrappers locally. Unknown HTTP(S) links are returned
// unchanged: arbitrary redirect parameters are deliberately not guessed.
func Decode(input string) (Result, error) {
	if len(input) > maxInputBytes {
		return Result{}, errors.New("The link is too long")
	}
	current := normalizeInput(input)
	if current == "" {
		return Result{}, errors.New("Paste a link into the input box")
	}
	result := Result{URL: current}
	seen := make(map[string]bool)
	for depth := 0; depth <= maxLayers; depth++ {
		parsed, err := validateURL(current)
		if err != nil {
			return Result{}, err
		}
		host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		var next, layer string
		var recognized bool
		switch host {
		case "urldefense.com", "urldefense.proofpoint.com":
			recognized = true
			next, layer, err = decodeProofpoint(current, parsed)
		case "track.editorialmanager.com":
			if strings.HasPrefix(parsed.EscapedPath(), "/CL0/") {
				recognized = true
				layer = "Editorial Manager tracking"
				next, err = decodeEditorial(current)
			}
		}
		if err != nil {
			return Result{}, err
		}
		if !recognized {
			result.URL = current
			return result, nil
		}
		if depth == maxLayers {
			return Result{}, errors.New("The link contains too many nested redirects")
		}
		if next == current || seen[next] {
			return Result{}, errors.New("The link contains a redirect loop")
		}
		seen[current] = true
		result.Layers = append(result.Layers, layer)
		current = next
	}
	return Result{}, errors.New("The link could not be recovered")
}

func normalizeInput(input string) string {
	s := strings.TrimSpace(strings.TrimPrefix(input, "\ufeff"))
	s = decodeEntities(s)
	if match := markdown.FindStringSubmatch(s); match != nil {
		s = strings.TrimSpace(match[1])
	}
	// Common clipboard wrappers, without removing punctuation from the URL itself.
	for len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '<' && last == '>') || (first == '`' && last == '`') ||
			(first == '"' && last == '"') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}

func decodeEntities(s string) string {
	return entities.ReplaceAllStringFunc(s, html.UnescapeString)
}

func validateURL(s string) (*url.URL, error) {
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return nil, errors.New("Paste one complete link without spaces or line breaks")
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || u.Opaque != "" {
		return nil, errors.New("This is not a valid link; it must start with https:// or http://")
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return nil, errors.New("Only links starting with https:// or http:// are supported")
	}
	return u, nil
}

func decodeProofpoint(raw string, u *url.URL) (string, string, error) {
	switch {
	case strings.HasPrefix(u.Path, "/v3/"):
		s, err := decodeV3(raw)
		return s, "Proofpoint URL Defense v3", err
	case strings.HasPrefix(u.Path, "/v2/"):
		value, ok := rawQueryValue(u.RawQuery, "u")
		if !ok || value == "" {
			return "", "", errors.New("The Proofpoint v2 link is incomplete: the destination is missing")
		}
		value = strings.NewReplacer("-", "%", "_", "/").Replace(value)
		s, err := url.PathUnescape(value)
		if err != nil {
			return "", "", errors.New("The Proofpoint v2 link contains invalid encoding")
		}
		return decodeEntities(s), "Proofpoint URL Defense v2", nil
	case strings.HasPrefix(u.Path, "/v1/"):
		match := v1Value.FindStringSubmatch(u.RawQuery)
		if match == nil {
			return "", "", errors.New("The Proofpoint v1 link is incomplete: the destination is missing")
		}
		s, err := url.PathUnescape(match[1])
		if err != nil {
			return "", "", errors.New("The Proofpoint v1 link contains invalid encoding")
		}
		return decodeEntities(s), "Proofpoint URL Defense v1", nil
	default:
		return "", "", errors.New("The version of this Proofpoint link is not supported")
	}
}

func rawQueryValue(query, key string) (string, bool) {
	for _, field := range strings.Split(query, "&") {
		name, value, ok := strings.Cut(field, "=")
		if ok && name == key {
			return value, true
		}
	}
	return "", false
}

func decodeV3(raw string) (string, error) {
	match := v3Pattern.FindStringSubmatch(raw)
	if match == nil {
		return "", errors.New("The Proofpoint v3 link is incomplete or incorrectly formatted")
	}
	// URL Defense's own percent escaping is removed before replacing its stars.
	// Percent signs restored by the stars must not be decoded a second time.
	encoded, err := url.PathUnescape(match[1])
	if err != nil {
		return "", errors.New("The Proofpoint v3 link contains invalid encoding")
	}
	base := strings.TrimRight(match[2], "=")
	if len(match[2])-len(base) > 2 {
		return "", errors.New("The Proofpoint v3 link contains invalid character replacement data")
	}
	data, err := base64.RawURLEncoding.DecodeString(base)
	if err != nil || !utf8.Valid(data) {
		return "", errors.New("The Proofpoint v3 link contains invalid character replacement data")
	}
	replacements := []rune(string(data))
	var out strings.Builder
	out.Grow(len(encoded))
	pos := 0
	for i := 0; i < len(encoded); {
		if encoded[i] != '*' {
			out.WriteByte(encoded[i])
			i++
			continue
		}
		count, width := 1, 1
		if i+1 < len(encoded) && encoded[i+1] == '*' {
			if i+2 >= len(encoded) {
				return "", errors.New("The Proofpoint v3 link ends with an incomplete character replacement")
			}
			index := strings.IndexByte(runAlphabet, encoded[i+2])
			if index < 0 {
				return "", errors.New("The Proofpoint v3 link contains an unknown character replacement")
			}
			count, width = index+2, 3
		}
		if pos+count > len(replacements) {
			return "", errors.New("The Proofpoint v3 link is truncated: replacement characters are missing")
		}
		out.WriteString(string(replacements[pos : pos+count]))
		pos += count
		i += width
	}
	if pos != len(replacements) {
		return "", errors.New("The Proofpoint v3 character replacement data does not match the link")
	}
	return out.String(), nil
}

func decodeEditorial(raw string) (string, error) {
	match := editorialPattern.FindStringSubmatch(raw)
	if match == nil {
		return "", errors.New("The destination in this Editorial Manager tracking link is not recognized")
	}
	target := match[1]
	lower := strings.ToLower(target)
	// A raw HTTP(S) target already has its original escapes. Encoded targets need
	// exactly one unescape to preserve escapes inside tokens or query values.
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		var err error
		target, err = url.PathUnescape(target)
		if err != nil {
			return "", errors.New("The Editorial Manager destination contains invalid encoding")
		}
	}
	if _, err := validateURL(target); err != nil {
		return "", fmt.Errorf("Could not recover the Editorial Manager destination: %w", err)
	}
	return target, nil
}

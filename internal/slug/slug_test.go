package slug

import (
	"regexp"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	for _, list := range [][]string{adjectives, animals} {
		if len(list) == 0 || len(list) > 256 {
			t.Fatalf("word list has invalid length: %d", len(list))
		}
		for _, word := range list {
			if !regexp.MustCompile(`^[a-z]{1,28}$`).MatchString(word) {
				t.Fatalf("invalid word: %q", word)
			}
		}
	}
	format := regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z0-9]{6}$`)
	for i := 0; i < 500; i++ {
		if id := New(); !Valid(id) || !format.MatchString(id) {
			t.Fatalf("invalid generated site ID: %q", id)
		}
	}
}

func TestValid(t *testing.T) {
	for _, id := range []string{"", "a/b", "-a", "a-", "a.b", "A", "a_b", "ä", strings.Repeat("a", 64)} {
		if Valid(id) {
			t.Errorf("accepted invalid DNS label %q", id)
		}
	}
	for _, id := range []string{"0", "a-b", strings.Repeat("a", 63)} {
		if !Valid(id) {
			t.Errorf("rejected valid DNS label %q", id)
		}
	}
}

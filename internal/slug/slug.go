// Package slug generates DNS-safe, human-readable IDs for public sites.
package slug

import (
	"crypto/rand"
	_ "embed"
	"strings"
)

//go:embed adjectives.txt
var adjectiveWords string

//go:embed animals.txt
var animalWords string

var adjectives = strings.Fields(adjectiveWords)
var animals = strings.Fields(animalWords)

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// New picks an adjective and animal independently, then adds a uniform
// six-character suffix. The database, not randomness, enforces uniqueness.
func New() string {
	var suffix [6]byte
	for i := range suffix {
		suffix[i] = alphabet[randomIndex(len(alphabet))]
	}
	return adjectives[randomIndex(len(adjectives))] + "-" + animals[randomIndex(len(animals))] + "-" + string(suffix[:])
}

// randomIndex rejection-samples to avoid modulo bias. All callers pass a
// nonempty embedded list or alphabet of at most 256 choices.
func randomIndex(n int) int {
	var b [1]byte
	for {
		rand.Read(b[:]) // Go 1.24's crypto/rand fails fatally if entropy is unavailable.
		if int(b[0]) < 256-256%n {
			return int(b[0]) % n
		}
	}
}

// Valid reports whether id is a lowercase ASCII DNS label. Check it before
// using an externally sourced ID as a host label or directory name.
func Valid(id string) bool {
	if len(id) == 0 || len(id) > 63 || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for _, b := range []byte(id) {
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-') {
			return false
		}
	}
	return true
}

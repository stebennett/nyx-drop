package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// suffix multipliers, longest-suffix-first so "kib" isn't shadowed by "k".
// Decimal SI (KB/MB/GB = 1000^n) and binary (KiB/MiB/GiB = 1024^n); bare
// K/M/G are aliases for the decimal forms. See ADR-0001.
var sizeSuffixes = []struct {
	suffix string
	mult   int64
}{
	{"kib", 1024},
	{"mib", 1024 * 1024},
	{"gib", 1024 * 1024 * 1024},
	{"kb", 1_000},
	{"mb", 1_000_000},
	{"gb", 1_000_000_000},
	{"k", 1_000},
	{"m", 1_000_000},
	{"g", 1_000_000_000},
}

// ParseSize parses a human-readable byte size such as "100MB" (decimal,
// 1000^n), "1KiB" (binary, 1024^n), or a bare number of bytes ("1024").
// Suffixes are case-insensitive. See ADR-0001.
func ParseSize(s string) (int64, error) {
	orig := s
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("invalid size %q: empty", orig)
	}

	lower := strings.ToLower(trimmed)
	numPart := lower
	mult := int64(1)
	for _, sfx := range sizeSuffixes {
		if strings.HasSuffix(lower, sfx.suffix) {
			numPart = strings.TrimSuffix(lower, sfx.suffix)
			mult = sfx.mult
			break
		}
	}
	numPart = strings.TrimSpace(numPart)
	if numPart == "" {
		return 0, fmt.Errorf("invalid size %q: missing number", orig)
	}

	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", orig, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid size %q: must not be negative", orig)
	}

	// Overflow check: n * mult must not exceed math.MaxInt64.
	if mult != 0 && n > math.MaxInt64/mult {
		return 0, fmt.Errorf("invalid size %q: overflow", orig)
	}

	return n * mult, nil
}

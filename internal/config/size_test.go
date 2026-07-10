package config

import "testing"

func TestParseSize_Table(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "100MB", want: 100_000_000},
		{in: "500MB", want: 500_000_000},
		{in: "1KiB", want: 1024},
		{in: "1MiB", want: 1024 * 1024},
		{in: "1GiB", want: 1024 * 1024 * 1024},
		{in: "1024", want: 1024},
		{in: "10mb", want: 10_000_000}, // case-insensitive
		{in: "1KB", want: 1_000},
		{in: "1GB", want: 1_000_000_000},
		{in: "1K", want: 1_000}, // bare-letter alias
		{in: "1M", want: 1_000_000},
		{in: "1G", want: 1_000_000_000},
		{in: "", wantErr: true},
		{in: "10PB", wantErr: true}, // unknown suffix
		{in: "-5MB", wantErr: true}, // negative
		{in: "abc", wantErr: true},
		{in: "MB", wantErr: true},                 // numPart empty after stripping suffix
		{in: "9223372036854776kb", wantErr: true}, // multiply overflow, not ParseInt range
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseSize(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseSize(%q) = %d, nil; want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSize(%q) unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func FuzzParseSize(f *testing.F) {
	seeds := []string{"100MB", "1KiB", "", "abc", "-5MB", "10PB", "1024", "10mb", "1G"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Must never panic; error or a value is fine.
		_, _ = ParseSize(s)
	})
}

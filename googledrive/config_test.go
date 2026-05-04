package googledrive

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: go-libs, Property 7: Keep count formula is correct
func TestKeepCountFormulaIsCorrect(t *testing.T) {
	// Validates: Requirements 7.2

	type cycleCase struct {
		cycle      string
		multiplier float64
	}

	cases := []cycleCase{
		{"daily", 30},
		{"weekly", 4},
		{"monthly", 1},
	}

	for _, cc := range cases {
		cc := cc // capture range variable
		t.Run(cc.cycle, func(t *testing.T) {
			rapid.Check(t, func(t *rapid.T) {
				retainMonths := rapid.Float64Range(0.001, 1000).Draw(t, "retainMonths")

				cfg := GoogleDriveConfig{
					GDriveBackupCycle:  cc.cycle,
					GDriveRetainMonths: retainMonths,
				}

				got, err := keep(cfg)
				if err != nil {
					t.Fatalf("unexpected error for cycle %q: %v", cc.cycle, err)
				}

				want := int(cc.multiplier * retainMonths)
				if got != want {
					t.Fatalf("keep(%q, %v) = %d, want %d", cc.cycle, retainMonths, got, want)
				}
			})
		})
	}
}

// Feature: go-libs, Property 8: Invalid backup cycle returns an error
func TestInvalidBackupCycleReturnsError(t *testing.T) {
	// Validates: Requirements 7.4

	validCycles := map[string]bool{
		"daily":   true,
		"weekly":  true,
		"monthly": true,
	}

	// Build a rune slice covering printable ASCII characters
	var printableRunes []rune
	for r := rune(0x20); r <= rune(0x7E); r++ {
		printableRunes = append(printableRunes, r)
	}

	rapid.Check(t, func(t *rapid.T) {
		cycle := rapid.StringOf(rapid.RuneFrom(printableRunes)).
			Filter(func(s string) bool {
				return !validCycles[s]
			}).
			Draw(t, "invalidCycle")

		cfg := GoogleDriveConfig{
			GDriveBackupCycle:  cycle,
			GDriveRetainMonths: 1.0,
		}

		_, err := keep(cfg)
		if err == nil {
			t.Fatalf("expected error for invalid cycle %q, got nil", cycle)
		}
	})
}

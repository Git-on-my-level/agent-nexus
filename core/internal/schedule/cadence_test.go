package schedule

import (
	"testing"
	"time"
)

func TestValidateCadence(t *testing.T) {
	t.Parallel()

	valid := []string{
		"reactive",
		"daily",
		"weekly",
		"monthly",
		"custom",
		"0 9 * * *",
		"*/15 * * * *",
	}

	for _, cadence := range valid {
		if err := ValidateCadence(cadence); err != nil {
			t.Fatalf("expected cadence %q to be valid, got error: %v", cadence, err)
		}
	}

	invalid := []string{"", "every-day", "* * * *", "0 0 1 1 1 1"}
	for _, cadence := range invalid {
		if err := ValidateCadence(cadence); err == nil {
			t.Fatalf("expected cadence %q to be invalid", cadence)
		}
	}
}

func TestCadenceMatchesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		threadCadence string
		filterCadence string
		want          bool
	}{
		{threadCadence: "daily", filterCadence: "daily", want: true},
		{threadCadence: "0 9 * * *", filterCadence: "daily", want: true},
		{threadCadence: "daily", filterCadence: "0 9 * * *", want: true},
		{threadCadence: "*/15 * * * *", filterCadence: "custom", want: true},
		{threadCadence: "custom", filterCadence: "custom", want: true},
		{threadCadence: "0 9 * * *", filterCadence: "custom", want: false},
		{threadCadence: "*/15 * * * *", filterCadence: "*/15 * * * *", want: true},
		{threadCadence: "*/15 * * * *", filterCadence: "*/30 * * * *", want: false},
	}

	for _, tc := range tests {
		got := CadenceMatchesFilter(tc.threadCadence, tc.filterCadence)
		if got != tc.want {
			t.Fatalf("CadenceMatchesFilter(%q, %q) = %v, want %v", tc.threadCadence, tc.filterCadence, got, tc.want)
		}
	}
}

func TestPreviousCronRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 4, 12, 34, 0, 0, time.UTC)

	hourly, ok := PreviousCronRun("0 * * * *", now)
	if !ok {
		t.Fatal("expected hourly previous run to be found")
	}
	if want := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC); !hourly.Equal(want) {
		t.Fatalf("unexpected hourly previous run: got %s want %s", hourly, want)
	}

	earlyNow := time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)
	daily, ok := PreviousCronRun("0 9 * * *", earlyNow)
	if !ok {
		t.Fatal("expected daily previous run to be found")
	}
	if want := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC); !daily.Equal(want) {
		t.Fatalf("unexpected daily previous run: got %s want %s", daily, want)
	}

	if _, ok := PreviousCronRun("not-a-cadence", now); ok {
		t.Fatal("expected invalid cadence to fail previous run lookup")
	}
}

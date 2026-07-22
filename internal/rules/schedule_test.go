package rules

import "testing"

func TestNewScheduler_skips_disabled_schedule_rules(t *testing.T) {
	// Given
	rule := &Rule{
		Slug:    "disabled-schedule",
		Enabled: false,
		When:    When{Cron: "* * * * *"},
	}

	// When
	scheduler, err := NewScheduler([]*Rule{rule}, func(*Rule) {})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if entries := scheduler.c.Entries(); len(entries) != 0 {
		t.Fatalf("registered entries = %d, want 0", len(entries))
	}
}

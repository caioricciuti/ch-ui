package ui

import "testing"

func TestJournalPriority(t *testing.T) {
	t.Setenv("JOURNAL_STREAM", "")
	if got := journalPriority(journalWarning); got != "" {
		t.Errorf("outside journald: got %q, want no prefix", got)
	}
	t.Setenv("JOURNAL_STREAM", "9:12345")
	if got := journalPriority(journalWarning); got != "<4>" {
		t.Errorf("warning under journald: got %q, want <4>", got)
	}
	if got := journalPriority(journalError); got != "<3>" {
		t.Errorf("error under journald: got %q, want <3>", got)
	}
}

package workspace

import (
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/runner"
)

func TestSummarize(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	at := func(day, hour int) time.Time { return time.Date(2026, 12, day, hour, 0, 0, 0, loc) }
	task := func(day, hour int) Completion { return Completion{Kind: "task", At: at(day, hour)} }
	lesson := func(day, hour int) Completion { return Completion{Kind: "lesson", At: at(day, hour)} }

	// Dec 1-3 in a row (best 3), a gap, then Dec 6-7.
	cs := []Completion{task(1, 9), lesson(2, 23), task(3, 8), task(3, 20), task(6, 10), lesson(7, 9)}

	t.Run("today counts", func(t *testing.T) {
		a := Summarize(cs, at(7, 18))
		if a.Streak != 2 || a.Today != 1 || a.Best != 3 || a.Total != 6 {
			t.Errorf("got streak %d today %d best %d total %d, want 2 1 3 6", a.Streak, a.Today, a.Best, a.Total)
		}
		if len(a.Days) != 5 || a.Days[2] != (ActivityDay{Date: "2026-12-03", Tasks: 2}) {
			t.Errorf("days = %+v", a.Days)
		}
	})
	t.Run("streak survives an empty today", func(t *testing.T) {
		if a := Summarize(cs, at(8, 9)); a.Streak != 2 || a.Today != 0 {
			t.Errorf("got streak %d today %d, want 2 0", a.Streak, a.Today)
		}
	})
	t.Run("a missed day ends it", func(t *testing.T) {
		if a := Summarize(cs, at(9, 9)); a.Streak != 0 {
			t.Errorf("got streak %d, want 0", a.Streak)
		}
	})
	t.Run("days are local", func(t *testing.T) {
		// 23:30 UTC on Dec 4 is Dec 5 in CET.
		late := Completion{Kind: "task", At: time.Date(2026, 12, 4, 23, 30, 0, 0, time.UTC)}
		if a := Summarize([]Completion{late}, at(5, 12)); a.Today != 1 {
			t.Errorf("got today %d, want 1", a.Today)
		}
	})
}

func TestCompletions(t *testing.T) {
	s := &Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Go", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Attempt{
		{TaskID: "t1", Kind: "submit", Status: runner.StatusFailed},
		{TaskID: "t1", Kind: "submit", Status: runner.StatusPassed},
		{TaskID: "t1", Kind: "submit", Status: runner.StatusPassed}, // passing again isn't new
		{TaskID: "t1", Kind: "run", Status: runner.StatusPassed},
	} {
		if err := w.RecordAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	cs, err := w.Completions()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Kind != "task" {
		t.Errorf("completions = %+v, want one task", cs)
	}
}

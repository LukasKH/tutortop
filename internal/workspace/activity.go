package workspace

import (
	"slices"
	"strings"
	"time"
)

// Completion is something the learner finished: a task passed for the first
// time, or a lesson whose quiz they answered in full. Both come from the
// attempt log, so the record can't drift from progress.
type Completion struct {
	Kind string    `json:"kind"` // "task" | "lesson"
	At   time.Time `json:"at"`
}

func (w *Workspace) Completions() ([]Completion, error) {
	attempts, err := w.Attempts()
	if err != nil {
		return nil, err
	}
	lessons, _ := w.Lessons()
	questions := map[string]int{}
	for _, l := range lessons {
		questions[l.ID] = l.Questions
	}
	var out []Completion
	passed := map[string]bool{}
	answered := map[string]map[int]bool{}
	for _, a := range attempts {
		switch a.Kind {
		case AttemptSubmit:
			if a.passedTask() && !passed[a.TaskID] {
				passed[a.TaskID] = true
				out = append(out, Completion{Kind: "task", At: a.At})
			}
		case AttemptQuiz:
			qs := answered[a.LessonID]
			if qs == nil {
				qs = map[int]bool{}
				answered[a.LessonID] = qs
			}
			if qs[a.Question] {
				continue
			}
			qs[a.Question] = true
			if n := questions[a.LessonID]; n > 0 && len(qs) == n {
				out = append(out, Completion{Kind: "lesson", At: a.At})
			}
		case AttemptRun, AttemptHint:
			// Neither finishes anything.
		}
	}
	return out, nil
}

// Activity is what the learner finished, day by day, across workspaces.
type Activity struct {
	Days   []ActivityDay `json:"days"`   // days with something finished, oldest first
	Total  int           `json:"total"`  // tasks and lessons finished, ever
	Today  int           `json:"today"`  // finished today
	Streak int           `json:"streak"` // days in a row up to today; today may still be empty
	Best   int           `json:"best"`   // longest streak ever
}

type ActivityDay struct {
	Date    string `json:"date"` // YYYY-MM-DD in local time
	Tasks   int    `json:"tasks"`
	Lessons int    `json:"lessons"`
}

// Summarize groups completions by local day as of now. A streak counts days
// in a row with at least one completion. It stays alive through today until
// midnight, so a learner who hasn't finished anything yet today keeps it.
func Summarize(cs []Completion, now time.Time) Activity {
	byDate := map[string]*ActivityDay{}
	for _, c := range cs {
		date := c.At.In(now.Location()).Format(dayLayout)
		d := byDate[date]
		if d == nil {
			d = &ActivityDay{Date: date}
			byDate[date] = d
		}
		if c.Kind == "lesson" {
			d.Lessons++
		} else {
			d.Tasks++
		}
	}

	act := Activity{Days: []ActivityDay{}, Total: len(cs)}
	for _, d := range byDate {
		act.Days = append(act.Days, *d)
	}
	slices.SortFunc(act.Days, func(a, b ActivityDay) int { return strings.Compare(a.Date, b.Date) })

	// Noon keeps AddDate away from daylight-saving edges.
	day := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	if d := byDate[day.Format(dayLayout)]; d != nil {
		act.Today = d.Tasks + d.Lessons
	} else {
		day = day.AddDate(0, 0, -1)
	}
	for byDate[day.Format(dayLayout)] != nil {
		act.Streak++
		day = day.AddDate(0, 0, -1)
	}

	run := 0
	var prev time.Time
	for _, d := range act.Days {
		t, _ := time.ParseInLocation(dayLayout, d.Date, now.Location())
		t = t.Add(12 * time.Hour)
		if run > 0 && prev.AddDate(0, 0, 1).Format(dayLayout) == d.Date {
			run++
		} else {
			run = 1
		}
		prev = t
		act.Best = max(act.Best, run)
	}
	return act
}

const dayLayout = "2006-01-02"

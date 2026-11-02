package workspace

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// A lesson teaches the knowledge for one objective before its task gives the
// practice, like theory before an exercise at school. It ends in a short quiz
// the app checks, so the learner gets instant feedback. Quiz results are
// recorded for the tutor but never count as progress; only passed tests do.
//
// Lessons live in lessons/<id>/{lesson.yaml,README.md}. The quiz with its
// answers is kept privately, so the app does the checking.

// LessonThread is the chat thread for questions about a lesson. Task threads
// are named by task id, so lesson threads get a prefix.
func LessonThread(id string) string { return "lesson-" + id }

// LessonOfThread returns the lesson a chat thread belongs to, if any.
func LessonOfThread(thread string) (string, bool) { return strings.CutPrefix(thread, "lesson-") }

// LessonSpec is what the tutor submits through the create_lesson tool.
type LessonSpec struct {
	ObjectiveID   string         `json:"objectiveId" jsonschema:"id of the curriculum objective this lesson teaches"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary" jsonschema:"one sentence: what the learner will be able to do after this lesson"`
	Body          string         `json:"body" jsonschema:"markdown: the theory, short enough to read in 5-10 minutes. Explain only the knowledge the objective's task needs, tied to the learner's mission, building from what they already know. Use small headings, worked examples in code blocks, a small picture for each idea (call tree, structure tree, diff or mermaid diagram), and cite sources with links for claims. Start at the first section: the app shows the title and summary above the body and primarySource below it, so don't repeat them. Don't solve the task."`
	PrimarySource Source         `json:"primarySource" jsonschema:"the single best high-trust resource to read or watch next on this topic"`
	Quiz          []QuizQuestion `json:"quiz" jsonschema:"3-5 questions that make the learner recall and apply the lesson (retrieval practice), not recognise wording"`
}

type Source struct {
	Title string `json:"title" yaml:"title"`
	URL   string `json:"url" yaml:"url"`
}

type QuizQuestion struct {
	Question    string   `json:"question" yaml:"question" jsonschema:"markdown; may include a code block"`
	Options     []string `json:"options" yaml:"options" jsonschema:"3-4 plausible answers with the same number of words, so length and wording give nothing away"`
	Answer      int      `json:"answer" yaml:"answer" jsonschema:"0-based index of the correct option; vary its position across questions"`
	Explanation string   `json:"explanation" yaml:"explanation" jsonschema:"shown after answering: why the correct answer is right and what the tempting wrong ones get wrong"`
}

// LessonInfo is lesson.yaml.
type LessonInfo struct {
	ID        string    `yaml:"id" json:"id"`
	Title     string    `yaml:"title" json:"title"`
	Objective string    `yaml:"objective" json:"objective"`
	Questions int       `yaml:"questions" json:"questions"`
	CreatedAt time.Time `yaml:"created_at" json:"createdAt"`
}

// LessonStatus is a lesson plus how far the learner got with its quiz.
type LessonStatus struct {
	LessonInfo
	Answered int `json:"answered"`
	Correct  int `json:"correct"`
}

// Done reports whether every quiz question has an answer.
func (s LessonStatus) Done() bool { return s.Answered >= s.Questions }

// LessonDetail is everything the lesson screen needs. Answers for questions
// not yet answered stay hidden.
type LessonDetail struct {
	LessonStatus
	Summary       string             `json:"summary"`
	Body          string             `json:"body"`
	PrimarySource Source             `json:"primarySource"`
	Quiz          []QuizPrompt       `json:"quiz"`
	Results       map[int]QuizResult `json:"results"` // by question index
}

type QuizPrompt struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// QuizResult is the feedback on one answered question.
type QuizResult struct {
	Choice      int    `json:"choice"`
	Correct     bool   `json:"correct"`
	Answer      int    `json:"answer"`
	Explanation string `json:"explanation"`
}

type lessonFile struct {
	Summary       string         `yaml:"summary"`
	Body          string         `yaml:"body"`
	PrimarySource Source         `yaml:"primary_source"`
	Quiz          []QuizQuestion `yaml:"quiz"`
}

// ValidateLesson checks a lesson against the curriculum and the quiz rules.
func (w *Workspace) ValidateLesson(spec LessonSpec) error {
	cur, err := w.Curriculum()
	if err != nil {
		return err
	}
	if cur == nil {
		return errors.New("there is no approved curriculum yet")
	}
	if _, _, ok := cur.Objective(spec.ObjectiveID); !ok {
		return fmt.Errorf("objective %q isn't in the curriculum", spec.ObjectiveID)
	}
	lessons, _ := w.Lessons()
	for _, l := range lessons {
		if l.Objective == spec.ObjectiveID {
			return fmt.Errorf("objective %q already has the lesson %q; each objective gets one", spec.ObjectiveID, l.Title)
		}
	}

	var p problems
	if strings.TrimSpace(spec.Title) == "" || strings.TrimSpace(spec.Summary) == "" {
		p.addf("title and summary are required")
	}
	if !strings.Contains(spec.Body, "```") {
		p.addf("body needs at least one worked example in a code block")
	}
	for _, h := range headings(spec.Body) {
		switch {
		case strings.HasPrefix(h, "# "):
			p.addf("body repeats the title (%q): start at the first section; the app shows the title and summary above the body", h)
		case strings.EqualFold(strings.TrimSpace(strings.TrimLeft(h, "#")), "read next"):
			p.addf("body has its own Read next: leave it out; the app shows primarySource under the lesson")
		}
	}
	if !strings.Contains(spec.Body, "](http") {
		p.addf("body needs citations: link the sources behind its claims")
	}
	if n := utf8.RuneCountInString(spec.Body); n > 9000 {
		p.addf("body is %d characters; keep a lesson short (under about 9000), and split bigger topics", n)
	}
	if spec.PrimarySource.check() != "" {
		p.addf("primarySource needs a title and an http(s) url")
	}
	checkQuiz(spec.Quiz, &p)
	return p.err("lesson")
}

// checkQuiz applies the teach skill's quiz rules: nothing about an option's
// length or position should hint at the answer.
func checkQuiz(quiz []QuizQuestion, p *problems) {
	if len(quiz) < 3 || len(quiz) > 5 {
		p.addf("quiz needs 3-5 questions")
	}
	positions := map[int]bool{}
	for i, q := range quiz {
		n := i + 1
		if strings.TrimSpace(q.Question) == "" || strings.TrimSpace(q.Explanation) == "" {
			p.addf("question %d needs a question and an explanation", n)
		}
		if len(q.Options) < 3 || len(q.Options) > 4 {
			p.addf("question %d needs 3-4 options", n)
			continue
		}
		if q.Answer < 0 || q.Answer >= len(q.Options) {
			p.addf("question %d: answer must be an option index (0-%d)", n, len(q.Options)-1)
			continue
		}
		positions[q.Answer] = true
		minW, maxW, otherChars := 1<<30, 0, 0
		for j, o := range q.Options {
			words := len(strings.Fields(o))
			minW, maxW = min(minW, words), max(maxW, words)
			if j != q.Answer {
				otherChars = max(otherChars, utf8.RuneCountInString(o))
			}
		}
		if maxW-minW > 1 {
			p.addf("question %d: options have %d to %d words; give them the same number of words so length doesn't hint at the answer", n, minW, maxW)
		}
		if answer := utf8.RuneCountInString(q.Options[q.Answer]); answer > otherChars+otherChars/4 {
			p.addf("question %d: the correct option is clearly the longest; make the options the same length", n)
		}
		seen := map[string]bool{}
		for _, o := range q.Options {
			key := strings.ToLower(strings.TrimSpace(o))
			if seen[key] {
				p.addf("question %d has duplicate options", n)
			}
			seen[key] = true
		}
	}
	if len(quiz) >= 3 && len(positions) == 1 {
		p.addf("the correct answer is in the same position in every question; vary it")
	}
}

// SaveLesson stores a validated lesson.
func (w *Workspace) SaveLesson(spec LessonSpec) (LessonInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	lessons, err := w.Lessons()
	if err != nil {
		return LessonInfo{}, err
	}
	info := LessonInfo{
		ID:        fmt.Sprintf("%04d-%s", len(lessons)+1, slugify(spec.Title)),
		Title:     spec.Title,
		Objective: spec.ObjectiveID,
		Questions: len(spec.Quiz),
		CreatedAt: time.Now(),
	}
	if err := os.MkdirAll(w.path("lessons", info.ID), 0o755); err != nil {
		return info, err
	}
	if err := os.MkdirAll(w.private("lessons", info.ID), 0o755); err != nil {
		return info, err
	}
	file := lessonFile{Summary: spec.Summary, Body: spec.Body, PrimarySource: spec.PrimarySource, Quiz: spec.Quiz}
	if err := writeYAML(w.private("lessons", info.ID, "lesson.yaml"), file); err != nil {
		return info, err
	}
	if err := writeFileAtomic(w.path("lessons", info.ID, "README.md"), []byte(spec.markdown())); err != nil {
		return info, err
	}
	if err := writeYAML(w.path("lessons", info.ID, "lesson.yaml"), info); err != nil {
		return info, err
	}
	w.commit("Add lesson " + info.ID)
	return info, nil
}

// headings lists the markdown headings in text, outside code blocks (where
// a line starting with # is a comment, not a heading).
func headings(md string) []string {
	var out []string
	fenced := false
	for line := range strings.SplitSeq(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(t, "#") {
			out = append(out, t)
		}
	}
	return out
}

// markdown is the lesson as a readable file in the workspace, quiz without answers.
func (s LessonSpec) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n%s\n\n", s.Title, strings.TrimSpace(s.Summary), strings.TrimSpace(s.Body))
	fmt.Fprintf(&b, "## Read next\n\n[%s](%s)\n\n## Check yourself\n\n", s.PrimarySource.Title, s.PrimarySource.URL)
	for i, q := range s.Quiz {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(q.Question))
		for _, o := range q.Options {
			fmt.Fprintf(&b, "   - %s\n", strings.TrimSpace(o))
		}
	}
	return b.String()
}

func (w *Workspace) Lessons() ([]LessonInfo, error) {
	return readEach[LessonInfo](w.path("lessons"), "lesson.yaml")
}

// LessonStatuses lists lessons with the learner's quiz results.
func (w *Workspace) LessonStatuses() ([]LessonStatus, error) {
	lessons, err := w.Lessons()
	if err != nil {
		return nil, err
	}
	results := w.history().quiz
	out := make([]LessonStatus, len(lessons))
	for i, l := range lessons {
		out[i] = LessonStatus{LessonInfo: l}
		for _, r := range results[l.ID] {
			out[i].Answered++
			if r.Correct {
				out[i].Correct++
			}
		}
	}
	return out, nil
}

func (w *Workspace) lessonInfo(id string) (LessonInfo, error) {
	if !validName(id) {
		return LessonInfo{}, fmt.Errorf("invalid lesson id %q", id)
	}
	info, err := readYAML[LessonInfo](w.path("lessons", id, "lesson.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return info, fmt.Errorf("lesson %s not found", id)
	}
	return info, err
}

func (w *Workspace) lessonFile(id string) (lessonFile, error) {
	f, err := readYAML[lessonFile](w.private("lessons", id, "lesson.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return f, fmt.Errorf("lesson %s is missing its content", id)
	}
	return f, err
}

func (w *Workspace) Lesson(id string) (LessonDetail, error) {
	info, err := w.lessonInfo(id)
	if err != nil {
		return LessonDetail{}, err
	}
	f, err := w.lessonFile(id)
	if err != nil {
		return LessonDetail{}, err
	}
	d := LessonDetail{LessonStatus: LessonStatus{LessonInfo: info}, Summary: f.Summary, Body: f.Body, PrimarySource: f.PrimarySource, Results: map[int]QuizResult{}}
	results := w.history().quiz[id]
	for i, q := range f.Quiz {
		d.Quiz = append(d.Quiz, QuizPrompt{Question: q.Question, Options: q.Options})
		if r, ok := results[i]; ok {
			r.Answer, r.Explanation = q.Answer, q.Explanation
			d.Results[i] = r
			d.Answered++
			if r.Correct {
				d.Correct++
			}
		}
	}
	return d, nil
}

// AnswerQuiz checks one answer and records it. Only the first answer to a
// question counts; asking again returns that first result.
func (w *Workspace) AnswerQuiz(id string, question, choice int) (QuizResult, error) {
	if _, err := w.lessonInfo(id); err != nil {
		return QuizResult{}, err
	}
	f, err := w.lessonFile(id)
	if err != nil {
		return QuizResult{}, err
	}
	if question < 0 || question >= len(f.Quiz) {
		return QuizResult{}, fmt.Errorf("question %d isn't in the quiz", question)
	}
	q := f.Quiz[question]
	if r, ok := w.history().quiz[id][question]; ok {
		r.Answer, r.Explanation = q.Answer, q.Explanation
		return r, nil
	}
	if choice < 0 || choice >= len(q.Options) {
		return QuizResult{}, fmt.Errorf("choice %d isn't an option", choice)
	}
	r := QuizResult{Choice: choice, Correct: choice == q.Answer, Answer: q.Answer, Explanation: q.Explanation}
	a := Attempt{Kind: KindQuiz, LessonID: id, Question: question, Choice: choice, Total: 1}
	if r.Correct {
		a.Passed = 1
	}
	return r, w.RecordAttempt(a)
}

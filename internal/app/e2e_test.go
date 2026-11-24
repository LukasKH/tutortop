package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/mcp/mcptest"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// TestLearnerJourney follows one learner from a new workspace to a passed
// plan, calling the services the way the frontend does. Everything below
// them is real (the MCP server, the workspace's files and git repo, the
// Docker or Podman sandbox) except the AI: the test plays the agent CLI
// (tutorCLI) and answers each turn by calling that thread's MCP tools, as
// the tutor and the writers are told to. The plan has one objective in each
// built-in language, so both run end to end.
func TestLearnerJourney(t *testing.T) {
	j := startJourney(t)
	j.interview(t)
	j.writeObjectives(t)
	j.askTaskTutor(t)
	j.solveTasks(t)
	j.restart(t)
	if err := j.a.Workspaces.DeleteWorkspace(j.id); err != nil {
		t.Fatal(err)
	}
	if list, _ := j.a.Workspaces.ListWorkspaces(); len(list) != 0 {
		t.Fatalf("workspaces after the delete: %+v", list)
	}
	j.tutor.noMoreTurns(t)
}

// journey is TestLearnerJourney's app and workspace, carried from step to
// step.
type journey struct {
	dirs   appDirs
	tutor  *tutorCLI
	a      *testApp
	id     string
	w      *workspace.Workspace
	plan   *tutorSession // the plan's tutor, as the interview started it
	engine *engineCalls  // the sandbox's work, counted from after the images are built
}

// startJourney starts the app on a new workspace, with the sandbox images
// built first, so the budgets time checks, not image builds.
func startJourney(t *testing.T) *journey {
	j := &journey{dirs: tempDirs(t), tutor: &tutorCLI{turns: make(chan *turn, 8)}}
	j.a = newTestAppWith(t, j.dirs, j.tutor)
	needSandbox(t, j.a)
	for _, lang := range sandbox.BuiltinLanguages() {
		tc, _ := sandbox.Builtin(lang)
		if err := j.a.runner.Prepare(t.Context(), tc); err != nil {
			t.Fatal(err)
		}
	}
	j.engine = watchEngine(t, j.a.runner)
	id, err := j.a.Workspaces.CreateWorkspace("Learn to code", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.a.runner.RemoveCache(context.Background(), id) })
	j.id = id
	if j.w, err = j.a.store.Open(id); err != nil {
		t.Fatal(err)
	}
	return j
}

// interview: a new workspace starts the tutor's interview. The tutor writes
// the mission with its own file tools, picks an icon and proposes a plan.
// A plan citing a page that doesn't open is sent back.
func (j *journey) interview(t *testing.T) {
	interview := j.tutor.next(t, j.a, j.w, workspace.PlanThread)
	j.plan = interview.session
	if err := os.WriteFile(filepath.Join(interview.opts.Cwd, "MISSION.md"), []byte("# Mission\n\nWrite small command-line tools.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	interview.call(t, "set_icon", map[string]string{"icon": workspace.Icons()[0]})
	if out, isErr := interview.try(t, "propose_curriculum", journeyPlan(missingPage)); !isErr || !strings.Contains(out, "made-up returns 404") {
		t.Fatalf("a plan citing a missing page was accepted: %s", out)
	}
	interview.call(t, "propose_curriculum", journeyPlan("https://go.dev/doc/"))
	interview.reply("Here's a plan for you to review.")
	st := idle(t, j.a, j.id)
	if !strings.Contains(st.Mission, "command-line tools") || st.Icon == "" || st.Proposal == nil || st.Curriculum != nil {
		t.Fatalf("after the interview: mission %q, icon %q, proposal %v, curriculum %v", st.Mission, st.Icon, st.Proposal != nil, st.Curriculum != nil)
	}
}

// writeObjectives: approving the plan starts a writer for each objective.
// Each publishes a lesson, and a task that passes the quality gate.
func (j *journey) writeObjectives(t *testing.T) {
	if err := j.a.Workspaces.ApproveProposal(j.id); err != nil {
		t.Fatal(err)
	}
	writers := j.tutor.nextEach(t, j.a, j.w, workspace.WriterThread("go.words"), workspace.WriterThread("py.words"))
	for thread, writer := range writers {
		lang := objectiveLanguage(thread.ID)
		writer.call(t, "create_lesson", wordsLesson(thread.ID))
		unsound := wordsTask(thread.ID, lang)
		unsound.HiddenTests = sandbox.Files{}
		if out, isErr := writer.try(t, "create_task", unsound); !isErr || !strings.Contains(out, "has hidden tests") {
			t.Fatalf("a task without hidden tests wasn't sent back with the gate's report: %s", out)
		}
		j.engine.wantRuns(t, lang+" gate on a task without hidden tests", 0, noCache)
		out := within(t, gateBudget, lang+" quality gate", func() string {
			return writer.call(t, "create_task", wordsTask(thread.ID, lang))
		})
		// Reference with all tests, reference with the visible ones, starter:
		// each starts clean, so no learner's cache can sway the gate.
		j.engine.wantRuns(t, lang+" quality gate", 3, noCache)
		if created, err := decode.JSON[createTaskOutput]([]byte(out)); err != nil || !created.Published {
			t.Fatalf("%s's task wasn't published (%v):\n%s", thread.ID, err, out)
		}
		writer.reply("Published.")
	}
	st := idle(t, j.a, j.id)
	if len(st.Lessons) != 2 || len(st.Tasks) != 2 {
		t.Fatalf("published %d lessons and %d tasks, want 2 of each", len(st.Lessons), len(st.Tasks))
	}
	// The app numbers revisions; journeyPlan's own number is ignored.
	if st.Curriculum == nil || st.Curriculum.Revision != 1 {
		t.Fatalf("approved plan %+v, want revision 1", st.Curriculum)
	}
}

// askTaskTutor: the learner asks the first task's tutor for help, and it
// asks back with a form. The answers go to that tutor, not the plan's.
func (j *journey) askTaskTutor(t *testing.T) {
	thread := workspace.TaskThread(idle(t, j.a, j.id).Tasks[0].ID)
	if err := j.a.Chat.SendMessage(j.id, "I'm stuck.", thread.String()); err != nil {
		t.Fatal(err)
	}
	ask := j.tutor.next(t, j.a, j.w, thread)
	ask.call(t, "ask_user", map[string]any{"questions": []workspace.Question{
		{ID: "tried", Question: "What have you tried?"},
		{ID: "stuck", Question: "Which input goes wrong?"},
	}})
	ask.reply("Tell me more.")
	if st := idle(t, j.a, j.id); st.Questions == nil || st.Questions.Thread != thread.String() {
		t.Fatalf("pending form %+v, want one from %s", st.Questions, thread)
	}
	if err := j.a.Chat.AnswerQuestions(j.id, map[string]string{"tried": "splitting on spaces", "stuck": "double spaces"}); err != nil {
		t.Fatal(err)
	}
	answers := j.tutor.next(t, j.a, j.w, thread)
	if answers.session != ask.session || !strings.Contains(answers.prompt, "double spaces") {
		t.Fatalf("the answers reached a new session (%v) as %q", answers.session != ask.session, answers.prompt)
	}
	answers.reply("Look for a function that splits on runs of spaces.")
	idle(t, j.a, j.id)
}

// solveTasks: the learner reads each lesson and answers its quiz, then
// solves its task. Run checks the visible tests only; the starter fails
// them. Submit adds the hidden tests, and the reference passes them all.
// The first pass is told to the plan's tutor.
func (j *journey) solveTasks(t *testing.T) {
	st := idle(t, j.a, j.id)
	for _, task := range st.Tasks {
		lesson := lessonFor(t, st, task.Objective)
		if r, err := j.a.Lessons.AnswerQuiz(j.id, lesson.ID, 0, 0); err != nil || !r.Correct {
			t.Fatalf("quiz answer: %+v, %v", r, err)
		}
		fx := words[task.Language]
		res := within(t, runBudget, task.Language+" run", func() sandbox.Result {
			return runTests(t, j.a, j.id, task.ID, fx.starter, false)
		})
		if res.Status != sandbox.StatusFailed || len(res.Tests) != 1 {
			t.Fatalf("running the %s starter: %s with %+v, want one visible test failing", task.Language, res.Status, res.Tests)
		}
		// One container per check, with the workspace's warm build cache.
		j.engine.wantRuns(t, task.Language+" run", 1, fx.cache)
		res = within(t, runBudget, task.Language+" submit", func() sandbox.Result {
			return runTests(t, j.a, j.id, task.ID, fx.reference, true)
		})
		j.engine.wantRuns(t, task.Language+" submit", 1, fx.cache)
		if passed, total := res.Counts(); res.Status != sandbox.StatusPassed || passed != 3 || total != 3 {
			t.Fatalf("submitting the %s reference: %s, %d/%d passed\n%s", task.Language, res.Status, passed, total, res.Output)
		}
		notice := j.tutor.next(t, j.a, j.w, workspace.PlanThread)
		if !strings.Contains(notice.prompt, task.ID) {
			t.Fatalf("the plan's tutor was told %q, want a pass of %s", notice.prompt, task.ID)
		}
		notice.reply("Well done.")
	}
	if st = idle(t, j.a, j.id); st.Progress.Passed != 2 || st.Progress.Total != 2 {
		t.Fatalf("progress %+v, want 2 of 2 passed", st.Progress)
	}
}

// restart: after a restart the work is all there, and the plan's tutor
// resumes its conversation.
func (j *journey) restart(t *testing.T) {
	if err := j.a.Chat.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	j.a = newTestAppWith(t, j.dirs, j.tutor)
	var err error
	if j.w, err = j.a.store.Open(j.id); err != nil {
		t.Fatal(err)
	}
	st, err := j.a.Workspaces.Workspace(j.id)
	if err != nil || st.Progress.Passed != 2 || !st.Tasks[0].Passed {
		t.Fatalf("after a restart: progress %+v, tasks %+v, %v", st.Progress, st.Tasks, err)
	}
	d, _ := j.a.Tasks.Task(j.id, st.Tasks[0].ID)
	if fx := words[d.Language]; d.Work[fx.file] != fx.reference[fx.file] {
		t.Fatalf("the learner's code wasn't kept: %v", d.Work)
	}
	if err := j.a.Chat.SendMessage(j.id, "What's next?", workspace.PlanThread.String()); err != nil {
		t.Fatal(err)
	}
	resumed := j.tutor.next(t, j.a, j.w, workspace.PlanThread)
	if resumed.opts.ResumeID != j.plan.id {
		t.Fatalf("the plan's tutor resumed %q, want %q", resumed.opts.ResumeID, j.plan.id)
	}
	resumed.reply("Pick a new topic.")
	idle(t, j.a, j.id)
	plan, _ := j.a.Chat.Chat(j.id, workspace.PlanThread.String())
	if replies := countRole(plan, workspace.RoleAssistant); replies != 4 {
		t.Fatalf("the plan's chat has %d tutor replies, want 4:\n%+v", replies, plan)
	}
}

// Budgets for the waits the learner sees. They sit well above what a CI
// runner takes, so they trip only on a large slowdown, such as a lost build
// cache or an image rebuilt for every check. bench_test.go measures the same
// waits precisely.
const (
	runBudget  = 15 * time.Second // Run or Submit
	gateBudget = 60 * time.Second // create_task's quality gate, before a new task shows
)

// within runs f, logs how long it took, and fails the test if that's over
// budget.
func within[T any](t *testing.T, budget time.Duration, what string, f func() T) T {
	t.Helper()
	start := time.Now()
	v := f()
	took := time.Since(start).Round(time.Millisecond)
	t.Logf("%s took %v", what, took)
	if took > budget {
		t.Errorf("%s took %v, over its budget of %v", what, took, budget)
	}
	return v
}

// tutorCLI is the agent CLI, played by the test. Every thread's session
// hands its turns to the test, which acts as the agent and replies.
type tutorCLI struct {
	turns   chan *turn
	started atomic.Int32
}

func (c *tutorCLI) Start(_ context.Context, _ string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
	return &tutorSession{
		cli:     c,
		id:      fmt.Sprint("session-", c.started.Add(1)),
		opts:    opts,
		onEvent: onEvent,
	}, nil
}

// next waits for the agent's next turn, and fails unless it's on thread.
func (c *tutorCLI) next(t *testing.T, a *testApp, w *workspace.Workspace, thread workspace.Thread) *turn {
	t.Helper()
	return c.nextEach(t, a, w, thread)[thread]
}

// nextEach waits for one turn on each of threads, in any order.
func (c *tutorCLI) nextEach(t *testing.T, a *testApp, w *workspace.Workspace, threads ...workspace.Thread) map[workspace.Thread]*turn {
	t.Helper()
	byURL := map[string]workspace.Thread{}
	for _, thread := range threads {
		byURL[a.mcp.URL(w, thread, a.agents)] = thread
	}
	got := map[workspace.Thread]*turn{}
	for len(got) < len(threads) {
		select {
		case tn := <-c.turns:
			thread, ok := byURL[tn.opts.MCPURL]
			if !ok || got[thread] != nil {
				t.Fatalf("waiting for turns on %v, an unexpected one started: %q", threads, tn.prompt)
			}
			got[thread] = tn
		case <-time.After(10 * time.Second):
			t.Fatalf("waiting for turns on %v, %d started", threads, len(got))
		}
	}
	return got
}

// noMoreTurns fails if an agent started a turn the test didn't expect.
func (c *tutorCLI) noMoreTurns(t *testing.T) {
	t.Helper()
	select {
	case tn := <-c.turns:
		t.Fatalf("an unexpected turn started: %q", tn.prompt)
	default:
	}
}

type tutorSession struct {
	cli     *tutorCLI
	id      string
	opts    agent.Options
	onEvent func(agent.Event)
}

// Prompt hands the turn to the test, and streams its reply as the agent's.
func (s *tutorSession) Prompt(ctx context.Context, text string) error {
	tn := &turn{session: s, opts: s.opts, prompt: text, done: make(chan string, 1)}
	select {
	case s.cli.turns <- tn:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case reply := <-tn.done:
		s.onEvent(agent.Event{Kind: agent.EventText, Text: reply})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *tutorSession) ID() string   { return s.id }
func (s *tutorSession) Cancel()      {}
func (s *tutorSession) Close() error { return nil }

// turn is one message to the agent, which the test answers.
type turn struct {
	session *tutorSession
	opts    agent.Options
	prompt  string
	done    chan string
}

// call calls one of the thread's MCP tools, and fails the test if the tool
// returns an error.
func (tn *turn) call(t *testing.T, name string, args any) string {
	t.Helper()
	out, isErr := tn.try(t, name, args)
	if isErr {
		t.Fatalf("%s returned an error: %s", name, out)
	}
	return out
}

// try calls one of the thread's MCP tools, as its agent would.
func (tn *turn) try(t *testing.T, name string, args any) (string, bool) {
	t.Helper()
	return mcptest.Caller(t, tn.opts.MCPURL)(name, args)
}

// reply ends the turn with the agent's reply.
func (tn *turn) reply(text string) { tn.done <- text }

// idle waits until no tutor or writer in the workspace is working, and
// returns the snapshot the frontend would then show.
func idle(t *testing.T, a *testApp, id string) WorkspaceSnapshot {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		st, err := a.Workspaces.Workspace(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Busy) == 0 {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("still busy: %v", st.Busy)
		}
	}
}

func runTests(t *testing.T, a *testApp, wsID, taskID string, files sandbox.Files, submit bool) sandbox.Result {
	t.Helper()
	res, err := a.Tasks.RunTests(wsID, taskID, files, submit)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func lessonFor(t *testing.T, st WorkspaceSnapshot, objective string) workspace.LessonStatus {
	t.Helper()
	for _, l := range st.Lessons {
		if l.Objective == objective {
			return l
		}
	}
	t.Fatalf("no lesson for %s in %+v", objective, st.Lessons)
	return workspace.LessonStatus{}
}

func countRole(chat []workspace.ChatItem, role workspace.ChatRole) int {
	n := 0
	for _, item := range chat {
		if item.Role == role {
			n++
		}
	}
	return n
}

// createTaskOutput is the part of create_task's result the tests read.
type createTaskOutput struct {
	Published bool `json:"published"`
}

// journeyPlan is a plan with one objective in each built-in language,
// citing ref.
func journeyPlan(ref string) workspace.Curriculum {
	return workspace.Curriculum{
		Language: "go",
		Revision: 7, // the app numbers revisions itself
		Modules: []workspace.Module{{
			ID:         "words",
			Title:      "Words",
			References: []workspace.Source{{Title: "Docs", URL: ref}},
			Objectives: []workspace.Objective{
				{ID: "go.words", Name: "Words in Go", Title: "Split and join words in Go", Verify: "tests"},
				{ID: "py.words", Name: "Words in Python", Title: "Split and join words in Python", Verify: "tests", Language: "python"},
			},
		}},
	}
}

// objectiveLanguage is the language of journeyPlan's objective.
func objectiveLanguage(objective string) string {
	if strings.HasPrefix(objective, "py.") {
		return "python"
	}
	return "go"
}

// wordsLesson is a lesson for objective that create_lesson accepts. The
// first question's answer is its first option.
func wordsLesson(objective string) workspace.LessonSpec {
	options := []string{"1", "2", "0"}
	return workspace.LessonSpec{
		ObjectiveID:   objective,
		Title:         "Words",
		Summary:       "You'll split a sentence into words and join them back.",
		Body:          "## Splitting\n\nSplit on runs of spaces ([docs](https://go.dev/doc/)):\n\n```\n\"a  b\" -> [\"a\", \"b\"]\n```\n",
		PrimarySource: workspace.Source{Title: "Docs", URL: "https://go.dev/doc/"},
		Sources:       []workspace.Source{{Title: "Docs", URL: "https://go.dev/doc/"}},
		Quiz: []workspace.QuizQuestion{
			{Question: "How many words are in \"hi\"?", Options: options, Answer: 0, Explanation: "One word."},
			{Question: "How many words are in \"a  b\"?", Options: options, Answer: 1, Explanation: "Runs of spaces split once."},
			{Question: "How many words are in \"  \"?", Options: options, Answer: 2, Explanation: "Spaces alone make no words."},
		},
	}
}

// wordsTask is create_task's input for objective in lang: a ReverseWords
// task that passes the quality gate.
func wordsTask(objective, lang string) workspace.TaskSpec {
	fx := words[lang]
	return workspace.TaskSpec{
		ObjectiveID: objective,
		Title:       "Reverse words in " + lang,
		Statement: workspace.Statement{
			Task:         "Write ReverseWords, which returns the words of a sentence in reverse order.",
			Context:      "Command-line tools often reorder user input.",
			Signature:    fx.signature,
			Requirements: []string{"Words are separated by runs of spaces.", "Join the result with single spaces."},
			Examples: []workspace.Example{
				{Input: `"hello world"`, Output: `"world hello"`},
				{Input: `"  a   b "`, Output: `"b a"`, Explanation: "Extra spaces don't make empty words."},
			},
		},
		Starter:     fx.starter,
		Reference:   fx.reference,
		Tests:       fx.tests,
		HiddenTests: fx.hiddenTests,
	}
}

// words is the ReverseWords task's files in each built-in language: one
// visible test and two hidden ones.
var words = map[string]struct {
	file, signature                        string
	starter, reference, tests, hiddenTests sandbox.Files
	names                                  sandbox.TestNames // as the quality gate finds them
	cache                                  cacheMount        // whether the toolchain keeps a build cache between runs
}{
	"go": {
		cache:     buildCache,
		file:      "solution.go",
		signature: "func ReverseWords(s string) string",
		names:     sandbox.TestNames{Visible: []string{"TestBasic"}, Hidden: []string{"TestExtraSpaces", "TestEmpty"}},
		starter:   sandbox.Files{"solution.go": "package solution\n\nfunc ReverseWords(s string) string {\n\treturn \"\"\n}\n"},
		reference: sandbox.Files{"solution.go": `package solution

import "strings"

func ReverseWords(s string) string {
	words := strings.Fields(s)
	for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
		words[i], words[j] = words[j], words[i]
	}
	return strings.Join(words, " ")
}
`},
		tests: sandbox.Files{"visible_test.go": `package solution

import "testing"

func TestBasic(t *testing.T) {
	if got := ReverseWords("hello world"); got != "world hello" {
		t.Fatalf("got %q", got)
	}
}
`},
		hiddenTests: sandbox.Files{"hidden_test.go": `package solution

import "testing"

func TestExtraSpaces(t *testing.T) {
	if got := ReverseWords("  a   b  "); got != "b a" {
		t.Fatalf("got %q", got)
	}
}

func TestEmpty(t *testing.T) {
	if got := ReverseWords(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
`},
	},
	"python": {
		cache:     noCache,
		file:      "words.py",
		signature: "def reverse_words(s: str) -> str",
		names:     sandbox.TestNames{Visible: []string{"test_basic"}, Hidden: []string{"test_extra_spaces", "test_empty"}},
		starter:   sandbox.Files{"words.py": "def reverse_words(s: str) -> str:\n    return \"\"\n"},
		reference: sandbox.Files{"words.py": "def reverse_words(s: str) -> str:\n    return \" \".join(reversed(s.split()))\n"},
		tests: sandbox.Files{"test_words.py": `from words import reverse_words


def test_basic():
    assert reverse_words("hello world") == "world hello"
`},
		hiddenTests: sandbox.Files{"test_words_hidden.py": `from words import reverse_words


def test_extra_spaces():
    assert reverse_words("  a   b  ") == "b a"


def test_empty():
    assert reverse_words("") == ""
`},
	},
}

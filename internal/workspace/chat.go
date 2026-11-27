package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/atomicfile"
)

// ChatRole is who or what a chat item comes from.
type ChatRole string

const (
	RoleUser      ChatRole = "user"      // the learner's words
	RoleAssistant ChatRole = "assistant" // the tutor's reply
	RoleTool      ChatRole = "tool"      // a tutor action worth showing, e.g. "Updated your mission"
	RoleNotice    ChatRole = "notice"    // an app event, e.g. "Stopped" or "You passed …"
)

// ChatItem is one entry in the conversation shown in the UI.
type ChatItem struct {
	ID     string     `json:"id"`
	Thread string     `json:"thread"`
	Role   ChatRole   `json:"role"`
	Text   string     `json:"text"`
	Images []string   `json:"images,omitempty"` // pictures the learner attached, by file name (ChatImage)
	Status ChatStatus `json:"status,omitempty"` // tools always have one; notices only ChatFailed
	Mark   ChatMark   `json:"mark,omitempty"`
	At     time.Time  `json:"at"`
}

// ChatMark tells what an item is, for code that looks for it, so nothing
// matches on the text the learner reads.
type ChatMark string

const (
	// MarkPlanApproved is the notice that the learner approved a plan. The
	// chat panel hides the plan thread up to the first one (afterSetup in
	// frontend/src/lib/chat.ts). Notices saved before marks existed have
	// none, and it finds them by their text.
	MarkPlanApproved ChatMark = "plan-approved"
)

// ChatStatus is how a tool's call went, or marks a notice as a failure.
type ChatStatus string

const (
	ChatRunning ChatStatus = "running" // a tool call still at work
	ChatDone    ChatStatus = "done"
	ChatFailed  ChatStatus = "failed" // a notice about a problem, or a tool call that failed (sent live, never saved)
)

func (w *Workspace) AppendChat(item ChatItem) error {
	if !validName(item.Thread) {
		return fmt.Errorf("invalid chat thread %q", item.Thread)
	}
	// A tutor still winding down after its workspace was deleted mustn't
	// bring the folder back.
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := os.Stat(w.path(".tutortop", "workspace.json")); err != nil {
		return err
	}
	if err := os.MkdirAll(w.path(".tutortop", "chat"), 0o755); err != nil {
		return err
	}
	return appendJSONL(w.path(".tutortop", "chat", item.Thread+".jsonl"), item)
}

// Chat returns one thread's transcript.
func (w *Workspace) Chat(thread string) ([]ChatItem, error) {
	if !validName(thread) {
		return nil, fmt.Errorf("invalid chat thread %q", thread)
	}
	return readJSONL[ChatItem](w.path(".tutortop", "chat", thread+".jsonl"))
}

// What a learner may attach to one message. The composer checks the same
// limits first (frontend/src/features/chat/attachments.ts).
const (
	MaxChatImages     = 4
	MaxChatImageBytes = 5 << 20 // the most Claude takes
)

// ChatImage is a picture attached to a chat message. It's kept beside the
// thread's chat, in .tutortop/chat/<thread>/, so the chat shows it again
// after a restart.
type ChatImage struct {
	Name     string // its file name, as ChatItem.Images lists it
	Path     string
	MIMEType string
}

// SaveChatImage keeps a picture the learner attached in a thread. It must be
// a PNG, JPEG, GIF or WebP, as its bytes tell, of at most MaxChatImageBytes.
func (w *Workspace) SaveChatImage(thread string, data []byte) (ChatImage, error) {
	if !validName(thread) {
		return ChatImage{}, fmt.Errorf("invalid chat thread %q", thread)
	}
	if len(data) > MaxChatImageBytes {
		return ChatImage{}, fmt.Errorf("the image is %.1f MB; images can be at most %d MB", float64(len(data))/(1<<20), MaxChatImageBytes>>20)
	}
	ext := imageExt(data)
	if ext == "" {
		return ChatImage{}, errors.New("only PNG, JPEG, GIF and WebP images can be attached")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := os.Stat(w.path(".tutortop", "workspace.json")); err != nil {
		return ChatImage{}, err // deleted
	}
	img := ChatImage{Name: uuid.New().String() + ext, MIMEType: chatImageTypes[ext]}
	img.Path = w.path(".tutortop", "chat", thread, img.Name)
	if err := os.MkdirAll(filepath.Dir(img.Path), 0o755); err != nil {
		return ChatImage{}, err
	}
	return img, atomicfile.Write(img.Path, data)
}

// ChatImage returns a picture attached in a thread, by its name.
func (w *Workspace) ChatImage(thread, name string) (ChatImage, error) {
	mimeType := chatImageTypes[filepath.Ext(name)]
	if !validName(thread) || !validName(name) || mimeType == "" {
		return ChatImage{}, fmt.Errorf("invalid chat image %q in %q", name, thread)
	}
	img := ChatImage{Name: name, Path: w.path(".tutortop", "chat", thread, name), MIMEType: mimeType}
	if _, err := os.Stat(img.Path); err != nil {
		return ChatImage{}, err
	}
	return img, nil
}

// chatImageTypes are the pictures a chat takes, by the extension they're
// saved with.
var chatImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// imageExt names a picture's type, by its extension in chatImageTypes, from
// the signature its file starts with, or returns "" for any other type.
// (http.DetectContentType knows the same signatures, but net/http stays out
// of this package.)
func imageExt(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return ".gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return ".webp"
	}
	return ""
}

type Question struct {
	ID          string   `json:"id" jsonschema:"short key for the answer"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty" jsonschema:"2-4 suggested answers; the learner can always write their own"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

// Questions is a form from the tutor that waits for the learner's answers.
type Questions struct {
	Thread    string     `json:"thread"` // the thread that asked, which gets the answers
	Intro     string     `json:"intro,omitempty"`
	Questions []Question `json:"questions"`
}

func (w *Workspace) SetQuestions(q *Questions) error {
	p := w.path(".tutortop", "questions.json")
	if q == nil {
		return removeIfExists(p)
	}
	return writeJSON(p, q)
}

// PendingQuestions returns the unanswered form, or nil.
func (w *Workspace) PendingQuestions() *Questions {
	q, err := readJSON[Questions](w.path(".tutortop", "questions.json"))
	if err != nil {
		return nil
	}
	return &q
}

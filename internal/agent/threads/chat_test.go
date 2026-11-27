package threads

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// The learner's pictures reach the agent with the message, and the chat
// lists them, so they show again after a restart. A message with too many
// isn't sent at all.
func TestSendMessageWithImages(t *testing.T) {
	ag, fake, w := newHeldAgents(t)
	chat := NewChatService(&workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}, ag)
	png := []byte("\x89PNG\r\n\x1a\nthe error")
	plan := workspace.PlanThread.String()

	// Only a picture, no words.
	if err := chat.SendMessage(w.ID, " ", plan, [][]byte{png}); err != nil {
		t.Fatal(err)
	}
	turn := fake.next(t)
	if len(turn.images) != 1 || turn.images[0].MIMEType != "image/png" {
		t.Fatalf("the agent got images %+v, want the PNG", turn.images)
	}
	if b, _ := os.ReadFile(turn.images[0].Path); string(b) != string(png) {
		t.Errorf("the agent's image file holds %q, want the PNG", b)
	}
	turn.end(nil)
	items, _ := chat.Chat(w.ID, plan)
	if len(items) != 1 || len(items[0].Images) != 1 {
		t.Fatalf("chat %+v, want the message with its image", items)
	}
	url, err := chat.ChatImage(w.ID, plan, items[0].Images[0])
	if want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png); err != nil || url != want {
		t.Errorf("ChatImage = %q, %v; want %q", url, err, want)
	}

	tooMany := make([][]byte, workspace.MaxChatImages+1)
	for i := range tooMany {
		tooMany[i] = png
	}
	if err := chat.SendMessage(w.ID, "look", plan, tooMany); err == nil {
		t.Error("sent a message with too many images")
	}
	if err := chat.SendMessage(w.ID, "look", plan, [][]byte{[]byte("%PDF-1.7")}); err == nil {
		t.Error("sent a message with a PDF")
	}
	if items, _ := chat.Chat(w.ID, plan); len(items) != 1 {
		t.Errorf("chat %+v, want nothing posted for the refused messages", items)
	}
}

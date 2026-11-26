package workspace_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// A picture the learner attached stays with its thread, so the chat shows it
// again after a restart, and is saved under the type its bytes tell.
func TestChatImagesAreKept(t *testing.T) {
	w := newWorkspace(t)
	thread := workspace.PlanThread.String()
	png := []byte("\x89PNG\r\n\x1a\nrest of the picture")
	img, err := w.SaveChatImage(thread, png)
	if err != nil {
		t.Fatal(err)
	}
	if img.MIMEType != "image/png" || filepath.Ext(img.Name) != ".png" {
		t.Errorf("saved %+v, want a PNG", img)
	}
	if err := w.AppendChat(workspace.ChatItem{Thread: thread, Role: workspace.RoleUser, Images: []string{img.Name}}); err != nil {
		t.Fatal(err)
	}

	s := &workspace.Store{Root: filepath.Dir(w.Dir), PrivateRoot: filepath.Dir(w.PrivateDir)}
	reopened, err := s.Open(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	chat, _ := reopened.Chat(thread)
	if len(chat) != 1 || len(chat[0].Images) != 1 {
		t.Fatalf("chat %+v, want the message with its image", chat)
	}
	got, err := reopened.ChatImage(thread, chat[0].Images[0])
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got.Path); !bytes.Equal(b, png) || got.MIMEType != "image/png" {
		t.Errorf("image %+v holds %q, want the PNG", got, b)
	}
	if _, err := reopened.ChatImage("0001-other", img.Name); err == nil {
		t.Error("found the image in another thread")
	}
	// The chat folder is ignored by git, which would otherwise commit every screenshot.
	if !strings.HasPrefix(got.Path, filepath.Join(w.Dir, ".tutortop", "chat")+string(filepath.Separator)) {
		t.Errorf("image saved at %s, outside the chat folder", got.Path)
	}
}

func TestChatImageTypes(t *testing.T) {
	w := newWorkspace(t)
	for _, c := range []struct {
		name, data, mimeType string // mimeType "" means refused
	}{
		{"PNG", "\x89PNG\r\n\x1a\n...", "image/png"},
		{"JPEG", "\xff\xd8\xff\xe0...", "image/jpeg"},
		{"GIF", "GIF89a...", "image/gif"},
		{"WebP", "RIFF\x10\x00\x00\x00WEBPVP8 ", "image/webp"},
		{"PDF", "%PDF-1.7", ""},
		{"SVG", "<svg xmlns='http://www.w3.org/2000/svg'/>", ""},
		{"other RIFF", "RIFF\x10\x00\x00\x00WAVEfmt ", ""},
		{"empty", "", ""},
		{"too big", "\x89PNG\r\n\x1a\n" + strings.Repeat("x", workspace.MaxChatImageBytes), ""},
	} {
		img, err := w.SaveChatImage(workspace.PlanThread.String(), []byte(c.data))
		if c.mimeType == "" {
			if err == nil {
				t.Errorf("%s: saved as %+v, want it refused", c.name, img)
			}
			continue
		}
		if err != nil || img.MIMEType != c.mimeType {
			t.Errorf("%s: saved %+v (%v), want %s", c.name, img, err, c.mimeType)
		}
	}
}

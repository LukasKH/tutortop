package workspace

import "testing"

func TestSetIcon(t *testing.T) {
	s := &Store{Root: t.TempDir(), PrivateRoot: t.TempDir()}
	w, err := s.Create("Build your own shell", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetIcon("terminal"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetIcon("teapot"); err == nil {
		t.Error("an unknown icon was accepted")
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Icon != "terminal" {
		t.Errorf("summary = %+v, want the terminal icon", list)
	}
}

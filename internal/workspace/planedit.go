package workspace

import (
	"strings"
)

// SaveMission stores the learner's own edit of their mission. The curriculum
// isn't edited by hand: the tutor proposes changes to it, which the learner
// reviews. It reports whether the mission changed.
func (w *Workspace) SaveMission(mission string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mission = strings.TrimSpace(mission)
	if mission == strings.TrimSpace(w.ReadDoc("MISSION.md")) {
		return false, nil
	}
	if err := WriteFileAtomic(w.path("MISSION.md"), []byte(mission+"\n")); err != nil {
		return false, err
	}
	w.commit("Edit mission by hand")
	return true, nil
}

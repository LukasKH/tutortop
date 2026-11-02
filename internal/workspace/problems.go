package workspace

import (
	"fmt"
	"strings"
)

// problems collects every validation problem, so the tutor can fix them all
// in one round instead of one per call.
type problems []string

func (p *problems) addf(format string, a ...any) { *p = append(*p, fmt.Sprintf(format, a...)) }

// err returns nil when nothing was added.
func (p problems) err(what string) error {
	if len(p) == 0 {
		return nil
	}
	return fmt.Errorf("invalid %s:\n- %s", what, strings.Join(p, "\n- "))
}

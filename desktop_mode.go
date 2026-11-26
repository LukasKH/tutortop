//go:build production && !server

package main

import "github.com/LukasKH/tutortop/internal/update"

func updateSupport() update.Support {
	return update.DetectSupport()
}

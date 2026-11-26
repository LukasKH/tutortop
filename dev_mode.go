//go:build !production && !server

package main

import "github.com/LukasKH/tutortop/internal/update"

func updateSupport() update.Support {
	return update.ManualOnly("Automatic updates aren't available in development builds.")
}

// Package shellenv gives the app the PATH the user has in a terminal.
package shellenv

// LoadPath does nothing on Windows, where apps get the user's full PATH.
func LoadPath() {}

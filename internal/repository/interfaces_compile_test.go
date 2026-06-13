package repository

import (
	"testing"
)

// Compile-time checks that concrete repositories implement store interfaces.
var (
	_ UserStore    = (*UserRepository)(nil)
	_ ProjectStore = (*ProjectRepository)(nil)
)

func TestInterfacesCompile(t *testing.T) {}

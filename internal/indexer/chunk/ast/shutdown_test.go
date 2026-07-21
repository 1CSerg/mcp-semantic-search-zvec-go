//go:build zvec && treesitter

package ast

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	CloseResources()
	CloseResources() // idempotent: closeResourcesOnce runs body at most once
	os.Exit(code)
}

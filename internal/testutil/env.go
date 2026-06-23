package testutil

import (
	"os"
	"strings"
)

// HelperProcessEnv returns os.Environ() without GOCOVERDIR plus optional extra entries.
func HelperProcessEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GOCOVERDIR=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, extra...)
}

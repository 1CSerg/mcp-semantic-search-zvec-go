//go:build !unix && !windows

package lifecycle

func stopLauncherWrappers(workspace string) ([]int, error) {
	return nil, nil
}

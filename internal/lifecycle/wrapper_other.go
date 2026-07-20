//go:build !unix && !windows

package lifecycle

func stopLauncherWrappers(workspace string, exclude map[int]int64) ([]int, error) {
	return nil, nil
}

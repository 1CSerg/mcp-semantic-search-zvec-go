//go:build !windows

package zvec

func collectionMmapEnabled(string) bool {
	return true
}

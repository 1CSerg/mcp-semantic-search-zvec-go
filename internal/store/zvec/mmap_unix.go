//go:build !windows && zvec

package zvec

func collectionMmapEnabled(string) bool {
	return true
}

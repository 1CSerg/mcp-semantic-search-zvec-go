package watcher

func shouldSkipDir(name string, skipDirs []string) bool {
	for _, s := range skipDirs {
		if name == s {
			return true
		}
	}
	return false
}

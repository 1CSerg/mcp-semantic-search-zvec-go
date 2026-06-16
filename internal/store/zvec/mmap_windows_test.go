//go:build windows

package zvec

import "testing"

func TestContainsNonASCII(t *testing.T) {
	if !containsNonASCII("путь") {
		t.Fatal("expected cyrillic to be non-ascii")
	}
	if containsNonASCII("ascii/path") {
		t.Fatal("expected ascii path")
	}
}

func TestCollectionMmapEnabled(t *testing.T) {
	if collectionMmapEnabled(`C:\index\путь`) {
		t.Fatal("expected mmap disabled for non-ascii path")
	}
	if !collectionMmapEnabled(`C:\index\workspace`) {
		t.Fatal("expected mmap enabled for ascii path")
	}
}

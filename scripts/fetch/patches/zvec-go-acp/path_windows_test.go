//go:build windows

package zvec

import (
	"strings"
	"testing"
)

func TestContainsNonASCII(t *testing.T) {
	if !containsNonASCII("zvec-тест") {
		t.Fatal("expected non-ascii")
	}
	if containsNonASCII(`D:\ascii\path`) {
		t.Fatal("expected ascii only")
	}
}

func TestNativePathBytesASCII(t *testing.T) {
	got, err := nativePathBytes(`D:\ascii\path`)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `D:\ascii\path` {
		t.Fatalf("got %q", got)
	}
}

func TestNativePathBytesCyrillic(t *testing.T) {
	path := `C:\temp\zvec-тест\collection`
	got, err := nativePathBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty native bytes")
	}
	back, err := systemACPToUTF16(got)
	if err != nil {
		t.Skipf("ACP roundtrip unsupported: %v", err)
	}
	if back != path {
		if strings.Contains(back, "?") {
			t.Skipf("ACP cannot encode Cyrillic on this runner: roundtrip=%q", back)
		}
		t.Fatalf("roundtrip=%q want %q", back, path)
	}
}

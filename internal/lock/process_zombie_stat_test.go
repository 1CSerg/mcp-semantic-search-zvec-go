package lock

import "testing"

func TestLinuxStatIsZombie(t *testing.T) {
	// Minimal /proc/<pid>/stat shapes: "pid (comm) STATE ..." — state is the
	// first token after the last ')'.
	cases := []struct {
		name string
		stat string
		want bool
	}{
		{
			name: "zombie",
			stat: "123 (foo) Z 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 1 0 100",
			want: true,
		},
		{
			name: "running",
			stat: "123 (foo) R 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 1 0 100",
			want: false,
		},
		{
			name: "sleeping",
			stat: "123 (foo) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 1 0 100",
			want: false,
		},
		{
			name: "comm with spaces and parens",
			stat: "42 (a) b) Z 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 1 0 100",
			want: true,
		},
		{
			name: "comm with spaces running",
			stat: "42 (a) b) R 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 1 0 100",
			want: false,
		},
		{
			name: "malformed no paren",
			stat: "123 foo Z 1",
			want: false,
		},
		{
			name: "malformed empty after paren",
			stat: "123 (foo)",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linuxStatIsZombie(tc.stat); got != tc.want {
				t.Fatalf("linuxStatIsZombie(%q)=%v want %v", tc.stat, got, tc.want)
			}
		})
	}
}

func TestParseStatState(t *testing.T) {
	state, ok := parseStatState("1 (x) S 0")
	if !ok || state != 'S' {
		t.Fatalf("state=%q ok=%v", state, ok)
	}
	_, ok = parseStatState("broken")
	if ok {
		t.Fatal("expected false for malformed stat")
	}
}

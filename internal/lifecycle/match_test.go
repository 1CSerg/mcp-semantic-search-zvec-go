package lifecycle

import "testing"

func TestMatchesStaleStdio(t *testing.T) {
	workspace := `D:\projects\my-app`
	self := 1234

	tests := []struct {
		name    string
		cmdline string
		pid     int
		want    bool
	}{
		{
			name:    "match staging via workspace-root marker",
			cmdline: `"C:\Users\serg2\AppData\Local\mcp-semantic-search-zvec-go\cursor\abc123\mcp-semantic-search-zvec-go.exe" --stdio`,
			pid:     5678,
			want:    false, // no marker file on disk in unit test
		},
		{
			name:    "match with workspace in cmdline",
			cmdline: `D:\install\bin\mcp-semantic-search-zvec-go.exe --stdio WORKSPACE_ROOT=D:\projects\my-app`,
			pid:     5678,
			want:    true,
		},
		{
			name:    "workspace path in args",
			cmdline: `D:\projects\my-app\.mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe --stdio`,
			pid:     5678,
			want:    true,
		},
		{
			name:    "forward slash workspace",
			cmdline: `D:/projects/my-app/.mcp-semantic-search-zvec-go/bin/mcp-semantic-search-zvec-go.exe --stdio`,
			pid:     5678,
			want:    true,
		},
		{
			name:    "case insensitive drive letter",
			cmdline: `D:\projects\my-app\.mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe --stdio`,
			pid:     5678,
			want:    true,
		},
		{
			name:    "no stdio flag",
			cmdline: `D:\projects\my-app\bin\mcp-semantic-search-zvec-go.exe --http`,
			pid:     5678,
			want:    false,
		},
		{
			name:    "different workspace",
			cmdline: `D:\other\bin\mcp-semantic-search-zvec-go.exe --stdio D:\projects\other-app`,
			pid:     5678,
			want:    false,
		},
		{
			name:    "self pid",
			cmdline: `D:\projects\my-app\bin\mcp-semantic-search-zvec-go.exe --stdio`,
			pid:     self,
			want:    false,
		},
		{
			name:    "wrong binary name",
			cmdline: `D:\projects\my-app\bin\other.exe --stdio`,
			pid:     5678,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesStaleStdio(tt.cmdline, workspace, tt.pid, self)
			if got != tt.want {
				t.Fatalf("matchesStaleStdio() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCmdlineContainsWorkspaceEmpty(t *testing.T) {
	if cmdlineContainsWorkspace("anything", "") {
		t.Fatal("expected false for empty workspace")
	}
}

func TestPathContainsPathLinuxInstallDir(t *testing.T) {
	workspace := `/tmp/ws`
	installDir := `/tmp/ws/.mcp-semantic-search-zvec-go`
	cmdline := installDir + `/bin/mcp-semantic-search-zvec-go --stdio`
	if !pathContainsPath(cmdline, installDir) {
		t.Fatal("expected install dir match in Linux-style cmdline path")
	}
	if !cmdlineContainsWorkspace(cmdline, workspace) {
		t.Fatal("expected cmdlineContainsWorkspace via install dir on Linux paths")
	}
}

func TestPathContainsPathSegmentBoundary(t *testing.T) {
	// A workspace path must not match a sibling with a shared prefix.
	if pathContainsPath(`/home/user/proj-evil/bin/app --stdio`, `/home/user/proj`) {
		t.Fatal("expected no match across segment boundary (proj vs proj-evil)")
	}
	if !pathContainsPath(`/home/user/proj/bin/app --stdio`, `/home/user/proj`) {
		t.Fatal("expected match when needle ends on a separator")
	}
	if !pathContainsPath(`--stdio WORKSPACE_ROOT=/home/user/proj`, `/home/user/proj`) {
		t.Fatal("expected match when needle ends at end of string")
	}
}

func TestCmdlineContainsWorkspace(t *testing.T) {
	ws := `/home/user/project`
	if !cmdlineContainsWorkspace(`/home/user/project/bin/mcp-semantic-search-zvec-go --stdio`, ws) {
		t.Fatal("expected workspace match")
	}
	if cmdlineContainsWorkspace(`/home/other/project/bin/mcp-semantic-search-zvec-go --stdio`, ws) {
		t.Fatal("expected no match for different path")
	}
}

func TestCmdlineContainsWorkspaceDriveLetterCase(t *testing.T) {
	ws := `d:\projects\my-app`
	cmdline := `D:\projects\my-app\.mcp-semantic-search-zvec-go\bin\mcp-semantic-search-zvec-go.exe --stdio`
	if !cmdlineContainsWorkspace(cmdline, ws) {
		t.Fatal("expected case-insensitive drive letter match")
	}
	if !matchesStaleStdio(cmdline, ws, 5678, 1234) {
		t.Fatal("expected stale stdio match with mixed drive letter case")
	}
}

func TestCmdlineContainsWorkspaceBackslash(t *testing.T) {
	ws := `D:\projects\my-app`
	if !cmdlineContainsWorkspace(`D:/projects/my-app/bin/mcp-semantic-search-zvec-go.exe --stdio`, ws) {
		t.Fatal("expected forward-slash cmdline match")
	}
	if !cmdlineContainsWorkspace(`D:\projects\my-app\bin\mcp-semantic-search-zvec-go.exe --stdio`, ws) {
		t.Fatal("expected backslash cmdline match")
	}
}

func TestFirstExecutableToken(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{`"C:\path\mcp-semantic-search-zvec-go.exe" --stdio`, `C:\path\mcp-semantic-search-zvec-go.exe`},
		{`/usr/bin/mcp-semantic-search-zvec-go --stdio`, `/usr/bin/mcp-semantic-search-zvec-go`},
		{`no-space`, `no-space`},
	}
	for _, tt := range tests {
		if got := firstExecutableToken(tt.in); got != tt.want {
			t.Fatalf("firstExecutableToken(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestPathsEqual(t *testing.T) {
	if pathsEqual("", "x") || pathsEqual("x", "") {
		t.Fatal("expected false for empty path")
	}
	if !pathsEqual(`D:\a\b`, `D:/a/b`) {
		t.Fatal("expected slash-normalized match")
	}
	if pathsEqual(`D:\a\b`, `D:\c\d`) {
		t.Fatal("expected different paths to differ")
	}
}

func TestWorkspaceFromCmdlineNoMarker(t *testing.T) {
	if got := workspaceFromCmdline(`D:\bin\mcp-semantic-search-zvec-go.exe --stdio`); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

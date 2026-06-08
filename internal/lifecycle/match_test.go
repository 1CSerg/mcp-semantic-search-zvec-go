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
			name:    "match stdio same workspace",
			cmdline: `D:\install\bin\mcp-semantic-search-zvec-go.exe --stdio`,
			pid:     5678,
			want:    false, // no workspace in cmdline
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

func TestCmdlineContainsWorkspace(t *testing.T) {
	ws := `/home/user/project`
	if !cmdlineContainsWorkspace(`/home/user/project/bin/mcp-semantic-search-zvec-go --stdio`, ws) {
		t.Fatal("expected workspace match")
	}
	if cmdlineContainsWorkspace(`/home/other/project/bin/mcp-semantic-search-zvec-go --stdio`, ws) {
		t.Fatal("expected no match for different path")
	}
}

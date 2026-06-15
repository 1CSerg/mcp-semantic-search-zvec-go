//go:build windows

package lifecycle

import "testing"

func TestMatchesLauncherWrapperWindows(t *testing.T) {
	workspace := `G:\Мой диск\База знаний`
	installDir := workspace + `\.mcp-semantic-search-zvec-go`

	tests := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{
			name:    "powershell wrapper with install dir",
			cmdline: `powershell.exe -NoProfile -ExecutionPolicy Bypass -File G:\Мой диск\База знаний\.mcp-semantic-search-zvec-go\bin\run-mcp-stdio.ps1`,
			want:    true,
		},
		{
			name:    "pwsh wrapper different workspace",
			cmdline: `C:\Program Files\PowerShell\7\pwsh.exe -File D:\project\.mcp-semantic-search-zvec-go\bin\run-mcp-stdio.ps1`,
			want:    false,
		},
		{
			name:    "unrelated powershell",
			cmdline: `powershell.exe -File C:\other\script.ps1`,
			want:    false,
		},
		{
			name:    "wrapper without marker script",
			cmdline: `powershell.exe -File G:\Мой диск\База знаний\.mcp-semantic-search-zvec-go\bin\other.ps1`,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesLauncherWrapper(tt.cmdline, workspace, installDir)
			if got != tt.want {
				t.Fatalf("matchesLauncherWrapper() = %v, want %v", got, tt.want)
			}
		})
	}
}

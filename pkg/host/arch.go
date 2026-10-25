package host

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Returns the host machine architecture, not what the program was compiled with
func GetMachineArch() string {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// Windows stores the architecture in environment variables
		arch := os.Getenv("PROCESSOR_ARCHITEW6432")
		if arch == "" {
			arch = os.Getenv("PROCESSOR_ARCHITECTURE")
		}
		return arch
	default:
		// Unix-like systems (Linux, macOS, FreeBSD) use `uname -m`
		cmd = exec.Command("uname", "-m")
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "unknown"
	}
	raw := strings.TrimSpace(out.String())

	return normalizeArch(raw)
}

// Maps system architecture names (like from uname -m) to OCI/Docker standard names
func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	case "i386", "i686":
		return "386"
	case "armv7l", "armv8l":
		return "arm"
	default:
		return arch // Fallback to returning it exactly as-is
	}
}

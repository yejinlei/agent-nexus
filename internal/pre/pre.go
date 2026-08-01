package pre

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"agent-nexus/internal/color"
)

// Dependency describes a runtime tool that agent-nexus depends on.
type Dependency struct {
	// Name is the human-readable display name (e.g. "Node.js").
	Name string
	// Binary is the command name used to check existence and get version
	// (e.g. "node", "python", "git").
	Binary string
	// WingetPackage is the package identifier for winget install (Windows).
	WingetPackage string
	// AptPackage is the package name for apt install (Debian/Ubuntu).
	AptPackage string
	// YumPackage is the package name for yum/dnf install (RHEL/CentOS/Fedora).
	YumPackage string
	// BrewPackage is the package/formula name for brew install (macOS).
	BrewPackage string
	// ManualInstall is the manual install instruction shown when auto-install fails.
	ManualInstall string
}

// Status represents the result of a dependency check.
type Status struct {
	Installed bool
	Path      string
	Version   string
}

// deps defines the dependency tools required for the agent runtime.
var deps = []Dependency{
	{
		Name:          "Node.js / npm",
		Binary:        "node",
		WingetPackage: "OpenJS.NodeJS.LTS",
		AptPackage:    "nodejs npm",
		YumPackage:    "nodejs npm",
		BrewPackage:   "node",
		ManualInstall: "下载 https://nodejs.org/ 安装 Node.js (含 npm)",
	},
	{
		Name:          "Python / pip",
		Binary:        "python",
		WingetPackage: "Python.Python.3",
		AptPackage:    "python3 python3-pip",
		YumPackage:    "python3 python3-pip",
		BrewPackage:   "python",
		ManualInstall: "下载 https://python.org/ 安装 Python (含 pip)",
	},
	{
		Name:          "Git",
		Binary:        "git",
		WingetPackage: "Git.Git",
		AptPackage:    "git",
		YumPackage:    "git",
		BrewPackage:   "git",
		ManualInstall: "下载 https://git-scm.com/ 安装 Git",
	},
}

// All returns the list of dependency definitions.
func All() []Dependency {
	return deps
}

// GetByTool returns the dependency matching the given tool name, or nil.
// Recognized names: "node", "npm", "python", "pip", "git", "all".
func GetByTool(tool string) []Dependency {
	tool = strings.ToLower(strings.TrimSpace(tool))
	switch tool {
	case "node", "npm":
		return []Dependency{deps[0]}
	case "python", "pip":
		return []Dependency{deps[1]}
	case "git":
		return []Dependency{deps[2]}
	case "all":
		return deps
	default:
		return nil
	}
}

// Check looks up the binary in PATH and optionally queries its version.
func (d Dependency) Check() Status {
	s := Status{}
	path, err := exec.LookPath(d.Binary)
	if err != nil {
		return s // not installed
	}
	s.Installed = true
	s.Path = path

	// Attempt to get version string.
	// For non-python binaries, use -v flag for version.
	versionCmd := d.Binary
	if d.Binary == "python" {
		// python --version prints to stderr; --version on some setups uses stdout.
		verOut, err := exec.Command(d.Binary, "--version").Output()
		if err != nil {
			verOut, _ = exec.Command(d.Binary, "--version").CombinedOutput()
		}
		if len(verOut) > 0 {
			s.Version = strings.TrimSpace(string(verOut))
		}
		return s
	}
	verOut, err := exec.Command(versionCmd, "-v").Output()
	if err == nil && len(verOut) > 0 {
		s.Version = strings.TrimSpace(string(verOut))
	}
	return s
}

// Install attempts to install the dependency using the platform-specific
// package manager (winget / apt / yum / brew). Prints a manual install
// hint on failure. Returns true if the installation command succeeded.
func (d Dependency) Install() bool {
	platform := runtime.GOOS
	var cmdName string
	var cmdArgs []string

	switch platform {
	case "windows":
		if !isWindowsAdmin() {
			fmt.Printf("  %s %s: winget needs admin privileges.\n", color.FailIcon(), d.Name)
			fmt.Printf("  Run agent-nexus as administrator, or install manually: %s\n", d.ManualInstall)
			return false
		}
		cmdName = "winget"
		cmdArgs = []string{"install", "-e", "--id", d.WingetPackage, "--accept-package-agreements", "--accept-source-agreements", "--silent"}
	case "linux":
		// Try apt first, then yum/dnf.
		_, aptErr := exec.LookPath("apt")
		_, yumErr := exec.LookPath("yum")
		_, dnfErr := exec.LookPath("dnf")
		if aptErr == nil {
			cmdName = "apt"
			cmdArgs = []string{"install", "-y"}
			cmdArgs = append(cmdArgs, strings.Fields(d.AptPackage)...)
		} else if yumErr == nil || dnfErr == nil {
			if dnfErr == nil {
				cmdName = "dnf"
			} else {
				cmdName = "yum"
			}
			cmdArgs = []string{"install", "-y"}
			cmdArgs = append(cmdArgs, strings.Fields(d.YumPackage)...)
		} else {
			fmt.Printf("  %s %s: 当前 Linux 发行版未检测到 apt / yum / dnf，无法自动安装。\n",
				color.FailIcon(), d.Name)
			fmt.Printf("  手动安装: %s\n", d.ManualInstall)
			return false
		}
	case "darwin":
		cmdName = "brew"
		cmdArgs = []string{"install", d.BrewPackage}
	default:
		fmt.Printf("  %s %s: 不支持的平台 %s，无法自动安装。\n",
			color.FailIcon(), d.Name, platform)
		fmt.Printf("  手动安装: %s\n", d.ManualInstall)
		return false
	}

	// Verify the package manager itself exists.
	if _, pmErr := exec.LookPath(cmdName); pmErr != nil {
		fmt.Printf("  %s %s: 未找到包管理器 %s，无法自动安装。\n",
			color.FailIcon(), d.Name, cmdName)
		fmt.Printf("  手动安装: %s\n", d.ManualInstall)
		return false
	}

	fmt.Printf("  正在安装 %s (%s)...\n", d.Name, cmdName)
	execCmd := exec.Command(cmdName, cmdArgs...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	err := execCmd.Run()
	if err != nil {
		fmt.Printf("  %s %s: 自动安装失败 (%v)\n", color.FailIcon(), d.Name, err)
		fmt.Printf("  手动安装: %s\n", d.ManualInstall)
		return false
	}

	// Verify installation succeeded.
	st := d.Check()
	if st.Installed {
		fmt.Printf("  %s %s 安装成功 (路径: %s %s)\n",
			color.SuccessIcon(), d.Name, st.Path, st.Version)
		return true
	}
	fmt.Printf("  %s %s: 安装命令返回成功，但 %s 仍未在 PATH 中。\n",
		color.FailIcon(), d.Name, d.Binary)
	fmt.Printf("  手动安装: %s\n", d.ManualInstall)
	return false
}

// CheckAll runs Check() on every dependency and returns the results.
func CheckAll() []Status {
	results := make([]Status, len(deps))
	for i, d := range deps {
		results[i] = d.Check()
	}
	return results
}

// CheckFiltered runs Check() on the dependencies matching the tool filter.
func CheckFiltered(tool string) []Status {
	filtered := GetByTool(tool)
	if filtered == nil {
		return nil
	}
	results := make([]Status, len(filtered))
	for i, d := range filtered {
		results[i] = d.Check()
	}
	return results
}

// InstallMissing attempts to install the dependencies matching the tool filter
// that are not yet installed. Returns (successCount, failCount).
func InstallMissing(tool string) (int, int) {
	filtered := GetByTool(tool)
	if filtered == nil {
		return 0, 0
	}
	var ok, fail int
	for _, d := range filtered {
		if !d.Check().Installed {
			if d.Install() {
				ok++
			} else {
				fail++
			}
		} else {
			ok++ // already installed counts as success
		}
	}
	return ok, fail
}

// RenderStatusTable prints a formatted status table for a list of
// (Dependency, Status) pairs.
func RenderStatusTable(items []Dependency, statuses []Status) {
	fmt.Println()
	fmt.Printf("  %s  %s  %s  %s\n", color.Bold("Tool"), color.Bold("Status"), color.Bold("Path"), color.Bold("Version"))
	fmt.Println(color.Separator(80))
	for i, d := range items {
		st := statuses[i]
		statusStr := color.FailIcon() + " Missing"
		versionStr := "-"
		if st.Installed {
			statusStr = color.SuccessIcon() + " Installed"
			versionStr = st.Version
		}
		fmt.Printf("  %-14s %-12s %-22s %s\n", d.Name, statusStr, st.Path, versionStr)
	}
	fmt.Println()
}

// SuccessIcon returns a colored success icon (delegated to color package).
func SuccessIcon() string { return color.SuccessIcon() }

// FailIcon returns a colored failure icon (delegated to color package).
func FailIcon() string { return color.FailIcon() }

// Separator returns a colored separator line (delegated to color package).
func Separator(length int) string { return color.Separator(length) }
func isWindowsAdmin() bool {
	if runtime.GOOS != "windows" {
		return true
	}
	// SECURITY hive at HKLM\SECURITY is only readable by administrators.
	// Use Run() instead of Start()+Wait() to avoid compiler quirks.
	cmd := exec.Command("reg", "query", "HKLM\\SECURITY\\Policy\\System")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}


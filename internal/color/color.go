package color

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

const (
	reset   = "\033[0m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
	gray    = "\033[90m"
	bold    = "\033[1m"
)

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func isAnsiCapableTerminal() bool {
	if runtime.GOOS != "windows" {
		return isTerminal()
	}
	if force, ok := os.LookupEnv("COLOR"); ok {
		return force == "1"
	}
	for _, name := range []string{
		"WT_SESSION",
		"TERM_PROGRAM",
		"ConEmuANSI",
		"VSCODE_PID",
		"WSLENV",
		"SSH_CONNECTION",
	} {
		if v, ok := os.LookupEnv(name); ok {
			if name == "ConEmuANSI" && v != "ON" {
				continue
			}
			return true
		}
	}
	return false
}

func Enabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" || os.Getenv("TERM") == "none" {
		return false
	}
	return isAnsiCapableTerminal()
}

func Red(s string) string     { return apply(red, s) }
func Green(s string) string   { return apply(green, s) }
func Yellow(s string) string  { return apply(yellow, s) }
func Blue(s string) string    { return apply(blue, s) }
func Magenta(s string) string { return apply(magenta, s) }
func Cyan(s string) string    { return apply(cyan, s) }
func Gray(s string) string    { return apply(gray, s) }
func White(s string) string   { return apply(white, s) }
func Bold(s string) string    { return apply(bold, s) }

func apply(code, s string) string {
	if !Enabled() {
		return s
	}
	return code + s + reset
}

func SuccessIcon() string { return apply(green, "✅") }
func FailIcon() string    { return apply(red, "❌") }
func WarnIcon() string    { return apply(yellow, "⚠") }
func LinkedIcon() string  { return apply(magenta, "🔗") }
func ConfigIcon() string  { return apply(cyan, "⚙️") }
func SectionLabel(label string) string { return apply(bold, label) }
func Separator(length int) string {
	if !Enabled() {
		return strings.Repeat("-", length)
	}
	return apply(gray, strings.Repeat("-", length))
}
func Format(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
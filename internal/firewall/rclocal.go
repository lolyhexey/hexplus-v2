package firewall

import (
	"os"
	"strings"

	"github.com/lolyhexey/hexplus/internal/atomicfile"
)

// RCLocalPath is the boot script hexplus persists its iptables rules in.
const RCLocalPath = "/etc/rc.local"

const rcLocalSkeleton = "#!/bin/sh -e\nexit 0\n"

// addRCLocalLine returns content with line inserted before the final
// "exit 0" (appended if there is none). Unchanged if line is present.
func addRCLocalLine(content, line string) string {
	if strings.TrimSpace(content) == "" {
		content = rcLocalSkeleton
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == line {
			return content
		}
	}
	at := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "exit 0" {
			at = i
			break
		}
	}
	out := append([]string{}, lines[:at]...)
	out = append(out, line)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n") + "\n"
}

// removeRCLocalLines returns content without the lines drop matches.
func removeRCLocalLines(content string, drop func(line string) bool) string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		if !drop(strings.TrimSpace(l)) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// AddRCLocalLine persists line in the rc.local at path, creating the file
// if needed. It is a no-op when the line is already there.
func AddRCLocalLine(path, line string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out := addRCLocalLine(string(raw), line)
	if out == string(raw) {
		return nil
	}
	return atomicfile.Write(path, []byte(out), 0o755)
}

// RemoveRCLocalLines deletes the lines drop matches from the rc.local at
// path. A missing file is not an error.
func RemoveRCLocalLines(path string, drop func(line string) bool) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	out := removeRCLocalLines(string(raw), drop)
	if out == string(raw) {
		return nil
	}
	return atomicfile.Write(path, []byte(out), 0o755)
}

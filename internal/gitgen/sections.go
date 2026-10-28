package gitgen

import (
	"regexp"
	"strings"
)

var subsectionHeader = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9.-]+)\s+"((?:[^"\\]|\\.)*)"\s*\]`)

func RemoveSectionOutsideBlock(content, section, subsection string) (string, int) {
	lines := strings.SplitAfter(content, "\n")
	var out []string
	inBlock, skipping, removed := false, false, 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == BlockStart:
			inBlock, skipping = true, false
		case trimmed == BlockEnd:
			inBlock = false
		case !inBlock && strings.HasPrefix(trimmed, "["):
			skipping = matchesHeader(trimmed, section, subsection)
			if skipping {
				removed++
			}
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, ""), removed
}

func matchesHeader(line, section, subsection string) bool {
	m := subsectionHeader.FindStringSubmatch(line)
	if m == nil || !strings.EqualFold(m[1], section) {
		return false
	}
	unescaped := strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[2])
	return unescaped == subsection
}

func (a *Applier) RemoveLegacySection(section, subsection string) (backup string, removed int, err error) {
	current, err := readOptional(a.GlobalConfig)
	if err != nil {
		return "", 0, err
	}
	updated, removed := RemoveSectionOutsideBlock(current, section, subsection)
	if removed == 0 {
		return "", 0, nil
	}
	if backup, err = a.Backup(a.GlobalConfig); err != nil {
		return "", 0, err
	}
	return backup, removed, atomicWrite(a.GlobalConfig, updated)
}

package input

import (
	"os"
	"path/filepath"
	"strings"
)

type fileCompleter struct{}

func (fc *fileCompleter) Do(line []rune, pos int) ([][]rune, int) {
	// Scan backward from cursor to find the nearest '@' that starts a word.
	atIdx := -1
	for i := pos - 1; i >= 0; i-- {
		if line[i] == '@' {
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				atIdx = i
			}
			break
		}
		if line[i] == ' ' || line[i] == '\t' {
			break
		}
	}
	if atIdx < 0 {
		return nil, 0
	}

	partial := string(line[atIdx+1 : pos])
	dir, prefix := filepath.Split(partial)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}

	var candidates [][]rune
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		suffix := name[len(prefix):]
		if e.IsDir() {
			suffix += "/"
		}
		candidates = append(candidates, []rune(suffix))
	}

	return candidates, len(partial)
}

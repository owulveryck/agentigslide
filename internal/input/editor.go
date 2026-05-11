package input

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func editorCommand() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return "vi"
}

func openEditor(content string) (string, error) {
	f, err := os.CreateTemp("", "slidegen-outline-*.md")
	if err != nil {
		return content, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return content, fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	editor := editorCommand()
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return content, fmt.Errorf("editor %q: %w", editor, err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return content, fmt.Errorf("read back: %w", err)
	}
	return string(data), nil
}

const editorHeader = "% Editez ce plan. Vos modifications seront envoyees comme feedback a Claude.\n% Les lignes commencant par % seront ignorees.\n\n"

func processEditorOutput(original, edited string) string {
	var lines []string
	for _, l := range strings.Split(edited, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "%") {
			lines = append(lines, l)
		}
	}
	result := strings.TrimSpace(strings.Join(lines, "\n"))
	if result == strings.TrimSpace(original) {
		return ""
	}
	return result
}

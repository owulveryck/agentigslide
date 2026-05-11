package input

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/ergochat/readline"
)

type Config struct {
	HistoryFile string
}

type Reader struct {
	cfg Config
	rl  *readline.Instance
}

func New(cfg Config) (*Reader, error) {
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       "> ",
		HistoryFile:  cfg.HistoryFile,
		HistoryLimit: 500,
		AutoComplete: &fileCompleter{},
		Stderr:       os.Stderr,
	})
	if err != nil {
		return nil, err
	}
	return &Reader{cfg: cfg, rl: rl}, nil
}

func (r *Reader) ReadMultiLine() (string, error) {
	fmt.Fprintln(os.Stderr, "Decrivez votre presentation (@fichier pour importer, ligne vide pour envoyer) :")
	r.rl.SetPrompt("> ")

	var lines []string
	for {
		line, err := r.rl.ReadLine()
		if err == readline.ErrInterrupt {
			if len(lines) == 0 {
				return "", fmt.Errorf("interrupted")
			}
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if line == "" {
			break
		}
		lines = append(lines, ExpandFileRefs(line))
	}

	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", fmt.Errorf("empty input")
	}
	return text, nil
}

func (r *Reader) ReadFeedback(formattedOutline string) (string, error) {
	fmt.Fprint(os.Stderr, formattedOutline)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Feedback pour affiner, /edit pour ouvrir dans $EDITOR, ou Enter / \"go\" pour lancer :")
	r.rl.SetPrompt("> ")

	line, err := r.rl.ReadLine()
	if err == readline.ErrInterrupt || err == io.EOF {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)

	if line == "/edit" || line == "e" {
		return r.handleEditorFeedback(formattedOutline)
	}

	switch strings.ToLower(line) {
	case "", "ok", "done", "yes", "y", "go", "lance", "lgtm":
		return "", nil
	}

	return ExpandFileRefs(line), nil
}

func (r *Reader) handleEditorFeedback(formattedOutline string) (string, error) {
	r.rl.Close()
	defer r.reopen()

	edited, err := openEditor(editorHeader + formattedOutline)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur editeur: %v\n", err)
		return "", nil
	}

	return processEditorOutput(formattedOutline, edited), nil
}

func (r *Reader) reopen() {
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       "> ",
		HistoryFile:  r.cfg.HistoryFile,
		HistoryLimit: 500,
		AutoComplete: &fileCompleter{},
		Stderr:       os.Stderr,
	})
	if err == nil {
		r.rl = rl
	}
}

func (r *Reader) Close() {
	if r.rl != nil {
		r.rl.Close()
	}
}

var fileRefPattern = regexp.MustCompile(`@(\S+)`)

func ExpandFileRefs(line string) string {
	return fileRefPattern.ReplaceAllStringFunc(line, func(match string) string {
		path := match[1:]
		data, err := os.ReadFile(path)
		if err != nil {
			return match
		}
		return strings.TrimSpace(string(data))
	})
}

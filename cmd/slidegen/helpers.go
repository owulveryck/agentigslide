package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/monitor"
)

// readUserRequest reads the user request from a file or stdin.
func readUserRequest(filePath string) []byte {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Failed to read file: %v", err)
		}
		if len(data) == 0 {
			log.Fatal("Empty input")
		}
		return data
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		printUsage()
		os.Exit(1)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin: %v", err)
	}
	if len(data) == 0 {
		log.Fatal("Empty input")
	}
	return data
}

// hasUserRequest returns true if a user request is available (via --file flag
// or piped stdin).
func hasUserRequest(filePath string) bool {
	if filePath != "" {
		return true
	}
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// loadPlanFromFile reads and parses a PresentationPlan JSON from a file path,
// or from stdin if path is "-".
func loadPlanFromFile(path string) *model.PresentationPlan {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		log.Fatalf("Failed to read plan: %v", err)
	}

	var p model.PresentationPlan
	if err := json.Unmarshal(data, &p); err != nil {
		log.Fatalf("Failed to parse plan: %v", err)
	}

	if len(p.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	return &p
}

// savePlanToTempFile writes the PresentationPlan as indented JSON to a
// temporary file and returns the file path.
func savePlanToTempFile(p *model.PresentationPlan) (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan: %w", err)
	}
	f, err := os.CreateTemp("", "slidegen-plan-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("failed to write plan: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close plan file: %w", err)
	}
	return name, nil
}

// fatalWithPlanDump saves the plan to a temp file, prints recovery instructions
// to stderr, then exits with a fatal error.
func fatalWithPlanDump(p *model.PresentationPlan, mon *monitor.Monitor, format string, args ...any) {
	if p != nil {
		path, saveErr := savePlanToTempFile(p)
		if saveErr != nil {
			slog.Error("failed to save plan for recovery", "error", saveErr)
		} else {
			fmt.Fprintf(os.Stderr, "\nPlan saved to: %s\n", path)
			fmt.Fprintf(os.Stderr, "To retry:  slidegen --plan %s\n", path)
			fmt.Fprintf(os.Stderr, "To amend:  slidegen --plan %s --file amendments.md\n\n", path)
		}
	}
	if mon != nil {
		mon.SendError(fmt.Sprintf(format, args...))
		time.Sleep(500 * time.Millisecond)
	}
	log.Fatalf(format, args...)
}

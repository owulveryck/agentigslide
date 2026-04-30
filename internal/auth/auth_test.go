package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestSaveTokenAndTokenFromFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	tok := &oauth2.Token{
		AccessToken: "test-access-token-12345",
		TokenType:   "Bearer",
	}

	if err := saveToken(path, tok); err != nil {
		t.Fatalf("saveToken failed: %v", err)
	}

	got, err := tokenFromFile(path)
	if err != nil {
		t.Fatalf("tokenFromFile failed: %v", err)
	}

	if got.AccessToken != tok.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, tok.AccessToken)
	}
	if got.TokenType != tok.TokenType {
		t.Errorf("TokenType = %q, want %q", got.TokenType, tok.TokenType)
	}
}

func TestTokenFromFileNonExistent(t *testing.T) {
	_, err := tokenFromFile("/nonexistent/path/token.json")
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestTokenFromFileInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("not valid json{{{"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := tokenFromFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestTokenCachePath(t *testing.T) {
	p := tokenCachePath()

	suffix := filepath.Join(".credentials", "slideappscripter-token.json")
	if !strings.HasSuffix(p, suffix) {
		t.Errorf("tokenCachePath() = %q, want suffix %q", p, suffix)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not get user home dir: %v", err)
	}
	if !strings.HasPrefix(p, home) {
		t.Errorf("tokenCachePath() = %q, want prefix %q (home dir)", p, home)
	}
}

func TestGetOAuthClientNonExistentCredentials(t *testing.T) {
	_, err := GetOAuthClient(context.Background(), "/nonexistent/credentials.json")
	if err == nil {
		t.Fatal("expected error for non-existent credentials file, got nil")
	}
	if !strings.Contains(err.Error(), "unable to read credentials file") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "unable to read credentials file")
	}
}

func TestGetOAuthClientInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	if err := os.WriteFile(path, []byte("not json at all!!!"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := GetOAuthClient(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for invalid JSON credentials, got nil")
	}
}

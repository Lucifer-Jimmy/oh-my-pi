package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstRunWritesAnthropicSkeletonWithoutAKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	var out strings.Builder
	ok, err := runFirstRun(strings.NewReader("1\nclaude-sonnet-5\n"), &out)
	if err != nil || !ok {
		t.Fatalf("runFirstRun = %v, %v", ok, err)
	}
	path := filepath.Join(home, ".omp", "config.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	body := string(b)
	for _, want := range []string{"providers:", "anthropic:", "modelRoles:", "default: anthropic/claude-sonnet-5", "apiKey:"} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "PUT-YOUR-KEY") {
		t.Errorf("the key must be a placeholder the user replaces, not something the wizard asks for:\n%s", body)
	}
	if strings.Contains(out.String(), "API key:") {
		t.Errorf("the wizard must never prompt for a key:\n%s", out.String())
	}
}

func TestFirstRunWritesCustomProviderWithBaseURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	var out strings.Builder
	ok, err := runFirstRun(strings.NewReader("3\nhttps://api.deepseek.com/v1\ndeepseek\n"), &out)
	if err != nil || !ok {
		t.Fatalf("runFirstRun = %v, %v", ok, err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".omp", "config.yml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	body := string(b)
	for _, want := range []string{"deepseek:", "api: openai-completions", "baseUrl: https://api.deepseek.com/v1", "default: deepseek/PUT-MODEL-ID-HERE"} {
		if !strings.Contains(body, want) {
			t.Errorf("custom provider config missing %q:\n%s", want, body)
		}
	}
}

func TestFirstRunRefusesAnUnknownProvider(t *testing.T) {
	var out strings.Builder
	ok, err := runFirstRun(strings.NewReader("9\n"), &out)
	if err == nil || ok {
		t.Fatalf("an unknown choice must stop without writing, got ok=%v err=%v", ok, err)
	}
}

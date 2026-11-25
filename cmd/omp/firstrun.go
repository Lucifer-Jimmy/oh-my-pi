package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type firstRunAnswers struct {
	Provider string
	Model    string
}

func promptLine(in *bufio.Reader, out io.Writer, question, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(out, "%s [%s]: ", question, fallback)
	} else {
		fmt.Fprintf(out, "%s: ", question)
	}
	line, err := in.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		answer = fallback
	}
	if answer == "" {
		return "", errors.New("an answer is required")
	}
	return answer, nil
}

func askFirstRun(in *bufio.Reader, out io.Writer) (firstRunAnswers, error) {
	fmt.Fprintln(out, "First run: omp has no model configured.")
	fmt.Fprintln(out, "Answer three questions and the config file is written for you.")
	fmt.Fprintln(out, "API keys are never asked for here; you add one line to the file afterwards.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Providers:")
	fmt.Fprintln(out, "    1  anthropic   (Claude, api.anthropic.com)")
	fmt.Fprintln(out, "    2  openai      (OpenAI, api.openai.com)")
	fmt.Fprintln(out, "    3  other       (any OpenAI-compatible base URL)")
	choice, err := promptLine(in, out, "Provider", "1")
	if err != nil {
		return firstRunAnswers{}, err
	}
	var provider string
	switch choice {
	case "1", "anthropic":
		provider = "anthropic"
	case "2", "openai":
		provider = "openai"
	case "3", "other":
		url, err := promptLine(in, out, "Base URL (for example https://api.deepseek.com/v1)", "")
		if err != nil {
			return firstRunAnswers{}, err
		}
		name, err := promptLine(in, out, "A short name for this provider", "custom")
		if err != nil {
			return firstRunAnswers{}, err
		}
		a := firstRunAnswers{Provider: name, Model: url}
		return a, nil
	default:
		return firstRunAnswers{}, fmt.Errorf("unknown provider choice %q", choice)
	}
	model, err := promptLine(in, out, "Model id (the part after the provider, e.g. claude-sonnet-5)", "")
	if err != nil {
		return firstRunAnswers{}, err
	}
	return firstRunAnswers{Provider: provider, Model: model}, nil
}

func renderFirstRunConfig(a firstRunAnswers) string {
	var sb strings.Builder
	sb.WriteString("# written by omp's first-run wizard\n")
	sb.WriteString("# put your API key where the placeholder is: either the key itself or\n")
	sb.WriteString("# a !command whose stdout is the key. Keys are never read from the environment.\n")
	sb.WriteString("providers:\n")
	sb.WriteString("  " + a.Provider + ":\n")
	if strings.HasPrefix(a.Model, "http") {
		sb.WriteString("    api: openai-completions\n")
		sb.WriteString("    baseUrl: " + a.Model + "\n")
		sb.WriteString("    apiKey: \"!echo PUT-YOUR-KEY-COMMAND-HERE\"\n")
		sb.WriteString("modelRoles:\n")
		sb.WriteString("  default: " + a.Provider + "/PUT-MODEL-ID-HERE\n")
		return sb.String()
	}
	sb.WriteString("    apiKey: \"!echo PUT-YOUR-KEY-COMMAND-HERE\"\n")
	sb.WriteString("modelRoles:\n")
	sb.WriteString("  default: " + a.Provider + "/" + a.Model + "\n")
	return sb.String()
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".omp", "config.yml"), nil
}

func runFirstRun(in io.Reader, out io.Writer) (bool, error) {
	answers, err := askFirstRun(bufio.NewReader(in), out)
	if err != nil {
		return false, err
	}
	path, err := configPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(renderFirstRunConfig(answers)), 0o644); err != nil {
		return false, err
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Wrote %s\n", path)
	fmt.Fprintln(out, "Open it and replace the apiKey placeholder, then run omp again.")
	return true, nil
}

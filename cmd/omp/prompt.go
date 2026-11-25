package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

func buildPrompts(piped string, positional []string) []string {
	if piped == "" {
		return positional
	}
	if len(positional) == 0 {
		return []string{piped}
	}
	return append([]string{piped + "\n" + positional[0]}, positional[1:]...)
}

func readPiped(r io.Reader, stderr io.Writer) (string, error) {
	notice := time.AfterFunc(time.Second, func() {
		fmt.Fprintln(stderr, "Reading prompt from piped stdin (waiting for EOF, Ctrl+C to abort)...")
	})
	defer notice.Stop()
	b, err := io.ReadAll(r)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "", err
	}
	return string(b), nil
}

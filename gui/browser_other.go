//go:build !windows && !darwin && !linux

package gui

import "errors"

func openBrowser(string) error {
	return errors.New("opening a browser is not supported on this platform; open the printed URL yourself")
}

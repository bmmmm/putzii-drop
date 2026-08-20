// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bmmmm/putzii-drop/internal/config"
)

func cmdConfig(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dropii config path|edit|template")
	}
	switch args[0] {
	case "path":
		abs, _ := filepath.Abs(config.DefaultPath())
		fmt.Println(abs)
		return nil
	case "edit":
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, config.DefaultPath())
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	case "template":
		fmt.Print(config.Template())
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

func Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(LaunchAgentPlist(exe)), 0o644); err != nil {
		return err
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", uid, p).Run() // ignore if not loaded
	return exec.Command("launchctl", "bootstrap", uid, p).Run()
}

func Uninstall() error {
	p := plistPath()
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", uid, p).Run()
	return os.Remove(p)
}

func Status() (string, error) {
	out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), Label)).CombinedOutput()
	if err != nil {
		return "not running", nil
	}
	return string(out), nil
}

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/snowarch/mak1zu/provider"
)

func isTTY(f *os.File) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}

// readSecret reads a line from the terminal without echoing it.
func readSecret(r *bufio.Reader) string {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old))); e == 0 {
		quiet := old
		quiet.Lflag &^= syscall.ECHO
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&quiet)))
		defer func() {
			syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
			fmt.Println()
		}()
	}
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// chooseProvider asks which backend to use and, for hosted ones, for the key.
// It only runs on a real terminal; scripts use --provider and --key.
func chooseProvider() (p provider.Preset, key string, err error) {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("which model provider?")
	for i, pr := range provider.Presets {
		fmt.Printf("  %2d  %-26s %s\n", i+1, pr.Label, pr.Model)
	}
	fmt.Print("number (or a preset id): ")
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if n, e := strconv.Atoi(line); e == nil && n >= 1 && n <= len(provider.Presets) {
		p = provider.Presets[n-1]
	} else if pr, ok := provider.PresetByID(line); ok {
		p = pr
	} else {
		return p, "", fmt.Errorf("%q is not one of the presets", line)
	}
	if p.Note != "" {
		fmt.Println("note:", p.Note)
	}
	if p.KeyEnv == "" {
		return p, "", nil
	}
	if p.KeyURL != "" {
		fmt.Printf("get a key at %s\n", p.KeyURL)
	}
	fmt.Printf("paste it (hidden; it only goes into .makizu/.env as %s), or enter to add it later: ", p.KeyEnv)
	return p, readSecret(r), nil
}

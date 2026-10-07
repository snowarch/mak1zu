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
	fmt.Println("which model provider? (more detail: mak1zu providers, docs/PROVIDERS.md)")
	for i, pr := range provider.Presets {
		fmt.Printf("  %2d  %-26s %s\n", i+1, pr.Label, pr.Cost)
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

// cmdProviders prints every preset so a script or a person can pick one
// without opening the docs.
func cmdProviders() {
	for _, p := range provider.Presets {
		fmt.Printf("%-16s %s\n", p.ID, p.Label)
		fmt.Printf("  cost    %s\n  model   %s\n", p.Cost, p.Model)
		switch {
		case p.KeyEnv == "":
			fmt.Println("  key     none needed")
		default:
			fmt.Printf("  key     %s  (get one: %s)\n", p.KeyEnv, p.KeyURL)
		}
		if p.Note != "" {
			fmt.Printf("  note    %s\n", p.Note)
		}
		fmt.Println()
	}
	fmt.Println("use one:  mak1zu init --provider <id>")
}

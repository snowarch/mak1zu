package main

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// ui is how the wizard talks to a person on a real terminal: arrow keys,
// type-to-filter lists, a hidden key prompt. The wizard keeps its plain
// line-by-line path for pipes and tests, so nothing about the questions
// changes, only how they are asked.
type ui interface {
	pick(title string, options []string) (int, error)
	input(title string, secret bool) (string, error)
	confirm(title string) (bool, error)
}

type huhUI struct{}

var errCancelled = errors.New("cancelled")

// theme is the brand: Charm's base with one shu accent.
func theme(isDark bool) *huh.Styles {
	s := huh.ThemeCharm(isDark)
	shu := lipgloss.Color("#E4412B")
	s.Focused.Title = s.Focused.Title.Foreground(shu).Bold(true)
	s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(shu)
	s.Focused.NextIndicator = s.Focused.NextIndicator.Foreground(shu)
	s.Focused.PrevIndicator = s.Focused.PrevIndicator.Foreground(shu)
	s.Focused.FocusedButton = s.Focused.FocusedButton.Background(shu)
	return s
}

func cancelled(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return errCancelled
	}
	return err
}

func (huhUI) pick(title string, options []string) (int, error) {
	var idx int
	opts := make([]huh.Option[int], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, i)
	}
	sel := huh.NewSelect[int]().Options(opts...).Value(&idx)
	if len(options) > 8 {
		// with filtering on, huh shows the filter box where the title would be
		fmt.Println(title)
		sel.Filtering(true).Height(min(len(options)+2, 16))
	} else {
		sel.Title(title)
	}
	err := sel.WithTheme(huh.ThemeFunc(theme)).Run()
	return idx, cancelled(err)
}

func (huhUI) input(title string, secret bool) (string, error) {
	var v string
	in := huh.NewInput().Title(strings.TrimRight(strings.TrimSpace(title), ":")).Value(&v).Password(secret)
	err := in.WithTheme(huh.ThemeFunc(theme)).Run()
	return strings.TrimSpace(v), cancelled(err)
}

func (huhUI) confirm(title string) (bool, error) {
	var v bool
	err := huh.NewConfirm().Title(strings.TrimRight(strings.TrimSpace(title), "?[]yYnN/: ") + "?").Value(&v).
		WithTheme(huh.ThemeFunc(theme)).Run()
	return v, cancelled(err)
}

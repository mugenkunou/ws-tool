package editor

import (
	"slices"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestResolveOrder(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		env  map[string]string
		want []string
	}{
		{"config wins", "code -w", map[string]string{"VISUAL": "nano", "EDITOR": "emacs"}, []string{"code", "-w"}},
		{"VISUAL before EDITOR", "", map[string]string{"VISUAL": "nano", "EDITOR": "emacs"}, []string{"nano"}},
		{"EDITOR", "", map[string]string{"EDITOR": "nvim -p"}, []string{"nvim", "-p"}},
		{"blank values are unset", "  ", map[string]string{"VISUAL": " ", "EDITOR": ""}, []string{"vim"}},
		{"vim by default", "", nil, []string{"vim"}},
	}
	for _, c := range cases {
		if got := Resolve(c.cfg, env(c.env)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSplit(t *testing.T) {
	cases := map[string][]string{
		`vim`:                    {"vim"},
		`code -w --new-window`:   {"code", "-w", "--new-window"},
		`emacs -nw`:              {"emacs", "-nw"},
		`"/opt/my editor/ed" -x`: {"/opt/my editor/ed", "-x"},
		`sh -c 'vim "$@"'`:       {"sh", "-c", `vim "$@"`},
		`  spaced   out  `:       {"spaced", "out"},
		`a\ b c`:                 {"a b", "c"},
		``:                       nil,
	}
	for in, want := range cases {
		if got := Split(in); !slices.Equal(got, want) {
			t.Errorf("Split(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	yes := [][]string{{"vim"}, {"nvim", "-p"}, {"vi"}, {"nano"}, {"/usr/bin/hx"}, {"emacs", "-nw"}, {"micro"}, {"kak"}}
	no := [][]string{{"code"}, {"code", "-w"}, {"subl"}, {"emacs"}, {"gvim"}, {"idea"}, nil}
	for _, a := range yes {
		if !IsTerminal(a) {
			t.Errorf("%v should be a terminal editor", a)
		}
	}
	for _, a := range no {
		if IsTerminal(a) {
			t.Errorf("%v should not be a terminal editor", a)
		}
	}
}

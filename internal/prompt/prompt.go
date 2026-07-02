// Package prompt provides small interactive stdin helpers for the config
// wizard. Prompts are written to stderr so piped stdout stays clean and answers
// can be fed via stdin in tests.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Prompter reads answers from an input and writes prompts to an output.
type Prompter struct {
	r *bufio.Reader
	w io.Writer
}

// New returns a Prompter reading os.Stdin and writing prompts to os.Stderr.
func New() *Prompter { return &Prompter{r: bufio.NewReader(os.Stdin), w: os.Stderr} }

// Ask prompts with an optional default and returns the trimmed answer (or the
// default on an empty line).
func (p *Prompter) Ask(label, def string) string {
	if def != "" {
		fmt.Fprintf(p.w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.w, "%s: ", label)
	}
	line, _ := p.r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// AskRequired re-prompts until a non-empty value is given.
func (p *Prompter) AskRequired(label string) string {
	for {
		if v := p.Ask(label, ""); v != "" {
			return v
		}
		fmt.Fprintln(p.w, "  (required)")
	}
}

// AskChoice re-prompts until the answer is one of choices.
func (p *Prompter) AskChoice(label string, choices []string, def string) string {
	hint := fmt.Sprintf("%s (%s)", label, strings.Join(choices, "|"))
	for {
		v := p.Ask(hint, def)
		for _, c := range choices {
			if v == c {
				return v
			}
		}
		fmt.Fprintf(p.w, "  (choose one of: %s)\n", strings.Join(choices, ", "))
	}
}

// AskInt prompts for an integer, re-prompting on a non-numeric answer.
func (p *Prompter) AskInt(label string, def int) int {
	for {
		v := p.Ask(label, strconv.Itoa(def))
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		fmt.Fprintln(p.w, "  (enter a number)")
	}
}

// AskBool prompts a yes/no question with a default.
func (p *Prompter) AskBool(label string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	v := strings.ToLower(p.Ask(label+" ("+hint+")", ""))
	if v == "" {
		return def
	}
	return v == "y" || v == "yes"
}

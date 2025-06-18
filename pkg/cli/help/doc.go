// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
// SPDX-FileCopyrightText: The KCP Authors

package help

import (
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/MakeNowJust/heredoc"
	"github.com/muesli/reflow/wordwrap"
	"github.com/spf13/cobra"

	"k8s.io/component-base/term"
)

var reEmptyLine = regexp.MustCompile(`(?m)([\w[:punct:]]) *\n([\w[:punct:]])`)

func Doc(s string) string {
	s = heredoc.Doc(s)
	s = reEmptyLine.ReplaceAllString(s, "$1 $2")
	return s
}

func FitTerminal(out io.Writer) {
	cols, _, err := term.TerminalSize(out)
	if err != nil {
		cols = 80
	}

	cobra.AddTemplateFunc("trimTrailingWhitespaces", func(s string) string {
		return strings.TrimRightFunc(wordwrap.String(s, cols), unicode.IsSpace)
	})
}

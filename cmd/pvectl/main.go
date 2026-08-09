package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/davegallant/pvectl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// A command that ran something inside a guest reports that guest
		// command's exit status rather than a pvectl error: `ct exec`
		// surfaces SSH's status as *exec.ExitError, `qm exec` surfaces the
		// guest agent's as its own type. Both expose ExitCode(), so one
		// interface check covers both — and neither prints an "error:"
		// line, since the remote command's own output already said
		// whatever there was to say.
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

package cmd

import (
	"errors"
	"os/exec"
	"testing"
)

// main exits with a guest command's own status by looking for an
// ExitCode() method rather than a concrete type, so exitCodeError and
// *exec.ExitError (which ct exec produces via SSH) both work through the
// same check. This pins that shape — dropping the method would silently
// turn a guest's exit code into a generic exit 1.
func TestExitCodeErrorSatisfiesExitCoder(t *testing.T) {
	var err error = exitCodeError{code: 7}

	var coder interface{ ExitCode() int }
	if !errors.As(err, &coder) {
		t.Fatal("exitCodeError does not satisfy interface{ ExitCode() int }, so main would report exit 1 instead of the guest's status")
	}
	if got := coder.ExitCode(); got != 7 {
		t.Errorf("ExitCode() = %d, want 7", got)
	}

	// The same check must keep matching ct exec's error type.
	var sshErr error = &exec.ExitError{}
	if !errors.As(sshErr, &coder) {
		t.Error("*exec.ExitError no longer satisfies the interface main checks")
	}
}

func TestExitCodeErrorMessage(t *testing.T) {
	if got, want := (exitCodeError{code: 3}).Error(), "command exited with status 3"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

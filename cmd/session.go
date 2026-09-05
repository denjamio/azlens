package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// isInteractiveTerminal reports whether stdin and stdout are attached to a TTY.
// Interactive login is only launched on real terminals; CI runs fail fast
// instead of hanging on a device-code prompt nobody can answer.
func isInteractiveTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		info, err := f.Stat()
		if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
			return false
		}
	}
	return true
}

// azLoginArgs builds the az login arguments: '--tenant <id>' when the directory
// hosting the missing subscription is configured, plain 'az login' otherwise
func azLoginArgs(tenant string) []string {
	if tenant != "" {
		return []string{"login", "--tenant", tenant}
	}
	return []string{"login"}
}

// azLoginCmd builds the interactive 'az login' command for the given directory.
// The child process disables the v2 login experience (account picker) so the flow
// goes straight to authentication in the user's primary az profile.
func azLoginCmd(tenant string) *exec.Cmd {
	cmd := exec.Command("az", azLoginArgs(tenant)...)
	cmd.Env = append(os.Environ(), "AZURE_CORE_LOGIN_EXPERIENCE_V2=off")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// launchAzLogin hands the terminal over to 'az login' so the user can
// authenticate the directory owning the missing subscriptions
func launchAzLogin(tenant string) error {
	if tenant != "" {
		fmt.Fprintf(os.Stderr, "\n🔐 Launching 'az login --tenant %s' — authenticate the directory hosting the missing subscription (tokens stay inside the az CLI)...\n\n", tenant)
	} else {
		fmt.Fprintln(os.Stderr, "\n🔐 Launching 'az login' — pick the account/directory that owns the missing subscription (tokens stay inside the az CLI)...")
	}
	return azLoginCmd(tenant).Run()
}

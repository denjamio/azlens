package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:    "completion [bash|zsh|fish|powershell]",
	Short:  "Generate shell completion scripts for azlens",
	Hidden: true,
	Long: `To load completions:

Bash:
  $ source <(azlens completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ azlens completion bash > /etc/bash_completion.d/azlens
  # macOS:
  $ azlens completion bash > $(brew --prefix)/etc/bash_completion.d/azlens

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ azlens completion zsh > "${fpath[1]}/_azlens"

Fish:
  $ azlens completion fish | source
  # To load completions for each session, execute once:
  $ azlens completion fish > ~/.config/fish/completions/azlens.fish

PowerShell:
  PS> azlens completion powershell | Out-String | Invoke-Expression
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(completionCmd)
}

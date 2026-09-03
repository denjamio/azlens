package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
)

// completeTelemetryValue builds a cobra flag completion function that completes
// --role / --pod with the values declared in the config file (shared target +
// profile overrides, already merged). One path: nothing declared means nothing
// completed — filters are picked from the list, never typed from memory.
func completeTelemetryValue(kind string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		cfg, err := config.LoadConfig(configPathFlag)
		if err != nil || cfg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		profileName := cfg.GetDefaultProfile()
		if profileFlag != "" {
			profileName = profileFlag
		}
		prof, err := cfg.GetProfile(profileName)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var declared []string
		if kind == "role" {
			declared = prof.Target.Roles
		} else {
			declared = prof.Target.Pods
		}
		return prefixFilter(declared, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// prefixFilter keeps values starting with the word being completed
func prefixFilter(values []string, toComplete string) []string {
	if toComplete == "" {
		return values
	}
	var out []string
	for _, v := range values {
		if strings.HasPrefix(v, toComplete) {
			out = append(out, v)
		}
	}
	return out
}

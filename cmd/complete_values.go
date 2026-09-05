package cmd

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
)

// completeServiceValue builds a cobra flag completion function that completes
// --service / -s with the service names declared in the config file
// (shared.services + profile services).
func completeServiceValue() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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
		for name := range prof.Target.Services {
			declared = append(declared, name)
		}
		sort.Strings(declared)
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

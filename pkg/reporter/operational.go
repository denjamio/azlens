package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/denjamio/azlens/pkg/domain"
)

// PrintOperationalTerminal renders the primary `azlens` operational status to terminal (Section 6.1).
func PrintOperationalTerminal(w io.Writer, res *domain.AnalysisResult) {
	if w == nil {
		w = os.Stdout
	}

	header := formatContextBanner(res.Profile, res.Scope, res.Window)
	fmt.Fprintf(w, "\n%s\n\n", header)

	switch res.State {
	case domain.HealthStateHealthy:
		colorGreen.Fprintln(w, "Everything looks normal.")
		if len(res.Watching) > 0 {
			fmt.Fprintln(w)
			printWatchingTerminal(w, res.Watching)
		}
		return

	case domain.HealthStateUnknown:
		msg := res.StatusMessage
		if msg == "" {
			msg = "AzLens lacks sufficient telemetry data to determine environment health."
		}
		colorYellow.Fprintln(w, msg)
		return

	case domain.HealthStateDegraded:
		numProbs := len(res.Problems)
		probWord := "problems need"
		if numProbs == 1 {
			probWord = "problem needs"
		}
		colorRed.Fprintf(w, "%d %s attention\n\n", numProbs, probWord)

		for i, p := range res.Problems {
			printProblemTerminal(w, p)
			if i < len(res.Problems)-1 {
				fmt.Fprintln(w)
			}
		}

		if len(res.Watching) > 0 {
			fmt.Fprintln(w)
			printWatchingTerminal(w, res.Watching)
		}
	}
}

func printProblemTerminal(w io.Writer, p domain.Problem) {
	// Symbol and Summary
	colorRed.Fprintf(w, "✕ %s\n\n", p.Summary)

	// Impact
	if p.Impact.P95Current != "" || p.Impact.ErrorCurrent != "" || p.Impact.TrafficPct > 0 || p.StartedAt != nil {
		if p.Impact.P95Current != "" {
			baseStr := p.Impact.P95Baseline
			if baseStr == "" {
				baseStr = "normal"
			}
			fmt.Fprintf(w, "  %-12s %s -> %s\n", "p95", baseStr, p.Impact.P95Current)
		}
		if p.Impact.ErrorCurrent != "" {
			baseStr := p.Impact.ErrorBaseline
			if baseStr == "" {
				baseStr = "0.0%"
			}
			fmt.Fprintf(w, "  %-12s %s -> %s\n", "error rate", baseStr, p.Impact.ErrorCurrent)
		}
		if p.Impact.TrafficPct > 0 {
			fmt.Fprintf(w, "  %-12s %.0f%% of traffic\n", "affects", p.Impact.TrafficPct)
		}
		if p.StartedAt != nil && !p.StartedAt.IsZero() {
			fmt.Fprintf(w, "  %-12s %s\n", "since", p.StartedAt.Format("15:04"))
		}
		fmt.Fprintln(w)
	}

	// Likely Cause
	if p.Cause != nil && p.Cause.Summary != "" {
		fmt.Fprintf(w, "  %s\n", color.CyanString("Likely cause"))
		fmt.Fprintf(w, "  %s\n\n", p.Cause.Summary)

		// Evidence
		if len(p.Cause.Evidence) > 0 {
			fmt.Fprintf(w, "  %s\n", color.CyanString("Evidence"))
			for idx, ev := range p.Cause.Evidence {
				prefix := "|-"
				if idx == len(p.Cause.Evidence)-1 {
					prefix = "`-"
				}
				fmt.Fprintf(w, "  %s %s\n", prefix, ev.Signal)
			}
			fmt.Fprintln(w)
		}
	} else if p.Kind == domain.ProblemKindDegradation {
		fmt.Fprintf(w, "  %s\n", color.YellowString("Cause is not clear yet."))
		fmt.Fprintln(w)
	}

	// Next Action
	if p.Action != nil {
		fmt.Fprintf(w, "  %s\n", color.CyanString("Next"))
		fmt.Fprintf(w, "  %s\n\n", p.Action.Summary)

		if p.Action.Command != nil && p.Action.Command.Display != "" {
			colorYellow.Fprintf(w, "  -> %s\n", p.Action.Command.Display)
		}
	}
}

func printWatchingTerminal(w io.Writer, watching []domain.WatchingItem) {
	numItems := len(watching)
	itemWord := "things worth watching"
	if numItems == 1 {
		itemWord = "thing worth watching"
	}
	colorYellow.Fprintf(w, "%d %s\n\n", numItems, itemWord)

	for _, item := range watching {
		colorYellow.Fprintf(w, "! %s\n", item.Summary)
		if item.Detail != "" {
			fmt.Fprintf(w, "  %s\n", item.Detail)
		}
		fmt.Fprintln(w)
	}
}

// PrintOperationalMarkdown renders primary operational status to Markdown (Section 18.3).
func PrintOperationalMarkdown(w io.Writer, res *domain.AnalysisResult) {
	if w == nil {
		w = os.Stdout
	}

	header := formatContextBanner(res.Profile, res.Scope, res.Window)
	fmt.Fprintf(w, "# %s\n\n", header)

	switch res.State {
	case domain.HealthStateHealthy:
		fmt.Fprintln(w, "**Everything looks normal.**")
		if len(res.Watching) > 0 {
			fmt.Fprintln(w)
			printWatchingMarkdown(w, res.Watching)
		}
		return

	case domain.HealthStateUnknown:
		fmt.Fprintf(w, "> **Warning**: %s\n\n", res.StatusMessage)
		return

	case domain.HealthStateDegraded:
		numProbs := len(res.Problems)
		probWord := "problems need"
		if numProbs == 1 {
			probWord = "problem needs"
		}
		fmt.Fprintf(w, "### ✕ %d %s attention\n\n", numProbs, probWord)

		for _, p := range res.Problems {
			printProblemMarkdown(w, p)
		}

		if len(res.Watching) > 0 {
			printWatchingMarkdown(w, res.Watching)
		}
	}
}

func printProblemMarkdown(w io.Writer, p domain.Problem) {
	fmt.Fprintf(w, "#### ✕ %s\n\n", p.Summary)

	if p.Impact.P95Current != "" || p.Impact.ErrorCurrent != "" || p.Impact.TrafficPct > 0 {
		fmt.Fprintln(w, "| Metric | Value |")
		fmt.Fprintln(w, "| --- | --- |")
		if p.Impact.P95Current != "" {
			fmt.Fprintf(w, "| p95 | %s -> %s |\n", p.Impact.P95Baseline, p.Impact.P95Current)
		}
		if p.Impact.ErrorCurrent != "" {
			fmt.Fprintf(w, "| error rate | %s -> %s |\n", p.Impact.ErrorBaseline, p.Impact.ErrorCurrent)
		}
		if p.Impact.TrafficPct > 0 {
			fmt.Fprintf(w, "| affects | %.0f%% of traffic |\n", p.Impact.TrafficPct)
		}
		if p.StartedAt != nil && !p.StartedAt.IsZero() {
			fmt.Fprintf(w, "| since | %s |\n", p.StartedAt.Format("15:04"))
		}
		fmt.Fprintln(w)
	}

	if p.Cause != nil && p.Cause.Summary != "" {
		fmt.Fprintf(w, "**Likely cause**\n\n`%s`\n\n", p.Cause.Summary)
		if len(p.Cause.Evidence) > 0 {
			fmt.Fprintln(w, "**Evidence**")
			fmt.Fprintln(w)
			for _, ev := range p.Cause.Evidence {
				fmt.Fprintf(w, "- %s\n", ev.Signal)
			}
			fmt.Fprintln(w)
		}
	} else if p.Kind == domain.ProblemKindDegradation {
		fmt.Fprintln(w, "*Cause is not clear yet.*")
		fmt.Fprintln(w)
	}

	if p.Action != nil {
		fmt.Fprintf(w, "**Next**: %s\n\n", p.Action.Summary)
		if p.Action.Command != nil && p.Action.Command.Display != "" {
			fmt.Fprintf(w, "```bash\n%s\n```\n\n", p.Action.Command.Display)
		}
	}
}

func printWatchingMarkdown(w io.Writer, watching []domain.WatchingItem) {
	fmt.Fprintf(w, "### ⚠️  Worth Watching (%d)\n\n", len(watching))
	for _, item := range watching {
		fmt.Fprintf(w, "- **%s**: %s\n", item.Summary, item.Detail)
	}
	fmt.Fprintln(w)
}

// PrintExplainTerminal renders `azlens explain [subject]` (Section 6.2).
func PrintExplainTerminal(w io.Writer, res *domain.AnalysisResult, problem *domain.Problem, subject string) {
	if w == nil {
		w = os.Stdout
	}

	servicePart := ""
	if svc := resolveScopeServiceName(res.Scope); svc != "" {
		servicePart = fmt.Sprintf(" · %s", svc)
	}
	subjPart := ""
	if subject != "" {
		subjPart = fmt.Sprintf(" · %s", subject)
	}
	header := fmt.Sprintf("%s%s%s · %s", res.Profile.DisplayName, servicePart, subjPart, res.Window.Label)
	fmt.Fprintf(w, "\n%s\n\n", header)

	if problem == nil {
		colorGreen.Fprintln(w, "No active problems detected for this subject.")
		return
	}

	colorRed.Fprintf(w, "%s.\n\n", problem.Summary)

	// Impact block
	fmt.Fprintf(w, "%s\n\n", color.CyanString("Impact"))
	if problem.Impact.P95Current != "" {
		baseStr := problem.Impact.P95Baseline
		if baseStr == "" {
			baseStr = "normal"
		}
		fmt.Fprintf(w, "  %-12s %s -> %s\n", "p95", baseStr, problem.Impact.P95Current)
	}
	if problem.Impact.ErrorCurrent != "" {
		baseStr := problem.Impact.ErrorBaseline
		if baseStr == "" {
			baseStr = "0.0%"
		}
		fmt.Fprintf(w, "  %-12s %s -> %s\n", "errors", baseStr, problem.Impact.ErrorCurrent)
	}
	if problem.Impact.TrafficPct > 0 {
		fmt.Fprintf(w, "  %-12s %.0f%%\n", "traffic", problem.Impact.TrafficPct)
	}
	fmt.Fprintln(w)

	// Cause & Why
	if problem.Cause != nil && problem.Cause.Summary != "" {
		fmt.Fprintf(w, "%s\n\n", color.CyanString("Likely cause"))
		fmt.Fprintf(w, "  %s\n\n", problem.Cause.Summary)

		if len(problem.Cause.Evidence) > 0 {
			fmt.Fprintf(w, "%s\n\n", color.CyanString("Why"))
			for _, ev := range problem.Cause.Evidence {
				fmt.Fprintf(w, "  %-24s\n", ev.Signal)
			}
			fmt.Fprintln(w)
		}

		strength := problem.Cause.Strength
		if strength == "" {
			strength = domain.EvidenceStrengthModerate
		}
		capitalizedStrength := strings.ToUpper(string(strength)[:1]) + string(strength)[1:]
		fmt.Fprintf(w, "%s\n\n", color.CyanString("Evidence strength"))
		fmt.Fprintf(w, "  %s\n\n", capitalizedStrength)
	} else {
		colorYellow.Fprintln(w, "Cause is not clear yet.")
		fmt.Fprintln(w)
	}

	// Next
	if problem.Action != nil {
		fmt.Fprintf(w, "%s\n\n", color.CyanString("Next"))
		fmt.Fprintf(w, "  %s\n\n", problem.Action.Summary)

		if problem.Action.Command != nil && problem.Action.Command.Display != "" {
			colorYellow.Fprintf(w, "  -> %s\n", problem.Action.Command.Display)
		}
	}
}

// PrintExplainMarkdown renders `azlens explain` to Markdown.
func PrintExplainMarkdown(w io.Writer, res *domain.AnalysisResult, problem *domain.Problem, subject string) {
	if w == nil {
		w = os.Stdout
	}

	servicePart := ""
	if svc := resolveScopeServiceName(res.Scope); svc != "" {
		servicePart = fmt.Sprintf(" · %s", svc)
	}
	subjPart := ""
	if subject != "" {
		subjPart = fmt.Sprintf(" · %s", subject)
	}
	fmt.Fprintf(w, "# %s%s%s · %s\n\n", res.Profile.DisplayName, servicePart, subjPart, res.Window.Label)

	if problem == nil {
		fmt.Fprintln(w, "No active problems detected for this subject.")
		return
	}

	fmt.Fprintf(w, "## %s.\n\n", problem.Summary)

	fmt.Fprintln(w, "### Impact")
	fmt.Fprintln(w)
	if problem.Impact.P95Current != "" {
		fmt.Fprintf(w, "- **p95**: %s -> %s\n", problem.Impact.P95Baseline, problem.Impact.P95Current)
	}
	if problem.Impact.ErrorCurrent != "" {
		fmt.Fprintf(w, "- **errors**: %s -> %s\n", problem.Impact.ErrorBaseline, problem.Impact.ErrorCurrent)
	}
	if problem.Impact.TrafficPct > 0 {
		fmt.Fprintf(w, "- **traffic**: %.0f%%\n", problem.Impact.TrafficPct)
	}
	fmt.Fprintln(w)

	if problem.Cause != nil && problem.Cause.Summary != "" {
		fmt.Fprintf(w, "### Likely cause\n\n`%s`\n\n", problem.Cause.Summary)
		if len(problem.Cause.Evidence) > 0 {
			fmt.Fprintln(w, "### Why")
			fmt.Fprintln(w)
			for _, ev := range problem.Cause.Evidence {
				fmt.Fprintf(w, "- %s\n", ev.Signal)
			}
			fmt.Fprintln(w)
		}
		strength := problem.Cause.Strength
		if strength == "" {
			strength = domain.EvidenceStrengthModerate
		}
		capitalizedStrength := strings.ToUpper(string(strength)[:1]) + string(strength)[1:]
		fmt.Fprintf(w, "### Evidence strength\n\n**%s**\n\n", capitalizedStrength)
	} else {
		fmt.Fprintln(w, "*Cause is not clear yet.*")
		fmt.Fprintln(w)
	}

	if problem.Action != nil {
		fmt.Fprintf(w, "### Next\n\n%s\n\n", problem.Action.Summary)
		if problem.Action.Command != nil && problem.Action.Command.Display != "" {
			fmt.Fprintf(w, "```bash\n%s\n```\n", problem.Action.Command.Display)
		}
	}
}

// PrintDeployTerminal renders `azlens deploy` to terminal (Section 6.4).
func PrintDeployTerminal(w io.Writer, res *domain.AnalysisResult, deployTimeLabel string) {
	if w == nil {
		w = os.Stdout
	}

	servicePart := ""
	if svc := resolveScopeServiceName(res.Scope); svc != "" {
		servicePart = fmt.Sprintf(" · %s", svc)
	}
	header := fmt.Sprintf("%s%s · deploy at %s", res.Profile.DisplayName, servicePart, deployTimeLabel)
	if deployTimeLabel == "" {
		header = fmt.Sprintf("%s%s · %s", res.Profile.DisplayName, servicePart, res.Window.Label)
	}
	fmt.Fprintf(w, "\n%s\n\n", header)

	if res.State == domain.HealthStateUnknown {
		colorYellow.Fprintln(w, "Deploy comparison is insufficient.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Baseline period has insufficient samples to determine safety.")
		return
	}

	if res.State == domain.HealthStateHealthy {
		colorGreen.Fprintln(w, "This deploy looks safe.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "No meaningful regression detected.")
		return
	}

	// Regressed
	for _, p := range res.Problems {
		subject := cleanSubject(p.Scope.String())
		if subject == "" {
			subject = "system"
		}
		colorRed.Fprintf(w, "This deploy made %s worse.\n\n", subject)

		if p.Impact.P95Current != "" {
			fmt.Fprintf(w, "%-14s %s\n", "p95", p.Impact.P95Current)
		}
		if p.Impact.ErrorCurrent != "" {
			fmt.Fprintf(w, "%-14s %s\n", "error rate", p.Impact.ErrorCurrent)
		}
		fmt.Fprintln(w)

		if p.Cause != nil && p.Cause.Summary != "" {
			fmt.Fprintf(w, "%s\n", color.CyanString("New"))
			fmt.Fprintf(w, "%s\n\n", p.Cause.Summary)
		}

		if p.Scope.Endpoint != "" {
			fmt.Fprintf(w, "%s\n", color.CyanString("Likely affected"))
			fmt.Fprintf(w, "%s\n\n", p.Scope.Endpoint)
		}

		colorYellow.Fprintf(w, "-> azlens explain %s\n", subject)
	}
}

// PrintDeployMarkdown renders `azlens deploy` to Markdown.
func PrintDeployMarkdown(w io.Writer, res *domain.AnalysisResult, deployTimeLabel string) {
	if w == nil {
		w = os.Stdout
	}

	servicePart := ""
	if svc := resolveScopeServiceName(res.Scope); svc != "" {
		servicePart = fmt.Sprintf(" · %s", svc)
	}
	header := fmt.Sprintf("%s%s · deploy at %s", res.Profile.DisplayName, servicePart, deployTimeLabel)
	if deployTimeLabel == "" {
		header = fmt.Sprintf("%s%s · %s", res.Profile.DisplayName, servicePart, res.Window.Label)
	}
	fmt.Fprintf(w, "# %s\n\n", header)

	if res.State == domain.HealthStateUnknown {
		fmt.Fprintln(w, "> **Warning**: Deploy comparison is insufficient.\n\nBaseline period has insufficient samples to determine safety.")
		return
	}

	if res.State == domain.HealthStateHealthy {
		fmt.Fprintln(w, "### This deploy looks safe.\n\nNo meaningful regression detected.")
		return
	}

	for _, p := range res.Problems {
		subject := cleanSubject(p.Scope.String())
		if subject == "" {
			subject = "system"
		}
		fmt.Fprintf(w, "### This deploy made %s worse.\n\n", subject)

		if p.Impact.P95Current != "" {
			fmt.Fprintf(w, "- **p95**: %s\n", p.Impact.P95Current)
		}
		if p.Impact.ErrorCurrent != "" {
			fmt.Fprintf(w, "- **error rate**: %s\n", p.Impact.ErrorCurrent)
		}
		fmt.Fprintln(w)

		if p.Cause != nil && p.Cause.Summary != "" {
			fmt.Fprintf(w, "**New**\n\n`%s`\n\n", p.Cause.Summary)
		}
		if p.Scope.Endpoint != "" {
			fmt.Fprintf(w, "**Likely affected**: `%s`\n\n", p.Scope.Endpoint)
		}

		fmt.Fprintf(w, "```bash\nazlens explain %s\n```\n", subject)
	}
}

// DoctorResult represents the diagnosis of authentication, backends, and capability coverage.
type DoctorResult struct {
	ProfileName string
	AzureAuth   bool
	AuthUser    string
	AuthSub     string
	Backends    []BackendStatus
	Coverage    []domain.CapabilityStatus
}

type BackendStatus struct {
	Name      string
	Available bool
	Details   string
}

// PrintDoctorTerminal renders `azlens doctor` output (Section 6.5).
func PrintDoctorTerminal(w io.Writer, doc *DoctorResult) {
	if w == nil {
		w = os.Stdout
	}

	fmt.Fprintf(w, "\n%s\n\n", color.CyanString(doc.ProfileName))

	// Azure section
	fmt.Fprintln(w, "Azure")
	fmt.Fprintln(w)
	if doc.AzureAuth {
		colorGreen.Fprintf(w, "✓ authenticated\n")
	} else {
		colorRed.Fprintf(w, "✗ not authenticated\n")
	}

	for _, b := range doc.Backends {
		if b.Available {
			colorGreen.Fprintf(w, "✓ %s\n", b.Name)
		} else {
			colorYellow.Fprintf(w, "! %s (%s)\n", b.Name, b.Details)
		}
	}
	fmt.Fprintln(w)

	// Coverage section
	fmt.Fprintln(w, "Coverage")
	fmt.Fprintln(w)
	var missing []domain.CapabilityStatus
	for _, c := range doc.Coverage {
		capName := formatCapabilityName(c.Capability)
		switch c.State {
		case domain.CapabilityStateAvailable:
			colorGreen.Fprintf(w, "✓ %s\n", capName)
		case domain.CapabilityStateStale:
			colorYellow.Fprintf(w, "! %s (stale)\n", capName)
		case domain.CapabilityStateUnavailable:
			missing = append(missing, c)
		case domain.CapabilityStateNotConfigured:
			// Not configured optional capabilities are not shown in available
		}
	}

	// Missing section
	if len(missing) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Missing")
		fmt.Fprintln(w)
		for _, m := range missing {
			capName := formatCapabilityName(m.Capability)
			colorYellow.Fprintf(w, "! %s\n\n", capName)
			if m.Consequence != "" {
				fmt.Fprintf(w, "%s\n\n", m.Consequence)
			}
		}
	}
}

func formatCapabilityName(cap domain.CapabilityType) string {
	switch cap {
	case domain.CapabilityRequests:
		return "requests"
	case domain.CapabilityDependencies:
		return "dependencies"
	case domain.CapabilityExceptions:
		return "exceptions"
	case domain.CapabilityAvailability:
		return "availability"
	case domain.CapabilityDatabaseSlowLogs:
		return "database slow logs"
	default:
		return string(cap)
	}
}

func resolveScopeServiceName(scope domain.ScopeContext) string {
	if scope.Service != "" {
		return scope.Service
	}
	return scope.Role
}

func formatContextBanner(prof domain.ProfileContext, scope domain.ScopeContext, win domain.WindowContext) string {
	name := prof.DisplayName
	if name == "" {
		name = prof.Name
	}
	if name == "" {
		name = "Production"
	}
	servicePart := ""
	if svc := resolveScopeServiceName(scope); svc != "" {
		servicePart = fmt.Sprintf(" · %s", svc)
	}
	label := win.Label
	if label == "" {
		label = "last 60m"
	}
	return fmt.Sprintf("%s%s · %s", name, servicePart, label)
}

func cleanSubject(raw string) string {
	return domain.CleanSubject(raw)
}

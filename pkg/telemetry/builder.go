// Package telemetry coordinates telemetry acquisition from Azure and constructs snapshots.
package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

// SnapshotBuilder coordinates telemetry acquisition from Azure and constructs
// a source-neutral Snapshot (Section 16).
type SnapshotBuilder struct {
	client azure.AzureClient
}

func NewSnapshotBuilder(client azure.AzureClient) *SnapshotBuilder {
	return &SnapshotBuilder{client: client}
}

// BuildSnapshot fetches current and baseline telemetry in parallel, assembling
// a complete domain.Snapshot for analysis.
func (b *SnapshotBuilder) BuildSnapshot(
	ctx context.Context,
	profileName string,
	prof config.Profile,
	start, end time.Time,
	windowLabel string,
) (*domain.Snapshot, error) {
	displayName := prof.Name
	if displayName == "" {
		displayName = profileName
	}

	primaryRole := ""
	if len(prof.Target.Roles) > 0 {
		primaryRole = prof.Target.Roles[0]
	}
	primaryPod := ""
	if len(prof.Target.Pods) > 0 {
		primaryPod = prof.Target.Pods[0]
	}

	dur := end.Sub(start)
	durationStr := dur.Round(time.Minute).String()

	snapshot := domain.NewSnapshot(
		domain.ProfileContext{
			Name:        profileName,
			DisplayName: displayName,
		},
		domain.Scope{
			Role:      primaryRole,
			Pod:       primaryPod,
			Namespace: prof.Target.Logs.Namespace,
		},
		domain.WindowContext{
			Label:    windowLabel,
			Duration: durationStr,
			Start:    start,
			End:      end,
		},
	)

	// Mark configured capabilities
	if prof.Target.Insights.Name != "" || prof.Target.Logs.WorkspaceID != "" {
		snapshot.ConfiguredCapabilities[domain.CapabilityRequests] = true
		snapshot.ConfiguredCapabilities[domain.CapabilityDependencies] = true
		snapshot.ConfiguredCapabilities[domain.CapabilityExceptions] = true
	}
	if prof.Target.Logs.Database != "" {
		snapshot.ConfiguredCapabilities[domain.CapabilityDatabaseSlowLogs] = true
	}
	if prof.Target.Logs.Namespace != "" || len(prof.Target.Pods) > 0 {
		snapshot.ConfiguredCapabilities[domain.CapabilityKubernetesWorkloads] = true
		snapshot.ConfiguredCapabilities[domain.CapabilityKubernetesEvents] = true
		snapshot.ConfiguredCapabilities[domain.CapabilityResourceSaturation] = true
	}

	// Calculate equal baseline window preceding the current window
	baseStart := start.Add(-dur)
	baseEnd := start

	var (
		wg      sync.WaitGroup
		baseWM  model.WindowMetrics
		currWM  model.WindowMetrics
		baseErr error
		currErr error
	)

	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()

	wg.Add(2)
	go func() {
		defer wg.Done()
		baseCtx := azure.WithBaseline(fetchCtx)
		baseWM, baseErr = b.client.QueryWindowMetrics(baseCtx, baseStart, baseEnd, 30)
		if baseErr != nil {
			cancelFetch()
		}
	}()

	go func() {
		defer wg.Done()
		currWM, currErr = b.client.QueryWindowMetrics(fetchCtx, start, end, 30)
		if currErr != nil {
			cancelFetch()
		}
	}()

	wg.Wait()

	if currErr != nil {
		snapshot.QueryErrors[domain.CapabilityRequests] = currErr
		return snapshot, fmt.Errorf("telemetry query failed: %w", currErr)
	}

	if baseErr == nil {
		snapshot.BaselineOverall = &baseWM.Overall
		snapshot.BaselineEndpoints = baseWM.Endpoints
		snapshot.BaselineDependencies = baseWM.Deps
		snapshot.BaselineExceptions = baseWM.Errors
		snapshot.BaselineFanout = baseWM.Fanout
	}

	snapshot.CurrentOverall = currWM.Overall
	snapshot.CurrentEndpoints = currWM.Endpoints
	snapshot.CurrentDependencies = currWM.Deps
	snapshot.CurrentExceptions = currWM.Errors
	snapshot.CurrentFanout = currWM.Fanout

	// Set freshness
	now := time.Now()
	if currWM.Overall.TotalCalls > 0 {
		snapshot.Freshness.RequestsLastSeen = &now
	}

	// If slow logs are configured, fetch them
	if prof.Target.Logs.Database != "" {
		slowGroups, err := b.client.QueryMySQLSlowLogsGrouped(ctx, start, end, prof.Target.Logs.Database, 15)
		if err == nil {
			snapshot.SlowLogs = slowGroups
		}
	}

	return snapshot, nil
}

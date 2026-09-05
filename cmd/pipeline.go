package cmd

import (
	"context"
	"io"
	"time"

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/telemetry"
)

// pipelineResult holds the telemetry snapshot and analysis result produced by the analysis pipeline
type pipelineResult struct {
	Snapshot *domain.Snapshot
	Analysis *domain.AnalysisResult
}

// runAnalysisPipeline builds a snapshot and executes the analysis engine against it
func runAnalysisPipeline(ctx context.Context, rt *appRuntime, start, end time.Time, windowLabel string) (*pipelineResult, error) {
	builder := telemetry.NewSnapshotBuilder(rt.Client)
	snapshot, err := builder.BuildSnapshot(ctx, rt.ProfileName, rt.Profile, start, end, windowLabel)
	if err != nil && snapshot == nil {
		return nil, err
	}

	detCfg := detectors.ConfigFromThresholds(rt.Profile.Thresholds)
	engine := analysis.NewEngine(detCfg)
	res := engine.Analyze(snapshot)

	return &pipelineResult{
		Snapshot: snapshot,
		Analysis: res,
	}, nil
}

// closeClient closes any resources held by the Azure client (e.g. isolated sandbox directories)
func closeClient(client azure.AzureClient) {
	if closer, ok := client.(io.Closer); ok {
		_ = closer.Close()
	}
}

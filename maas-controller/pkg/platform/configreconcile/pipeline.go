package configreconcile

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

// RunResult is returned from Run for reconcile pacing
type RunResult struct {
	EnvoyFilterCreated bool
	Detail             string
}

// Run executes the Config platform pipeline:
// 1. Check if telemetry.usage is configured
// 2. If yes, render and apply EnvoyFilter with Config as owner
// 3. If no, ensure EnvoyFilter is deleted (GC handles this)
func Run(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	scheme *runtime.Scheme,
	config *maasv1alpha1.Config,
) (*RunResult, error) {
	// Check if usage tracking is enabled
	if config.Spec.Telemetry == nil ||
		config.Spec.Telemetry.Usage == nil ||
		config.Spec.Telemetry.Usage.OTELEndpoint == "" {
		log.V(1).Info("Usage tracking not configured, ensuring EnvoyFilter deleted")
		if err := ensureEnvoyFilterDeleted(ctx, c); err != nil {
			return nil, fmt.Errorf("failed to delete EnvoyFilter: %w", err)
		}
		return &RunResult{Detail: "usage tracking disabled"}, nil
	}

	endpoint := config.Spec.Telemetry.Usage.OTELEndpoint

	// Parse endpoint
	host, port, err := parseOTELEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid otelEndpoint format: %w", err)
	}

	// Render EnvoyFilter template
	ef, err := renderEnvoyFilterTemplate(host, port)
	if err != nil {
		return nil, fmt.Errorf("failed to render EnvoyFilter: %w", err)
	}

	// Set Config as controller owner
	if err := controllerutil.SetControllerReference(config, ef, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Apply EnvoyFilter
	if err := applyEnvoyFilter(ctx, c, ef); err != nil {
		return nil, fmt.Errorf("failed to apply EnvoyFilter: %w", err)
	}

	log.Info("EnvoyFilter applied successfully",
		"name", EnvoyFilterUsageName,
		"namespace", EnvoyFilterNamespace,
		"endpoint", endpoint)

	return &RunResult{
		EnvoyFilterCreated: true,
		Detail:             "usage tracking enabled",
	}, nil
}

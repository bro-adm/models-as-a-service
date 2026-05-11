package configreconcile

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// EnvoyFilterUsageName is the name of the cluster-wide EnvoyFilter for usage tracking
	EnvoyFilterUsageName = "maas-usage-observability-envoy-filter"

	// EnvoyFilterNamespace is where the EnvoyFilter is deployed (hardcoded per user requirement)
	EnvoyFilterNamespace = "openshift-ingress"
)

var (
	// GVKEnvoyFilter is the GroupVersionKind for Istio EnvoyFilter
	GVKEnvoyFilter = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "EnvoyFilter",
	}
)

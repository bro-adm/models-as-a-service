package tenantreconcile

import (
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestIsLogsEnabled(t *testing.T) {
	tests := []struct {
		name      string
		telemetry *maasv1alpha1.TenantTelemetryConfig
		want      bool
	}{
		{
			name:      "telemetry is nil",
			telemetry: nil,
			want:      false,
		},
		{
			name: "telemetry.enabled is nil",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: nil,
			},
			want: false,
		},
		{
			name: "telemetry.enabled is false",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := false; return &b }(),
			},
			want: false,
		},
		{
			name: "telemetry.enabled is true but logs is nil",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := true; return &b }(),
				Logs:    nil,
			},
			want: false,
		},
		{
			name: "telemetry.enabled is true and logs is configured",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := true; return &b }(),
				Logs: &maasv1alpha1.TenantLogsConfig{
					OTELEndpoint: "otel-collector.observability.svc.cluster.local:4317",
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got := isLogsEnabled(tt.telemetry)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestConfigureEnvoyFilterLogsResources_LogsDisabled(t *testing.T) {
	tests := []struct {
		name      string
		telemetry *maasv1alpha1.TenantTelemetryConfig
	}{
		{
			name:      "telemetry is nil",
			telemetry: nil,
		},
		{
			name: "telemetry.enabled is nil",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: nil,
			},
		},
		{
			name: "telemetry.enabled is false",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := false; return &b }(),
			},
		},
		{
			name: "telemetry.enabled is true but logs is nil",
			telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := true; return &b }(),
				Logs:    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			log := logr.Discard()

			tenant := &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tenant",
					Namespace: "test-ns",
				},
				Spec: maasv1alpha1.TenantSpec{
					GatewayRef: maasv1alpha1.TenantGatewayRef{
						Namespace: "gateway-ns",
						Name:      "test-gateway",
					},
					Telemetry: tt.telemetry,
				},
			}

			var resources []unstructured.Unstructured
			initialLen := len(resources)

			err := configureEnvoyFilterLogsResources(log, tenant, &resources)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(resources).To(HaveLen(initialLen), "should not add resources when logs disabled")
		})
	}
}

func TestConfigureEnvoyFilterLogsResources_InvalidEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		expectedError string
	}{
		{
			name:          "missing port",
			endpoint:      "no-port",
			expectedError: "invalid otelEndpoint format",
		},
		{
			name:          "invalid port - non-numeric",
			endpoint:      "host:invalid",
			expectedError: "invalid port",
		},
		{
			name:          "invalid port - empty",
			endpoint:      "host:",
			expectedError: "invalid port",
		},
		{
			name:          "multiple colons",
			endpoint:      "host:port:extra",
			expectedError: "invalid otelEndpoint format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			log := logr.Discard()

			tenant := &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tenant",
					Namespace: "test-ns",
				},
				Spec: maasv1alpha1.TenantSpec{
					GatewayRef: maasv1alpha1.TenantGatewayRef{
						Namespace: "gateway-ns",
						Name:      "test-gateway",
					},
					Telemetry: &maasv1alpha1.TenantTelemetryConfig{
						Enabled: func() *bool { b := true; return &b }(),
						Logs: &maasv1alpha1.TenantLogsConfig{
							OTELEndpoint: tt.endpoint,
						},
					},
				},
			}

			var resources []unstructured.Unstructured

			err := configureEnvoyFilterLogsResources(log, tenant, &resources)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.expectedError))
		})
	}
}

func TestConfigureEnvoyFilterLogsResources_LogsEnabled(t *testing.T) {
	g := NewWithT(t)
	log := logr.Discard()

	tenant := &maasv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tenant",
			Namespace: "test-ns",
		},
		Spec: maasv1alpha1.TenantSpec{
			GatewayRef: maasv1alpha1.TenantGatewayRef{
				Namespace: "gateway-ns",
				Name:      "test-gateway",
			},
			Telemetry: &maasv1alpha1.TenantTelemetryConfig{
				Enabled: func() *bool { b := true; return &b }(),
				Logs: &maasv1alpha1.TenantLogsConfig{
					OTELEndpoint: "otel-collector.observability.svc.cluster.local:4317",
				},
			},
		},
	}

	var resources []unstructured.Unstructured
	initialLen := len(resources)

	err := configureEnvoyFilterLogsResources(log, tenant, &resources)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resources).To(HaveLen(initialLen+1), "should add one EnvoyFilter resource")

	// Get the last added resource (the EnvoyFilter)
	ef := resources[len(resources)-1]

	// Verify GVK
	g.Expect(ef.GroupVersionKind()).To(Equal(GVKEnvoyFilter))

	// Verify metadata
	name, found, err := unstructured.NestedString(ef.Object, "metadata", "name")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(name).To(Equal(EnvoyFilterLogsName))

	namespace, found, err := unstructured.NestedString(ef.Object, "metadata", "namespace")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(namespace).To(Equal("gateway-ns"))

	labels, found, err := unstructured.NestedStringMap(ef.Object, "metadata", "labels")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(labels).To(HaveKeyWithValue(LabelTenantName, "test-tenant"))
	g.Expect(labels).To(HaveKeyWithValue(LabelTenantNamespace, "test-ns"))
	g.Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "maas-observability"))

	// Verify workloadSelector
	workloadLabels, found, err := unstructured.NestedStringMap(ef.Object, "spec", "workloadSelector", "labels")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(workloadLabels).To(HaveKeyWithValue("gateway.networking.k8s.io/gateway-name", "test-gateway"))

	// Verify configPatches
	configPatches, found, err := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(configPatches).To(HaveLen(4), "should have 4 config patches: CLUSTER, HTTP_FILTER (json_to_metadata), HTTP_FILTER (header_mutation), NETWORK_FILTER")

	// Verify Patch 1: CLUSTER
	patch1, ok := configPatches[0].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(patch1["applyTo"]).To(Equal("CLUSTER"))

	patchOp, found, err := unstructured.NestedString(patch1, "patch", "operation")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(patchOp).To(Equal("ADD"))

	clusterName, found, err := unstructured.NestedString(patch1, "patch", "value", "name")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(clusterName).To(Equal("otel_als_cluster"))

	// Access array elements properly
	endpoints, found, err := unstructured.NestedSlice(patch1, "patch", "value", "load_assignment", "endpoints")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(endpoints).To(HaveLen(1))

	endpoint0, ok := endpoints[0].(map[string]any)
	g.Expect(ok).To(BeTrue())

	lbEndpoints, found, err := unstructured.NestedSlice(endpoint0, "lb_endpoints")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(lbEndpoints).To(HaveLen(1))

	lbEndpoint0, ok := lbEndpoints[0].(map[string]any)
	g.Expect(ok).To(BeTrue())

	socketAddress, found, err := unstructured.NestedString(lbEndpoint0, "endpoint", "address", "socket_address", "address")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(socketAddress).To(Equal("otel-collector.observability.svc.cluster.local"))

	portValue, found, err := unstructured.NestedInt64(lbEndpoint0, "endpoint", "address", "socket_address", "port_value")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(portValue).To(Equal(int64(4317)))

	// Verify Patch 2: HTTP_FILTER (json_to_metadata)
	patch2, ok := configPatches[1].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(patch2["applyTo"]).To(Equal("HTTP_FILTER"))

	filterName, found, err := unstructured.NestedString(patch2, "patch", "value", "name")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(filterName).To(Equal("envoy.filters.http.json_to_metadata"))

	// Verify response_rules has 4 rules
	rules, found, err := unstructured.NestedSlice(patch2, "patch", "value", "typed_config", "response_rules", "rules")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(rules).To(HaveLen(4), "should have 4 rules: tokens_total, tokens_prompt, tokens_completion, model")

	// Verify Patch 3: HTTP_FILTER (header_mutation)
	patch3, ok := configPatches[2].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(patch3["applyTo"]).To(Equal("HTTP_FILTER"))

	headerMutationFilterName, found, err := unstructured.NestedString(patch3, "patch", "value", "name")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(headerMutationFilterName).To(Equal("envoy.filters.http.header_mutation"))

	// Verify request_mutations removes 3 headers
	requestMutations, found, err := unstructured.NestedSlice(patch3, "patch", "value", "typed_config", "mutations", "request_mutations")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(requestMutations).To(HaveLen(3), "should remove 3 headers: X-MaaS-Username, X-MaaS-Group, X-MaaS-Key-Id")

	// Verify each header removal
	removedHeaders := make([]string, 0, 3)
	for _, mutation := range requestMutations {
		mutationMap, ok := mutation.(map[string]any)
		if ok {
			if remove, ok := mutationMap["remove"].(string); ok {
				removedHeaders = append(removedHeaders, remove)
			}
		}
	}
	g.Expect(removedHeaders).To(ContainElement("X-MaaS-Username"))
	g.Expect(removedHeaders).To(ContainElement("X-MaaS-Group"))
	g.Expect(removedHeaders).To(ContainElement("X-MaaS-Key-Id"))

	// Verify Patch 4: NETWORK_FILTER
	patch4, ok := configPatches[3].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(patch4["applyTo"]).To(Equal("NETWORK_FILTER"))

	accessLogs, found, err := unstructured.NestedSlice(patch4, "patch", "value", "typed_config", "access_log")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(accessLogs).To(HaveLen(1))

	accessLog0, ok := accessLogs[0].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(accessLog0["name"]).To(Equal("envoy.access_loggers.open_telemetry"))

	logName, found, err := unstructured.NestedString(accessLog0, "typed_config", "common_config", "log_name")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(logName).To(Equal("maas-usage-log"))

	// Verify some key attributes exist
	attributes, found, err := unstructured.NestedSlice(accessLog0, "typed_config", "attributes", "values")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(attributes).ToNot(BeEmpty())

	// Check that key attributes are present (note: some are now commented out in template)
	attributeKeys := make([]string, 0, len(attributes))
	for _, attr := range attributes {
		attrMap, ok := attr.(map[string]any)
		if ok {
			if key, ok := attrMap["key"].(string); ok {
				attributeKeys = append(attributeKeys, key)
			}
		}
	}
	g.Expect(attributeKeys).To(ContainElement("user_id"))
	g.Expect(attributeKeys).To(ContainElement("subscription"))
	g.Expect(attributeKeys).To(ContainElement("groups"))
	g.Expect(attributeKeys).To(ContainElement("key_id"))
	g.Expect(attributeKeys).To(ContainElement("tokens_total"))
	g.Expect(attributeKeys).To(ContainElement("tokens_prompt"))
	g.Expect(attributeKeys).To(ContainElement("tokens_completion"))
	g.Expect(attributeKeys).To(ContainElement("model"))

	// Verify that commented-out fields are NOT present
	g.Expect(attributeKeys).NotTo(ContainElement("subscription_key"))
	g.Expect(attributeKeys).NotTo(ContainElement("subscription_labels"))
	g.Expect(attributeKeys).NotTo(ContainElement("organization_id"))
	g.Expect(attributeKeys).NotTo(ContainElement("cost_center"))
	g.Expect(attributeKeys).NotTo(ContainElement("auth_error"))
	g.Expect(attributeKeys).NotTo(ContainElement("auth_error_msg"))
}

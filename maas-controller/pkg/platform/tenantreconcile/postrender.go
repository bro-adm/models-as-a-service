package tenantreconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"

	_ "embed"
)

//go:embed templates/envoyfilter-logs.yaml
var envoyFilterLogsTemplate string

// PostRender mutates rendered resources after kustomize build. It patches all
// dynamic values (images, gateway config, namespace, audience, env vars) and
// applies OIDC, telemetry, and managed-annotation customizations.
func PostRender(ctx context.Context, log logr.Logger, tenant *maasv1alpha1.Tenant, resources []unstructured.Unstructured, params PlatformParams) ([]unstructured.Unstructured, error) {
	gatewayNamespace := tenant.Spec.GatewayRef.Namespace
	gatewayName := tenant.Spec.GatewayRef.Name
	tenantID := params.TenantIdentifier

	var filteredResources []unstructured.Unstructured
	for i := range resources {
		resource := &resources[i]

		annotations := resource.GetAnnotations()
		if annotations != nil && annotations[AnnotationManaged] == "false" {
			log.V(2).Info("Skipping resource due to opendatahub.io/managed=false annotation",
				"kind", resource.GetKind(), "name", resource.GetName(), "namespace", resource.GetNamespace())
			continue
		}

		gvk := resource.GroupVersionKind()
		switch {
		case gvk == GVKTokenRateLimitPolicy && resource.GetName() == baseGatewayTokenRateLimitDefaultDenyPolicyName:
			if err := configureTokenRateLimitPolicy(log, resource, gatewayNamespace, gatewayName); err != nil {
				return nil, err
			}
		case gvk == GVKDestinationRule && resource.GetName() == GatewayDestinationRuleName(tenantID):
			configureDestinationRule(log, resource, gatewayNamespace)
		}

		filteredResources = append(filteredResources, *resource)
	}

	if err := configureExternalOIDC(log, tenant, filteredResources); err != nil {
		return nil, err
	}
	if err := configureTelemetryPolicyResources(log, tenant, &filteredResources, tenantID); err != nil {
		return nil, err
	}
	if err := configureIstioTelemetryResources(log, tenant, &filteredResources, tenantID); err != nil {
		return nil, err
	}
	if err := configureEnvoyFilterLogsResources(log, tenant, &filteredResources); err != nil {
		return nil, err
	}
	if err := applyPlatformParams(log, filteredResources, params); err != nil {
		return nil, err
	}
	_ = ctx
	return filteredResources, nil
}

func configureTokenRateLimitPolicy(log logr.Logger, resource *unstructured.Unstructured, gatewayNamespace, gatewayName string) error {
	log.V(4).Info("Configuring TokenRateLimitPolicy", "name", resource.GetName(), "newNamespace", gatewayNamespace, "newTargetGateway", gatewayName)
	resource.SetNamespace(gatewayNamespace)
	if err := unstructured.SetNestedField(resource.Object, gatewayName, "spec", "targetRef", "name"); err != nil {
		return fmt.Errorf("failed to set spec.targetRef.name on TokenRateLimitPolicy: %w", err)
	}
	return nil
}

func configureDestinationRule(log logr.Logger, resource *unstructured.Unstructured, gatewayNamespace string) {
	log.V(4).Info("Configuring DestinationRule", "name", resource.GetName(), "newNamespace", gatewayNamespace)
	resource.SetNamespace(gatewayNamespace)
}

func configureExternalOIDC(log logr.Logger, tenant *maasv1alpha1.Tenant, resources []unstructured.Unstructured) error {
	if tenant.Spec.ExternalOIDC == nil {
		return nil
	}
	// OIDC is configured in the singleton maas-gateway-auth AuthPolicy managed by
	// maas-controller (see MaaSAuthPolicyReconciler.buildGatewayAuthPolicySpec).
	// The route-level maas-api-auth-policy has been removed, so there is nothing
	// to patch in the kustomize-rendered resources here.
	log.V(1).Info("external OIDC configured via gateway-level AuthPolicy; no kustomize resources to patch")
	return nil
}

func patchAuthPolicyWithOIDC(log logr.Logger, resource *unstructured.Unstructured, oidc *maasv1alpha1.TenantExternalOIDCConfig) error {
	ttl := int64(oidc.TTL)
	if ttl == 0 {
		ttl = 300
	}
	if err := unstructured.SetNestedField(resource.Object, map[string]any{
		"when": []any{
			map[string]any{
				"predicate": `!request.headers.authorization.startsWith("Bearer sk-oai-") && request.headers.authorization.matches("^Bearer [^.]+\\.[^.]+\\.[^.]+$")`,
			},
		},
		"jwt": map[string]any{
			"issuerUrl": oidc.IssuerURL,
			"ttl":       ttl,
		},
		"priority": int64(1),
	}, "spec", "rules", "authentication", "oidc-identities"); err != nil {
		return fmt.Errorf("failed to set oidc-identities: %w", err)
	}
	if err := unstructured.SetNestedField(resource.Object, int64(2),
		"spec", "rules", "authentication", "openshift-identities", "priority"); err != nil {
		return fmt.Errorf("failed to set openshift-identities priority: %w", err)
	}
	if err := unstructured.SetNestedField(resource.Object, []any{
		map[string]any{
			"predicate": `!request.headers.authorization.startsWith("Bearer sk-oai-")`,
		},
	}, "spec", "rules", "authentication", "openshift-identities", "when"); err != nil {
		return fmt.Errorf("failed to set openshift-identities when: %w", err)
	}
	if err := unstructured.SetNestedField(resource.Object, map[string]any{
		"when": []any{
			map[string]any{
				"predicate": `!request.headers.authorization.startsWith("Bearer sk-oai-") && request.headers.authorization.matches("^Bearer [^.]+\\.[^.]+\\.[^.]+$")`,
			},
		},
		"patternMatching": map[string]any{
			"patterns": []any{
				map[string]any{
					"selector": "auth.identity.azp",
					"operator": "eq",
					"value":    oidc.ClientID,
				},
			},
		},
		"priority": int64(1),
	}, "spec", "rules", "authorization", "oidc-client-bound"); err != nil {
		return fmt.Errorf("failed to set oidc-client-bound: %w", err)
	}
	if err := unstructured.SetNestedField(resource.Object, map[string]any{
		"expression": `has(auth.identity.preferred_username) ? auth.identity.preferred_username : (has(auth.identity.sub) ? auth.identity.sub : auth.identity.user.username)`,
	}, "spec", "rules", "response", "success", "headers", "X-MaaS-Username-OC", "plain"); err != nil {
		return fmt.Errorf("failed to set X-MaaS-Username-OC: %w", err)
	}
	groupsExpr := `has(auth.identity.groups) ? ` +
		`(size(auth.identity.groups) > 0 ? ` +
		`'["system:authenticated","' + auth.identity.groups.join('","') + '"]' : ` +
		`'["system:authenticated"]') : ` +
		`'["' + auth.identity.user.groups.join('","') + '"]'`
	if err := unstructured.SetNestedField(resource.Object, map[string]any{
		"expression": groupsExpr,
	}, "spec", "rules", "response", "success", "headers", "X-MaaS-Group-OC", "plain"); err != nil {
		return fmt.Errorf("failed to set X-MaaS-Group-OC: %w", err)
	}
	log.Info("Patched maas-api AuthPolicy with external OIDC configuration", "issuerUrl", oidc.IssuerURL, "clientId", oidc.ClientID)
	return nil
}

func isTelemetryEnabled(t *maasv1alpha1.TenantTelemetryConfig) bool {
	if t == nil {
		return false
	}
	if t.Enabled == nil {
		return false
	}
	return *t.Enabled
}

func configureTelemetryPolicyResources(log logr.Logger, tenant *maasv1alpha1.Tenant, resources *[]unstructured.Unstructured, tenantID string) error {
	if !isTelemetryEnabled(tenant.Spec.Telemetry) {
		return nil
	}
	// Caller should have checked CRD; still skip if API missing at apply time.
	gatewayNamespace := tenant.Spec.GatewayRef.Namespace
	gatewayName := tenant.Spec.GatewayRef.Name
	metricLabels := buildTelemetryLabels(log, tenant.Spec.Telemetry)
	tp := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "extensions.kuadrant.io/v1alpha1",
			"kind":       "TelemetryPolicy",
			"metadata": map[string]any{
				"name":      TelemetryPolicyName(tenantID),
				"namespace": gatewayNamespace,
				"labels": map[string]any{
					"app.kubernetes.io/part-of": "maas-observability",
					LabelTenantName:             tenant.Name,
					LabelTenantNamespace:        tenant.Namespace,
				},
			},
			"spec": map[string]any{
				"targetRef": map[string]any{
					"group": "gateway.networking.k8s.io",
					"kind":  "Gateway",
					"name":  gatewayName,
				},
				"metrics": map[string]any{
					"default": map[string]any{
						"labels": metricLabels,
					},
				},
			},
		},
	}
	telemetryPolicyName := TelemetryPolicyName(tenantID)
	log.V(2).Info("Appending TelemetryPolicy", "name", telemetryPolicyName, "namespace", gatewayNamespace)
	*resources = append(*resources, *tp)
	return nil
}

func configureIstioTelemetryResources(log logr.Logger, tenant *maasv1alpha1.Tenant, resources *[]unstructured.Unstructured, tenantID string) error {
	if !isTelemetryEnabled(tenant.Spec.Telemetry) {
		return nil
	}
	gatewayNamespace := tenant.Spec.GatewayRef.Namespace
	gatewayName := tenant.Spec.GatewayRef.Name
	istioTelemetry := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "telemetry.istio.io/v1",
			"kind":       "Telemetry",
			"metadata": map[string]any{
				"name":      IstioTelemetryName(tenantID),
				"namespace": gatewayNamespace,
				"labels": map[string]any{
					"app.kubernetes.io/part-of": "maas-observability",
					LabelTenantName:             tenant.Name,
					LabelTenantNamespace:        tenant.Namespace,
				},
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"gateway.networking.k8s.io/gateway-name": gatewayName,
					},
				},
				"metrics": []any{
					map[string]any{
						"providers": []any{map[string]any{"name": "prometheus"}},
						"overrides": []any{
							map[string]any{
								"match": map[string]any{"metric": "REQUEST_DURATION", "mode": "CLIENT_AND_SERVER"},
								"tagOverrides": map[string]any{
									"subscription": map[string]any{
										"operation": "UPSERT",
										"value":     `request.headers["x-maas-subscription"]`,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	istioTelemetryName := IstioTelemetryName(tenantID)
	log.V(2).Info("Appending Istio Telemetry", "name", istioTelemetryName, "namespace", gatewayNamespace)
	*resources = append(*resources, *istioTelemetry)
	return nil
}

func buildTelemetryLabels(log logr.Logger, config *maasv1alpha1.TenantTelemetryConfig) map[string]any {
	captureOrganization := true
	captureUser := false
	captureGroup := false
	captureModelUsage := true
	if config != nil && config.Metrics != nil {
		metrics := config.Metrics
		if metrics.CaptureOrganization != nil {
			captureOrganization = *metrics.CaptureOrganization
		}
		if metrics.CaptureUser != nil {
			captureUser = *metrics.CaptureUser
		}
		if metrics.CaptureGroup != nil {
			captureGroup = *metrics.CaptureGroup
		}
		if metrics.CaptureModelUsage != nil {
			captureModelUsage = *metrics.CaptureModelUsage
		}
	}
	labels := map[string]any{
		"subscription": "auth.identity.selected_subscription",
		"cost_center":  "auth.identity.subscription_info.costCenter",
	}
	if captureOrganization {
		labels["organization_id"] = "auth.identity.subscription_info.organizationId"
	}
	if captureUser {
		log.Info("WARNING: User identity metrics enabled - ensure GDPR/privacy compliance", "field", "captureUser", "value", true)
		labels["user"] = "auth.identity.userid"
	}
	if captureGroup {
		labels["group"] = "auth.identity.group"
	}
	if captureModelUsage {
		labels["model"] = "responseBodyJSON(\"/model\")"
	}
	return labels
}

func isLogsEnabled(t *maasv1alpha1.TenantTelemetryConfig) bool {
	if t == nil || t.Logs == nil {
		return false
	}
	if t.Enabled == nil {
		return false
	}
	return *t.Enabled
}

type envoyFilterLogsTemplateData struct {
	Name            string
	Namespace       string
	TenantName      string
	TenantNamespace string
	GatewayName     string
	OTELHost        string
	OTELPort        int64
}

func configureEnvoyFilterLogsResources(log logr.Logger, tenant *maasv1alpha1.Tenant, resources *[]unstructured.Unstructured) error {
	if !isLogsEnabled(tenant.Spec.Telemetry) {
		return nil
	}

	gatewayNamespace := tenant.Spec.GatewayRef.Namespace
	gatewayName := tenant.Spec.GatewayRef.Name
	otelEndpoint := tenant.Spec.Telemetry.Logs.OTELEndpoint

	// Parse endpoint into host:port
	// Format: "user-usage-collector.opendatahub.svc.cluster.local:4317"
	parts := strings.Split(otelEndpoint, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid otelEndpoint format %q: expected host:port", otelEndpoint)
	}
	host := parts[0]
	port, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid port in otelEndpoint %q: %w", otelEndpoint, err)
	}

	// Render template with data
	tmplData := envoyFilterLogsTemplateData{
		Name:            EnvoyFilterLogsName,
		Namespace:       gatewayNamespace,
		TenantName:      tenant.Name,
		TenantNamespace: tenant.Namespace,
		GatewayName:     gatewayName,
		OTELHost:        host,
		OTELPort:        port,
	}

	tmpl, err := template.New("envoyfilter-logs").Parse(envoyFilterLogsTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse EnvoyFilter template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tmplData); err != nil {
		return fmt.Errorf("failed to execute EnvoyFilter template: %w", err)
	}

	// Parse YAML into unstructured object
	ef := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &ef.Object); err != nil {
		return fmt.Errorf("failed to unmarshal rendered EnvoyFilter YAML: %w", err)
	}

	log.V(2).Info("Appending EnvoyFilter for logs", "name", EnvoyFilterLogsName, "namespace", gatewayNamespace)
	*resources = append(*resources, *ef)
	return nil
}

func configureConfigHashAnnotation(log logr.Logger, resources []unstructured.Unstructured) error {
	var configMap *corev1.ConfigMap
	for idx := range resources {
		resource := &resources[idx]
		if resource.GroupVersionKind() == GVKConfigMap && resource.GetName() == MaaSParametersConfigMapName {
			cm := &corev1.ConfigMap{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, cm); err != nil {
				return fmt.Errorf("failed to convert ConfigMap: %w", err)
			}
			configMap = cm
			break
		}
	}
	if configMap == nil {
		log.V(1).Info("ConfigMap not found in rendered resources, skipping config hash annotation", "expectedName", MaaSParametersConfigMapName)
		return nil
	}

	configHash := hashConfigMapData(configMap.Data)
	log.V(4).Info("Computed ConfigMap hash", "hash", configHash, "configMap", configMap.Name)

	var deployment *appsv1.Deployment
	depIdx := -1
	for idx := range resources {
		resource := &resources[idx]
		if resource.GroupVersionKind() == GVKDeployment && resource.GetName() == MaaSAPIDeploymentName {
			dep := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, dep); err != nil {
				return fmt.Errorf("failed to convert Deployment: %w", err)
			}
			deployment = dep
			depIdx = idx
			break
		}
	}
	if deployment == nil {
		log.V(1).Info("Deployment not found in rendered resources, skipping config hash annotation", "expectedName", MaaSAPIDeploymentName)
		return nil
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	annotationKey := LabelODHAppPrefix + "/maas-config-hash"
	deployment.Spec.Template.Annotations[annotationKey] = configHash

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		return fmt.Errorf("failed to convert Deployment back to unstructured: %w", err)
	}
	resources[depIdx].Object = u

	return nil
}

func hashConfigMapData(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(data[k])
		sb.WriteString("\n")
	}
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}

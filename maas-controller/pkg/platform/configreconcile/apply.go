package configreconcile

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ssaFieldOwner = "maas-controller"

func applyEnvoyFilter(ctx context.Context, c client.Client, ef *unstructured.Unstructured) error {
	// Remove fields that shouldn't be in SSA
	unstructured.RemoveNestedField(ef.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(ef.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(ef.Object, "status")

	return c.Patch(ctx, ef, client.Apply, client.FieldOwner(ssaFieldOwner), client.ForceOwnership)
}

func ensureEnvoyFilterDeleted(ctx context.Context, c client.Client) error {
	ef := &unstructured.Unstructured{}
	ef.SetGroupVersionKind(GVKEnvoyFilter)
	ef.SetName(EnvoyFilterUsageName)
	ef.SetNamespace(EnvoyFilterNamespace)

	err := c.Delete(ctx, ef)
	if apierrors.IsNotFound(err) {
		return nil // Already deleted
	}
	return err
}

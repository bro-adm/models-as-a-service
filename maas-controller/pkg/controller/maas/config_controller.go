/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package maas

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	"github.com/opendatahub-io/models-as-a-service/maas-controller/pkg/platform/configreconcile"
)

// ConfigReconciler reconciles the Config CR (cluster-wide singleton).
// Manages cluster-wide resources like EnvoyFilter for usage tracking.
type ConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// RBAC for Config controller
//+kubebuilder:rbac:groups=maas.opendatahub.io,resources=configs,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

func (r *ConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("config", req.NamespacedName)

	var config maasv1alpha1.Config
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if Config is being deleted (GC will clean up EnvoyFilter)
	if !config.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Run platform reconcile logic
	result, err := configreconcile.Run(ctx, log, r.Client, r.Scheme, &config)
	if err != nil {
		log.Error(err, "Config platform reconcile failed")
		return ctrl.Result{}, err
	}

	log.V(1).Info("Config reconciled", "detail", result.Detail)
	return ctrl.Result{}, nil
}

func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Singleton predicate: only watch Config named "default"
	singletonPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetName() == maasv1alpha1.ConfigInstanceName
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&maasv1alpha1.Config{}, builder.WithPredicates(singletonPredicate)).
		Complete(r)
}

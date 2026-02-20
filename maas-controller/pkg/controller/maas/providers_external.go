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
	"fmt"

	"github.com/go-logr/logr"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// externalModelHandler implements BackendHandler for kind "ExternalModel".
// Until the logic below is implemented, ReconcileRoute and Status return ErrKindNotImplemented,
// which causes the controller to set status Phase=Failed and Condition Reason=Unsupported.
type externalModelHandler struct {
	r *MaaSModelReconciler
}

// ReconcileRoute creates or updates the HTTPRoute for an external model.
//
// Current behaviour: returns ErrKindNotImplemented so the controller marks the model as Unsupported.
//
// To implement:
//  1. Define or reuse a CRD for external model config (e.g. URL, auth, TLS). You may add
//     fields to ModelReference in the API for ExternalModel (e.g. URL, CACertSecretRef).
//  2. Create or update an HTTPRoute in model.Namespace named "maas-model-<model.Name>" that:
//     - References r.gatewayName() / r.gatewayNamespace() in ParentRefs.
//     - Has a path match prefix "/<model.Name>".
//     - Has a single BackendRef to the external URL (use Gateway API BackendRef to an
//     ExternalName Service or a custom backend type, depending on your gateway implementation).
//  3. Use controllerutil.CreateOrUpdate with the HTTPRoute and SetControllerReference(model, route, r.Scheme).
//  4. Populate model.Status with HTTPRouteName, HTTPRouteNamespace, HTTPRouteGatewayName,
//     HTTPRouteGatewayNamespace, and HTTPRouteHostnames (from the route or gateway) so that
//     Status() and discovery can derive the endpoint later.
//  5. Return nil on success; the controller will then call Status().
func (h *externalModelHandler) ReconcileRoute(ctx context.Context, log logr.Logger, model *maasv1alpha1.MaaSModel) error {
	return fmt.Errorf("%w: ExternalModel", ErrKindNotImplemented)
}

// Status returns the model endpoint URL and whether the model is ready.
//
// Current behaviour: returns ErrKindNotImplemented so the controller marks the model as Unsupported.
//
// To implement:
//  1. After ReconcileRoute has created/updated the HTTPRoute, read the route or gateway (e.g.
//     r.Get(ctx, gatewayKey, gateway)) to get a hostname or address.
//  2. Build the endpoint URL (e.g. "https://<hostname>/<model.Name>"). Prefer model.Status.HTTPRouteHostnames
//     if ReconcileRoute already set it from the HTTPRoute.
//  3. Optionally probe the external endpoint (HTTP GET/HEAD) to determine ready. If you do not
//     probe, you can return (endpoint, true, nil) once the HTTPRoute is in place.
//  4. Return (endpoint, ready, nil). The controller will set model.Status.Endpoint and Phase
//     (Ready or Pending) from this.
func (h *externalModelHandler) Status(ctx context.Context, log logr.Logger, model *maasv1alpha1.MaaSModel) (endpoint string, ready bool, err error) {
	return "", false, fmt.Errorf("%w: ExternalModel", ErrKindNotImplemented)
}

// GetModelEndpoint returns the endpoint URL for ExternalModel. When implemented, use your own logic
// (e.g. spec.endpoint or from your HTTPRoute); do not assume the same gateway hostname + path as llmisvc.
func (h *externalModelHandler) GetModelEndpoint(ctx context.Context, log logr.Logger, model *maasv1alpha1.MaaSModel) (string, error) {
	return "", fmt.Errorf("%w: ExternalModel", ErrKindNotImplemented)
}

// CleanupOnDelete is called when the MaaSModel is deleted.
//
// Current behaviour: no-op (no HTTPRoute is created yet).
//
// To implement:
//  1. Look up the HTTPRoute created by ReconcileRoute (name "maas-model-<model.Name>", namespace model.Namespace).
//  2. If found, delete it (r.Delete(ctx, route)). Ignore NotFound. The controller will only call
//     this for kinds that create their own route (unlike llmisvc, where the route is owned by KServe).
func (h *externalModelHandler) CleanupOnDelete(ctx context.Context, log logr.Logger, model *maasv1alpha1.MaaSModel) error {
	return nil
}

// externalModelRouteResolver returns the HTTPRoute name/namespace for ExternalModel.
// Used by findHTTPRouteForModel and by AuthPolicy/Subscription controllers to attach policies.
// When ReconcileRoute is implemented, the controller creates the route with this name/namespace,
// so this resolver stays as-is.
type externalModelRouteResolver struct{}

func (externalModelRouteResolver) HTTPRouteForModel(ctx context.Context, c client.Reader, model *maasv1alpha1.MaaSModel) (routeName, routeNamespace string, err error) {
	routeName = fmt.Sprintf("maas-model-%s", model.Name)
	routeNamespace = model.Namespace
	return routeName, routeNamespace, nil
}

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	_ "embed"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
)

func init() {
	base.RegisterController(&controller{})
}

// ControllerName is the name of this controller.
const ControllerName = "composeproject"

// APIGroup is the API group this controller belongs to.
const APIGroup = "containers"

//go:embed crd.yaml
var controllerCRD string

// controller implements the base.Controller interface for composeproject.
type controller struct {
	webhookPort     int
	webhookManagers []base.WebhookManager
}

// Verify that controller implements base.Controller and base.WebhookController interfaces.
var (
	_ base.Controller        = &controller{}
	_ base.WebhookController = &controller{}
)

// GetName implements [base.Controller].
func (c *controller) GetName() string {
	return ControllerName
}

// GetAPIGroup implements [base.Controller].
func (c *controller) GetAPIGroup() string {
	return APIGroup
}

// GetCRDData returns the embedded CRD YAML data, implementing [base.Controller].
func (c *controller) GetCRDData() string {
	return controllerCRD
}

// SetWebhookPort provides the actual webhook port to the controller, implementing [base.WebhookController].
func (c *controller) SetWebhookPort(port int) {
	c.webhookPort = port
}

// GetWebhookServiceName implements [base.WebhookController].
func (c *controller) GetWebhookServiceName() string {
	return ControllerName + "-webhook"
}

// GetWebhookManagers implements [base.WebhookController].
func (c *controller) GetWebhookManagers() []base.WebhookManager {
	return c.webhookManagers
}

// setupReconciler sets up the reconciler with the manager.
func (c *controller) setupReconciler(ctx context.Context, mgr ctrl.Manager) error {
	mgr.GetLogger().Info("Setting up compose project reconciler")
	return (&reconciler{
		ctx:              ctx,
		execCommand:      defaultCommandExecutor,
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		procs:            make(map[types.UID]processState),
		procCompletionCh: make(chan event.TypedGenericEvent[processCompletionEvent], 1024),
	}).SetupWithManager(ctx, mgr)
}

// setupWebhookWithRuntimeConfig registers a validating webhook that enforces
// spec.namespace/spec.name uniqueness on create, and validates the action
// annotation on both create and update.
func (c *controller) setupWebhookWithRuntimeConfig(mgr ctrl.Manager) error {
	mgr.GetLogger().Info("Setting up compose project webhook")
	validatingConfig := base.WebhookConfig[*v1alpha1.ComposeProject]{
		Name:        "compose-project-validating",
		WebhookName: "compose-project-validating.containers.rancherdesktop.io",
		WebhookPort: c.webhookPort,
		Validator:   &composeProjectValidator{},
	}

	managers, err := base.SetupWebhookForResource(mgr, &v1alpha1.ComposeProject{}, validatingConfig)
	if err != nil {
		return err
	}
	c.webhookManagers = append(c.webhookManagers, managers...)

	return nil
}

// RegisterWithManager implements [base.Controller].
func (c *controller) RegisterWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Register the CRD types with the scheme
	if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	if err := c.setupReconciler(ctx, mgr); err != nil {
		mgr.GetLogger().Error(err, "Failed to set up compose project reconciler")
		return err
	}

	if err := c.setupWebhookWithRuntimeConfig(mgr); err != nil {
		mgr.GetLogger().Error(err, "Failed to set up compose project webhook")
		return err
	}

	return nil
}

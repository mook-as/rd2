// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	_ "embed"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
)

func init() {
	base.RegisterController(NewController())
}

// ControllerName is the name of this controller.
const ControllerName = "limavm"

// APIGroup is the API group this controller belongs to.
const APIGroup = "lima"

//go:embed crd.yaml
var limaCRD string

// Controller implements the base.Controller interface for limavm.
type Controller struct{}

// Verify that Controller implements base.Controller interface.
var _ base.Controller = &Controller{}

// NewController creates a new Controller instance.
func NewController() *Controller {
	return &Controller{}
}

// GetName returns the controller name.
func (c *Controller) GetName() string {
	return ControllerName
}

// GetAPIGroup returns the API group this controller belongs to.
func (c *Controller) GetAPIGroup() string {
	return APIGroup
}

// GetCRDData returns the embedded CRD YAML data.
func (c *Controller) GetCRDData() string {
	return limaCRD
}

// RegisterWithManager implements the complete controller registration for both embedded and external modes.
func (c *Controller) RegisterWithManager(mgr ctrl.Manager) error {
	// Register the CRD types with the scheme
	if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	// Create and set up the controller with the manager
	return (&controllers.LimaVMReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		FinalizerHelper: base.NewFinalizerHelper(mgr.GetClient(), mgr.GetScheme(), controllers.FinalizerName),
	}).SetupWithManager(mgr)
}

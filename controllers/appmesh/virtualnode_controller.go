/*
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

package controllers

import (
	"context"

	"github.com/aws/aws-app-mesh-controller-for-k8s/pkg/k8s"
	"github.com/aws/aws-app-mesh-controller-for-k8s/pkg/runtime"
	"github.com/aws/aws-app-mesh-controller-for-k8s/pkg/virtualnode"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appmesh "github.com/aws/aws-app-mesh-controller-for-k8s/apis/appmesh/v1beta2"
)

// NewVirtualNodeReconciler constructs new virtualNodeReconciler
func NewVirtualNodeReconciler(
	k8sClient client.Client,
	finalizerManager k8s.FinalizerManager,
	vnResManager virtualnode.ResourceManager,
	log logr.Logger,
	recorder record.EventRecorder) *virtualNodeReconciler {
	return &virtualNodeReconciler{
		k8sClient:                    k8sClient,
		finalizerManager:             finalizerManager,
		vnResManager:                 vnResManager,
		enqueueRequestsForMeshEvents: virtualnode.NewEnqueueRequestsForMeshEvents(k8sClient, log),
		log:                          log,
		recorder:                     recorder,
	}
}

// virtualNodeReconciler reconciles a VirtualNode object
type virtualNodeReconciler struct {
	k8sClient        client.Client
	finalizerManager k8s.FinalizerManager
	vnResManager     virtualnode.ResourceManager

	enqueueRequestsForMeshEvents handler.EventHandler
	log                          logr.Logger
	recorder                     record.EventRecorder
}

// +kubebuilder:rbac:groups=appmesh.k8s.aws,resources=virtualnodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=appmesh.k8s.aws,resources=virtualnodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *virtualNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return runtime.HandleReconcileError(r.reconcile(ctx, req), r.log)
}

func (r *virtualNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appmesh.VirtualNode{}).
		Watches(&source.Kind{Type: &appmesh.Mesh{}}, r.enqueueRequestsForMeshEvents).
		WithOptions(controller.Options{MaxConcurrentReconciles: 3}).
		Complete(r)
}

func (r *virtualNodeReconciler) reconcile(ctx context.Context, req ctrl.Request) error {
	vn := &appmesh.VirtualNode{}
	if err := r.k8sClient.Get(ctx, req.NamespacedName, vn); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !vn.DeletionTimestamp.IsZero() {
		return r.cleanupVirtualNode(ctx, vn)
	}
	if err := r.reconcileVirtualNode(ctx, vn); err != nil {
		r.recorder.Event(vn, corev1.EventTypeWarning, "ReconcileError", err.Error())
		return err
	}
	return nil
}

func (r *virtualNodeReconciler) reconcileVirtualNode(ctx context.Context, vn *appmesh.VirtualNode) error {
	if err := r.finalizerManager.AddFinalizers(ctx, vn, k8s.FinalizerAWSAppMeshResources); err != nil {
		return err
	}
	if err := r.vnResManager.Reconcile(ctx, vn); err != nil {
		return err
	}
	return nil
}

func (r *virtualNodeReconciler) cleanupVirtualNode(ctx context.Context, vn *appmesh.VirtualNode) error {
	if k8s.HasFinalizer(vn, k8s.FinalizerAWSAppMeshResources) {
		if err := r.vnResManager.Cleanup(ctx, vn); err != nil {
			return err
		}
		if err := r.finalizerManager.RemoveFinalizers(ctx, vn, k8s.FinalizerAWSAppMeshResources); err != nil {
			return err
		}
	}
	return nil
}

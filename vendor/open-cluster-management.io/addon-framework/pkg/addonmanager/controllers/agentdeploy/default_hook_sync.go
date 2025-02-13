package agentdeploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type defaultHookSyncer struct {
	controller *addonDeployController
	agentAddon agent.AgentAddon
}

func (s *defaultHookSyncer) sync(ctx context.Context,
	syncCtx factory.SyncContext,
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.ManagedClusterAddOn, error) {
	installMode := constants.InstallModeDefault
	deployWorkNamespace := addon.Namespace
	_, hookWork, err := s.controller.buildManifestWorks(ctx, s.agentAddon, installMode, deployWorkNamespace, cluster, addon)
	if err != nil {
		return addon, err
	}

	if hookWork == nil {
		addonRemoveFinalizer(addon, constants.PreDeleteHookFinalizer)
		return addon, nil
	}

	if addonAddFinalizer(addon, constants.PreDeleteHookFinalizer) {
		return addon, nil
	}

	if addon.DeletionTimestamp.IsZero() {
		return addon, nil
	}

	// will deploy the pre-delete hook manifestWork when the addon is deleting
	hookWork, err = s.controller.applyWork(ctx, constants.AddonManifestApplied, hookWork, addon)
	if err != nil {
		return addon, err
	}

	// TODO: will surface more message here
	if hookWorkIsCompleted(hookWork) {
		meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
			Type:    constants.AddonHookManifestCompleted,
			Status:  metav1.ConditionTrue,
			Reason:  "HookManifestIsCompleted",
			Message: fmt.Sprintf("hook manifestWork %v is completed.", hookWork.Name),
		})

		s.controller.cache.removeCache(constants.PreDeleteHookWorkName(addon.Name), deployWorkNamespace)
		addonRemoveFinalizer(addon, constants.PreDeleteHookFinalizer)
		return addon, nil
	}

	meta.SetStatusCondition(&addon.Status.Conditions, metav1.Condition{
		Type:    constants.AddonHookManifestCompleted,
		Status:  metav1.ConditionFalse,
		Reason:  "HookManifestIsNotCompleted",
		Message: fmt.Sprintf("hook manifestWork %v is not completed.", hookWork.Name),
	})

	return addon, nil

}

package syncrolebinding

import (
	"context"
	"time"

	"github.com/stolostron/multicloud-operators-foundation/pkg/helpers"
	"github.com/stolostron/multicloud-operators-foundation/pkg/utils"
	clustersetutils "github.com/stolostron/multicloud-operators-foundation/pkg/utils/clusterset"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/stolostron/multicloud-operators-foundation/pkg/cache"
)

//This controller apply clusterset related clusterrolebinding based on clustersetToClusters and clustersetAdminToSubject map
type Reconciler struct {
	kubeClient                 kubernetes.Interface
	clusterSetAdminCache       *cache.AuthCache
	clusterSetViewCache        *cache.AuthCache
	globalClustersetToClusters *helpers.ClusterSetMapper
	clustersetToClusters       *helpers.ClusterSetMapper
	clustersetToNamespace      *helpers.ClusterSetMapper
}

func NewReconciler(kubeClient kubernetes.Interface,
	clusterSetAdminCache *cache.AuthCache,
	clusterSetViewCache *cache.AuthCache,
	globalClustersetToClusters *helpers.ClusterSetMapper,
	clustersetToClusters *helpers.ClusterSetMapper,
	clustersetToNamespace *helpers.ClusterSetMapper) Reconciler {
	return Reconciler{
		kubeClient:                 kubeClient,
		clusterSetAdminCache:       clusterSetAdminCache,
		clusterSetViewCache:        clusterSetViewCache,
		globalClustersetToClusters: globalClustersetToClusters,
		clustersetToClusters:       clustersetToClusters,
		clustersetToNamespace:      clustersetToNamespace,
	}
}

// start a routine to sync the clusterrolebinding periodically.
func (r *Reconciler) Run(period time.Duration) {
	go utilwait.Forever(r.reconcile, period)
}

//This function sycn the rolebinding in namespace which in r.clustersetToNamespace and r.clustersetToClusters
func (r *Reconciler) reconcile() {
	ctx := context.Background()

	//union the clusterset to namespace and clusterset to cluster(it's same as managedcluster namespace).
	//so we can use unionclustersetToNamespace to generate role bindings.
	unionclustersetToNamespace := r.clustersetToNamespace.UnionObjectsInClusterSet(r.clustersetToClusters)
	clustersetToAdminSubjects := clustersetutils.GenerateClustersetSubjects(r.clusterSetAdminCache)
	clustersetToViewSubjects := clustersetutils.GenerateClustersetSubjects(r.clusterSetViewCache)

	r.syncRoleBinding(ctx, unionclustersetToNamespace, clustersetToAdminSubjects, "admin")

	//Sync clusters namespace view permission to the global clusterset users
	unionGlobalClustersetToNamespace := unionclustersetToNamespace.UnionObjectsInClusterSet(r.globalClustersetToClusters)

	r.syncRoleBinding(ctx, unionGlobalClustersetToNamespace, clustersetToViewSubjects, "view")
}

//syncRoleBinding sync two(admin/view) rolebindings in the clusterset's clusterpools/clusterclaims/clusterdeployment/managedcluster namespace.
//clustersetToSubject(map[string][]rbacv1.Subject) means the users/groups in "[]rbacv1.Subject" has admin/view permission to the clusterset
//clustersetToNamespace(map[string]sets.String) means the clusterset include the namespaces which has a clusterpools/clusterclaims/clusterdeployments/managedclusters.
// and these resources are in the clusterset.
//In current acm design, if a user has admin/view permissions to a clusterset, he/she should also has admin/view permissions to the clusterpools/clusterclaims/clusterdeployments/managedclusters which are in the set.
//So we will generate two(admin/view) rolebindings which grant the namespace admin/view permissions to clusterset users.
//For namespace, it will have two rolebindings, so if there are 2k clusters(namespaces), 4k rolebindings will be created.
func (r *Reconciler) syncRoleBinding(ctx context.Context, clustersetToNamespace *helpers.ClusterSetMapper, clustersetToSubject map[string][]rbacv1.Subject, role string) []error {
	//namespaceToSubject(map[<namespace>][]rbacv1.Subject) means the users/groups in subject has permission for this namespace.
	//for each item, we will create a rrolebinding
	namespaceToSubject := clustersetutils.GenerateObjectSubjectMap(clustersetToNamespace, clustersetToSubject)
	//apply all disired clusterrolebinding
	errs := []error{}
	for namespace, subjects := range namespaceToSubject {
		clustersetName := clustersetToNamespace.GetObjectClusterset(namespace)
		requiredRoleBinding := generateRequiredRoleBinding(namespace, subjects, clustersetName, role)
		err := utils.ApplyRoleBinding(ctx, r.kubeClient, requiredRoleBinding)
		if err != nil {
			klog.Errorf("Failed to apply rolebinding: %v, error:%v", requiredRoleBinding.Name, err)
			errs = append(errs, err)
		}
	}

	//Delete rolebinding
	roleBindingList, err := r.kubeClient.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{LabelSelector: clusterv1beta1.ClusterSetLabel})
	if err != nil {
		klog.Errorf("Error to list clusterrolebinding. error:%v", err)
	}
	for _, roleBinding := range roleBindingList.Items {
		curRoleBinding := roleBinding

		//only handle current resource rolebinding
		matchRoleBindingName := utils.GenerateClustersetResourceRoleBindingName(role)

		if matchRoleBindingName != curRoleBinding.Name {
			continue
		}

		if _, ok := namespaceToSubject[roleBinding.Namespace]; !ok {
			err := r.kubeClient.RbacV1().RoleBindings(curRoleBinding.Namespace).Delete(ctx, curRoleBinding.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("Error to delete clusterrolebinding, error:%v", err)
			}
		}
	}
	return errs
}

func generateRequiredRoleBinding(resourceNameSpace string, subjects []rbacv1.Subject, clustersetName string, role string) *rbacv1.RoleBinding {
	roleBindingName := utils.GenerateClustersetResourceRoleBindingName(role)
	var labels = make(map[string]string)
	labels[clusterv1beta1.ClusterSetLabel] = clustersetName
	labels[clustersetutils.ClusterSetRole] = role
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: resourceNameSpace,
			Labels:    labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     role,
		},
		Subjects: subjects,
	}
}

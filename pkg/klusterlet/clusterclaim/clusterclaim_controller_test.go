package clusterclaim

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

func newClusterClaimReconciler(clusterClient clusterclientset.Interface, listFunc ListClusterClaimsFunc) *ClusterClaimReconciler {
	return &ClusterClaimReconciler{
		Log:               ctrl.Log.WithName("controllers").WithName("ManagedClusterInfo"),
		ClusterClient:     clusterClient,
		ListClusterClaims: listFunc,
	}
}

func TestCreateOrUpdate(t *testing.T) {
	testcases := []struct {
		name                       string
		clusterClaimCreateOnlyList []string
		objects                    []runtime.Object
		clusterclaims              []*clusterv1alpha1.ClusterClaim
		validateAddonActions       func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                       "create cluster claim",
			clusterClaimCreateOnlyList: []string{},
			objects:                    []runtime.Object{},
			clusterclaims: []*clusterv1alpha1.ClusterClaim{
				newClusterClaim("x", "y"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect %d actions, but got: %v", 2, len(actions))
				}
				if actions[1].GetVerb() != "create" {
					t.Errorf("Expect action create, but got: %s", actions[1].GetVerb())
				}
			},
		},
		{
			name:                       "update cluster claim",
			clusterClaimCreateOnlyList: []string{},
			objects: []runtime.Object{
				newClusterClaim("x", "y"),
			},
			clusterclaims: []*clusterv1alpha1.ClusterClaim{
				newClusterClaim("x", "z"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but got: %v", len(actions))
				}
				if actions[1].GetVerb() != "update" {
					t.Errorf("Expect action update, but got: %s", actions[1].GetVerb())
				}
			},
		},
		{
			name:                       "update cluster claim with create only list",
			clusterClaimCreateOnlyList: []string{"x"},
			objects:                    []runtime.Object{},
			clusterclaims: []*clusterv1alpha1.ClusterClaim{
				newClusterClaim("x", "y"),
				newClusterClaim("x", "z"),
			},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("Expect 3 actions, but got: %v", len(actions))
				}
				if actions[0].GetVerb() != "get" {
					t.Errorf("Expect action get, but got: %s", actions[1].GetVerb())
				}
				if actions[1].GetVerb() != "create" {
					t.Errorf("Expect action create, but got: %s", actions[1].GetVerb())
				}
				if actions[2].GetVerb() != "get" {
					t.Errorf("Expect action get, but got: %s", actions[1].GetVerb())
				}
			},
		},
	}

	ctx := context.Background()
	for _, tc := range testcases {
		clusterClient := clusterfake.NewSimpleClientset(tc.objects...)
		reconciler := newClusterClaimReconciler(clusterClient, nil)
		for _, cc := range tc.clusterclaims {
			if err := reconciler.createOrUpdate(ctx, cc, tc.clusterClaimCreateOnlyList); err != nil {
				t.Errorf("%s: unexpected error: %v", tc.name, err)
			}
		}
		tc.validateAddonActions(t, clusterClient.Actions())
	}
}

func TestSyncClaims(t *testing.T) {
	ctx := context.Background()
	expected := []*clusterv1alpha1.ClusterClaim{
		newClusterClaim("x", "1"),
		newClusterClaim("y", "2"),
		newClusterClaim("z", "3"),
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "o",
			},
		},
	}

	deletedClaim := &clusterv1alpha1.ClusterClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "p",
			Labels: map[string]string{labelHubManaged: ""},
		},
	}

	clusterClient := clusterfake.NewSimpleClientset(deletedClaim)
	reconciler := newClusterClaimReconciler(clusterClient, func() ([]*clusterv1alpha1.ClusterClaim, error) {
		return expected, nil
	})

	if err := reconciler.syncClaims(ctx); err != nil {
		t.Errorf("Failed to sync cluster claims: %v", err)
	}

	for _, item := range expected {
		claim, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(context.Background(), item.Name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Unable to find cluster claims: %s", item.Name)
		}

		if !reflect.DeepEqual(item.Spec, claim.Spec) {
			t.Errorf("Expected cluster claim %v, but got %v", item, claim)
		}
	}

	if _, err := clusterClient.ClusterV1alpha1().ClusterClaims().Get(context.Background(),
		deletedClaim.Name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Errorf("deleted cluster claim %v is not deleted", deletedClaim.Name)
	}

}

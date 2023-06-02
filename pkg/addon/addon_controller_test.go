package addon

import (
	"context"
	"fmt"
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/scheme"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isv-managed-starburst-operator/api/v1alpha1"
	"github.com/isv-managed-starburst-operator/pkg/isv"
)

var _ = Describe("StarburstAddon Reconcile", Ordered, func() {
	Context("Creation Reconcile", func() {
		starburstAddon, r := prepareClusterForStarburstAddonCreateTest()
		crName := isv.CommonISVInstance.GetAddonCRName()
		crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
		var g = &v1alpha1.StarburstAddon{}

		It("First reconcile should not error", func() {
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: crNamespace,
					Name:      crName,
				},
			}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).ShouldNot(HaveOccurred())

		})
		It("Get the StarburstAddon CR from API - No Error", func() {
			err := r.Client.Get(context.TODO(), types.NamespacedName{
				Namespace: starburstAddon.Namespace,
				Name:      starburstAddon.Name,
			}, g)
			Expect(err).ShouldNot(HaveOccurred())

		})

		// Check finalizer
		It("Should add finalizers", func() {
			f := g.GetFinalizers()
			for _, e := range f {
				fmt.Println("The finalizer are: ", e)
			}
			Expect(controllerutil.ContainsFinalizer(g, isv.CommonISVInstance.GetISVPrefix()+"addons/finalizer")).To(BeTrue())
		})
		//Context("NFD related tests", func() {
		//	It("Should Have created an NFD CR and mark as owner", func() {
		//		nfdCr := &nfdv1.NodeFeatureDiscovery{}
		//		err := r.Client.Get(context.TODO(), types.NamespacedName{
		//			Name:      common.GlobalConfig.NfdCrName,
		//			Namespace: common.GlobalConfig.AddonNamespace,
		//		}, nfdCr)
		//		Expect(err).ShouldNot(HaveOccurred())
		//
		//		ownerRef := nfdCr.ObjectMeta.OwnerReferences[0]
		//		Expect(ownerRef.UID).To(Equal(g.UID))
		//	})
		//	It("Should contain condition", func() {
		//		Expect(common.ContainCondition(g.Status.Conditions, "NodeFeatureDiscoveryDeployed", "True")).To(BeTrue())
		//	})
		//})
		//Context("ClusterPolicy related tests", func() {
		//	It("Should Create gpu-operator ClusterPolicy CR", func() {
		//		clusterPolicyCr := &gpuv1.ClusterPolicy{}
		//		err := r.Client.Get(context.TODO(), types.NamespacedName{
		//			Name: common.GlobalConfig.ClusterPolicyName,
		//		}, clusterPolicyCr)
		//		Expect(err).ShouldNot(HaveOccurred())
		//	})
		//	It("Should contain condition", func() {
		//		Expect(common.ContainCondition(g.Status.Conditions, "ClusterPolicyDeployed", "True")).To(BeTrue())
		//	})
		//})
	})

	Context("Delete reconcile", func() {
		starburstAddon, r := prepareClusterForStarburstAddonDeletionTest()

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: starburstAddon.Namespace,
				Name:      starburstAddon.Name,
			},
		}

		_, err := r.Reconcile(context.TODO(), req)

		It("should return an error", func() {
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("not all resources have been deleted"))
		})

		It("should delete the StarburstAddon CSV", func() {
			g := &operatorsv1alpha1.ClusterServiceVersion{}
			crName := isv.CommonISVInstance.GetAddonCRName()
			crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
			err := r.Client.Get(context.TODO(), types.NamespacedName{
				Name:      crName,
				Namespace: crNamespace,
			}, g)
			Expect(err).Should(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		It("should already find the StarburstAddon CR deleted", func() {
			err := r.Client.Delete(context.TODO(), starburstAddon)
			Expect(err).Should(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		//It("should delete the ClusterPolicy CR", func() {
		//	cp := &gpuv1.ClusterPolicy{}
		//	err := r.Get(context.TODO(), client.ObjectKey{
		//		Name: common.GlobalConfig.ClusterPolicyName,
		//	}, cp)
		//	Expect(err).Should(HaveOccurred())
		//	Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		//})

		It("should delete the Subscription CR", func() {
			s := &operatorsv1alpha1.Subscription{}
			err := r.Get(context.TODO(), client.ObjectKey{
				Name:      "starburst-addon-subscription",
				Namespace: starburstAddon.Namespace,
			}, s)
			Expect(err).Should(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		//It("should delete the NFD CR", func() {
		//	nfd := &nfdv1.NodeFeatureDiscovery{}
		//	err := r.Get(context.TODO(), types.NamespacedName{
		//		Name:      common.GlobalConfig.NfdCrName,
		//		Namespace: starburstAddon.Namespace,
		//	}, nfd)
		//	Expect(err).Should(HaveOccurred())
		//	Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		//})

		Context("a second time", func() {
			_, err := r.Reconcile(context.TODO(), req)

			It("should not return an error", func() {
				Expect(err).ShouldNot(HaveOccurred())
			})
		})
	})
})

func prepareClusterForStarburstAddonCreateTest() (*v1alpha1.StarburstAddon, *StarburstAddonReconciler) {
	starburstAddon := &v1alpha1.StarburstAddon{}
	starburstAddon.Name = "TestAddon"
	crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
	starburstAddon.Namespace = crNamespace
	starburstAddon.APIVersion = "v1alpha1"
	starburstAddon.UID = types.UID("uid-uid")
	starburstAddon.Kind = "StarburstAddon"

	starburstLicense := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "starburst-license",
			Namespace: crNamespace,
		},
		Data: map[string][]byte{
			"starburstdata.license": []byte("dummyLicense"),
		},
	}

	f, err := os.ReadFile("../../test-resources/enterprise.yaml") // just pass the file name
	if err != nil {
		fmt.Print(err)
	}

	vaultSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      isv.CommonISVInstance.GetISVPrefix() + "-addon",
			Namespace: crNamespace,
		},
		Data: map[string][]byte{
			"enterprise":       f,
			"token-url":        []byte("dummyTokenURL"),
			"remote-write-url": []byte("dummyRemoteWriteURL"),
			"regex":            []byte("dummyRegex"),
			"metrics":          []byte("dummyMetrics"),
			"rules":            []byte("dummyRules"),
		},
	}

	r := newTestStarburstAddonReconciler(starburstAddon, starburstLicense, vaultSecret)

	return starburstAddon, r
}

func prepareClusterForStarburstAddonDeletionTest() (*v1alpha1.StarburstAddon, *StarburstAddonReconciler) {
	starburstAddon := &v1alpha1.StarburstAddon{}
	starburstAddon.Name = "TestAddon"
	crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
	crName := isv.CommonISVInstance.GetAddonCRName()
	starburstAddon.Namespace = crNamespace
	starburstAddon.UID = types.UID("uid-uid")
	starburstAddon.Finalizers = []string{isv.CommonISVInstance.GetISVPrefix() + "addons/finalizer"}
	now := metav1.NewTime(time.Now())
	starburstAddon.ObjectMeta.DeletionTimestamp = &now

	kind := "StarburstAddon"
	gvk := v1alpha1.GroupVersion.WithKind(kind)

	// This addon's CSV
	starburstAddonCsv := NewCsv(crNamespace, crName, "")

	ownerRef := metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               starburstAddon.Name,
		UID:                starburstAddon.UID,
		BlockOwnerDeletion: pointer.BoolPtr(true),
		Controller:         pointer.BoolPtr(true),
	}

	//// NFD
	//nfd := &nfdv1.NodeFeatureDiscovery{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name:            common.GlobalConfig.NfdCrName,
	//		Namespace:       starburstAddon.Namespace,
	//		OwnerReferences: []metav1.OwnerReference{ownerRef},
	//	},
	//}
	//
	//// ClusterPolicy
	//clusterPolicy := &gpuv1.ClusterPolicy{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name:            common.GlobalConfig.ClusterPolicyName,
	//		OwnerReferences: []metav1.OwnerReference{ownerRef},
	//	},
	//}

	// Subscription
	subscription := &operatorsv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "starburst-addon-subscription",
			Namespace:       starburstAddon.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}

	r := newTestStarburstAddonReconciler(starburstAddon, starburstAddonCsv, subscription)

	return starburstAddon, r
}

func newTestStarburstAddonReconciler(objs ...runtime.Object) *StarburstAddonReconciler {
	s := scheme.Scheme

	Expect(operatorsv1alpha1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(operatorsv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(v1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(v1alpha1.AddToScheme(s)).ShouldNot(HaveOccurred())
	//Expect(gpuv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	//Expect(nfdv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(configv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	//Expect(appsv1.AddToScheme(s)).ShouldNot(HaveOccurred())

	clusterVersion := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Status: configv1.ClusterVersionStatus{
			History: []configv1.UpdateHistory{
				{
					State:   configv1.CompletedUpdate,
					Version: "4.9.7",
				},
			},
		},
	}

	objs = append(objs, clusterVersion)

	starburstOperatorCsv := &operatorsv1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "isv-starburst-operator.v0.51.0",
			Namespace: isv.CommonISVInstance.GetAddonCRNamespace(),
		},
	}
	fmt.Println("The starburst csv: ", starburstOperatorCsv)

	objs = append(objs, starburstOperatorCsv)

	fmt.Println("newTestStarburstAddonReconciler objs: ", objs)
	c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
	fmt.Println("The Client c: ", c)
	return &StarburstAddonReconciler{
		Client: c,
		Scheme: s,
	}
}

func NewCsv(namespace string, name string, almExample string) *operatorsv1alpha1.ClusterServiceVersion {
	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	csv.Name = name
	csv.Namespace = namespace
	csv.ObjectMeta.Annotations = map[string]string{
		"alm-examples": almExample,
	}
	return csv
}

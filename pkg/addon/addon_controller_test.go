package addon

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/scheme"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
			Expect(controllerutil.ContainsFinalizer(g, isv.CommonISVInstance.GetISVPrefix()+"addons/finalizer")).To(BeTrue())
		})
	})

	Context("Delete reconcile", func() {
		starburstAddon, r := prepareClusterForStarburstAddonDeletionTest()

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: starburstAddon.Namespace,
				Name:      starburstAddon.Name,
			},
		}

		It("should already find the Starburst enterprise CR deleted", func() {
			_, err := r.Reconcile(context.TODO(), req)

			Expect(err).Should(Not(HaveOccurred()))

			enterprise := &unstructured.Unstructured{}
			enterprise.SetKind("StarburstEnterprise")
			enterprise.SetAPIVersion("charts.starburstdata.com/v1alpha1")

			err = r.Client.Get(context.TODO(), types.NamespacedName{
				Name:      buildEnterpriseName(req.Name),
				Namespace: isv.CommonISVInstance.GetAddonCRNamespace(),
			}, enterprise)
			Expect(err).Should(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		It("should already find the StarburstAddon CR deleted", func() {
			err := r.Client.Delete(context.TODO(), starburstAddon)
			Expect(err).Should(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		Context("a second time", func() {
			It("should not return an error", func() {
				_, err := r.Reconcile(context.TODO(), req)
				Expect(err).ShouldNot(HaveOccurred())
			})
		})
	})
})

func prepareClusterForStarburstAddonCreateTest() (*v1alpha1.StarburstAddon, *StarburstAddonReconciler) {
	starburstAddon := &v1alpha1.StarburstAddon{}
	crName := isv.CommonISVInstance.GetAddonCRName()
	starburstAddon.Name = crName
	crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
	starburstAddon.Namespace = crNamespace
	starburstAddon.APIVersion = "v1alpha1"
	starburstAddon.UID = types.UID("uid-uid")
	starburstAddon.Kind = "StarburstAddon"

	addonParamsSecret, vaultSecret := createSecretObjs(crNamespace)

	p, sm, fm, promRules, enterprise := createAdditionalObjs(crName, crNamespace)

	r := newTestStarburstAddonReconciler(starburstAddon, addonParamsSecret, vaultSecret, p, sm, fm, promRules, &enterprise)

	return starburstAddon, r
}

func createAdditionalObjs(crName string, crNamespace string) (*promv1.Prometheus, *promv1.ServiceMonitor, *promv1.ServiceMonitor, *promv1.PrometheusRule, unstructured.Unstructured) {
	p := &promv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName + "-prometheus",
			Namespace: crNamespace,
		}}

	sm := &promv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName + "-servicemonitor",
			Namespace: crNamespace,
		}}

	fm := &promv1.ServiceMonitor{}
	fm.APIVersion = "monitoring.coreos.com/v1"
	fm.Kind = "ServiceMonitor"
	fm.Name = crName + "-federation"
	fm.Namespace = crNamespace

	promRules := &promv1.PrometheusRule{}
	promRules.APIVersion = "monitoring.coreos.com/v1"
	promRules.Kind = "PrometheusRule"
	promRules.Name = crName + "-rules"
	promRules.Namespace = crNamespace

	enterprise := createBasicUnstructureEnterpriseObj(crName, crNamespace)

	return p, sm, fm, promRules, enterprise
}

func createBasicUnstructureEnterpriseObj(crName string, crNamespace string) unstructured.Unstructured {
	enterprise := unstructured.Unstructured{}
	enterprise.SetName(buildEnterpriseName(crName))
	enterprise.SetNamespace(crNamespace)
	enterprise.SetGroupVersionKind(schema.GroupVersionKind{Kind: "StarburstEnterprise", Version: "v1alpha1", Group: "charts.starburstdata.com"})
	return enterprise
}

func createSecretObjs(crNamespace string) (*v1.Secret, *v1.Secret) {
	addonParamsSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon-isv-starburst-operator-parameters",
			Namespace: crNamespace,
		},
		Data: map[string][]byte{
			"starburst-license": []byte("dummyLicense"),
		},
	}

	//Hack because fake client has issues with unstructured data (so we make sure that the enterprise CR wont be created/updated
	f, err := os.ReadFile("../../test-resources/enterprise.yaml") // just pass the file name
	if err != nil {
		fmt.Print(err)
	}

	vaultSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon",
			Namespace: crNamespace,
		},
		Data: map[string][]byte{
			"starburstenterprise.yaml": f,
			"token-url":                []byte("dummyTokenURL"),
			"remote-write-url":         []byte("dummyRemoteWriteURL"),
			"regex":                    []byte("dummyRegex"),
			"metrics":                  []byte("dummyMetrics"),
			"rules":                    []byte("dummyRules"),
		},
	}
	return addonParamsSecret, vaultSecret
}

func prepareClusterForStarburstAddonDeletionTest() (*v1alpha1.StarburstAddon, *StarburstAddonReconciler) {
	starburstAddon := &v1alpha1.StarburstAddon{}
	crName := isv.CommonISVInstance.GetAddonCRName()
	starburstAddon.Name = crName
	crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
	starburstAddon.Namespace = crNamespace
	starburstAddon.UID = types.UID("uid-uid")
	starburstAddon.Finalizers = []string{isv.CommonISVInstance.GetISVPrefix() + "addons/finalizer"}
	now := metav1.NewTime(time.Now())
	starburstAddon.ObjectMeta.DeletionTimestamp = &now

	addonParamsSecret, vaultSecret := createSecretObjs(crNamespace)

	p, sm, fm, promRules, enterprise := createAdditionalObjs(crName, crNamespace)

	r := newTestStarburstAddonReconciler(starburstAddon, addonParamsSecret, vaultSecret, p, sm, fm, promRules, &enterprise)

	return starburstAddon, r
}

func newTestStarburstAddonReconciler(objs ...runtime.Object) *StarburstAddonReconciler {
	s := scheme.Scheme

	Expect(operatorsv1alpha1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(operatorsv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(v1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(v1alpha1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(promv1.AddToScheme(s)).ShouldNot(HaveOccurred())
	Expect(configv1.AddToScheme(s)).ShouldNot(HaveOccurred())

	clusterVersion := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
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

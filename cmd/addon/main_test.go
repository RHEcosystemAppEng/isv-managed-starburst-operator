package main

import (
	"context"
	"testing"

	"github.com/isv-managed-starburst-operator/api/v1alpha1"
	"github.com/isv-managed-starburst-operator/pkg/isv"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Main Suite")
}

var _ = Describe("Jumpstart StarburstAddon", func() {
	It("Should create StarburstAddon CR if not exists", func() {
		c := newTestClientWith()
		crName := isv.CommonISVInstance.GetAddonCRName()
		crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
		Expect(jumpstartAddon(c)).ShouldNot(HaveOccurred())
		g := &v1alpha1.StarburstAddon{}
		Expect(c.Get(context.TODO(), types.NamespacedName{
			Name:      crName,
			Namespace: crNamespace,
		}, g)).ShouldNot(HaveOccurred())
	})
	It("Should not fail if a StarburstAddon CR already Exists", func() {
		g := &v1alpha1.StarburstAddon{}
		crName := isv.CommonISVInstance.GetAddonCRName()
		crNamespace := isv.CommonISVInstance.GetAddonCRNamespace()
		g.Name = crName
		g.Namespace = crNamespace
		c := newTestClientWith(g)
		Expect(jumpstartAddon(c)).ShouldNot(HaveOccurred())
		Expect(c.Get(context.TODO(), types.NamespacedName{
			Name:      crName,
			Namespace: crNamespace,
		}, g)).Should(Succeed())
	})
})

func newTestClientWith(objs ...runtime.Object) client.Client {
	s := clientgoscheme.Scheme
	Expect(v1alpha1.AddToScheme(s)).ShouldNot(HaveOccurred())
	return fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
}

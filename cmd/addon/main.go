/*
Copyright 2023.

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

package main

import (
	"context"
	"flag"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/isv-managed-starburst-operator/api/v1alpha1"
	"github.com/isv-managed-starburst-operator/pkg/addon"
	"github.com/isv-managed-starburst-operator/pkg/isv"
	configv1 "github.com/openshift/api/config/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	// load options and flags
	addon.LoadAddonOptions()

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// create the manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     addon.Flags.MetricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: addon.Flags.ProbeAddr,
		LeaderElection:         addon.Flags.EnableLeaderElection,
		LeaderElectionID:       "2827ef65.isv.managed",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// create the controller
	if err = (&addon.StarburstAddonReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StarburstAddon")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	c, err := client.New(ctrlconfig.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "failed to create client to jumpstart addon")
		os.Exit(1)
	}

	if err := jumpstartAddon(c); err != nil {
		setupLog.Error(err, "failed to jumpstart addon")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func jumpstartAddon(client client.Client) error {
	starburstAddon := &v1alpha1.StarburstAddon{}
	crName := isv.CommonISVInstance.GetCRName()
	crNamespace := isv.CommonISVInstance.GetCRNamespace()
	err := client.Get(context.TODO(), types.NamespacedName{
		Name:      crName,
		Namespace: crNamespace,
	}, starburstAddon)
	isNotFound := k8serrors.IsNotFound(err)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to fetch StarburstAddon CR %s in %s: %w", crName, crNamespace, err)
	}

	starburstAddon.ObjectMeta = metav1.ObjectMeta{
		Name:      crName,
		Namespace: crNamespace,
	}

	result, err := controllerutil.CreateOrPatch(context.TODO(), client, starburstAddon, nil)

	if err != nil {
		return fmt.Errorf("failed to reconcile StarburstAddon CR %s in %s, result: %v. error: %w", crName, crNamespace, result, err)
	}

	return nil
}

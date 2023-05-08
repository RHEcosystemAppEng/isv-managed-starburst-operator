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
	"flag"
	"os"

	"github.com/isv-managed-starburst-operator/pkg/webhook"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	// load and parse flags and options
	webhook.LoadEnterpriseValidatorFlags()
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := ctrl.Log.WithName("isv-managed-starburst-operator-webhook")
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// create the manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 runtime.NewScheme(),
		MetricsBindAddress:     webhook.Flags.MetricsAddr,
		HealthProbeBindAddress: webhook.Flags.ProbeAddr,
		LeaderElection:         webhook.Flags.EnableLeaderElection,
		LeaderElectionID:       "4769ef65.isv-managed-starburst-operator-webhook",
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// add the webhook controller
	if err := webhook.Add(mgr); err != nil {
		logger.Error(err, "unable to load manager functions")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start the manager")
		os.Exit(1)
	}
}

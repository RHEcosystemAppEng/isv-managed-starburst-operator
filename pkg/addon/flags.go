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

package addon

import "flag"

// AddonFlags encapsulates the addon flags
type AddonFlags struct {
	MetricsAddr          string
	ProbeAddr            string
	EnableLeaderElection bool
}

// our option instance
var Flags = AddonFlags{
	MetricsAddr:          ":8080",
	ProbeAddr:            ":8081",
	EnableLeaderElection: false,
}

// use to load command line flags to options
func LoadAddonOptions() {
	flag.StringVar(&Flags.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&Flags.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&Flags.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager")
}

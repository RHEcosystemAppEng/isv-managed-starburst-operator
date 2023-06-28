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

import (
	"context"
	"fmt"
	"github.com/isv-managed-starburst-operator/api/v1alpha1"
	"github.com/isv-managed-starburst-operator/pkg/isv"
	"github.com/mitchellh/mapstructure"
	configv1 "github.com/openshift/api/config/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

// StarburstAddonReconciler reconciles a StarburstAddon object
type StarburstAddonReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=starburst.isv.managed,resources=starburstaddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=starburst.isv.managed,resources=starburstaddons/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=starburst.isv.managed,resources=starburstaddons/finalizers,verbs=update
// +kubebuilder:rbac:groups=charts.starburstdata.com,resources=starburstenterprises,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=charts.starburstdata.com,resources=starburstenterprises/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=charts.starburstdata.com,resources=starburstenterprises/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources={alertmanagers,prometheuses,alertmanagerconfigs},verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch;update;patch;create;delete

func (r *StarburstAddonReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	fmt.Println("Start reconcile loop!!!")
	// fetch subject addon
	addon := &v1alpha1.StarburstAddon{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, addon); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("addon not found, probably deleted")
			return ctrl.Result{}, nil
		}

		logger.Error(err, "failed to fetch addon")
		return ctrl.Result{}, err
	}

	// Secret
	vault := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      "addon",
		Namespace: req.Namespace,
	}, vault); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Addon Secret not found.")
			return ctrl.Result{Requeue: true}, err
		}

		return ctrl.Result{}, fmt.Errorf("could not get Addon Secret: %v", err)
	}

	manifest := vault.Data["starburstenterprise.yaml"]
	if manifest == nil {
		return ctrl.Result{}, fmt.Errorf("could not get value %v from Addon Secret", "starburstenterprise.yaml")
	}

	// build enterprise resource from file propagated by a secret created for the vault keys
	desiredEnterprise, err := r.buildEnterpriseResource(ctx, addon, req.Namespace, manifest)
	if err != nil {
		logger.Error(err, "failed to fetch enterprise manifest")
		return ctrl.Result{}, err
	}

	finalizerName := isv.CommonISVInstance.GetISVPrefix() + "addons/finalizer"

	// cleanup for deletion
	if !addon.ObjectMeta.DeletionTimestamp.IsZero() {
		// object is currently being deleted
		if controllerutil.ContainsFinalizer(addon, finalizerName) {
			// finalizer exists, delete child enterprise
			enterpriseDelete := &unstructured.Unstructured{}
			enterpriseDelete.SetGroupVersionKind(desiredEnterprise.GroupVersionKind())

			if err := r.Client.Get(
				ctx,
				types.NamespacedName{
					Namespace: req.Namespace,
					Name:      buildEnterpriseName(req.Name),
				},
				enterpriseDelete); err == nil {
				// found enterprise resource, delete it
				if err := r.Client.Delete(ctx, enterpriseDelete); err != nil {
					if k8serrors.IsInternalError(err) && strings.Contains(err.Error(), "connection refused") {
						// if the webhooks's cert secret is not yes fully propagated as files,
						// we might encounter a connection refused scenario, therefore requeueing
						logger.Info("failed deleting the enterprise cr, webhook validation refused connection, requeueing")
						return ctrl.Result{Requeue: true}, nil
					}
					logger.Error(err, "failed deleting enterprise cr")
					return ctrl.Result{}, err
				}
			}

			if err := r.removeSelfCsv(ctx); err != nil {
				return ctrl.Result{}, err
			}

			// deletion done, remove finalizer
			controllerutil.RemoveFinalizer(addon, finalizerName)
			if err := r.Client.Update(ctx, addon); err != nil {
				logger.Error(err, "failed to remove finalizer from new addon")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// object is NOT currently being deleted, add finalizer
	if !controllerutil.ContainsFinalizer(addon, finalizerName) {
		controllerutil.AddFinalizer(addon, finalizerName)

		if err := r.Client.Update(ctx, addon); err != nil {
			logger.Error(err, "failed to set finalizer for new addon")
			return ctrl.Result{}, err
		}
	}

	// Fetch clusterversion instance
	cv := &configv1.ClusterVersion{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name: "version",
	}, cv); err != nil {

		if k8serrors.IsNotFound(err) {
			logger.Info("ClusterVersion not found")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("could not get ClusterVersion CR: %v", err)
	}

	prometheusName := addon.Name + "-prometheus"

	// Deploy Prometheus
	prometheus := &promv1.Prometheus{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      prometheusName,
		Namespace: addon.Namespace,
	}, prometheus); err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Prometheus not found. Creating...")
		// tokenURL, remoteWriteURL, clusterID string
		prometheus = r.DeployPrometheus(vault.Name, string(vault.Data["token-url"]), string(vault.Data["remote-write-url"]), string(vault.Data["regex"]), fetchClusterID(cv), prometheusName, addon.Namespace)
		if err := r.Client.Create(ctx, prometheus); err != nil {
			logger.Error(err, "Could not create Prometheus")
			return ctrl.Result{Requeue: true}, fmt.Errorf("could not create Prometheus: %v", err)
		}

		// Prometheus created successfully
		// We will requeue the request to ensure the Prometheus is created
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		logger.Error(err, "could not get Prometheus")
		// return the error for the next reconcile
		return ctrl.Result{Requeue: true}, fmt.Errorf("could not get Prometheus: %v", err)
	}

	serviceMonitorName := addon.Name + "-servicemonitor"

	// Deploy ServiceMonitor
	serviceMonitor := &promv1.ServiceMonitor{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      serviceMonitorName,
		Namespace: addon.Namespace,
	}, serviceMonitor); err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Service Monitor not found. Creating...")
		serviceMonitor = r.DeployEnterpriseServiceMonitor(serviceMonitorName, addon.Namespace, desiredEnterprise.GetLabels())
		if err := r.Client.Create(ctx, serviceMonitor); err != nil {
			logger.Error(err, "Could not create Service Monitor")
			return ctrl.Result{Requeue: true}, fmt.Errorf("could not create service monitor: %v", err)
		}
	}

	fedServiceMonitorName := addon.Name + "-federation"

	// Deploy Federation ServiceMonitor
	fedServiceMonitor := &promv1.ServiceMonitor{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      fedServiceMonitorName,
		Namespace: addon.Namespace,
	}, fedServiceMonitor); err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Federation Service Monitor not found. Creating...")
		fedServiceMonitor = r.DeployFederationServiceMonitor(fedServiceMonitorName, addon.Namespace, string(vault.Data["metrics.yaml"]))
		if err := r.Client.Create(ctx, fedServiceMonitor); err != nil {
			logger.Error(err, "Could not create Federation Service Monitor")
			return ctrl.Result{Requeue: true}, fmt.Errorf("could not create federation service monitor: %v", err)
		}
	}

	prometheusRuleName := addon.Name + "-rules"

	// Deploy PrometheusRules
	prometheusRule := &promv1.PrometheusRule{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      prometheusRuleName,
		Namespace: addon.Namespace,
	}, prometheusRule); err != nil && k8serrors.IsNotFound(err) {
		logger.Info("Prometheus Rules not found. Creating...")
		prometheusRule, err = r.DeployPrometheusRules(prometheusRuleName, addon.Namespace, vault.Data["rules.yaml"])
		if err != nil {
			logger.Error(err, "could not create prometheus rules")
			return ctrl.Result{Requeue: true}, err
		}

		if err := r.Client.Create(ctx, prometheusRule); err != nil {
			logger.Error(err, "Could not create Prometheus Rules")
			return ctrl.Result{Requeue: true}, fmt.Errorf("could not create Prometheus Rules: %v", err)
		}
	}

	// load isv custom functions
	if err := isv.LoadCustomFuncs(ctx, r.Client, req.Namespace); err != nil {
		logger.Error(err, "failed loading isv custom functions")
		return ctrl.Result{}, err
	}

	// reconcile unstructured enterprise resource
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desiredEnterprise.GroupVersionKind())

	// fetch the existing enterprise resource
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(&desiredEnterprise), current); err != nil {
		if k8serrors.IsNotFound(err) {
			// if current enterprise not found, create a new one
			if err := r.Client.Create(ctx, &desiredEnterprise); err != nil {
				if k8serrors.IsInternalError(err) && strings.Contains(err.Error(), "connection refused") {
					// if the webhooks's cert secret is not yes fully propagated as files,
					// we might encounter a connection refused scenario, therefore requeueing
					logger.Info("failed creating enterprise cr, webhook validation refused connection, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				if k8serrors.IsAlreadyExists(err) {
					logger.Info("enterprise cr was created recently")
					return ctrl.Result{}, nil
				}
				logger.Error(err, "failed creating enterprise cr")
				return ctrl.Result{}, err
			}
		}

		// if current enterprise exists but fetching it failed
		logger.Error(err, "failed creating enterprise cr.")
		return ctrl.Result{}, err
	}

	// reconcile back to desired if current was changed
	setDesiredEnterpriseCRBasicFields(desiredEnterprise, current)
	if !equality.Semantic.DeepDerivative(&desiredEnterprise, current) {
		// NOTE equality should be based on business logic
		if err := r.Client.Update(ctx, &desiredEnterprise); err != nil {
			logger.Error(err, "failed reconciling enterprise cr, requeuing")
			return ctrl.Result{Requeue: true}, err
		}
	}

	return ctrl.Result{}, nil
}

func setDesiredEnterpriseCRBasicFields(desiredEnterprise unstructured.Unstructured, current *unstructured.Unstructured) {
	desiredEnterprise.SetCreationTimestamp(current.GetCreationTimestamp())
	desiredEnterprise.SetResourceVersion(current.GetResourceVersion())
	desiredEnterprise.SetSelfLink(current.GetSelfLink())
	desiredEnterprise.SetUID(current.GetUID())
}

// SetupWithManager sets up the controller with the manager
func (r *StarburstAddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.StarburstAddon{}).
		Complete(r)
}

// builds enterprise resource from file setting the addon as the owner
func (r *StarburstAddonReconciler) buildEnterpriseResource(ctx context.Context, addon *v1alpha1.StarburstAddon, namespace string, manifest []byte) (unstructured.Unstructured, error) {
	enterprise := unstructured.Unstructured{}

	// deserialize the loaded manifest to a yaml
	deserialized := make(map[string]interface{})
	if err := yaml.Unmarshal(manifest, deserialized); err != nil {
		return enterprise, err
	}
	// set unstructured data from the deserialized yaml file
	enterprise.SetUnstructuredContent(deserialized)
	// load isv custom patches
	if err := isv.LoadCustomPatches(ctx, r.Client, namespace, deserialized); err != nil {
		return enterprise, err
	}
	// set name and owner refs
	enterprise.SetName(buildEnterpriseName(addon.Name))
	enterprise.SetNamespace(addon.Namespace)
	enterprise.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(addon, addon.GetObjectKind().GroupVersionKind()),
	})
	enterprise.SetLabels(map[string]string{
		"app": enterprise.GetName(),
	})

	return enterprise, nil

}

func (r *StarburstAddonReconciler) DeployEnterpriseServiceMonitor(serviceMonitorName string, serviceMonitorNamespace string, enterprise map[string]string) *promv1.ServiceMonitor {
	return &promv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName,
			Namespace: serviceMonitorNamespace,
		},
		Spec: promv1.ServiceMonitorSpec{
			NamespaceSelector: promv1.NamespaceSelector{
				MatchNames: []string{serviceMonitorNamespace},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: enterprise,
			},
			Endpoints: []promv1.Endpoint{
				{
					Port:     "metrics",
					Interval: "2s",
				},
			},
		},
	}
}

func (r *StarburstAddonReconciler) DeployPrometheusRules(prometheusRuleName string, prometheusRuleNamespace string, rules []byte) (*promv1.PrometheusRule, error) {
	// deserialize the rules to Rule array
	var rulesArray []promv1.Rule
	var raw interface{}

	// Unmarshal our input YAML file into empty interface
	if err := yaml.Unmarshal(rules, &raw); err != nil {
		return nil, err
	}

	// Use mapstructure to convert our interface{} to promv1.Rule
	decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{WeaklyTypedInput: true, Result: &rulesArray, DecodeHook: ConvertToIntOrStringFunc})
	if err := decoder.Decode(raw); err != nil {
		return nil, err
	}

	// Use the typed object.
	// create PrometheusRules step by step
	promRules := &promv1.PrometheusRule{}
	promRules.APIVersion = "monitoring.coreos.com/v1"
	promRules.Kind = "PrometheusRule"
	promRules.Name = prometheusRuleName
	promRules.Namespace = prometheusRuleNamespace
	promRules.Spec = promv1.PrometheusRuleSpec{
		Groups: []promv1.RuleGroup{
			{
				Name:  isv.CommonISVInstance.GetISVPrefix() + "_alert_rules",
				Rules: rulesArray, //promRules is a slice of promv1.Rule
			},
		},
	}
	return promRules, nil
}

// ConvertToIntOrStringFunc returns a DecodeHookFuncType that converts
// strings and int32 to intstr.IntOrString.
func ConvertToIntOrStringFunc(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if (f.Kind() == reflect.String || f.Kind() == reflect.Int32) &&
		t == reflect.TypeOf(intstr.IntOrString{}) {
		return intstr.Parse(data.(string)), nil
	}

	return data, nil
}

func (r *StarburstAddonReconciler) DeployFederationServiceMonitor(fedServiceMonitorName string, fedServiceMonitorNamespace string, metrics string) *promv1.ServiceMonitor {

	metric := make(map[string][]string)
	metric["match[]"] = append(metric["match[]"], metrics)

	// create federated serviceMonitor
	fedServiceMonitor := &promv1.ServiceMonitor{}
	fedServiceMonitor.APIVersion = "monitoring.coreos.com/v1"
	fedServiceMonitor.Kind = "ServiceMonitor"
	fedServiceMonitor.Name = fedServiceMonitorName
	fedServiceMonitor.Namespace = fedServiceMonitorNamespace
	fedServiceMonitor.Spec = promv1.ServiceMonitorSpec{
		JobLabel: "openshift-monitoring-federation",
		NamespaceSelector: promv1.NamespaceSelector{
			MatchNames: []string{
				"openshift-monitoring",
			},
		},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/instance": "k8s",
			},
		},
		Endpoints: []promv1.Endpoint{
			{
				BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				Port:            "web",
				Path:            "/federate",
				Interval:        "30s",
				Scheme:          "https",
				Params:          metric,
				TLSConfig: &promv1.TLSConfig{
					SafeTLSConfig: promv1.SafeTLSConfig{
						InsecureSkipVerify: true,
						ServerName:         "prometheus-k8s.openshift-monitoring.svc.cluster.local",
					},
					CAFile: "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt",
				},
			},
		},
	}

	return fedServiceMonitor
}

func (r *StarburstAddonReconciler) DeployPrometheus(vaultSecretName string, tokenURL, remoteWriteURL, regex, clusterID, prometheusName, namespace string) *promv1.Prometheus {

	//prometheusSelector := addon.Spec.ResourceSelector

	return &promv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prometheusName,
			Namespace: namespace,
		},
		Spec: promv1.PrometheusSpec{
			//RuleSelector: prometheusSelector,
			CommonPrometheusFields: promv1.CommonPrometheusFields{
				ExternalLabels: map[string]string{
					"cluster_id": clusterID,
				},
				LogLevel: "debug",
				RemoteWrite: []promv1.RemoteWriteSpec{
					{
						WriteRelabelConfigs: []promv1.RelabelConfig{
							{
								Action: "keep",
								Regex:  regex, //"csv_succeeded$|csv_abnormal$|cluster_version$|ALERTS$|subscription_sync_total|trino_.*$|jvm_heap_memory_used$|node_.*$|namespace_.*$|kube_.*$|cluster.*$|container_.*$",
							},
						},
						URL: remoteWriteURL,
						TLSConfig: &promv1.TLSConfig{
							SafeTLSConfig: promv1.SafeTLSConfig{
								InsecureSkipVerify: true,
							},
						},
						OAuth2: &promv1.OAuth2{
							ClientID: promv1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: vaultSecretName,
									},
									Key: "client-id",
								},
							},
							ClientSecret: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: vaultSecretName,
								},
								Key: "client-secret",
							},
							TokenURL: tokenURL,
						},
					},
				},
				ServiceMonitorNamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": namespace,
					},
				},

				ServiceMonitorSelector: &metav1.LabelSelector{},
				PodMonitorSelector:     &metav1.LabelSelector{},
				ServiceAccountName:     "starburst-enterprise-helm-operator-controller-manager",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("400Mi"),
					},
				},
			},
		},
	}
}

// build the desired enterprise name
func buildEnterpriseName(name string) string {
	return fmt.Sprintf("%s-enterprise", name)
}

func fetchClusterID(cv *configv1.ClusterVersion) string {
	//return "1v529ivvikohbpg8pgfihegcdjhudjng"
	clusterID := cv.Spec.ClusterID
	return string(clusterID)
}

func (r *StarburstAddonReconciler) removeSelfCsv(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("Reconcile Step", "Addon CSV Deletion")
	logger.Info("Cleanup Reconcile | Delete own CSV")

	addonCsv, err := getCsvWithPrefix(r.Client, isv.CommonISVInstance.GetAddonCRNamespace(), "isv-starburst-operator")
	if err != nil {
		return err
	}

	err = r.Delete(ctx, addonCsv)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete StarburstAddon Operator CSV %s: %w", addonCsv.Name, err)
	}

	return nil
}

func getCsvWithPrefix(c client.Client, namespace string, prefix string) (*operatorsv1alpha1.ClusterServiceVersion, error) {

	csvs := operatorsv1alpha1.ClusterServiceVersionList{}
	err := c.List(context.TODO(), &csvs, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}

	for _, csv := range csvs.Items {
		if strings.HasPrefix(csv.Name, prefix) {
			return &csv, nil
		}
	}
	return nil, k8serrors.NewNotFound(schema.ParseGroupResource(""), fmt.Sprintf("%v/%v*", namespace, prefix))
}

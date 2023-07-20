package isv

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func applyStarburstLicense(ctx context.Context, cl client.Client, ns string) error {
	// load secret containing addon parameters
	addonParamsKey := types.NamespacedName{
		Name:      "addon-isv-starburst-operator-parameters",
		Namespace: ns,
	}

	addonParams := &corev1.Secret{}
	if err := cl.Get(ctx, addonParamsKey, addonParams); err != nil {
		return err
	}

	licenseParamsKey := types.NamespacedName{
		Name:      "starburst-license",
		Namespace: ns,
	}

	// check if license secret already exists
	licenseSecret := &corev1.Secret{}
	if err := cl.Get(ctx, licenseParamsKey, licenseSecret); err == nil && licenseSecret != nil {
		return nil
	}

	// create license secret required by starburst enterprise
	starburstLicense := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "starburst-license",
			Namespace: ns,
		},
		Data: map[string][]byte{
			"starburstdata.license": addonParams.Data["starburst-license"],
		},
	}
	if err := cl.Create(ctx, starburstLicense); err != nil {
		return err
	}

	return nil
}

func applyStarburstTileParameters(ctx context.Context, cl client.Client, ns string, subject map[string]interface{}) error {
	fmt.Println("map:", subject)

	// load secret containing addon parameters
	addonParamsKey := types.NamespacedName{
		Name:      "addon-isv-starburst-operator-parameters",
		Namespace: ns,
	}

	addonParams := &corev1.Secret{}
	if err := cl.Get(ctx, addonParamsKey, addonParams); err != nil {
		return err
	}

	starburstImageTag, err := getStarburstImageTag(subject)

	if err != nil {
		return err
	}

	cpu := addonParams.Data["cpu"]
	if cpu != nil {
		replaceCpuYamlVal(string(cpu), subject, starburstImageTag)
	}

	memory := addonParams.Data["memory"]
	if memory != nil {
		replaceMemoryYamlVal(string(memory), subject)
	}

	replicas := addonParams.Data["replicas"]
	if replicas != nil {
		replaceReplicasYamlVal(string(replicas), subject)
	}

	fmt.Println("updatedMap:", subject)

	return nil
}

func getStarburstImageTag(yaml map[string]interface{}) (string, error) {
	spec, ok := yaml["spec"]
	if ok {
		specMap, ok := spec.(map[string]interface{})
		if ok {
			imageObj, ok := specMap["image"]
			if ok {
				imageMap, ok := imageObj.(map[string]interface{})
				if ok {
					return imageMap["tag"].(string), nil
				}
			}
		}
	}

	return "", fmt.Errorf("could not get Starburst image tag")
}

func replaceCpuYamlVal(val string, yaml map[string]interface{}, imageTag string) {
	spec, ok := yaml["spec"]
	if ok {
		specMap, ok := spec.(map[string]interface{})
		if ok {
			if strings.HasPrefix(imageTag, "380") {
				replaceCpuInTag380("coordinator", val, specMap)
				replaceCpuInTag380("worker", val, specMap)
			} else if strings.HasPrefix(imageTag, "402") {
				replaceCpuInTag("coordinator", val, specMap)
				replaceCpuInTag("worker", val, specMap)
			}
		}
	}
}

func replaceReplicasYamlVal(val string, yaml map[string]interface{}) {
	spec, ok := yaml["spec"]
	if ok {
		specMap, ok := spec.(map[string]interface{})
		if ok {
			replaceReplicasInTag("worker", val, specMap)
			replaceReplicasInTag("coordinator", val, specMap)
		}
	}
}

func replaceMemoryYamlVal(val string, yaml map[string]interface{}) {
	spec, ok := yaml["spec"]
	if ok {
		specMap, ok := spec.(map[string]interface{})
		if ok {
			replaceMemoryInTag("coordinator", val, specMap)
			replaceMemoryInTag("worker", val, specMap)
		}
	}
}

func replaceReplicasInTag(tag string, replicasVal string, specMap map[string]interface{}) {
	tagObj, ok := specMap[tag]
	if ok {
		tagMap, ok := tagObj.(map[string]interface{})
		if ok {
			tagMap["replicas"] = replicasVal
		}
	}
}

func replaceCpuInTag(tag string, cpuVal string, specMap map[string]interface{}) {
	tagObj, ok := specMap[tag]
	if ok {
		tagMap, ok := tagObj.(map[string]interface{})
		if ok {
			resources, ok := tagMap["resources"]
			if ok {
				resourcesMap, ok := resources.(map[string]interface{})
				if ok {
					resourcesMap["cpu"] = cpuVal
				}
			} else {
				tagMap["resources"] = map[string]interface{}{"cpu": cpuVal}
			}
		}
	}
}

func replaceCpuInTag380(tag string, cpuVal string, specMap map[string]interface{}) {
	tagObj, ok := specMap[tag]
	if ok {
		tagMap, ok := tagObj.(map[string]interface{})
		if ok {
			resources, ok := tagMap["resources"]
			if ok {
				resourcesMap, ok := resources.(map[string]interface{})
				if ok {
					limits, ok := resourcesMap["limits"]
					if ok {
						limitsMap, ok := limits.(map[string]interface{})
						if ok {
							_, ok := limitsMap["cpu"]
							if ok {
								limitsMap["cpu"] = cpuVal
							}
						}
					}

					requests, ok := resourcesMap["requests"]
					if ok {
						requestsMap, ok := requests.(map[string]interface{})
						if ok {
							requestsMap["cpu"] = cpuVal
						}
					}
				}
			} else {
				tagMap["resources"] = map[string]interface{}{"limits": map[string]interface{}{"cpu": cpuVal},
					"requests": map[string]interface{}{"cpu": cpuVal}}
			}
		}
	}
}

func replaceMemoryInTag(tag string, memVal string, specMap map[string]interface{}) {
	tagObj, ok := specMap[tag]
	if ok {
		tagMap, ok := tagObj.(map[string]interface{})
		if ok {
			resources, ok := tagMap["resources"]
			if ok {
				resourcesMap, ok := resources.(map[string]interface{})
				if ok {
					resourcesMap["memory"] = memVal
				}
			} else {
				tagMap["resources"] = struct{ memory string }{memVal}
			}
		}
	}
}

type Starburst struct{}

func (starburst *Starburst) GetISVPrefix() string {
	return "starburst"
}

func (starburst *Starburst) GetAddonCRName() string {
	return "starburst-cr"
}

func (starburst *Starburst) GetAddonCRNamespace() string {
	return "redhat-starburst-op"
}

func (starburst *Starburst) GetAddonID() string {
	return "isv-starburst-operator"
}

func init() {
	CommonISVInstance = &Starburst{}

	isvCustomFuncs = append(isvCustomFuncs, applyStarburstLicense)

	isvCustomPatches = append(isvCustomPatches, applyStarburstTileParameters)
}

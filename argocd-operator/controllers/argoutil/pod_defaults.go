package argoutil

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// SetPodSpecDefaults fills in the fields the API server defaults on a PodSpec,
// so a desired spec compares equal to the live one when nothing actually changed.
// ponytail: covers only the defaults our builders leave unset; add more here if a new false diff shows up in logs.
func SetPodSpecDefaults(spec *corev1.PodSpec) {
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	for i := range spec.Volumes {
		v := &spec.Volumes[i].VolumeSource
		switch {
		case v.Secret != nil && v.Secret.DefaultMode == nil:
			v.Secret.DefaultMode = ptr.To(corev1.SecretVolumeSourceDefaultMode)
		case v.ConfigMap != nil && v.ConfigMap.DefaultMode == nil:
			v.ConfigMap.DefaultMode = ptr.To(corev1.ConfigMapVolumeSourceDefaultMode)
		case v.Projected != nil && v.Projected.DefaultMode == nil:
			v.Projected.DefaultMode = ptr.To(corev1.ProjectedVolumeSourceDefaultMode)
		case v.DownwardAPI != nil && v.DownwardAPI.DefaultMode == nil:
			v.DownwardAPI.DefaultMode = ptr.To(corev1.DownwardAPIVolumeSourceDefaultMode)
		}
	}
	setContainerDefaults(spec.InitContainers)
	setContainerDefaults(spec.Containers)
}

func setContainerDefaults(cs []corev1.Container) {
	for i := range cs {
		c := &cs[i]
		if c.TerminationMessagePath == "" {
			c.TerminationMessagePath = corev1.TerminationMessagePathDefault
		}
		if c.TerminationMessagePolicy == "" {
			c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
		}
		for j := range c.Ports {
			if c.Ports[j].Protocol == "" {
				c.Ports[j].Protocol = corev1.ProtocolTCP
			}
		}
		for j := range c.Env {
			if f := c.Env[j].ValueFrom; f != nil && f.FieldRef != nil && f.FieldRef.APIVersion == "" {
				f.FieldRef.APIVersion = "v1"
			}
		}
	}
}

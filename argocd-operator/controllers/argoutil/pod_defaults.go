package argoutil

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// SetPodSpecDefaults fills in the fields the API server defaults on a PodSpec,
// so a desired spec compares equal to the live one when nothing actually changed.
// ponytail: covers only the defaults our builders leave unset; add more here if a new false diff shows up in logs.
func SetPodSpecDefaults(spec *corev1.PodSpec) {
	// Detach from pointers shared with the ArgoCD CR (sidecar probes, user volumes) so the CR stays untouched.
	*spec = *spec.DeepCopy()
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
		if c.ImagePullPolicy == "" && c.Image != "" {
			c.ImagePullPolicy = defaultPullPolicy(c.Image)
		}
		setProbeDefaults(c.LivenessProbe)
		setProbeDefaults(c.ReadinessProbe)
		setProbeDefaults(c.StartupProbe)
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

// defaultPullPolicy mirrors the API server: Always for ":latest" or an untagged image, else IfNotPresent.
func defaultPullPolicy(image string) corev1.PullPolicy {
	if strings.Contains(image, "@") {
		return corev1.PullIfNotPresent
	}
	name := image[strings.LastIndex(image, "/")+1:]
	if i := strings.LastIndex(name, ":"); i == -1 || name[i+1:] == "latest" {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

func setProbeDefaults(p *corev1.Probe) {
	if p == nil {
		return
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = 1
	}
	if p.PeriodSeconds == 0 {
		p.PeriodSeconds = 10
	}
	if p.SuccessThreshold == 0 {
		p.SuccessThreshold = 1
	}
	if p.FailureThreshold == 0 {
		p.FailureThreshold = 3
	}
	if p.HTTPGet != nil && p.HTTPGet.Scheme == "" {
		p.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
}

package argoutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestSetPodSpecDefaults(t *testing.T) {
	spec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{Name: "s", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "x"}}},
			{Name: "c", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{DefaultMode: ptr.To[int32](0600)}}},
		},
		InitContainers: []corev1.Container{{Name: "i"}},
		Containers: []corev1.Container{{Name: "c", Ports: []corev1.ContainerPort{{ContainerPort: 80}},
			Env: []corev1.EnvVar{{Name: "NS", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}}}},
	}
	SetPodSpecDefaults(&spec)

	assert.NotNil(t, spec.SecurityContext)
	assert.Equal(t, int32(0644), *spec.Volumes[0].Secret.DefaultMode)
	assert.Equal(t, int32(0600), *spec.Volumes[1].ConfigMap.DefaultMode, "explicit mode must be kept")
	assert.Equal(t, corev1.TerminationMessagePathDefault, spec.InitContainers[0].TerminationMessagePath)
	assert.Equal(t, corev1.TerminationMessageReadFile, spec.Containers[0].TerminationMessagePolicy)
	assert.Equal(t, corev1.ProtocolTCP, spec.Containers[0].Ports[0].Protocol)
	assert.Equal(t, "v1", spec.Containers[0].Env[0].ValueFrom.FieldRef.APIVersion)
}

func TestSetPodSpecDefaultsPullPolicyAndProbes(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{
		{Name: "a", Image: "quay.io/x/y"},
		{Name: "b", Image: "localhost:5000/y:latest"},
		{Name: "c", Image: "localhost:5000/y:v1"},
		{Name: "d", Image: "y@sha256:abc"},
		{Name: "e", Image: "y:v1", ImagePullPolicy: corev1.PullNever,
			ReadinessProbe: &corev1.Probe{PeriodSeconds: 30, ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/"}}}},
	}}
	SetPodSpecDefaults(&spec)

	c := spec.Containers
	assert.Equal(t, corev1.PullAlways, c[0].ImagePullPolicy)
	assert.Equal(t, corev1.PullAlways, c[1].ImagePullPolicy)
	assert.Equal(t, corev1.PullIfNotPresent, c[2].ImagePullPolicy)
	assert.Equal(t, corev1.PullIfNotPresent, c[3].ImagePullPolicy)
	assert.Equal(t, corev1.PullNever, c[4].ImagePullPolicy, "explicit policy must be kept")
	p := c[4].ReadinessProbe
	assert.Equal(t, int32(30), p.PeriodSeconds, "explicit period must be kept")
	assert.Equal(t, []int32{1, 1, 3}, []int32{p.TimeoutSeconds, p.SuccessThreshold, p.FailureThreshold})
	assert.Equal(t, corev1.URISchemeHTTP, p.HTTPGet.Scheme)
}

func TestSetPodSpecDefaultsDoesNotMutateSource(t *testing.T) {
	probe := &corev1.Probe{}
	sidecars := []corev1.Container{{Name: "s", LivenessProbe: probe}}
	spec := corev1.PodSpec{Containers: append([]corev1.Container{}, sidecars...)}
	SetPodSpecDefaults(&spec)

	assert.Equal(t, int32(1), spec.Containers[0].LivenessProbe.TimeoutSeconds)
	assert.Equal(t, int32(0), probe.TimeoutSeconds, "CR-owned probe must not be defaulted")
}

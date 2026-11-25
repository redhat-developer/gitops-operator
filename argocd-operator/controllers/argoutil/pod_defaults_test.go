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

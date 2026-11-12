// Copyright 2019 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argocd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMergeOperatorVolumes(t *testing.T) {
	emptyDir := corev1.Volume{
		Name: "tmp",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	plugins := corev1.Volume{
		Name: "plugins",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	storageClass := "managed-csi"
	storage := resource.MustParse("20Gi")
	ephemeralTmp := corev1.Volume{
		Name: "tmp",
		VolumeSource: corev1.VolumeSource{
			Ephemeral: &corev1.EphemeralVolumeSource{
				VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: &storageClass,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: storage,
							},
						},
					},
				},
			},
		},
	}
	extra := corev1.Volume{
		Name: "custom-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	t.Run("no user volumes", func(t *testing.T) {
		got := mergeOperatorVolumes([]corev1.Volume{emptyDir, plugins}, nil)
		assert.Equal(t, []corev1.Volume{emptyDir, plugins}, got)
	})

	t.Run("replaces matching emptyDir and appends new volumes", func(t *testing.T) {
		got := mergeOperatorVolumes([]corev1.Volume{emptyDir, plugins}, []corev1.Volume{extra, ephemeralTmp})
		assert.Equal(t, []corev1.Volume{ephemeralTmp, plugins, extra}, got)
		assert.Nil(t, got[0].EmptyDir)
		assert.Equal(t, &storageClass, got[0].Ephemeral.VolumeClaimTemplate.Spec.StorageClassName)
	})

	t.Run("last user volume wins when the name is repeated", func(t *testing.T) {
		first := extra
		first.Name = "tmp"
		got := mergeOperatorVolumes([]corev1.Volume{emptyDir}, []corev1.Volume{first, ephemeralTmp})
		assert.Equal(t, []corev1.Volume{ephemeralTmp}, got)
	})
}

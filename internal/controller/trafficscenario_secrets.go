package controller

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	trafficv1alpha1 "github.com/mykyta-kravchenko98/Kurama/api/v1alpha1"
	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

const secretHeadersVolumeName = "request-secrets"

func secretHeaderRelativePath(operationIndex, headerIndex int) string {
	return fmt.Sprintf("operation-%03d-header-%03d", operationIndex, headerIndex)
}

func secretHeaderFilePath(operationIndex, headerIndex int) string {
	return path.Join(
		runner.SecretHeadersMountPath,
		secretHeaderRelativePath(operationIndex, headerIndex),
	)
}

func desiredSecretHeaderVolume(scenario *trafficv1alpha1.TrafficScenario) *corev1.Volume {
	projections := make([]corev1.VolumeProjection, 0)
	for operationIndex, operation := range scenario.Spec.Operations {
		for headerIndex, header := range operation.Request.SecretHeaders {
			projections = append(projections, corev1.VolumeProjection{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: header.ValueFrom.SecretKeyRef.Name,
					},
					Items: []corev1.KeyToPath{{
						Key:  header.ValueFrom.SecretKeyRef.Key,
						Path: secretHeaderRelativePath(operationIndex, headerIndex),
					}},
				},
			})
		}
	}
	if len(projections) == 0 {
		return nil
	}
	return &corev1.Volume{
		Name: secretHeadersVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources:     projections,
				DefaultMode: ptr.To(int32(0o444)),
			},
		},
	}
}

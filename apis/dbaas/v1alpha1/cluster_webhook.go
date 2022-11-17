/*
Copyright ApeCloud Inc.

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

package v1alpha1

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var clusterlog = logf.Log.WithName("cluster-resource")

func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-dbaas-kubeblocks-io-v1alpha1-cluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=dbaas.kubeblocks.io,resources=clusters,verbs=create;update,versions=v1alpha1,name=mcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	clusterlog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-dbaas-kubeblocks-io-v1alpha1-cluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=dbaas.kubeblocks.io,resources=clusters,verbs=create;update,versions=v1alpha1,name=vcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	clusterlog.Info("validate create", "name", r.Name)
	return r.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	clusterlog.Info("validate update", "name", r.Name)
	lastCluster := old.(*Cluster)
	if lastCluster.Spec.ClusterDefRef != r.Spec.ClusterDefRef {
		return newInvalidError(ClusterKind, r.Name, "spec.clusterDefinitionRef", "clusterDefinitionRef is immutable, you can not update it. ")
	}
	if err := r.validateVolumeClaimTemplates(lastCluster); err != nil {
		return err
	}
	return r.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() error {
	clusterlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

// validateVolumeClaimTemplates volumeClaimTemplates is forbidden modification except for storage size.
func (r *Cluster) validateVolumeClaimTemplates(lastCluster *Cluster) error {
	var allErrs field.ErrorList
	for i, component := range r.Spec.Components {
		lastComponent := getLastComponentByName(lastCluster, component.Name)
		if lastComponent == nil {
			continue
		}
		setVolumeClaimStorageSizeZero(component.VolumeClaimTemplates)
		setVolumeClaimStorageSizeZero(lastComponent.VolumeClaimTemplates)
		if !reflect.DeepEqual(component.VolumeClaimTemplates, lastComponent.VolumeClaimTemplates) {
			path := fmt.Sprintf("spec.components[%d].volumeClaimTemplates", i)
			allErrs = append(allErrs, field.Invalid(field.NewPath(path),
				nil, "volumeClaimTemplates is forbidden modification except for storage size."))
		}
	}
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: APIVersion, Kind: ClusterKind},
			r.Name, allErrs)
	}
	return nil
}

// getLastComponentByName the cluster maybe delete or add a component, so we get the component by name.
func getLastComponentByName(lastCluster *Cluster, componentName string) *ClusterComponent {
	for _, component := range lastCluster.Spec.Components {
		if component.Name == componentName {
			return &component
		}
	}
	return nil
}

// setVolumeClaimStorageSizeZero set the volumeClaimTemplates storage size to zero. then we can diff last/current volumeClaimTemplates.
func setVolumeClaimStorageSizeZero(volumeClaimTemplates []ClusterComponentVolumeClaimTemplate) {
	for i := range volumeClaimTemplates {
		volumeClaimTemplates[i].Spec.Resources = corev1.ResourceRequirements{}
	}
}

// Validate Cluster.spec is legal
func (r *Cluster) validate() error {
	var (
		allErrs    field.ErrorList
		ctx        = context.Background()
		clusterDef = &ClusterDefinition{}
	)
	if webhookMgr == nil {
		return nil
	}

	r.validateAppVersionRef(&allErrs)

	err := webhookMgr.client.Get(ctx, types.NamespacedName{Name: r.Spec.ClusterDefRef}, clusterDef)

	if err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec.clusterDefinitionRef"),
			r.Spec.ClusterDefRef, err.Error()))
	} else {
		r.validateComponents(&allErrs, clusterDef)
	}

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: APIVersion, Kind: ClusterKind},
			r.Name, allErrs)
	}
	return nil
}

// ValidateAppVersionRef validate spec.appVersionRef is legal
func (r *Cluster) validateAppVersionRef(allErrs *field.ErrorList) {
	appVersion := &AppVersion{}
	err := webhookMgr.client.Get(context.Background(), types.NamespacedName{
		Namespace: r.Namespace,
		Name:      r.Spec.AppVersionRef,
	}, appVersion)
	if err != nil {
		*allErrs = append(*allErrs, field.Invalid(field.NewPath("spec.appVersionRef"),
			r.Spec.ClusterDefRef, err.Error()))
	}
}

// ValidateComponents validate spec.components is legal
func (r *Cluster) validateComponents(allErrs *field.ErrorList, clusterDef *ClusterDefinition) {
	var (
		// invalid component type slice
		invalidComponentTypes = make([]string, 0)
		// duplicate component name map
		duplicateComponentNames = make(map[string]struct{})
		componentNameMap        = make(map[string]struct{})
		componentTypeMap        = make(map[string]struct{})
		componentMap            = make(map[string]ClusterDefinitionComponent)
	)

	for _, v := range clusterDef.Spec.Components {
		componentTypeMap[v.TypeName] = struct{}{}
		componentMap[v.TypeName] = v
	}

	for _, v := range r.Spec.Components {
		if _, ok := componentTypeMap[v.Type]; !ok {
			invalidComponentTypes = append(invalidComponentTypes, v.Type)
		}

		if _, ok := componentNameMap[v.Name]; ok {
			duplicateComponentNames[v.Name] = struct{}{}
		}
		componentNameMap[v.Name] = struct{}{}
	}
	if len(invalidComponentTypes) > 0 {
		*allErrs = append(*allErrs, field.NotFound(field.NewPath("spec.components[*].type"),
			getComponentTypeNotFoundMsg(invalidComponentTypes, r.Spec.ClusterDefRef)))
	}

	if len(duplicateComponentNames) > 0 {
		*allErrs = append(*allErrs, field.Duplicate(field.NewPath("spec.components[*].name"),
			fmt.Sprintf(" %v is duplicated", r.getDuplicateMapKeys(duplicateComponentNames))))
	}
}

func (r *Cluster) getDuplicateMapKeys(m map[string]struct{}) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}

// Package v1alpha1 contains the agentic.rancher.io/v1alpha1 API types.
// +kubebuilder:object:generate=true
// +groupName=agentic.rancher.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "agentic.rancher.io", Version: "v1alpha1"}

	// SchemeBuilder adds the types in this package to a scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(
		&AgentRepository{}, &AgentRepositoryList{},
		&Agent{}, &AgentList{},
		&AgentRun{}, &AgentRunList{},
	)
}

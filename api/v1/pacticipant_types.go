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

package v1

import (
	"fmt"
	"hash/fnv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PactTag string

type Contract struct {
	Name string  `json:"name"`
	Tag  PactTag `json:"tag,omitempty"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PacticipantSpec defines the desired state of Pacticipant
type PacticipantSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`

	Tags      []PactTag  `json:"tags,omitempty"`
	Contracts []Contract `json:"contracts,omitempty"`
}

func hash(s fmt.Stringer) string {
	h := fnv.New32a()
	h.Write([]byte(s.String()))
	return fmt.Sprintf("%x", h.Sum32())
}

func (p *PacticipantSpec) Hash() string {
	return hash(p)
}

func (p *PacticipantSpec) String() string {
	ret := fmt.Sprintf("%s:%s", p.Name, p.Version)
	for _, t := range p.Tags {
		ret += fmt.Sprintf(":%s", t)
	}
	for _, c := range p.Contracts {
		ret += fmt.Sprintf(":%s", c)
	}
	return ret
}

func (c *Contract) Hash() string {
	return hash(c)
}

func (c *Contract) String() string {
	return fmt.Sprintf("%s:%s", c.Name, c.Tag)
}

// PacticipantStatus defines the observed state of Pacticipant
type PacticipantStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Represents time when the job controller started processing contracts.
	// It is represented in RFC3339 form and is in UTC.
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// The total number of contracts
	Total int32 `json:"total,omitempty"`

	// The number of pending and running checks.
	Active int32 `json:"active,omitempty"`

	// The number of contracts that have passed can-i-deploy checks.
	Succeeded int32 `json:"succeeded,omitempty"`

	// The number of contracts that have failed can-i-deploy checks.
	Failed int32 `json:"failed,omitempty"`

	// Store refs to the jobs that are created for each contract
	Jobs []ContractJobRef `json:"jobs,omitempty"`

	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

func (p *PacticipantStatus) IsComplete() bool {
	for _, c := range p.Conditions {
		if c.Status == metav1.ConditionTrue && c.Type == "Complete" {
			return true
		}
	}
	return false
}

type ContractJobRef struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

func NewContractJobRef(name, space string) ContractJobRef {
	return ContractJobRef{
		Name:      name,
		Namespace: space,
	}
}

func (n *ContractJobRef) NamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: n.Namespace,
		Name:      n.Name,
	}
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Pacticipant is the Schema for the pacticipants API
type Pacticipant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PacticipantSpec   `json:"spec,omitempty"`
	Status PacticipantStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PacticipantList contains a list of Pacticipant
type PacticipantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Pacticipant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Pacticipant{}, &PacticipantList{})
}

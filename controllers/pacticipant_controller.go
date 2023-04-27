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

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pactv1 "github.com/aristanetworks/pact-operator/api/v1"
	httpq "github.com/imroc/req/v3"
)

// PacticipantReconciler reconciles a Pacticipant object
type PacticipantReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	BrokerURL    string
	BrokerClient *httpq.Client
}

//+kubebuilder:rbac:groups=pact.code.arista.io,resources=pacticipants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=pact.code.arista.io,resources=pacticipants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=pact.code.arista.io,resources=pacticipants/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pacticipant object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *PacticipantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("Reconciling Pacticipant")

	pacticipant := &pactv1.Pacticipant{}
	err := r.Get(ctx, req.NamespacedName, pacticipant)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	l.Info("Pacticipant found", "name", pacticipant.Name)

	jobs, err := r.NewJobList(pacticipant)
	if err != nil {
		l.Error(err, "Failed to create jobs for pacticipant", "name", pacticipant.Name)
		return ctrl.Result{}, err
	}

	modified, err := r.CleanupJobs(ctx, pacticipant.Status.Jobs, jobs.Items)
	if err != nil {
		l.Error(err, "Failed to cleanup jobs")
		return ctrl.Result{}, err
	}

	status := &pactv1.PacticipantStatus{
		Active:     0,
		Succeeded:  0,
		Failed:     0,
		Total:      0,
		StartTime:  pacticipant.Status.StartTime,
		Conditions: []metav1.Condition{},
		Jobs:       []pactv1.ContractJobRef{},
	}

	if !modified && pacticipant.Status.IsComplete() {
		return ctrl.Result{}, nil
	}

	for _, j := range jobs.Items {
		status.Jobs = append(status.Jobs, pactv1.NewContractJobRef(j.Name, j.Namespace))
	}

	for _, j := range jobs.Items {
		job, err := r.MaybeCreateJob(ctx, &j)
		if err != nil {
			l.Error(err, "Failed to create job", "name", j.Name)
			return ctrl.Result{}, err
		}

		l.Info("Job found", "name", job.Name, "status", job.Status)

		status.Total++
		if job.Status.Succeeded > 0 {
			status.Succeeded++
		}
		if job.Status.Failed > 0 {
			status.Failed++
		}
		if job.Status.Active > 0 {
			status.Active++
		}
	}

	if status.StartTime == nil {
		now := metav1.Now()
		status.StartTime = &now
	}

	if status.Total == status.Succeeded {
		err := r.PutTags(ctx, pacticipant)
		if err != nil {
			l.Error(err, "Failed to PUT tags")
			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}

		status.Conditions = []metav1.Condition{{
			Type:               "Complete",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: pacticipant.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "Complete",
			Message:            "All contracts passed",
		}}
	} else if status.Failed > 0 {
		status.Conditions = []metav1.Condition{{
			Type:               "Failed",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: pacticipant.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "Failed",
			Message: fmt.Sprintf("%d/%d contracts failed",
				status.Failed, status.Total),
		}}
	} else if status.Active > 0 {
		status.Conditions = []metav1.Condition{{
			Type:               "Active",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: pacticipant.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "Active",
			Message: fmt.Sprintf("%d/%d contracts active",
				status.Active, status.Total),
		}}
	}

	if !reflect.DeepEqual(status, pacticipant.Status) {
		pacticipant.Status = *status
		err := r.Status().Update(ctx, pacticipant)
		if err != nil {
			l.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *PacticipantReconciler) PutTags(ctx context.Context, pacticipant *pactv1.Pacticipant) error {
	l := log.FromContext(ctx)

	for _, v := range pacticipant.Spec.Tags {
		path := fmt.Sprintf("%s/pacticipants/%s/versions/%s/tags/%v",
			r.BrokerURL, pacticipant.Spec.Name,
			pacticipant.Spec.Version, v)

		resp := r.BrokerClient.
			Put(path).
			SetHeader("Content-Type", "application/json").
			Do(ctx)
		if resp.Err != nil {
			return resp.Err
		}
		if !resp.IsSuccessState() {
			return fmt.Errorf("Failed to PUT %s, %v", path, resp.String())
		}
		l.Info("Publish Tags", "path", path, "resp", resp.String())
	}

	return nil
}

func (r *PacticipantReconciler) CleanupJobs(
	ctx context.Context,
	active []pactv1.ContractJobRef,
	desired []batchv1.Job,
) (bool, error) {
	l := log.FromContext(ctx)

	isDesired := func(name, space string) bool {
		for _, j := range desired {
			if j.Name == name && j.Namespace == space {
				return true
			}
		}
		return false
	}

	var modified bool

	for _, j := range active {
		if isDesired(j.Name, j.Namespace) {
			continue
		}

		found := &batchv1.Job{}
		err := r.Get(ctx, j.NamespacedName(), found)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return false, fmt.Errorf("Failed to get job %s, %v", j, err)
		}

		// delete the job
		l.Info("Deleting job", "name", j)
		err = r.Delete(ctx, found)
		if err != nil {
			return false, fmt.Errorf("Failed to delete job %s, %v", j, err)
		}
		modified = true
	}

	return modified, nil
}

// MaybeCreateJob creates a job if it doesn't already exist in the cluster.
// If the job already exists, it returns the existing job.
func (r *PacticipantReconciler) MaybeCreateJob(ctx context.Context, j *batchv1.Job) (*batchv1.Job, error) {
	l := log.FromContext(ctx)

	found := &batchv1.Job{}

	err := r.Get(ctx, types.NamespacedName{
		Name:      j.Name,
		Namespace: j.Namespace,
	}, found)
	if err == nil {
		return found, nil
	}

	if !errors.IsNotFound(err) {
		return nil, err
	}

	l.Info("Creating new job", "name", j.Name)
	err = r.Create(ctx, j)
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (r *PacticipantReconciler) NewJobList(pacticipant *pactv1.Pacticipant) (batchv1.JobList, error) {
	jobList := batchv1.JobList{}

	labels := map[string]string{
		"app": pacticipant.Spec.Name,
	}
	for _, contract := range pacticipant.Spec.Contracts {

		// hash the name to make sure it's not too long
		name := fmt.Sprintf("pact-contract-%x-%x",
			pacticipant.Spec.Hash(), contract.Hash())
		backoffLimit := int32(0)
		// TODO: make configurable

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: pacticipant.Namespace,
				Labels:    labels,
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: &backoffLimit,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: "Never",
						Containers: []corev1.Container{
							{
								Image: "pactfoundation/pact-cli:0.40.0.0",
								Name:  "can-i-deploy",
								Args: []string{
									"broker", "can-i-deploy",
									"--broker-base-url", r.BrokerURL,
									"-a", pacticipant.Spec.Name, "-e", pacticipant.Spec.Version,
									"-a", contract.Name, "--latest", string(contract.Tag),
									"--retry-while-unknown", "25", "--retry-interval", "30",
								},
							},
						},
					},
				},
			},
		}
		ctrl.SetControllerReference(pacticipant, job, r.Scheme)

		jobList.Items = append(jobList.Items, *job)
	}

	return jobList, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PacticipantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pactv1.Pacticipant{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

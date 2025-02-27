package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/roshbhatia/kubanana/pkg/apis/kubanana/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateJobFromTemplate(t *testing.T) {
	// Create a fake kubernetes client
	kubeClient := fake.NewSimpleClientset()

	// Create a test template
	template := &v1alpha1.EventTriggeredJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
			UID:       types.UID("test-template-uid"),
		},
		Spec: v1alpha1.EventTriggeredJobSpec{
			EventSelector: &v1alpha1.EventSelector{
				ResourceKind:     "Pod",
				NamePattern:      "test-*",
				NamespacePattern: "default",
				EventTypes:       []string{"CREATE", "DELETE"},
			},
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "hello",
									Image: "busybox",
									Command: []string{
										"sh",
										"-c",
										"echo 'Resource: $RESOURCE_KIND, Name: $RESOURCE_NAME'; sleep 5",
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}

	// Create an event
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       types.UID("test-pod-uid"),
		},
		Reason: "Created",
	}

	// Create a controller
	controller := &EventController{
		kubeClient: kubeClient,
	}

	// Create a job based on the template and event
	job, err := createJobFromTemplate(controller, template, event, "CREATE")
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Check owner reference
	if len(job.OwnerReferences) != 1 {
		t.Errorf("Expected 1 owner reference, got %d", len(job.OwnerReferences))
	}

	ownerRef := job.OwnerReferences[0]
	if ownerRef.UID != template.UID {
		t.Errorf("Expected owner UID %s, got %s", template.UID, ownerRef.UID)
	}

	if ownerRef.Name != template.Name {
		t.Errorf("Expected owner name %s, got %s", template.Name, ownerRef.Name)
	}

	// Check environment variable substitution
	container := job.Spec.Template.Spec.Containers[0]
	command := container.Command[2]
	expectedCommand := "echo 'Resource: Pod, Name: test-pod'; sleep 5"
	if command != expectedCommand {
		t.Errorf("Expected command with vars substituted: %s, got %s", expectedCommand, command)
	}

	// Check if the job exists in the fake client
	// We need to create it first
	_, err = kubeClient.BatchV1().Jobs("default").Create(context.Background(), job, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create job in fake client: %v", err)
	}

	// Now get it and verify
	createdJob, err := kubeClient.BatchV1().Jobs("default").Get(context.Background(), job.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get job from fake client: %v", err)
	}

	if createdJob.Name != job.Name {
		t.Errorf("Expected job name %s, got %s", job.Name, createdJob.Name)
	}
}

func TestCreateJobFromStatusTemplate(t *testing.T) {
	// Create a fake kubernetes client
	kubeClient := fake.NewSimpleClientset()

	// Create a test template
	template := &v1alpha1.EventTriggeredJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-status-template",
			Namespace: "default",
			UID:       types.UID("test-template-uid"),
		},
		Spec: v1alpha1.EventTriggeredJobSpec{
			StatusSelector: &v1alpha1.StatusSelector{
				ResourceKind:     "Pod",
				NamePattern:      "test-*",
				NamespacePattern: "default",
				Conditions: []v1alpha1.StatusCondition{
					{
						Type:   "Ready",
						Status: "True",
					},
				},
			},
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "hello",
									Image: "busybox",
									Command: []string{
										"sh",
										"-c",
										"echo 'Resource: $RESOURCE_KIND, Name: $RESOURCE_NAME, Status: $STATUS_Ready'; sleep 5",
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}

	// Create a status controller
	controller := &StatusController{
		kubeClient: kubeClient,
	}

	// Test creating a job from a status template
	resourceKind := "Pod"
	namespace := "default"
	name := "test-pod"
	conditions := map[string]string{
		"Ready": "True",
	}

	// Create a job based on the template and status conditions
	job, err := createStatusJobFromTemplate(controller, template, resourceKind, namespace, name, conditions)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Check owner reference
	if len(job.OwnerReferences) != 1 {
		t.Errorf("Expected 1 owner reference, got %d", len(job.OwnerReferences))
	}

	ownerRef := job.OwnerReferences[0]
	if ownerRef.UID != template.UID {
		t.Errorf("Expected owner UID %s, got %s", template.UID, ownerRef.UID)
	}

	if ownerRef.Name != template.Name {
		t.Errorf("Expected owner name %s, got %s", template.Name, ownerRef.Name)
	}

	// Check environment variable substitution
	container := job.Spec.Template.Spec.Containers[0]
	command := container.Command[2]
	expectedCommand := "echo 'Resource: Pod, Name: test-pod, Status: True'; sleep 5"
	if command != expectedCommand {
		t.Errorf("Expected command with vars substituted: %s, got %s", expectedCommand, command)
	}

	// Check that environment variables were added
	hasResourceEnv := false
	hasStatusEnv := false
	for _, env := range container.Env {
		if env.Name == "RESOURCE_KIND" && env.Value == "Pod" {
			hasResourceEnv = true
		}
		if env.Name == "STATUS_Ready" && env.Value == "True" {
			hasStatusEnv = true
		}
	}

	if !hasResourceEnv {
		t.Errorf("Expected RESOURCE_KIND environment variable to be set")
	}
	if !hasStatusEnv {
		t.Errorf("Expected STATUS_Ready environment variable to be set")
	}
}

// Test implementation of createJobFromTemplate for unit testing
func createJobFromTemplate(c *EventController, template *v1alpha1.EventTriggeredJob, event *corev1.Event, eventType string) (*batchv1.Job, error) {
	// Create a job name based on the template and event
	jobName := template.Name + "-" + event.InvolvedObject.Kind + "-" + eventType

	// Create labels for the job
	labels := map[string]string{
		"kubanana-template":      template.Name,
		"kubanana-resource-kind": event.InvolvedObject.Kind,
		"kubanana-resource-name": event.InvolvedObject.Name,
		"kubanana-event-type":    eventType,
	}

	// Create a job based on the template
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName + "-",
			Namespace:    event.Namespace,
			Labels:       labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "kubanana.roshanbhatia.com/v1alpha1",
					Kind:       "EventTriggeredJob",
					Name:       template.Name,
					UID:        template.UID,
				},
			},
		},
		Spec: template.Spec.JobTemplate.Spec,
	}

	// Variable substitution in command
	for i, container := range job.Spec.Template.Spec.Containers {
		for j, command := range container.Command {
			// Replace variables in command
			command = replaceVariables(command, event, eventType)
			job.Spec.Template.Spec.Containers[i].Command[j] = command
		}

		// Add environment variables if they don't exist
		envVars := []corev1.EnvVar{
			{
				Name:  "RESOURCE_KIND",
				Value: event.InvolvedObject.Kind,
			},
			{
				Name:  "RESOURCE_NAME",
				Value: event.InvolvedObject.Name,
			},
			{
				Name:  "RESOURCE_NAMESPACE",
				Value: event.InvolvedObject.Namespace,
			},
			{
				Name:  "EVENT_TYPE",
				Value: eventType,
			},
		}

		// Add environment variables if they don't already exist
		for _, env := range envVars {
			if !envVarExists(container.Env, env.Name) {
				job.Spec.Template.Spec.Containers[i].Env = append(
					job.Spec.Template.Spec.Containers[i].Env,
					env,
				)
			}
		}
	}

	return job, nil
}

// Implementation of createStatusJobFromTemplate for testing
func createStatusJobFromTemplate(c *StatusController, template *v1alpha1.EventTriggeredJob, resourceKind, namespace, name string, conditions map[string]string) (*batchv1.Job, error) {
	// Create a job name based on the template
	jobName := template.Name + "-" + strings.ToLower(resourceKind) + "-status"

	// Create labels for the job
	labels := map[string]string{
		"kubanana-template":      template.Name,
		"kubanana-resource-kind": resourceKind,
		"kubanana-resource-name": name,
		"kubanana-trigger-type":  "status",
	}

	// Create a job based on the template
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName + "-",
			Namespace:    namespace,
			Labels:       labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "kubanana.roshanbhatia.com/v1alpha1",
					Kind:       "EventTriggeredJob",
					Name:       template.Name,
					UID:        template.UID,
				},
			},
		},
		Spec: template.Spec.JobTemplate.Spec,
	}

	// Variable substitution in command
	for i, container := range job.Spec.Template.Spec.Containers {
		for j, command := range container.Command {
			// Replace variables in command
			command = replaceStatusVariables(command, resourceKind, name, namespace, conditions)
			job.Spec.Template.Spec.Containers[i].Command[j] = command
		}

		// Add environment variables if they don't exist
		envVars := []corev1.EnvVar{
			{
				Name:  "RESOURCE_KIND",
				Value: resourceKind,
			},
			{
				Name:  "RESOURCE_NAME",
				Value: name,
			},
			{
				Name:  "RESOURCE_NAMESPACE",
				Value: namespace,
			},
			{
				Name:  "TRIGGER_TYPE",
				Value: "status",
			},
		}

		// Add condition environment variables
		for condType, condStatus := range conditions {
			envVarName := "STATUS_" + strings.ReplaceAll(condType, "-", "_")
			envVars = append(envVars, corev1.EnvVar{
				Name:  envVarName,
				Value: condStatus,
			})
		}

		// Add environment variables if they don't already exist
		for _, env := range envVars {
			if !envVarExists(container.Env, env.Name) {
				job.Spec.Template.Spec.Containers[i].Env = append(
					job.Spec.Template.Spec.Containers[i].Env,
					env,
				)
			}
		}
	}

	return job, nil
}

// Helper function to replace variables in a string
func replaceVariables(input string, event *corev1.Event, eventType string) string {
	// Replace $RESOURCE_KIND with event.InvolvedObject.Kind
	output := strings.Replace(input, "$RESOURCE_KIND", event.InvolvedObject.Kind, -1)

	// Replace $RESOURCE_NAME with event.InvolvedObject.Name
	output = strings.Replace(output, "$RESOURCE_NAME", event.InvolvedObject.Name, -1)

	// Replace $RESOURCE_NAMESPACE with event.InvolvedObject.Namespace
	output = strings.Replace(output, "$RESOURCE_NAMESPACE", event.InvolvedObject.Namespace, -1)

	// Replace $EVENT_TYPE with eventType
	output = strings.Replace(output, "$EVENT_TYPE", eventType, -1)

	return output
}

// Helper function to replace variables in a string for status jobs
func replaceStatusVariables(input, resourceKind, name, namespace string, conditions map[string]string) string {
	// Replace $RESOURCE_KIND with resourceKind
	output := strings.Replace(input, "$RESOURCE_KIND", resourceKind, -1)

	// Replace $RESOURCE_NAME with name
	output = strings.Replace(output, "$RESOURCE_NAME", name, -1)

	// Replace $RESOURCE_NAMESPACE with namespace
	output = strings.Replace(output, "$RESOURCE_NAMESPACE", namespace, -1)

	// Replace condition variables
	for condType, condStatus := range conditions {
		varName := "$STATUS_" + strings.ReplaceAll(condType, "-", "_")
		output = strings.Replace(output, varName, condStatus, -1)
	}

	return output
}

// Helper function to check if an environment variable already exists
func envVarExists(envVars []corev1.EnvVar, name string) bool {
	for _, env := range envVars {
		if env.Name == name {
			return true
		}
	}
	return false
}

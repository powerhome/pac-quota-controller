package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// GenerateResourceName generates a unique test name with the given prefix.
// Ensures the total length stays within Kubernetes 63-character limit.
// Format: prefix-timestamp-random (e.g., "test-pod-1642531200-ab12")
func GenerateResourceName(prefix string) string {
	// Use last 8 digits of Unix timestamp + 4-char random suffix
	timestamp := time.Now().Unix() % 100000000            // Last 8 digits
	randomSuffix := fmt.Sprintf("%04x", rand.Intn(65536)) // 4-char hex (0000-ffff)

	// Calculate max prefix length to stay under 63 chars
	// Format: prefix-timestamp-random = prefix-12345678-abcd
	// Suffix length: 1 + 8 + 1 + 4 = 14 chars
	maxPrefixLen := 63 - 14

	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
	}

	return fmt.Sprintf("%s-%08d-%s", prefix, timestamp, randomSuffix)
}

// GenerateTestSuffix generates a unique suffix for test resources.
// This is useful when tests need just a suffix for multiple resources.
// Keeps the suffix short to respect Kubernetes 63-character name limits.
// Format: timestamp-random (e.g., "12345678-ab12") - 13 chars max
func GenerateTestSuffix() string {
	timestamp := time.Now().Unix() % 100000000            // Last 8 digits
	randomSuffix := fmt.Sprintf("%04x", rand.Intn(65536)) // 4-char hex
	return fmt.Sprintf("%08d-%s", timestamp, randomSuffix)
}

// ServiceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API via a clientset created from the controller-runtime client config.
func ServiceAccountToken(
	ctx context.Context,
	clientSet *kubernetes.Clientset,
	k8sClient client.Client,
	namespace,
	serviceAccountName string,
) (string, error) {
	// First, verify the service account exists
	sa := &corev1.ServiceAccount{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceAccountName}, sa)
	if err != nil {
		return "", fmt.Errorf("failed to get service account %s/%s: %w", namespace, serviceAccountName, err)
	}

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{}, // default spec
	}
	result, err := clientSet.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx, serviceAccountName, tokenRequest, metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create token request for SA %s/%s: %w", namespace, serviceAccountName, err)
	}
	if result.Status.Token == "" {
		return "", fmt.Errorf("extracted token is empty for SA %s/%s", namespace, serviceAccountName)
	}
	return result.Status.Token, nil
}

// GetMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
// It now returns an error if any step fails or if the metrics endpoint doesn't return 200 OK.
func GetMetricsOutput(
	ctx context.Context,
	clientSet *kubernetes.Clientset,
	namespace, curlPodName string,
) (string, error) {
	podLogOpts := &corev1.PodLogOptions{}
	req := clientSet.CoreV1().Pods(namespace).GetLogs(curlPodName, podLogOpts)

	logStream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to stream logs from curl pod '%s': %w", curlPodName, err)
	}
	defer func() {
		if closeErr := logStream.Close(); closeErr != nil {
			// Log or handle the close error if necessary, though we can't return it from the main function here.
			// For now, we rely on the primary error handling of the function.
			fmt.Printf("Warning: Failed to close log stream for pod '%s': %v\n", curlPodName, closeErr)
		}
	}()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logStream)
	if err != nil {
		return "", fmt.Errorf("failed to copy log stream to buffer for pod '%s': %w", curlPodName, err)
	}

	output := buf.String()
	// Basic check for HTTP 200 OK. A more robust check might involve parsing the HTTP status line.
	if !bytes.Contains(buf.Bytes(), []byte("HTTP/1.1 200 OK")) { // Using bytes.Contains for efficiency
		return output, fmt.Errorf("metrics endpoint did not return 200 OK. Logs: %s", output)
	}
	return output, nil
}

// GetPodLogs retrieves logs from a specified pod.
func GetPodLogs(ctx context.Context, clientSet *kubernetes.Clientset, namespace, podName string) string {
	podLogOpts := &corev1.PodLogOptions{}
	req := clientSet.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)
	logStream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Sprintf("Failed to stream logs: %v", err)
	}
	defer func() {
		if err := logStream.Close(); err != nil {
			fmt.Printf("Failed to close log stream: %v\n", err)
		}
	}()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logStream)
	if err != nil {
		return fmt.Sprintf("Failed to copy log stream: %v", err)
	}
	return buf.String()
}

// GetEvents lists all events in a specified namespace.
func GetEvents(ctx context.Context, clientSet *kubernetes.Clientset, namespace string) string {
	events, err := clientSet.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("Failed to get events: %v", err)
	}
	var out bytes.Buffer
	for _, e := range events.Items {
		fmt.Fprintf(&out, "%s\t%s\t%s\n", e.LastTimestamp, e.InvolvedObject.Name, e.Message)
	}
	return out.String()
}

// GetCRQEvents lists events related to a specific ClusterResourceQuota in the given namespace.
func GetCRQEvents(ctx context.Context, clientSet *kubernetes.Clientset, namespace, crqName string) (
	[]corev1.Event, error) {
	events, err := clientSet.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	var crqEvents []corev1.Event
	for _, event := range events.Items {
		// Check if event is related to the CRQ by looking at annotations
		// Events are recorded against the controller pod but contain CRQ info in annotations
		if event.InvolvedObject.Kind == "Pod" {
			// Check for CRQ name in event annotations
			if annotatedCRQName, exists := event.Annotations["quota.pac.io/crq-name"]; exists && annotatedCRQName == crqName {
				crqEvents = append(crqEvents, event)
			}
		}
	}
	return crqEvents, nil
}

// WaitForCRQEvent waits for a specific event type to be recorded for a ClusterResourceQuota.
// Since events now use unique reasons (with suffix), we check the original reason in annotations.
func WaitForCRQEvent(ctx context.Context, clientSet *kubernetes.Clientset, namespace, crqName, eventReason string,
	timeout time.Duration) error {
	// First check if the event already exists
	events, err := GetCRQEvents(ctx, clientSet, namespace, crqName)
	if err == nil {
		for _, event := range events {
			if matchesEventReason(event, eventReason) {
				return nil
			}
		}
	}

	// If not found, use lazy reader approach with watch
	return WaitForCRQEventWithWatch(ctx, clientSet, namespace, crqName, eventReason, timeout)
}

// WaitForCRQEventWithWatch uses Kubernetes watch API to efficiently wait for events
func WaitForCRQEventWithWatch(ctx context.Context, clientSet *kubernetes.Clientset, namespace, crqName, eventReason string,
	timeout time.Duration) error {

	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a watch for events in the namespace
	watcher, err := clientSet.CoreV1().Events(namespace).Watch(ctxWithTimeout, metav1.ListOptions{
		Watch: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create event watcher: %w", err)
	}
	defer watcher.Stop()

	// Create a channel to receive matching events
	eventChan := make(chan *corev1.Event, 1)
	defer close(eventChan)

	// Start a goroutine to process watch events
	go func() {
		defer watcher.Stop()
		for {
			select {
			case <-ctxWithTimeout.Done():
				return
			case watchEvent, ok := <-watcher.ResultChan():
				if !ok {
					return
				}

				if watchEvent.Type == "ADDED" || watchEvent.Type == "MODIFIED" {
					if event, ok := watchEvent.Object.(*corev1.Event); ok {
						// Check if this event matches our CRQ and reason
						if isCRQEvent(event, crqName) && matchesEventReason(*event, eventReason) {
							select {
							case eventChan <- event:
							case <-ctxWithTimeout.Done():
								return
							}
							return
						}
					}
				}
			}
		}
	}()

	// Wait for either the event or timeout
	select {
	case <-ctxWithTimeout.Done():
		return fmt.Errorf("event %s for CRQ %s not found within %v", eventReason, crqName, timeout)
	case <-eventChan:
		return nil
	}
}

// isCRQEvent checks if an event is related to a specific CRQ
func isCRQEvent(event *corev1.Event, crqName string) bool {
	if event.InvolvedObject.Kind == "Pod" {
		if annotatedCRQName, exists := event.Annotations["quota.pac.io/crq-name"]; exists && annotatedCRQName == crqName {
			return true
		}
	}
	return false
}

// matchesEventReason checks if an event matches the expected reason
func matchesEventReason(event corev1.Event, expectedReason string) bool {
	return event.Reason == expectedReason ||
		(event.Annotations != nil && event.Annotations["quota.pac.io/event-type"] == expectedReason) ||
		strings.HasPrefix(event.Reason, expectedReason+"-")
}

// ValidateEventContent validates that an event contains expected content.
// This function handles both regular events and events with unique suffixes.
func ValidateEventContent(event corev1.Event, expectedReason, expectedType string, messageContains string) error {
	// Check the reason using our helper function
	if !matchesEventReason(event, expectedReason) {
		return fmt.Errorf("expected event reason %s, got %s", expectedReason, event.Reason)
	}
	if event.Type != expectedType {
		return fmt.Errorf("expected event type %s, got %s", expectedType, event.Type)
	}
	if messageContains != "" {
		// Check the Message field (combined events will have the details in the message)
		if !strings.Contains(event.Message, messageContains) {
			return fmt.Errorf("expected event message to contain %s, got message: %s",
				messageContains, event.Message)
		}
	}
	return nil
}

// GetEventsByReason filters events by reason for a specific CRQ.
// Since events now use unique reasons (with suffix), we check the original reason in annotations.
func GetEventsByReason(ctx context.Context, clientSet *kubernetes.Clientset, namespace, crqName, reason string) (
	[]corev1.Event, error) {
	allEvents, err := GetCRQEvents(ctx, clientSet, namespace, crqName)
	if err != nil {
		return nil, err
	}

	var filteredEvents []corev1.Event
	for _, event := range allEvents {
		if matchesEventReason(event, reason) {
			filteredEvents = append(filteredEvents, event)
		}
	}
	return filteredEvents, nil
}

// DescribePod provides a description of a specified pod.
func DescribePod(ctx context.Context, k8sClient client.Client, namespace, podName string) string {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod)
	if err != nil {
		return fmt.Sprintf("Failed to get pod: %v", err)
	}
	return fmt.Sprintf("Pod: %s\nStatus: %s\n", pod.Name, pod.Status.Phase)
}

// CreateNamespace creates a namespace with the specified name and labels.
func CreateNamespace(ctx context.Context, k8sClient client.Client, namespace string,
	nsLabels map[string]string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: nsLabels,
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		return nil, fmt.Errorf("failed to create namespace %s: %w", namespace, err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return nil, fmt.Errorf("failed to verify creation of namespace %s: %w", namespace, err)
	}
	return ns, nil
}

// GetNamespace retrieves a namespace by name.
func GetNamespace(ctx context.Context, k8sClient client.Client, namespace string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
	if err != nil {
		return nil, err
	}
	return ns, err
}

// CreatePod creates a pod in the specified namespace.
func CreatePod(ctx context.Context, k8sClient client.Client, namespace,
	podName string, requests, limits corev1.ResourceList) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "container",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, pod); err != nil {
		return nil, fmt.Errorf("failed to create pod %s: %w", podName, err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, fmt.Errorf("failed to verify creation of pod %s: %w", podName, err)
	}
	return pod, nil
}

// CreatePodWithContainers creates a Pod with the specified containers and optional init containers.
func CreatePodWithContainers(
	ctx context.Context,
	k8sClient client.Client,
	namespace, name string,
	containers []corev1.Container,
	initContainers []corev1.Container,
) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers:     containers,
			InitContainers: initContainers,
		},
	}
	if err := k8sClient.Create(ctx, pod); err != nil {
		return nil, err
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pod); err != nil {
		return nil, err
	}
	return pod, nil
}

// CreateJob creates a job in the specified namespace.
func CreateJob(ctx context.Context, k8sClient client.Client, namespace,
	jobName string, command []string, requests, limits corev1.ResourceList) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "container",
							Image:   "busybox:latest",
							Command: command,
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job %s: %w", jobName, err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: namespace}, job); err != nil {
		return nil, fmt.Errorf("failed to verify creation of job %s: %w", jobName, err)
	}
	return job, nil
}

// CreateClusterResourceQuota creates a ClusterResourceQuota resource.
func CreateClusterResourceQuota(ctx context.Context, k8sClient client.Client, name string,
	namespaceSelector *metav1.LabelSelector,
	hard quotav1alpha1.ResourceList) (*quotav1alpha1.ClusterResourceQuota, error) {
	crq := &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			NamespaceSelector: namespaceSelector,
			Hard:              hard,
		},
	}
	if err := k8sClient.Create(ctx, crq); err != nil {
		return nil, fmt.Errorf("failed to create ClusterResourceQuota %s: %w", name, err)
	}
	// Ensure the CRQ is created successfully
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, crq); err != nil {
		return nil, fmt.Errorf("failed to verify creation of ClusterResourceQuota %s: %w", name, err)
	}
	return crq, nil
}

// testutils.GetCRQStatusNamespaces returns the list of namespaces from the status
// of the given ClusterResourceQuota object.
func GetCRQStatusNamespaces(crq *quotav1alpha1.ClusterResourceQuota) []string {
	if crq == nil || crq.Status.Namespaces == nil {
		return nil
	}
	nsList := make([]string, 0, len(crq.Status.Namespaces))
	for _, ns := range crq.Status.Namespaces {
		nsList = append(nsList, ns.Namespace)
	}
	return nsList
}

// GetCRQStatusUsage retrieves the resource usage from the Total field of the given ClusterResourceQuota object.
func GetCRQStatusUsage(crq *quotav1alpha1.ClusterResourceQuota) quotav1alpha1.ResourceList {
	if crq == nil || crq.Status.Total.Used == nil {
		return nil
	}
	return crq.Status.Total.Used
}

// WaitForCRQStatus waits for the CRQ status to be updated with the expected namespaces.
func WaitForCRQStatus(ctx context.Context, k8sClient client.Client, crqName string,
	expectedNamespaces []string, timeout, interval time.Duration) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return wait.PollUntilContextTimeout(ctxWithTimeout, interval, timeout, true, func(ctx context.Context) (bool, error) {
		crq := &quotav1alpha1.ClusterResourceQuota{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq); err != nil {
			return false, err
		}

		actualNamespaces := GetCRQStatusNamespaces(crq)
		if len(actualNamespaces) != len(expectedNamespaces) {
			return false, nil
		}

		for _, ns := range expectedNamespaces {
			if !slices.Contains(actualNamespaces, ns) {
				return false, nil
			}
		}
		return true, nil
	})
}

// EnsureResourceDeleted deletes a list of resources and waits for their deletion.
func EnsureResourceDeleted(ctx context.Context, k8sClient client.Client,
	resourceKeys []client.ObjectKey, resourceType client.Object) error {
	for _, key := range resourceKeys {
		if err := k8sClient.Get(ctx, key, resourceType); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue // Resource already deleted
			}
			return fmt.Errorf("failed to get resource %s: %w", key.Name, err)
		}
		if err := k8sClient.Delete(ctx, resourceType); err != nil {
			return fmt.Errorf("failed to delete resource %s: %w", key.Name, err)
		}
		if err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 30*time.Second, true,
			func(ctx context.Context) (bool, error) {
				if err := k8sClient.Get(ctx, key, resourceType); err != nil {
					return client.IgnoreNotFound(err) == nil, nil // Deleted successfully
				}
				return false, nil
			}); err != nil {
			return fmt.Errorf("failed to wait for deletion of resource %s: %w", key.Name, err)
		}
	}
	return nil
}

// SelectNamespace selects a namespace based on a label selector.
func SelectNamespace(ctx context.Context, k8sClient client.Client, labelSelector string) (*corev1.Namespace, error) {
	parsedSelector, err := labels.Parse(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to parse label selector %s: %w", labelSelector, err)
	}
	list := &corev1.NamespaceList{}
	if err := k8sClient.List(ctx, list, client.MatchingLabelsSelector{Selector: parsedSelector}); err != nil {
		return nil, fmt.Errorf("failed to list namespaces with selector %s: %w", labelSelector, err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no namespaces found with selector %s", labelSelector)
	}
	return &list.Items[0], nil
}

// ValidateNamespace validates the properties of a namespace.
func ValidateNamespace(ctx context.Context, k8sClient client.Client, namespace string) (bool, error) {
	ns, err := GetNamespace(ctx, k8sClient, namespace)
	if err != nil {
		return false, fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}
	// Example validation: Check if the namespace has a specific label
	if _, exists := ns.Labels["validated"]; !exists {
		return false, fmt.Errorf("namespace %s does not have the 'validated' label", namespace)
	}
	return true, nil
}

// ListClusterResourceQuotas lists all ClusterResourceQuota resources matching the given label selector.
func ListClusterResourceQuotas(ctx context.Context, k8sClient client.Client,
	labelSelector *metav1.LabelSelector) (*quotav1alpha1.ClusterResourceQuotaList, error) {
	parsedSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to parse label selector: %w", err)
	}
	crqList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := k8sClient.List(ctx, crqList, client.MatchingLabelsSelector{Selector: parsedSelector}); err != nil {
		return nil, fmt.Errorf("failed to list ClusterResourceQuotas: %w", err)
	}
	return crqList, nil
}

// GetPod retrieves a Pod by name and namespace.
func GetPod(ctx context.Context, k8sClient client.Client, namespace, podName string) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, fmt.Errorf("failed to get pod %s in namespace %s: %w", podName, namespace, err)
	}
	return pod, nil
}

// GetRefreshedCRQStatusNamespaces fetches the latest CRQ from the cluster and returns its namespace list
func GetRefreshedCRQStatusNamespaces(ctx context.Context, k8sClient client.Client,
	crqName string) []string {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq); err != nil {
		return nil
	}
	return GetCRQStatusNamespaces(crq)
}

// GetRefreshedCRQStatusUsage fetches the latest CRQ from the cluster and returns its resource usage
func GetRefreshedCRQStatusUsage(
	ctx context.Context,
	k8sClient client.Client,
	crqName string) quotav1alpha1.ResourceList {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq); err != nil {
		return nil
	}
	return GetCRQStatusUsage(crq)
}

// CompareResourceUsage compares actual CRQ usage with expected values.
// Returns true if they match, false otherwise.
// Expected values are provided as a map[string]string.
// Any resource not mentioned in the expected map is expected to be zero.
func CompareResourceUsage(actual quotav1alpha1.ResourceList, expected map[string]string) (bool, error) {
	// Check that all expected resources have the correct values
	for resourceName, expectedValue := range expected {
		actualQuantity, exists := actual[corev1.ResourceName(resourceName)]
		if !exists {
			return false, fmt.Errorf("expected resource %s not found in actual usage", resourceName)
		}

		actualValue := actualQuantity.String()
		if actualValue != expectedValue {
			return false, fmt.Errorf("resource %s: expected %s, got %s", resourceName, expectedValue, actualValue)
		}
	}

	// Check that any resource not mentioned in expected is zero
	for resourceName, actualQuantity := range actual {
		resourceStr := string(resourceName)
		if _, mentioned := expected[resourceStr]; !mentioned {
			actualValue := actualQuantity.String()
			if actualValue != "0" {
				return false, fmt.Errorf("resource %s: expected 0 (not mentioned), got %s", resourceStr, actualValue)
			}
		}
	}

	return true, nil
}

// ExpectCRQUsageToMatch asserts that the CRQ usage matches the expected values.
// This is a helper for use in Ginkgo/Gomega tests.
// Usage: err := testutils.ExpectCRQUsageToMatch(crqUsage, expectedMap)
// Then use Expect(err).ToNot(HaveOccurred()) in your test
func ExpectCRQUsageToMatch(actual quotav1alpha1.ResourceList, expected map[string]string) error {
	match, err := CompareResourceUsage(actual, expected)
	if err != nil {
		return fmt.Errorf("CRQ usage assertion failed: %w", err)
	}
	if !match {
		return fmt.Errorf("CRQ usage assertion failed: resources do not match expected values")
	}
	return nil
}

// UpdateClusterResourceQuotaSpec updates only the spec of a ClusterResourceQuota with retry logic
// to handle conflicts with the controller's status updates. This prevents "object has been modified"
// errors that occur when tests update the spec while the controller is updating the status.
func UpdateClusterResourceQuotaSpec(
	ctx context.Context,
	k8sClient client.Client,
	crqName string,
	updateFunc func(*quotav1alpha1.ClusterResourceQuota) error,
) error {
	// Always get the latest version of the CRQ, apply the update, and try to update once.
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq); err != nil {
		return err
	}

	// Apply the update function to modify the spec
	if err := updateFunc(crq); err != nil {
		return err
	}

	// Try to update - do not retry on failure
	if err := k8sClient.Update(ctx, crq); err != nil {
		return err
	}

	return nil
}

// CreatePVC creates a PersistentVolumeClaim in the specified namespace
// with the given name, storage size, and optional labels.
func CreatePVC(
	ctx context.Context,
	k8sClient client.Client,
	namespace, name, storageSize string,
	pvcLabels map[string]string,
) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    pvcLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, pvc); err != nil {
		return nil, err
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pvc); err != nil {
		return nil, err
	}
	return pvc, nil
}

// NewReplicationController returns a ReplicationController object for testing
func NewReplicationController(name, namespace string, replicas int32) *corev1.ReplicationController {
	return &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ReplicationControllerSpec{
			Replicas: &replicas,
			Selector: map[string]string{"app": name},
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "container", Image: "nginx:latest"}},
				},
			},
		},
	}
}

// NewDeployment returns a Deployment object for testing
func NewDeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "container", Image: "nginx:latest"}},
				},
			},
		},
	}
}

// NewStatefulSet returns a StatefulSet object for testing
func NewStatefulSet(name, namespace string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: name + "-svc",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "container", Image: "nginx:latest"}},
				},
			},
		},
	}
}

// NewDaemonSet returns a DaemonSet object for testing
func NewDaemonSet(name, namespace string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "container", Image: "nginx:latest"}},
				},
			},
		},
	}
}

// NewCronJob returns a CronJob object for testing
func NewCronJob(name, namespace string) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "* * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "container",
									Image:   "busybox:latest",
									Command: []string{"echo", "hello"},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}
}

// NewHPA returns a HorizontalPodAutoscaler object for testing
func NewHPA(name, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := int32(1)
	maxReplicas := int32(2)
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       name,
				APIVersion: "apps/v1",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics:     []autoscalingv2.MetricSpec{},
		},
	}
}

// NewIngress returns an Ingress object for testing
func NewIngress(name, namespace string) *networkingv1.Ingress {
	pathType := networkingv1.PathType("Exact")
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: "test.local",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							PathType: &pathType,
							Path:     "/",
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "test-svc",
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						}},
					},
				},
			}},
		},
	}
}

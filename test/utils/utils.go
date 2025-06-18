package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// Helper functions for logs, events, pod description, and pointer helpers
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

func DescribePod(ctx context.Context, k8sClient client.Client, namespace, podName string) string {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod)
	if err != nil {
		return fmt.Sprintf("Failed to get pod: %v", err)
	}
	return fmt.Sprintf("Pod: %s\nStatus: %s\n", pod.Name, pod.Status.Phase)
}

func CreateNamespace(ctx context.Context, k8sClient client.Client, namespace string, labels map[string]string) error {
	return k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: labels,
		},
	})
}

func GetNamespace(ctx context.Context, k8sClient client.Client, namespace string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
	if err != nil {
		return nil, err
	}
	return ns, err
}

func DeleteNamespace(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	return k8sClient.Delete(ctx, ns)
}

// CreateClusterResourceQuota creates a ClusterResourceQuota resource using the provided client.
func CreateClusterResourceQuota(ctx context.Context, k8sClient client.Client, obj client.Object) error {
	return k8sClient.Create(ctx, obj)
}

// DeleteClusterResourceQuota deletes a ClusterResourceQuota resource using the provided client.
func DeleteClusterResourceQuota(ctx context.Context, k8sClient client.Client, obj client.Object) error {
	return k8sClient.Delete(ctx, obj)
}

func PtrBool(b bool) *bool    { return &b }
func PtrInt64(i int64) *int64 { return &i }

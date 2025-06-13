package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

// ServiceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API via a clientset created from the controller-runtime client config.
func ServiceAccountToken(ctx context.Context, clientSet *kubernetes.Clientset, k8sClient client.Client, namespace, serviceAccountName string) (string, error) {
	var tokenData string

	Eventually(func(g Gomega) {
		// First, verify the service account exists
		sa := &corev1.ServiceAccount{}
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceAccountName}, sa)
		g.Expect(err).NotTo(HaveOccurred(), "failed to get service account %s/%s", namespace, serviceAccountName)

		tokenRequest := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{}, // default spec
		}
		result, err := clientSet.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, serviceAccountName, tokenRequest, metav1.CreateOptions{})
		g.Expect(err).NotTo(HaveOccurred(), "failed to create token request for SA %s/%s", namespace, serviceAccountName)
		g.Expect(result.Status.Token).NotTo(BeEmpty(), "extracted token is empty")
		tokenData = result.Status.Token
	}).Should(Succeed())

	return tokenData, nil
}

// GetMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func GetMetricsOutput(ctx context.Context, clientSet *kubernetes.Clientset, namespace, curlPodName string) string {
	var metricsOutput string
	Eventually(func(g Gomega) {
		podLogOpts := &corev1.PodLogOptions{}
		req := clientSet.CoreV1().Pods(namespace).GetLogs(curlPodName, podLogOpts)

		logStream, err := req.Stream(ctx)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to stream logs from curl pod '%s'", curlPodName)
		defer logStream.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, logStream)
		g.Expect(err).NotTo(HaveOccurred(), "Failed to copy log stream to buffer for pod '%s'", curlPodName)

		output := buf.String()
		g.Expect(output).To(ContainSubstring("< HTTP/1.1 200 OK"), "Metrics endpoint did not return 200 OK. Logs: %s", output)
		metricsOutput = output
	}).Should(Succeed())
	return metricsOutput
}

// Helper functions for logs, events, pod description, and pointer helpers
func GetPodLogs(ctx context.Context, clientSet *kubernetes.Clientset, namespace, podName string) string {
	podLogOpts := &v1.PodLogOptions{}
	req := clientSet.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)
	logStream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Sprintf("Failed to stream logs: %v", err)
	}
	defer logStream.Close()
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
	pod := &v1.Pod{}
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

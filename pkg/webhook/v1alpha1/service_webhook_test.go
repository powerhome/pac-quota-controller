package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
)

var _ = Describe("ServiceWebhook", func() {
	var (
		ctx               context.Context
		webhook           *ServiceWebhook
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		logger            *zap.Logger
		ginEngine         *gin.Engine
		testNamespace     *corev1.Namespace
	)

	BeforeEach(func() {
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		}
		fakeClient = fake.NewSimpleClientset(testNamespace)
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		logger = zap.NewNop()
		webhook = &ServiceWebhook{
			client:            fakeClient,
			serviceCalculator: *services.NewServiceResourceCalculator(fakeClient),
			crqClient:         crqClient,
			log:               logger,
		}
		gin.SetMode(gin.TestMode)
		ginEngine = gin.New()
		ginEngine.POST("/webhook", webhook.Handle)
	})
	Describe("Service type and error handling (integration style)", func() {
		It("should allow ClusterIP service within quota", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clusterip",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)}},
				},
			}
			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeTrue())
		})
		It("should allow NodePort service within quota", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-nodeport",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)}},
				},
			}
			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeTrue())
		})
		It("should allow LoadBalancer service within quota", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-lb",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)}},
				},
			}
			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeTrue())
		})
		It("should allow ExternalName service within quota", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-external",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			}
			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeTrue())
		})
		It("should reject if namespace does not exist", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-missing-ns",
					Namespace: "does-not-exist",
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)}},
				},
			}
			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("failed to get namespace"))
		})
	})

	Describe("Handle", func() {
		It("should handle valid service creation request", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       80,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			}

			admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
			Expect(response.Response.UID).To(Equal(admissionReview.Request.UID))
		})

		It("should handle valid service update request", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       80,
							TargetPort: intstr.FromInt(8081),
						},
					},
				},
			}

			admissionReview := createServiceAdmissionReview(svc, admissionv1.Update)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
			Expect(response.Response.UID).To(Equal(admissionReview.Request.UID))
		})

		It("should reject request with nil admission review", func() {
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject request with nil admission review request", func() {
			admissionReview := &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
				},
				Request: nil,
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response).NotTo(BeNil())
			Expect(response.Response).NotTo(BeNil())
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Missing admission request"))
		})

		It("should reject request with wrong resource kind", func() {
			// Use a ConfigMap as a wrong kind
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-namespace",
				},
			}
			raw, _ := json.Marshal(cm)
			admissionReview := &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "ConfigMap",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "configmaps",
					},
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: raw,
					},
				},
			}
			response := sendWebhookRequest(ginEngine, admissionReview)
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Expected Service resource"))
		})

		It("should reject request with invalid JSON", func() {
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		Describe("validateCreate", func() {
			It("should validate service creation", func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}
				warnings, err := webhook.validateCreate(ctx, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})

		Describe("validateUpdate", func() {
			It("should validate service update", func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}
				warnings, err := webhook.validateUpdate(ctx, svc)
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeNil())
			})
		})

		Describe("Edge Cases", func() {
			It("should handle svc with very long name", func() {
				longName := "very-long-svc-name-that-exceeds-normal-length-limits-for-testing-purposes"
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      longName,
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}

				admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)

				Expect(response.Response.Allowed).To(BeTrue())
			})

			It("should handle svc with special characters in name", func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc-123_456-789",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}

				admissionReview := createServiceAdmissionReview(svc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)

				Expect(response.Response.Allowed).To(BeTrue())
			})
		})
		Describe("Cross-Namespace Quota Validation", func() {
			var (
				crq          *quotav1alpha1.ClusterResourceQuota
				namespace1   *corev1.Namespace
				namespace2   *corev1.Namespace
				namespace3   *corev1.Namespace // For non-matching namespace tests
				existingSvc1 *corev1.Service
				existingSvc2 *corev1.Service
				existingSvc3 *corev1.Service
			)

			BeforeEach(func() {
				// Create test namespaces with matching labels
				namespace1 = &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ns-1",
						Labels: map[string]string{
							"environment": "test",
							"team":        "platform",
						},
					},
				}
				namespace2 = &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ns-2",
						Labels: map[string]string{
							"environment": "test",
							"team":        "platform",
						},
					},
				}

				// Create a namespace that doesn't match the CRQ selector
				namespace3 = &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ns-3",
						Labels: map[string]string{
							"environment": "production",
							"team":        "backend",
						},
					},
				}

				// Create a ClusterResourceQuota that selects both test namespaces
				crq = &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crq",
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"environment": "test",
							},
						},
						Hard: quotav1alpha1.ResourceList{
							corev1.ResourceServices:              resource.MustParse("3"),
							corev1.ResourceServicesLoadBalancers: resource.MustParse("1"),
							corev1.ResourceServicesNodePorts:     resource.MustParse("1"),
						},
					},
				}

				// Create existing services in namespace1 with unique names
				existingSvc1 = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-svc-lb",
						Namespace: "test-ns-1",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}
				existingSvc2 = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-svc-nodeport",
						Namespace: "test-ns-1",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}
				existingSvc3 = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-svc-clusterip",
						Namespace: "test-ns-1",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}

				// Update the fake clients with the new resources
				fakeClient = fake.NewSimpleClientset(
					testNamespace,
					namespace1,
					namespace2,
					namespace3,
					existingSvc1,
					existingSvc2,
					existingSvc3,
				)
				scheme := runtime.NewScheme()
				_ = quotav1alpha1.AddToScheme(scheme)
				_ = corev1.AddToScheme(scheme)
				fakeRuntimeClient = ctrlclientfake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(crq, namespace1, namespace2, namespace3, existingSvc1, existingSvc2, existingSvc3).
					Build()
				crqClient = quota.NewCRQClient(fakeRuntimeClient)

				// Recreate webhook with updated clients
				webhook = NewServiceWebhook(fakeClient, crqClient, logger)

				// Re-setup gin engine
				ginEngine = gin.New()
				ginEngine.POST("/webhook", webhook.Handle)
			})

			AfterEach(func() {
				// Clean up cross-namespace test resources
				crq = nil
				namespace1 = nil
				namespace2 = nil
				namespace3 = nil
				existingSvc1 = nil
				existingSvc2 = nil
				existingSvc3 = nil
			})

			It("should reject svc that would exceed cross-namespace quota limits", func() {
				// Try to create a new service in namespace2 that would exceed the total quota
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-svc",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}

				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)

				Expect(response.Response.Allowed).To(BeFalse())
				Expect(response.Response.Result.Message).
					To(ContainSubstring("ClusterResourceQuota service count validation failed for"))
				Expect(response.Response.Result.Message).
					To(ContainSubstring("test-crq"))
				Expect(response.Response.Result.Message).
					To(ContainSubstring("limit exceeded"))
			})

			It("should allow svc that fits within cross-namespace quota limits", func() {
				// Increase the quota to allow one more LoadBalancer service (from 1 to 2)
				// Also increase the quota total number of services to 4 (from 3)
				crq.Spec.Hard[corev1.ResourceServices] = resource.MustParse("4")
				crq.Spec.Hard[corev1.ResourceServicesLoadBalancers] = resource.MustParse("2")

				// Rebuild the fake runtime client with updated CRQ
				scheme := runtime.NewScheme()
				_ = quotav1alpha1.AddToScheme(scheme)
				_ = corev1.AddToScheme(scheme)
				fakeRuntimeClient = ctrlclientfake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(crq, namespace1, namespace2, namespace3, existingSvc1, existingSvc2, existingSvc3).
					Build()
				crqClient = quota.NewCRQClient(fakeRuntimeClient)
				webhook = NewServiceWebhook(fakeClient, crqClient, logger)
				ginEngine = gin.New()
				ginEngine.POST("/webhook", webhook.Handle)

				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-svc",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(8080),
							},
						},
					},
				}

				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)

				Expect(response.Response.Allowed).To(BeTrue())
			})

		})

		// Service admission tests for all service resource types and quota scenarios
		Describe("Service admission", func() {
			var (
				fakeClient        *fake.Clientset
				fakeRuntimeClient client.Client
				ginEngine         *gin.Engine
				crq               *quotav1alpha1.ClusterResourceQuota
			)

			BeforeEach(func() {
				// Namespaces
				ns1 := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns-1",
						Labels: map[string]string{"environment": "test"},
					},
				}
				ns2 := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns-2",
						Labels: map[string]string{"environment": "test"},
					},
				}
				ns3 := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns-3",
						Labels: map[string]string{"environment": "production"},
					},
				}
				// CRQ for all service types
				crq = &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crq",
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"environment": "test",
							},
						},
						Hard: quotav1alpha1.ResourceList{
							corev1.ResourceServices:              resource.MustParse("5"),
							corev1.ResourceServicesNodePorts:     resource.MustParse("2"),
							corev1.ResourceServicesLoadBalancers: resource.MustParse("1"),
						},
					},
				}
				fakeClient = fake.NewSimpleClientset(ns1, ns2, ns3)
				scheme := runtime.NewScheme()
				_ = quotav1alpha1.AddToScheme(scheme)
				_ = corev1.AddToScheme(scheme)
				fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithObjects(crq, ns1, ns2, ns3).Build()
				crqClient = quota.NewCRQClient(fakeRuntimeClient)
				logger = zap.NewNop()
				ginEngine = gin.New()
				webhook = &ServiceWebhook{
					client:            fakeClient,
					serviceCalculator: *services.NewServiceResourceCalculator(fakeClient),
					crqClient:         crqClient,
					log:               logger,
				}
				ginEngine.POST("/webhook", webhook.Handle)
			})

			AfterEach(func() {
				fakeClient = nil
				fakeRuntimeClient = nil
				crqClient = nil
				logger = nil
				ginEngine = nil
			})

			It("should allow creation of a ClusterIP service within quota", func() {
				// No existing services, quota is 5
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-clusterip",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)}},
					},
				}
				_, err := fakeClient.CoreV1().Services("test-ns-2").Create(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-svc-1",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{{Name: "http", Port: 81, TargetPort: intstr.FromInt(8081)}},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)
				Expect(response.Response.Allowed).To(BeTrue())
			})

			It("should reject creation of a NodePort service if NodePort quota exceeded", func() {
				// Add two NodePort services to reach the quota (quota is 2)
				_, err := fakeClient.CoreV1().Services("test-ns-1").Create(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-nodeport-1",
						Namespace: "test-ns-1",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{{Name: "http", Port: 82, TargetPort: intstr.FromInt(8082)}},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				_, err = fakeClient.CoreV1().Services("test-ns-2").Create(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-nodeport-2",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{{Name: "http", Port: 83, TargetPort: intstr.FromInt(8083)}},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-nodeport",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{{Name: "http", Port: 84, TargetPort: intstr.FromInt(8084)}},
					},
				}
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)
				Expect(response.Response.Allowed).To(BeFalse())
				Expect(response.Response.Result.Message).To(ContainSubstring("service count validation failed"))
			})

			It("should allow creation of an ExternalName service within quota", func() {
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-externalname",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:         corev1.ServiceTypeExternalName,
						ExternalName: "example.com",
					},
				}
				// Add a ClusterIP service to test-ns-2 to ensure quota logic is exercised
				_, err := fakeClient.CoreV1().Services("test-ns-2").Create(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-svc-ext",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{{Name: "http", Port: 87, TargetPort: intstr.FromInt(8087)}},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)
				Expect(response.Response.Allowed).To(BeTrue())
			})

			It("should reject creation of a LoadBalancer service if quota exceeded", func() {
				// Add a LoadBalancer service to reach the quota (quota is 1)
				_, err := fakeClient.CoreV1().Services("test-ns-2").Create(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-lb",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{{Name: "http", Port: 88, TargetPort: intstr.FromInt(8088)}},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-loadbalancer",
						Namespace: "test-ns-2",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{{Name: "http", Port: 89, TargetPort: intstr.FromInt(8089)}},
					},
				}
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)
				Expect(response.Response.Allowed).To(BeFalse())
				Expect(response.Response.Result.Message).To(ContainSubstring("service count validation failed"))
			})

			It("should allow creation of a service in a namespace not matching CRQ selector", func() {
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unmatched-svc",
						Namespace: "test-ns-3",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{{Name: "http", Port: 85, TargetPort: intstr.FromInt(8085)}},
					},
				}
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(ginEngine, admissionReview)
				Expect(response.Response.Allowed).To(BeTrue())
			})

			It("should allow creation when no quota set", func() {
				// Remove quota for services
				crq.Spec.Hard = nil
				// Use a fresh Gin engine to avoid handler registration panic
				scheme := runtime.NewScheme()
				_ = quotav1alpha1.AddToScheme(scheme)
				_ = corev1.AddToScheme(scheme)
				ns1 := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns-1",
						Labels: map[string]string{"environment": "test"},
					},
				}
				fakeRuntimeClient := ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithObjects(crq, ns1).Build()
				crqClient := quota.NewCRQClient(fakeRuntimeClient)
				webhook := &ServiceWebhook{
					client:            fakeClient,
					serviceCalculator: *services.NewServiceResourceCalculator(fakeClient),
					crqClient:         crqClient,
					log:               logger,
				}
				freshGin := gin.New()
				freshGin.POST("/webhook", webhook.Handle)
				newSvc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-quota-svc",
						Namespace: "test-ns-1",
					},
					Spec: corev1.ServiceSpec{
						Type:  corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{{Name: "http", Port: 86, TargetPort: intstr.FromInt(8086)}},
					},
				}
				admissionReview := createServiceAdmissionReview(newSvc, admissionv1.Create)
				response := sendWebhookRequest(freshGin, admissionReview)
				Expect(response.Response.Allowed).To(BeTrue())
			})

			// Add more tests for error paths, CRQ lookup errors, and edge cases as needed
		})
	})
})

// Helper functions for testing
func createServiceAdmissionReview(
	service *corev1.Service,
	operation admissionv1.Operation,
) *admissionv1.AdmissionReview {
	raw, _ := json.Marshal(service)
	return &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "services",
			},
			Operation: operation,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}
}

package quota

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

var validScopes = map[corev1.ResourceQuotaScope]bool{
	corev1.ResourceQuotaScopeTerminating:               true,
	corev1.ResourceQuotaScopeNotTerminating:            true,
	corev1.ResourceQuotaScopeBestEffort:                true,
	corev1.ResourceQuotaScopeNotBestEffort:             true,
	corev1.ResourceQuotaScopePriorityClass:             true,
	corev1.ResourceQuotaScopeCrossNamespacePodAffinity: true,
}

var mutuallyExclusiveScopePairs = [][2]corev1.ResourceQuotaScope{
	{corev1.ResourceQuotaScopeBestEffort, corev1.ResourceQuotaScopeNotBestEffort},
	{corev1.ResourceQuotaScopeTerminating, corev1.ResourceQuotaScopeNotTerminating},
}

// podScopeEligibleResources are the hard keys any pod scope other than BestEffort
// may limit; BestEffort further restricts to pods. Mirrors upstream
// podComputeQuotaResources in pkg/apis/core/helper/helpers.go @ release-1.36 —
// notably, ephemeral-storage is a standard quota resource but is NOT scope-eligible.
var podScopeEligibleResources = map[corev1.ResourceName]bool{
	usage.ResourcePods:           true,
	usage.ResourceRequestsCPU:    true,
	usage.ResourceRequestsMemory: true,
	usage.ResourceLimitsCPU:      true,
	usage.ResourceLimitsMemory:   true,
}

// standardNonScopeResources are standard quota resources that no scope may restrict.
var standardNonScopeResources = map[corev1.ResourceName]bool{
	usage.ResourceRequestsEphemeralStorage: true,
	usage.ResourceLimitsEphemeralStorage:   true,
	usage.ResourceServices:                 true,
	usage.ResourceServicesLoadBalancers:    true,
	usage.ResourceServicesNodePorts:        true,
	usage.ResourceRequestsStorage:          true,
	usage.ResourcePersistentVolumeClaims:   true,
	usage.ResourceConfigMaps:               true,
	usage.ResourceSecrets:                  true,
	usage.ResourceReplicationControllers:   true,
	usage.ResourceDeployments:              true,
	usage.ResourceStatefulSets:             true,
	usage.ResourceDaemonSets:               true,
	usage.ResourceJobs:                     true,
	usage.ResourceCronJobs:                 true,
	usage.ResourceHorizontalPodAutoscalers: true,
	usage.ResourceIngresses:                true,
}

// isStandardQuotaResource mirrors upstream IsStandardQuotaResourceName: extended
// resources (requests.<domain>/<res>, hugepages-*) bypass scope restrictions.
func isStandardQuotaResource(name corev1.ResourceName) bool {
	return podScopeEligibleResources[name] || standardNonScopeResources[name] ||
		strings.Contains(string(name), ".storageclass.storage.k8s.io/")
}

func scopeAllowsResource(scope corev1.ResourceQuotaScope, name corev1.ResourceName) bool {
	if !isStandardQuotaResource(name) {
		return true
	}
	if scope == corev1.ResourceQuotaScopeBestEffort {
		return name == usage.ResourcePods
	}
	return podScopeEligibleResources[name]
}

// ValidateScopeSpec validates spec.scopes and spec.scopeSelector, mirroring
// upstream ValidateResourceQuotaSpec (pkg/apis/core/validation/validation.go
// @ release-1.36). A non-nil error rejects the CRQ; warnings flag legal-but-suspect
// combinations upstream does not check.
func ValidateScopeSpec(crq *quotav1alpha1.ClusterResourceQuota) ([]string, error) {
	seen := map[corev1.ResourceQuotaScope]bool{}
	for _, s := range crq.Spec.Scopes {
		if !validScopes[s] {
			return nil, fmt.Errorf("spec.scopes: invalid quota scope %q", s)
		}
		if seen[s] {
			return nil, fmt.Errorf("spec.scopes: duplicate scope %q", s)
		}
		seen[s] = true
	}
	if err := rejectMutuallyExclusive("spec.scopes", seen); err != nil {
		return nil, err
	}

	selSeen := map[corev1.ResourceQuotaScope]bool{}
	if sel := crq.Spec.ScopeSelector; sel != nil {
		for _, req := range sel.MatchExpressions {
			if err := validateScopeRequirement(req); err != nil {
				return nil, err
			}
			selSeen[req.ScopeName] = true
		}
	}
	// Upstream validates each field's internal consistency independently; a
	// contradiction living entirely within scopeSelector is its own hard error,
	// same as one entirely within scopes.
	if err := rejectMutuallyExclusive("spec.scopeSelector", selSeen); err != nil {
		return nil, err
	}

	all := pod.BuildScopeRequirements(crq.Spec.Scopes, crq.Spec.ScopeSelector)
	for _, req := range all {
		for name := range crq.Spec.Hard {
			if !scopeAllowsResource(req.ScopeName, name) {
				return nil, fmt.Errorf("unsupported scope %s applied to resource %q", req.ScopeName, name)
			}
		}
	}

	// Upstream does not check contradictions that span both fields (one scope via
	// `scopes`, its opposite via `scopeSelector`); such a quota is legal but matches
	// no pods, so it's surfaced here as an admission warning instead.
	var warnings []string
	names := map[corev1.ResourceQuotaScope]bool{}
	for _, req := range all {
		names[req.ScopeName] = true
	}
	for _, pair := range mutuallyExclusiveScopePairs {
		if names[pair[0]] && names[pair[1]] {
			warnings = append(warnings,
				fmt.Sprintf("scopes %s and %s together match no pods; the quota will never accrue usage",
					pair[0], pair[1]))
		}
	}
	return warnings, nil
}

func rejectMutuallyExclusive(fld string, present map[corev1.ResourceQuotaScope]bool) error {
	for _, pair := range mutuallyExclusiveScopePairs {
		if present[pair[0]] && present[pair[1]] {
			return fmt.Errorf("%s: %s and %s are mutually exclusive", fld, pair[0], pair[1])
		}
	}
	return nil
}

func validateScopeRequirement(req corev1.ScopedResourceSelectorRequirement) error {
	if !validScopes[req.ScopeName] {
		return fmt.Errorf("spec.scopeSelector: invalid quota scope %q", req.ScopeName)
	}
	switch req.Operator {
	case corev1.ScopeSelectorOpIn, corev1.ScopeSelectorOpNotIn:
		if len(req.Values) == 0 {
			return fmt.Errorf("spec.scopeSelector: operator %s requires values for scope %q",
				req.Operator, req.ScopeName)
		}
	case corev1.ScopeSelectorOpExists, corev1.ScopeSelectorOpDoesNotExist:
		if len(req.Values) != 0 {
			return fmt.Errorf("spec.scopeSelector: operator %s must not have values for scope %q",
				req.Operator, req.ScopeName)
		}
	default:
		return fmt.Errorf("spec.scopeSelector: invalid operator %q", req.Operator)
	}
	if req.ScopeName != corev1.ResourceQuotaScopePriorityClass && req.Operator != corev1.ScopeSelectorOpExists {
		return fmt.Errorf("spec.scopeSelector: scope %q only supports the Exists operator", req.ScopeName)
	}
	return nil
}

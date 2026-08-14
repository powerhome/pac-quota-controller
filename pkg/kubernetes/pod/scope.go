package pod

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// HasScopes reports whether the CRQ declares any scope constraint.
func HasScopes(scopes []corev1.ResourceQuotaScope, sel *corev1.ScopeSelector) bool {
	return len(scopes) > 0 || (sel != nil && len(sel.MatchExpressions) > 0)
}

// ValidScopes are the ResourceQuotaScope values podMatchesScopeRequirement
// knows how to match. The single source of truth for "is this scope name
// valid" — callers validating a scope spec (e.g. quota.ValidateScopeSpec)
// should check membership here rather than keeping a second copy.
var ValidScopes = map[corev1.ResourceQuotaScope]bool{
	corev1.ResourceQuotaScopeTerminating:               true,
	corev1.ResourceQuotaScopeNotTerminating:            true,
	corev1.ResourceQuotaScopeBestEffort:                true,
	corev1.ResourceQuotaScopeNotBestEffort:             true,
	corev1.ResourceQuotaScopePriorityClass:             true,
	corev1.ResourceQuotaScopeCrossNamespacePodAffinity: true,
}

// BuildScopeRequirements normalizes plain scopes (each an implicit Exists) and
// scopeSelector.matchExpressions into a single ANDed requirement list.
func BuildScopeRequirements(
	scopes []corev1.ResourceQuotaScope,
	sel *corev1.ScopeSelector,
) []corev1.ScopedResourceSelectorRequirement {
	reqs := make([]corev1.ScopedResourceSelectorRequirement, 0, len(scopes))
	for _, s := range scopes {
		reqs = append(reqs, corev1.ScopedResourceSelectorRequirement{
			ScopeName: s,
			Operator:  corev1.ScopeSelectorOpExists,
		})
	}
	if sel != nil {
		reqs = append(reqs, sel.MatchExpressions...)
	}
	return reqs
}

// PodInScope reports whether the pod matches every scope requirement.
// A CRQ without scopes matches all pods.
func PodInScope(p *corev1.Pod, scopes []corev1.ResourceQuotaScope, sel *corev1.ScopeSelector) (bool, error) {
	if p == nil {
		return false, nil
	}
	return podMatchesRequirements(p, BuildScopeRequirements(scopes, sel))
}

// FilterInScope returns the pods matching every scope requirement.
// Without scopes it returns the input slice unchanged.
func FilterInScope(
	pods []corev1.Pod,
	scopes []corev1.ResourceQuotaScope,
	sel *corev1.ScopeSelector,
) ([]corev1.Pod, error) {
	if !HasScopes(scopes, sel) {
		return pods, nil
	}
	// Built once: the requirement list is the same for every pod in this call,
	// so rebuilding it per pod would be a pointless O(n) reallocation.
	reqs := BuildScopeRequirements(scopes, sel)
	filtered := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		matches, err := podMatchesRequirements(&pods[i], reqs)
		if err != nil {
			return nil, err
		}
		if matches {
			filtered = append(filtered, pods[i])
		}
	}
	return filtered, nil
}

// podMatchesRequirements reports whether the pod matches every requirement.
func podMatchesRequirements(p *corev1.Pod, reqs []corev1.ScopedResourceSelectorRequirement) (bool, error) {
	for _, req := range reqs {
		matches, err := podMatchesScopeRequirement(p, req)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

// podMatchesScopeRequirement mirrors upstream podMatchesScopeFunc; the operator is
// only consulted for PriorityClass (validation restricts the other scopes to Exists).
// Ported from k8s.io/kubernetes/pkg/quota/v1/evaluator/core/pods.go @ release-1.36.
func podMatchesScopeRequirement(p *corev1.Pod, req corev1.ScopedResourceSelectorRequirement) (bool, error) {
	switch req.ScopeName {
	case corev1.ResourceQuotaScopeTerminating:
		return isTerminating(p), nil
	case corev1.ResourceQuotaScopeNotTerminating:
		return !isTerminating(p), nil
	case corev1.ResourceQuotaScopeBestEffort:
		return isBestEffort(p), nil
	case corev1.ResourceQuotaScopeNotBestEffort:
		return !isBestEffort(p), nil
	case corev1.ResourceQuotaScopePriorityClass:
		if req.Operator == corev1.ScopeSelectorOpExists {
			return len(p.Spec.PriorityClassName) != 0, nil
		}
		return podMatchesPriorityClassSelector(p, req)
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return usesCrossNamespacePodAffinity(p), nil
	default:
		return false, fmt.Errorf("invalid quota scope %q", req.ScopeName)
	}
}

func isTerminating(p *corev1.Pod) bool {
	return p.Spec.ActiveDeadlineSeconds != nil && *p.Spec.ActiveDeadlineSeconds >= 0
}

func isBestEffort(p *corev1.Pod) bool {
	return ComputePodQOS(p) == corev1.PodQOSBestEffort
}

// podMatchesPriorityClassSelector matches priorityClassName as a label set, so
// NotIn/DoesNotExist match pods without a priority class, exactly as upstream.
// Ported from ScopedResourceSelectorRequirementsAsSelector in
// k8s.io/kubernetes/pkg/apis/core/v1/helper/helpers.go @ release-1.36.
func podMatchesPriorityClassSelector(p *corev1.Pod, req corev1.ScopedResourceSelectorRequirement) (bool, error) {
	var op selection.Operator
	switch req.Operator {
	case corev1.ScopeSelectorOpIn:
		op = selection.In
	case corev1.ScopeSelectorOpNotIn:
		op = selection.NotIn
	case corev1.ScopeSelectorOpExists:
		op = selection.Exists
	case corev1.ScopeSelectorOpDoesNotExist:
		op = selection.DoesNotExist
	default:
		return false, fmt.Errorf("invalid scope selector operator %q", req.Operator)
	}
	r, err := labels.NewRequirement(string(req.ScopeName), op, req.Values)
	if err != nil {
		return false, err
	}
	var m map[string]string
	if len(p.Spec.PriorityClassName) != 0 {
		m = map[string]string{string(corev1.ResourceQuotaScopePriorityClass): p.Spec.PriorityClassName}
	}
	return labels.NewSelector().Add(*r).Matches(labels.Set(m)), nil
}

// usesCrossNamespacePodAffinity and crossNamespaceTerm are ported from
// k8s.io/kubernetes/pkg/quota/v1/evaluator/core/pods.go @ release-1.36.
func crossNamespaceTerm(term corev1.PodAffinityTerm) bool {
	return len(term.Namespaces) != 0 || term.NamespaceSelector != nil
}

func usesCrossNamespacePodAffinity(p *corev1.Pod) bool {
	if p.Spec.Affinity == nil {
		return false
	}
	var terms []corev1.PodAffinityTerm
	var weighted []corev1.WeightedPodAffinityTerm
	if a := p.Spec.Affinity.PodAffinity; a != nil {
		terms = append(terms, a.RequiredDuringSchedulingIgnoredDuringExecution...)
		weighted = append(weighted, a.PreferredDuringSchedulingIgnoredDuringExecution...)
	}
	if a := p.Spec.Affinity.PodAntiAffinity; a != nil {
		terms = append(terms, a.RequiredDuringSchedulingIgnoredDuringExecution...)
		weighted = append(weighted, a.PreferredDuringSchedulingIgnoredDuringExecution...)
	}
	for _, t := range terms {
		if crossNamespaceTerm(t) {
			return true
		}
	}
	for _, w := range weighted {
		if crossNamespaceTerm(w.PodAffinityTerm) {
			return true
		}
	}
	return false
}

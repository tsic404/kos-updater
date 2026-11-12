package updater

import (
	"context"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/abi"
	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/pages/cluster"
	"github.com/tsic404/kosd/pkg/provider"
)

// capabilities reads the registered capabilities and their verification results
// from the Provider objects (C-1).
func (r *Readings) capabilities(ctx context.Context) (any, *controlplane.Error) {
	providers, failure := r.providers(ctx)
	if failure != nil {
		return nil, failure
	}
	entries := make([]capabilityEntry, 0)
	for _, provider := range providers {
		registered := provider.Spec.Capabilities
		if len(registered) == 0 {
			// A registration path may carry the set only in status; reading it
			// there keeps the value equal to the object either way.
			registered = provider.Status.EffectiveCapabilities
		}
		for _, capability := range registered {
			entry := capabilityEntry{
				Provider:   provider.Name,
				Capability: capability.Name,
				Version:    capability.Version,
				Scope:      string(capability.Scope),
				Effective:  isEffective(provider.Status.EffectiveCapabilities, capability.Name, capability.Version),
			}
			if result, found := verificationOf(provider.Status.Capabilities, capability.Name, capability.Version); found {
				entry.Source = result.Source
				entry.State = result.State
				entry.Reason = result.Reason
				entry.FailedCases = append([]string(nil), result.FailedCases...)
			}
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(left, right int) bool {
		return capabilityOrder(entries[left]) < capabilityOrder(entries[right])
	})
	return capabilitiesValue{availability: observed(), Capabilities: entries}, nil
}

// isEffective reports whether one registered capability is in the effective set.
func isEffective(effective []v1beta1.ProviderCapability, name, version string) bool {
	for _, capability := range effective {
		if capability.Name == name && capability.Version == version {
			return true
		}
	}
	return false
}

// verificationOf finds one capability's verification result.
func verificationOf(results []v1beta1.ProviderCapabilityResult, name, version string) (v1beta1.ProviderCapabilityResult, bool) {
	for _, result := range results {
		if result.Name == name && result.Version == version {
			return result, true
		}
	}
	return v1beta1.ProviderCapabilityResult{}, false
}

// capabilityOrder sorts the capability entries by provider, name and version.
func capabilityOrder(entry capabilityEntry) string {
	return entry.Provider + "\x00" + entry.Capability + "\x00" + entry.Version
}

// gaps compares the declared capability requirements against the effective
// capability set, missing and unsatisfied kept apart (C-1, C-11).
func (r *Readings) gaps(ctx context.Context, session identity.Session) (any, *controlplane.Error) {
	definitions, failure := r.definitions(ctx)
	if failure != nil {
		return nil, failure
	}
	providers, failure := r.providers(ctx)
	if failure != nil {
		return nil, failure
	}
	definitionGaps := make([]gapEntry, 0, len(definitions))
	requirements := make(map[string][]v1beta1.CapabilityRequirement, len(definitions))
	for _, definition := range definitions {
		requirements[definition.Name] = definition.Spec.Requires
		entry, unmet := gapOf("", definition.Name, definition.Spec.Requires, providers)
		if unmet {
			definitionGaps = append(definitionGaps, entry)
		}
	}
	applicationGaps, failure := r.applicationGaps(ctx, session, requirements, providers)
	if failure != nil {
		return nil, failure
	}
	return gapsValue{
		availability: observed(),
		Rule:         "按对象声明的能力需求与 Provider 的有效能力集合逐项比较：缺失与保证不足分开，作用域不相容的登记不参与比较",
		Definitions:  definitionGaps,
		Applications: applicationGaps,
	}, nil
}

// applicationGaps reports the installed applications whose definition requires a
// capability no provider serves.
func (r *Readings) applicationGaps(ctx context.Context, session identity.Session, requirements map[string][]v1beta1.CapabilityRequirement, providers []v1beta1.Provider) ([]gapEntry, *controlplane.Error) {
	applications, failure := r.applications(ctx, session)
	if failure != nil {
		return nil, failure
	}
	gaps := make([]gapEntry, 0)
	for _, application := range applications {
		required, known := requirements[application.Spec.DefinitionRef]
		if !known {
			continue
		}
		entry, unmet := gapOf(application.Namespace, application.Name, required, providers)
		if unmet {
			gaps = append(gaps, entry)
		}
	}
	sort.Slice(gaps, func(left, right int) bool {
		if gaps[left].Namespace != gaps[right].Namespace {
			return gaps[left].Namespace < gaps[right].Namespace
		}
		return gaps[left].Name < gaps[right].Name
	})
	return gaps, nil
}

// gapOf compares one object's requirements and reports whether it has a gap.
func gapOf(namespace, name string, requirements []v1beta1.CapabilityRequirement, providers []v1beta1.Provider) (gapEntry, bool) {
	result, err := cluster.Compare(requirements, providers)
	if err != nil {
		return gapEntry{Namespace: namespace, Name: name, Invalid: err.Error()}, true
	}
	gaps := cluster.Gaps(result)
	if len(gaps) == 0 {
		return gapEntry{}, false
	}
	return gapEntry{Namespace: namespace, Name: name, Gaps: gaps}, true
}

// applications lists the applications the session may see (C-59).
func (r *Readings) applications(ctx context.Context, session identity.Session) ([]v1beta1.Application, *controlplane.Error) {
	objects, failure := r.reader().List(ctx, "Application", "")
	if failure != nil {
		return nil, failure
	}
	applications := make([]v1beta1.Application, 0, len(objects))
	for _, object := range objects {
		var application v1beta1.Application
		if failure := cluster.Decode(object, &application); failure != nil {
			return nil, failure
		}
		visible, failure := r.reader().Visible(ctx, session, "Application", object)
		if failure != nil {
			return nil, failure
		}
		if !visible {
			continue
		}
		applications = append(applications, application)
	}
	sort.Slice(applications, func(left, right int) bool {
		if applications[left].Namespace != applications[right].Namespace {
			return applications[left].Namespace < applications[right].Namespace
		}
		return applications[left].Name < applications[right].Name
	})
	return applications, nil
}

// installable lists the definitions whose required capabilities are served (C-1).
func (r *Readings) installable(ctx context.Context) (any, *controlplane.Error) {
	definitions, failure := r.definitions(ctx)
	if failure != nil {
		return nil, failure
	}
	providers, failure := r.providers(ctx)
	if failure != nil {
		return nil, failure
	}
	entries := make([]installableEntry, 0, len(definitions))
	for _, definition := range definitions {
		entry := installableEntry{Name: definition.Name, Version: definition.Spec.Package.Version}
		result, err := cluster.Compare(definition.Spec.Requires, providers)
		if err != nil {
			entry.Invalid = err.Error()
			entries = append(entries, entry)
			continue
		}
		// A missing required capability blocks the install; an unsatisfied
		// optional requirement only degrades the application (C-11).
		entry.Installable = len(result.Missing) == 0
		entry.Gaps = cluster.Gaps(result)
		entries = append(entries, entry)
	}
	return installableValue{
		availability: observed(),
		Rule:         "定义存在且必需的能力需求全部被满足的组件可安装；可选需求缺失或保证不足只使应用降级，不改变可安装性",
		Definitions:  entries,
	}, nil
}

// extensionState reads the enablement state and missing dependencies of every
// system component (C-1, C-28).
func (r *Readings) extensionState(ctx context.Context) (any, *controlplane.Error) {
	components, failure := r.components(ctx)
	if failure != nil {
		return nil, failure
	}
	states := make([]extensionStateEntry, 0, len(components))
	for _, component := range components {
		state := extensionStateEntry{
			Name:       component.Name,
			Phase:      component.Status.Phase,
			Health:     string(component.Status.Health.State),
			Disabled:   component.Spec.Disable,
			Conditions: make([]extensionCondition, 0, len(component.Status.Conditions)),
		}
		for _, condition := range component.Status.Conditions {
			state.Conditions = append(state.Conditions, extensionCondition{
				Type:    condition.Type,
				Status:  string(condition.Status),
				Reason:  condition.Reason,
				Message: condition.Message,
			})
		}
		states = append(states, state)
	}
	return extensionStateValue{availability: observed(), Components: states}, nil
}

// blockedProviders reads the providers whose UpgradeBlocked condition is true and
// the objects that keep them from advancing (C-1, C-48).
func (r *Readings) blockedProviders(ctx context.Context) (any, *controlplane.Error) {
	providers, failure := r.providers(ctx)
	if failure != nil {
		return nil, failure
	}
	blocked := make([]blockedProviderEntry, 0)
	for _, registered := range providers {
		condition, found := conditionOf(registered.Status.Conditions, provider.ConditionUpgradeBlocked)
		if !found || string(condition.Status) != "True" {
			continue
		}
		blocked = append(blocked, blockedProviderEntry{
			Name:       registered.Name,
			Version:    registered.Spec.Version,
			Reason:     condition.Reason,
			Message:    condition.Message,
			Dependents: referencesOf(registered.Status.Dependents),
			References: referencesOf(registered.Status.References),
		})
	}
	return blockedProvidersValue{availability: observed(), Providers: blocked}, nil
}

// conditionOf finds one condition by type.
func conditionOf(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

// referencesOf renders the object references a Provider status carries.
func referencesOf(references []v1beta1.ObjectRef) []objectReference {
	rendered := make([]objectReference, 0, len(references))
	for _, reference := range references {
		rendered = append(rendered, objectReference{
			Group:     reference.Group,
			Version:   reference.Version,
			Kind:      reference.Kind,
			Namespace: reference.Namespace,
			Name:      reference.Name,
		})
	}
	return rendered
}

// versions reports the KOS versions this control plane serves and the versions
// the cluster nodes run (C-2).
func (r *Readings) versions(ctx context.Context) (any, *controlplane.Error) {
	nodes, failure := r.reader().Nodes(ctx)
	if failure != nil {
		return nil, failure
	}
	clusterNodes := make([]nodeVersion, 0, len(nodes))
	for _, node := range nodes {
		clusterNodes = append(clusterNodes, nodeVersion{
			Name:           node.Name,
			KubeletVersion: node.Status.NodeInfo.KubeletVersion,
			OSImage:        node.Status.NodeInfo.OSImage,
		})
	}
	sort.Slice(clusterNodes, func(left, right int) bool {
		return clusterNodes[left].Name < clusterNodes[right].Name
	})
	return versionsValue{
		availability: observed(),
		KOS: kosVersion{
			APIVersions:       append([]string(nil), abi.Versions...),
			StorageVersion:    abi.StorageVersion,
			FrameworkVersions: extension.SupportedFrameworks(),
		},
		Cluster: clusterVersion{Nodes: clusterNodes},
	}, nil
}

// compatibility reports each installed extension's declared framework range, the
// framework version this kernel serves and the conclusion drawn from the two
// (C-3).
func (r *Readings) compatibility(ctx context.Context) (any, *controlplane.Error) {
	if r.Artifacts == nil {
		return nil, &controlplane.Error{
			Code:        controlplane.CodeCapabilityMissing,
			Message:     "发行版没有接入扩展包来源，已安装扩展的目标框架版本范围不可读",
			Stage:       controlplane.StageCapability,
			SideEffects: controlplane.SideEffectsNone,
		}
	}
	components, failure := r.components(ctx)
	if failure != nil {
		return nil, failure
	}
	supported := extension.SupportedFrameworks()
	entries := make([]compatibilityEntry, 0, len(components))
	for _, component := range components {
		reference := component.Spec.Package.Reference
		if reference == "" || component.DeletionTimestamp != nil {
			// An adopted component declares no package and no framework range;
			// a deleting one is on its way out (C-17, C-28).
			continue
		}
		entry := compatibilityEntry{
			Available:        true,
			Component:        component.Name,
			FrameworkVersion: extension.FrameworkVersion,
			ComponentPhase:   component.Status.Phase,
		}
		if condition, found := conditionOf(component.Status.Conditions, extension.ConditionFrameworkCompatible); found {
			entry.ComponentFrameworkCompatible = string(condition.Status)
		}
		content, err := r.Artifacts.Fetch(ctx, reference)
		if err != nil {
			entry.Available = false
			entry.Conclusion = ConclusionUnknown
			entry.Reason = "扩展包不可读取，目标框架版本范围无法判定：" + err.Error()
			entries = append(entries, entry)
			continue
		}
		declaration, err := extension.Parse(content)
		if err != nil {
			entry.Available = false
			entry.Conclusion = ConclusionUnknown
			entry.Reason = "扩展声明不可读，目标框架版本范围无法判定：" + err.Error()
			entries = append(entries, entry)
			continue
		}
		loaded, err := declaration.Load()
		if err != nil {
			entry.Available = false
			entry.Conclusion = ConclusionUnknown
			entry.Reason = "扩展声明无效，目标框架版本范围无法判定：" + err.Error()
			entries = append(entries, entry)
			continue
		}
		entry.Identifier = loaded.Identifier
		entry.Version = loaded.Version
		entry.TargetRange = loaded.FrameworkVersion
		compatible, reason := admitsFramework(loaded.FrameworkVersion, supported)
		if compatible {
			entry.Conclusion = ConclusionCompatible
		} else {
			entry.Conclusion = ConclusionIncompatible
			entry.Reason = reason
		}
		entries = append(entries, entry)
	}
	return compatibilityValue{
		availability:        observed(),
		FrameworkVersion:    extension.FrameworkVersion,
		SupportedFrameworks: supported,
		Extensions:          entries,
	}, nil
}

// admitsFramework applies C-16's rule to one declared target range: a range
// admits at least one served framework version. An empty range admits every
// version, an unreadable one admits none.
func admitsFramework(target string, supported []string) (bool, string) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return true, ""
	}
	constraint, err := extension.ParseConstraint(trimmed)
	if err != nil {
		return false, "目标框架版本 " + trimmed + " 无法解析：" + err.Error()
	}
	for _, version := range supported {
		if constraint.Allows(version) {
			return true, ""
		}
	}
	return false, "目标框架版本 " + trimmed + " 不含当前框架版本 " + strings.Join(supported, ", ")
}

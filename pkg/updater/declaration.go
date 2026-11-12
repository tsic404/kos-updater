// Package updater is the kos-updater product: the component and update views
// of the panel. Both views are server-side data projections, so the product
// declares pages and ships no process of its own (C-18, C-53).
package updater

import "github.com/tsic404/kosd/pkg/extension"

// Identifier is the product's extension identity across versions (C-17).
const Identifier = "kos-updater"

// Version is the packaged product version.
const Version = "1.0.0"

// FrameworkVersion is the framework range this package targets; a declaration
// that admits none of the served versions is incompatible (C-16).
const FrameworkVersion = ">=1.0 <2.0"

// Compatibility conclusions one installed extension entry reports (C-3).
const (
	// ConclusionCompatible means the declared range admits the served framework
	// version; the entry does not change the component state.
	ConclusionCompatible = "compatible"
	// ConclusionIncompatible means the declared range admits no served version;
	// the component state is incompatible with it.
	ConclusionIncompatible = "incompatible"
	// ConclusionUnknown means the declaration could not be read, so no
	// conclusion is drawn from an invented range.
	ConclusionUnknown = "unknown"
)

// Page identities the product declares (C-1, C-2).
const (
	// PageComponents lists what the cluster installed and can install (C-1).
	PageComponents = "components"
	// PageUpdates shows versions and compatibility (C-2).
	PageUpdates = "updates"
)

// Item identities of the component page, one per source C-1 names.
const (
	ItemCapabilities     = "installed-capabilities"
	ItemGaps             = "capability-gaps"
	ItemInstallable      = "installable-components"
	ItemExtensionState   = "extension-state"
	ItemBlockedProviders = "blocked-providers"
)

// Item identities of the update page, one per source C-2 names. The preflight
// and the blocked-upgrade items belong to the follow-up increment; this package
// declares the two read-only items alone.
const (
	ItemVersions      = "versions"
	ItemCompatibility = "extension-compatibility"
)

// ComponentItems lists the item identities of the component page, in declaration
// order (C-1). Each item appears on that page and nowhere else.
var ComponentItems = []string{
	ItemCapabilities,
	ItemGaps,
	ItemInstallable,
	ItemExtensionState,
	ItemBlockedProviders,
}

// UpdateItems lists the item identities of the update page, in declaration order
// (C-2).
var UpdateItems = []string{
	ItemVersions,
	ItemCompatibility,
}

// Read-only reasons: neither page carries a modification entry in this
// increment. The component view is read-only by its contract (C-1); the update
// view states why it offers none until its preflight and upgrade entries
// arrive with the follow-up increment (C-6).
const (
	ComponentsReadOnlyReason = "本页只读：组件启停经系统组件资源与安装流程发起，能力来自 Provider 的注册与验证结果"
	UpdatesReadOnlyReason    = "本页只读：平台与扩展升级由 kosadm 与 kosd 执行，本页呈现版本与兼容性"
)

// Declaration returns the contribution kos-updater ships (C-18): the two
// pages C-1 and C-2 put in the panel.
func Declaration() extension.Declaration {
	return extension.Declaration{
		Identifier:       Identifier,
		Version:          Version,
		FrameworkVersion: FrameworkVersion,
		Contributions: extension.Contributions{
			Pages: []extension.Page{componentsPage(), updatesPage()},
		},
	}
}

// componentsPage declares the component items C-1 names, each with the source
// type that judges it.
func componentsPage() extension.Page {
	return extension.Page{
		ID:             PageComponents,
		Title:          "组件",
		Location:       "panel",
		ReadOnly:       true,
		ReadOnlyReason: ComponentsReadOnlyReason,
		Items: []extension.PageItem{
			{ID: ItemCapabilities, Label: "已装能力", Source: extension.PageSourceObject, Reference: reference(ItemCapabilities)},
			{ID: ItemGaps, Label: "能力缺口", Source: extension.PageSourceComputed, Reference: reference(ItemGaps)},
			{ID: ItemInstallable, Label: "可安装组件", Source: extension.PageSourceComputed, Reference: reference(ItemInstallable)},
			{ID: ItemExtensionState, Label: "扩展启用状态与缺失依赖", Source: extension.PageSourceObject, Reference: reference(ItemExtensionState)},
			{ID: ItemBlockedProviders, Label: "升级被阻塞的 Provider 与依赖者", Source: extension.PageSourceObject, Reference: reference(ItemBlockedProviders)},
		},
	}
}

// updatesPage declares the update items C-2 names, each with the source type
// that judges it.
func updatesPage() extension.Page {
	return extension.Page{
		ID:             PageUpdates,
		Title:          "更新",
		Location:       "panel",
		ReadOnly:       true,
		ReadOnlyReason: UpdatesReadOnlyReason,
		Items: []extension.PageItem{
			{ID: ItemVersions, Label: "KOS 版本与集群版本", Source: extension.PageSourceComputed, Reference: reference(ItemVersions)},
			{ID: ItemCompatibility, Label: "扩展与框架版本兼容性", Source: extension.PageSourceComputed, Reference: reference(ItemCompatibility)},
		},
	}
}

// reference is the item reference the router dispatches on; the identifier in
// front of it names this product (C-22).
func reference(item string) string { return Identifier + "/" + item }

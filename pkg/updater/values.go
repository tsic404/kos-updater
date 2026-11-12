package updater

// The value types of the declared items. Every item is one JSON object whose
// fields are the source's own facts; a source that cannot be observed yields the
// unavailable shape instead (C-7).

// extensionStateValue is the enablement state of the installed components (C-1).
type extensionStateValue struct {
	availability
	Components []extensionStateEntry `json:"components"`
}

// extensionStateEntry is one system component with the conditions its status
// carries; the missing dependencies are read off the DependenciesMet condition.
type extensionStateEntry struct {
	Name       string               `json:"name"`
	Phase      string               `json:"phase,omitempty"`
	Health     string               `json:"health,omitempty"`
	Disabled   bool                 `json:"disabled"`
	Conditions []extensionCondition `json:"conditions"`
}

// extensionCondition is one condition as the component status stores it.
type extensionCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// blockedProvidersValue is the providers whose upgrade a dependent blocks (C-1).
type blockedProvidersValue struct {
	availability
	Providers []blockedProviderEntry `json:"providers"`
}

// blockedProviderEntry is one blocked Provider with the objects holding it.
type blockedProviderEntry struct {
	Name       string            `json:"name"`
	Version    string            `json:"version,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Message    string            `json:"message,omitempty"`
	Dependents []objectReference `json:"dependents,omitempty"`
	References []objectReference `json:"references,omitempty"`
}

// objectReference is one object a Provider status names.
type objectReference struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// versionsValue is the version discovery of C-2.
type versionsValue struct {
	availability
	KOS     kosVersion     `json:"kos"`
	Cluster clusterVersion `json:"cluster"`
}

// kosVersion is the KOS side of the discovery: the served API versions, the
// storage version and the framework versions this kernel accepts.
type kosVersion struct {
	APIVersions       []string `json:"apiVersions"`
	StorageVersion    string   `json:"storageVersion"`
	FrameworkVersions []string `json:"frameworkVersions"`
}

// clusterVersion is the cluster side of the discovery.
type clusterVersion struct {
	Nodes []nodeVersion `json:"nodes"`
}

// nodeVersion is one node's reported version.
type nodeVersion struct {
	Name           string `json:"name"`
	KubeletVersion string `json:"kubeletVersion,omitempty"`
	OSImage        string `json:"osImage,omitempty"`
}

// compatibilityValue is the extension and framework compatibility item (C-3).
type compatibilityValue struct {
	availability
	FrameworkVersion    string               `json:"frameworkVersion"`
	SupportedFrameworks []string             `json:"supportedFrameworks"`
	Extensions          []compatibilityEntry `json:"extensions"`
}

// compatibilityEntry is one installed extension's declared framework range and
// the conclusion this control plane draws from it (C-3).
type compatibilityEntry struct {
	Available                    bool   `json:"available"`
	Reason                       string `json:"reason,omitempty"`
	Component                    string `json:"component"`
	Identifier                   string `json:"identifier,omitempty"`
	Version                      string `json:"version,omitempty"`
	TargetRange                  string `json:"targetRange,omitempty"`
	FrameworkVersion             string `json:"frameworkVersion"`
	Conclusion                   string `json:"conclusion"`
	ComponentPhase               string `json:"componentPhase,omitempty"`
	ComponentFrameworkCompatible string `json:"componentFrameworkCompatible,omitempty"`
}

package updater

import (
	"context"
	"sort"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/pages/cluster"
)

// Readings resolves the items the component and update pages declare (C-53).
// Each read stamps the moment it observed the value, so the shell applies C-13's
// one freshness bound.
type Readings struct {
	// Dynamic reads the cluster objects the page sources judge.
	Dynamic dynamic.Interface
	// Artifacts reads the package an installed component names; nil leaves the
	// compatibility item stating that it cannot be observed (C-7).
	Artifacts extension.ArtifactSource
	// Now supplies the read clock; defaults to time.Now.
	Now func() time.Time
}

// Read implements pages.Readings.
func (r *Readings) Read(ctx context.Context, session identity.Session, item extension.PageItem) (pages.Reading, *controlplane.Error) {
	value, failure := r.value(ctx, session, item)
	if failure != nil {
		return pages.Reading{}, failure
	}
	return pages.Reading{Value: value, ObservedAt: r.clock()}, nil
}

// value resolves one item from the source it declares. A source the control
// plane cannot observe keeps its item on the page and states the reason, rather
// than failing the page or blanking the item (C-7).
func (r *Readings) value(ctx context.Context, session identity.Session, item extension.PageItem) (any, *controlplane.Error) {
	var (
		value   any
		failure *controlplane.Error
	)
	switch item.ID {
	case ItemCapabilities:
		value, failure = r.capabilities(ctx)
	case ItemGaps:
		value, failure = r.gaps(ctx, session)
	case ItemInstallable:
		value, failure = r.installable(ctx)
	case ItemExtensionState:
		value, failure = r.extensionState(ctx)
	case ItemBlockedProviders:
		value, failure = r.blockedProviders(ctx)
	case ItemVersions:
		value, failure = r.versions(ctx)
	case ItemCompatibility:
		value, failure = r.compatibility(ctx)
	default:
		return nil, unknownItem(item.ID)
	}
	if failure != nil {
		return unavailableValue(failure.Message), nil
	}
	return value, nil
}

// clock is the moment a read observed the value (C-13).
func (r *Readings) clock() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// reader is the cluster read seam for this call.
func (r *Readings) reader() *cluster.Reader {
	return &cluster.Reader{Dynamic: r.Dynamic, Now: r.Now}
}

// availability marks whether a source could be observed and, when it could not,
// why the item is still on the page (C-7).
type availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// observed is the availability of an item whose source answered.
func observed() availability { return availability{Available: true} }

// unavailableValue is the value of an item whose source is not observable. The
// item keeps its place and states why it carries no current value (C-7).
func unavailableValue(reason string) map[string]any {
	return map[string]any{"available": false, "reason": reason}
}

// capabilitiesValue is the installed capability set (C-1).
type capabilitiesValue struct {
	availability
	Capabilities []capabilityEntry `json:"capabilities"`
}

// capabilityEntry is one registered capability with the verification result the
// Provider object carries (C-1).
type capabilityEntry struct {
	Provider    string   `json:"provider"`
	Capability  string   `json:"capability"`
	Version     string   `json:"version"`
	Scope       string   `json:"scope,omitempty"`
	Effective   bool     `json:"effective"`
	Source      string   `json:"source,omitempty"`
	State       string   `json:"state"`
	Reason      string   `json:"reason,omitempty"`
	FailedCases []string `json:"failedCases,omitempty"`
}

// gapsValue is the capability-requirement comparison (C-1).
type gapsValue struct {
	availability
	Rule         string     `json:"rule"`
	Definitions  []gapEntry `json:"definitions"`
	Applications []gapEntry `json:"applications"`
}

// gapEntry names one object and the requirements it does not meet.
type gapEntry struct {
	Namespace string        `json:"namespace,omitempty"`
	Name      string        `json:"name"`
	Gaps      []cluster.Gap `json:"gaps,omitempty"`
	Invalid   string        `json:"invalid,omitempty"`
}

// installableValue is the installable component set (C-1).
type installableValue struct {
	availability
	Rule        string             `json:"rule"`
	Definitions []installableEntry `json:"definitions"`
}

// installableEntry is one definition and whether its required capabilities are
// served (C-1).
type installableEntry struct {
	Name        string        `json:"name"`
	Version     string        `json:"version,omitempty"`
	Installable bool          `json:"installable"`
	Gaps        []cluster.Gap `json:"gaps,omitempty"`
	Invalid     string        `json:"invalid,omitempty"`
}

// providers lists the installed providers, ordered by name.
func (r *Readings) providers(ctx context.Context) ([]v1beta1.Provider, *controlplane.Error) {
	objects, failure := r.reader().List(ctx, "Provider", "")
	if failure != nil {
		return nil, failure
	}
	providers := make([]v1beta1.Provider, 0, len(objects))
	for _, object := range objects {
		var provider v1beta1.Provider
		if failure := cluster.Decode(object, &provider); failure != nil {
			return nil, failure
		}
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(left, right int) bool {
		return providers[left].Name < providers[right].Name
	})
	return providers, nil
}

// definitions lists the application definitions, ordered by name.
func (r *Readings) definitions(ctx context.Context) ([]v1beta1.ApplicationDefinition, *controlplane.Error) {
	objects, failure := r.reader().List(ctx, "ApplicationDefinition", "")
	if failure != nil {
		return nil, failure
	}
	definitions := make([]v1beta1.ApplicationDefinition, 0, len(objects))
	for _, object := range objects {
		var definition v1beta1.ApplicationDefinition
		if failure := cluster.Decode(object, &definition); failure != nil {
			return nil, failure
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Name < definitions[right].Name
	})
	return definitions, nil
}

// components lists the installed system components, ordered by name.
func (r *Readings) components(ctx context.Context) ([]v1beta1.SystemComponent, *controlplane.Error) {
	objects, failure := r.reader().List(ctx, "SystemComponent", "")
	if failure != nil {
		return nil, failure
	}
	components := make([]v1beta1.SystemComponent, 0, len(objects))
	for _, object := range objects {
		var component v1beta1.SystemComponent
		if failure := cluster.Decode(object, &component); failure != nil {
			return nil, failure
		}
		components = append(components, component)
	}
	sort.Slice(components, func(left, right int) bool {
		return components[left].Name < components[right].Name
	})
	return components, nil
}

// unknownItem marks an item this product does not declare.
func unknownItem(id string) *controlplane.Error {
	return &controlplane.Error{
		Code:        controlplane.CodeCapabilityMissing,
		Message:     "kos-updater 没有声明页面条目 " + id,
		Stage:       controlplane.StageCapability,
		SideEffects: controlplane.SideEffectsNone,
	}
}

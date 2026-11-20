// Package updatertc1 verifies that the two pages' display items do not overlap,
// that their union is the list the two contracts declare, that every item
// carries one source type, and that each value equals what its own source
// judges: object-source values equal the stored object fields and
// computed-source values equal the rule recomputed here (UPDATER-AC-1,
// UPDATER-TC-1).
package updatertc1

import (
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tsic404/kosd/pkg/extension"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

// componentSources are the source types C-1 names for the component page's
// items, read off the contract rather than off the implementation.
var componentSources = map[string]string{
	updater.ItemCapabilities:     extension.PageSourceObject,
	updater.ItemGaps:             extension.PageSourceComputed,
	updater.ItemInstallable:      extension.PageSourceComputed,
	updater.ItemExtensionState:   extension.PageSourceObject,
	updater.ItemBlockedProviders: extension.PageSourceObject,
}

// updateSources are the source types C-2 names for the update page's items.
var updateSources = map[string]string{
	updater.ItemVersions:        extension.PageSourceComputed,
	updater.ItemPreflight:       extension.PageSourceDatabase,
	updater.ItemBlockedUpgrades: extension.PageSourceComputed,
	updater.ItemCompatibility:   extension.PageSourceComputed,
}

// sourceTypes are C-53's seven source types; an item declares exactly one.
var sourceTypes = map[string]bool{
	extension.PageSourceObject:   true,
	extension.PageSourceComputed: true,
	extension.PageSourceMetric:   true,
	extension.PageSourceProbe:    true,
	extension.PageSourceRead:     true,
	extension.PageSourceLocal:    true,
	extension.PageSourceDatabase: true,
}

func TestTC1TheTwoPagesPartitionTheDeclaredItems(t *testing.T) {
	world := newWorld(t)
	shell := world.PageShell(t)
	components := fixtures.ItemsByID(t, fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageComponents))
	updates := fixtures.ItemsByID(t, fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageUpdates))

	// The intersection is empty and the union is the declared list (C-1, C-2).
	shared := make([]string, 0)
	for id := range components {
		if _, duplicated := updates[id]; duplicated {
			shared = append(shared, id)
		}
	}
	if len(shared) != 0 {
		sort.Strings(shared)
		t.Fatalf("both pages carry the items %v, want no item on both", shared)
	}
	union := make([]string, 0, len(components)+len(updates))
	for id := range components {
		union = append(union, id)
	}
	for id := range updates {
		union = append(union, id)
	}
	want := append(append([]string{}, updater.ComponentItems...), updater.UpdateItems...)
	sort.Strings(union)
	sort.Strings(want)
	if !equal(union, want) {
		t.Fatalf("the two pages carry %v, want the declared %v", union, want)
	}

	// Each item carries one source type, and the type is the one its contract
	// names for it (C-1, C-2).
	for id, item := range components {
		assertSource(t, id, item, componentSources[id])
	}
	for id, item := range updates {
		assertSource(t, id, item, updateSources[id])
	}
}

// assertSource checks that one projected item declares the expected source type
// and nothing else.
func assertSource(t *testing.T, id string, item map[string]any, want string) {
	t.Helper()
	source := fixtures.String(item, "source")
	if source == "" {
		t.Fatalf("item %q carries no source type: %v", id, item)
	}
	if !sourceTypes[source] {
		t.Fatalf("item %q declares source %q, which is not one of C-53's seven", id, source)
	}
	if source != want {
		t.Fatalf("item %q declares source %q, want the contract's %q", id, source, want)
	}
}

func TestTC1ObjectAndComputedValuesEqualTheirSources(t *testing.T) {
	world := newWorld(t)
	shell := world.PageShell(t)
	items := fixtures.ItemsByID(t, fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageComponents))

	// Object source: the installed capability set equals the Provider objects'
	// registered capabilities and their verification results (C-1).
	value := fixtures.ValueOf(t, items, updater.ItemCapabilities)
	entries := objectsOf(t, fixtures.ListOf(t, value, "capabilities"))
	if len(entries) != 2 {
		t.Fatalf("the capability item lists %d capabilities, want the two the providers registered: %v", len(entries), entries)
	}
	for name, capability := range map[string]fixtures.Capability{
		"storage.block": {Name: "storage.block", Version: "1", Scope: "node", State: "Verified"},
		"network.route": {Name: "network.route", Version: "2", Scope: "cluster", State: "Degraded"},
	} {
		entry := entries[name]
		if entry == nil {
			t.Fatalf("the capability item does not list %q: %v", name, entries)
		}
		if got := fixtures.String(entry, "version"); got != capability.Version {
			t.Fatalf("capability %q version = %q, want the registered %q", name, got, capability.Version)
		}
		if got := fixtures.String(entry, "state"); got != capability.State {
			t.Fatalf("capability %q state = %q, want the stored verification result %q", name, got, capability.State)
		}
		if got := fixtures.String(entry, "scope"); got != capability.Scope {
			t.Fatalf("capability %q scope = %q, want the registered %q", name, got, capability.Scope)
		}
	}

	// Computed source: the gap item equals the comparison of the declared
	// requirements against the effective capability set, recomputed here
	// (C-1, C-11).
	gaps := fixtures.ValueOf(t, items, updater.ItemGaps)
	definitionGaps := objectsOf(t, fixtures.ListOf(t, gaps, "definitions"))
	if got, want := sortedKeys(definitionGaps), []string{"shop"}; !equal(got, want) {
		t.Fatalf("definitions with a capability gap = %v, want %v", got, want)
	}
	gapList := fixtures.ListOf(t, definitionGaps["shop"], "gaps")
	if len(gapList) != 1 {
		t.Fatalf("the shop definition carries %d gaps, want the one missing capability: %v", len(gapList), gapList)
	}
	gap, ok := gapList[0].(map[string]any)
	if !ok {
		t.Fatalf("the gap entry is not an object: %v", gapList[0])
	}
	if got := fixtures.String(gap, "kind"); got != "missing" {
		t.Fatalf("the shop gap kind = %q, want missing", got)
	}
	if got := fixtures.String(gap, "requirement"); got != "storage.snapshot" {
		t.Fatalf("the shop gap names %q, want the unserved storage.snapshot", got)
	}

	// Computed source: the installable item judges each definition by its
	// required capabilities, so the same definition is not installable while
	// the served one is (C-1, C-11).
	installable := fixtures.ValueOf(t, items, updater.ItemInstallable)
	definitions := objectsOf(t, fixtures.ListOf(t, installable, "definitions"))
	if len(definitions) != 2 {
		t.Fatalf("the installable item lists %d definitions, want the two declared: %v", len(definitions), definitions)
	}
	if got := definitions["shop"]["installable"]; got != false {
		t.Fatalf("the definition whose requirement no provider serves is installable = %v, want false", got)
	}
	if got := definitions["cache"]["installable"]; got != true {
		t.Fatalf("the definition whose requirement is served is installable = %v, want true", got)
	}

	// Object source: the extension state equals the stored SystemComponent
	// status the kernel wrote, condition by condition (C-1, C-18, C-28).
	state := fixtures.ValueOf(t, items, updater.ItemExtensionState)
	components := objectsOf(t, fixtures.ListOf(t, state, "components"))
	for name := range components {
		stored := fixtures.ComponentOf(t, world.Cluster, name)
		if got, want := fixtures.String(components[name], "phase"), stored.Status.Phase; got != want {
			t.Fatalf("component %q phase = %q, want the stored %q", name, got, want)
		}
		if got, want := components[name]["disabled"], stored.Spec.Disable; got != want {
			t.Fatalf("component %q disabled = %v, want the stored %v", name, got, want)
		}
		presented := conditionsOf(t, fixtures.ListOf(t, components[name], "conditions"))
		if len(presented) != len(stored.Status.Conditions) {
			t.Fatalf("component %q presents %d conditions, want the stored %d: %v",
				name, len(presented), len(stored.Status.Conditions), presented)
		}
		for index, condition := range stored.Status.Conditions {
			entry := presented[index]
			if got := fixtures.String(entry, "type"); got != condition.Type {
				t.Fatalf("component %q condition %d type = %q, want the stored %q", name, index, got, condition.Type)
			}
			if got := fixtures.String(entry, "status"); got != string(condition.Status) {
				t.Fatalf("component %q condition %q status = %q, want the stored %q", name, condition.Type, got, condition.Status)
			}
			if got := fixtures.String(entry, "reason"); got != condition.Reason {
				t.Fatalf("component %q condition %q reason = %q, want the stored %q", name, condition.Type, got, condition.Reason)
			}
			if got := fixtures.String(entry, "message"); got != condition.Message {
				t.Fatalf("component %q condition %q message = %q, want the stored %q", name, condition.Type, got, condition.Message)
			}
		}
	}

	// One installed extension declares a component no install provides, so the
	// kernel wrote the unmet edge into its DependenciesMet condition; the page
	// must carry that condition and its dependency text, not only phase and
	// disabled (C-1, KOSD-C-18).
	dependencyMissing := fixtures.ComponentOf(t, world.Cluster, "needy-ext")
	unmet, found := storedConditionOf(dependencyMissing.Status.Conditions, extension.ConditionDependenciesMet)
	if !found {
		t.Fatalf("the kernel wrote no %s condition on the dependency-missing extension: %+v",
			extension.ConditionDependenciesMet, dependencyMissing.Status.Conditions)
	}
	if unmet.Status != metav1.ConditionFalse || unmet.Reason != extension.ReasonDependencyMissing {
		t.Fatalf("the dependency-missing condition = %s/%s, want %s/%s",
			unmet.Status, unmet.Reason, metav1.ConditionFalse, extension.ReasonDependencyMissing)
	}
	if !strings.Contains(unmet.Message, "absent-ext") {
		t.Fatalf("the dependency-missing message names no missing component: %q", unmet.Message)
	}

	page := components["needy-ext"]
	if page == nil {
		t.Fatalf("the extension state does not list the dependency-missing extension: %v", sortedKeys(components))
	}
	presentedConditions := conditionsOf(t, fixtures.ListOf(t, page, "conditions"))
	shown, found := presentedConditionOf(presentedConditions, extension.ConditionDependenciesMet)
	if !found {
		t.Fatalf("the page dropped the %s condition of the dependency-missing extension: %v",
			extension.ConditionDependenciesMet, presentedConditions)
	}
	if !strings.Contains(fixtures.String(shown, "message"), "absent-ext") {
		t.Fatalf("the page dropped the missing dependency text: %v", shown)
	}

	// Object source: the blocked Provider item equals the Provider object's
	// UpgradeBlocked condition and its dependents (C-1, C-48).
	blocked := fixtures.ValueOf(t, items, updater.ItemBlockedProviders)
	providers := objectsOf(t, fixtures.ListOf(t, blocked, "providers"))
	if len(providers) != 1 || providers["legacy.example"] == nil {
		t.Fatalf("the blocked item lists %v, want the one blocked provider", sortedKeys(providers))
	}
	if got, want := fixtures.String(providers["legacy.example"], "reason"), "WithdrawalBlocked"; got != want {
		t.Fatalf("blocked provider reason = %q, want the stored %q", got, want)
	}
	dependents := fixtures.ListOf(t, providers["legacy.example"], "dependents")
	if len(dependents) != 1 {
		t.Fatalf("the blocked provider carries %d dependents, want the one stored: %v", len(dependents), dependents)
	}
	dependent, ok := dependents[0].(map[string]any)
	if !ok {
		t.Fatalf("the dependent is not an object: %v", dependents[0])
	}
	if got := fixtures.String(dependent, "name"); got != "shop" {
		t.Fatalf("the dependent name = %q, want the stored shop", got)
	}
}

// newWorld builds the cluster UPDATER-TC-1 describes: installed capabilities, a
// capability gap, installable components, a blocked Provider and an extension
// with a missing dependency.
func newWorld(t *testing.T) *fixtures.World {
	t.Helper()
	world := fixtures.NewWorld(t)
	world.Install(t,
		fixtures.Product(t),
		fixtures.Extension(t, "compatible-ext", ">=1.0 <2.0"),
		fixtures.ExtensionRequiring(t, "needy-ext", ">=1.0 <2.0", "absent-ext"),
	)
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	world.Provider(t, "standard.example",
		fixtures.Capability{Name: "storage.block", Version: "1", Scope: "node", State: "Verified"},
		fixtures.Capability{Name: "network.route", Version: "2", Scope: "cluster", State: "Degraded"},
	)
	// One definition whose requirement is served and one whose requirement is
	// not, so the computed items have both answers to report (C-11).
	world.ApplicationDefinition(t, "cache", "1.0.0", fixtures.Requirement{Name: "storage.block", Version: ">=1"})
	world.ApplicationDefinition(t, "shop", "1.0.0", fixtures.Requirement{Name: "storage.snapshot", Version: ">=1"})
	// One Provider object whose UpgradeBlocked condition names a dependent
	// (C-48).
	world.BlockedProvider(t, "legacy.example", "1.2.0", "WithdrawalBlocked")
	return world
}

// conditionsOf indexes one projected condition list by the order the object
// stores it, so a case can compare it with the stored conditions position by
// position.
func conditionsOf(t *testing.T, entries []any) []map[string]any {
	t.Helper()
	conditions := make([]map[string]any, 0, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("condition %d is not an object: %v", index, raw)
		}
		conditions = append(conditions, entry)
	}
	return conditions
}

// presentedConditionOf finds one projected condition by type.
func presentedConditionOf(conditions []map[string]any, conditionType string) (map[string]any, bool) {
	for _, condition := range conditions {
		if fixtures.String(condition, "type") == conditionType {
			return condition, true
		}
	}
	return nil, false
}

// storedConditionOf finds one condition on a stored object by type.
func storedConditionOf(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

// objectsOf indexes one projected object list by a key field.
func objectsOf(t *testing.T, entries []any) map[string]map[string]any {
	t.Helper()
	objects := make(map[string]map[string]any, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("entry %d is not an object: %v", index, raw)
		}
		key := fixtures.String(entry, "name")
		if key == "" {
			key = fixtures.String(entry, "capability")
		}
		objects[key] = entry
	}
	return objects
}

// sortedKeys returns a map's keys in order.
func sortedKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// equal compares two sorted string sets.
func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

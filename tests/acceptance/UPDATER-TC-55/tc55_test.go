// Package updatertc55 verifies that the update page reports each installed
// extension's target framework range, the current framework version and the
// conclusion drawn from the two, and that the conclusion agrees with the
// component state the kernel wrote (UPDATER-AC-28, UPDATER-TC-55).
package updatertc55

import (
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/pages"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

// The two range classes the case installs: one admits the served framework
// version, the other admits none of it.
const (
	compatibleRange   = ">=1.0 <2.0"
	incompatibleRange = ">=2.0 <3.0"
)

func TestTC55ConclusionAgreesWithTheComponentState(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t,
		fixtures.Product(t),
		fixtures.Extension(t, "compatible-ext", compatibleRange),
		fixtures.Extension(t, "incompatible-ext", incompatibleRange),
	)
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	shell := world.PageShell(t)
	page := fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageUpdates)
	items := fixtures.ItemsByID(t, page)
	if len(items) != len(updater.UpdateItems) {
		t.Fatalf("update page has %d items, want the %d UPDATER-C-2 declares: %v",
			len(items), len(updater.UpdateItems), items)
	}

	value := fixtures.ValueOf(t, items, updater.ItemCompatibility)
	if got := fixtures.String(value, "frameworkVersion"); got != extension.FrameworkVersion {
		t.Fatalf("framework version = %q, want the served %q", got, extension.FrameworkVersion)
	}
	supported := fixtures.ListOf(t, value, "supportedFrameworks")
	if len(supported) != 1 || supported[0] != extension.FrameworkVersion {
		t.Fatalf("supported frameworks = %v, want the served %q", supported, extension.FrameworkVersion)
	}

	entries := entriesByComponent(t, fixtures.ListOf(t, value, "extensions"))

	// Every installed extension that declares a package is judged, and no
	// other entry is invented.
	want := []string{"compatible-ext", "incompatible-ext", "kos-updater"}
	if got := sortedKeys(entries); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("judged extensions = %v, want %v", got, want)
	}

	// The range admits the served version: the entry reports compatibly and does
	// not change the component state (C-3).
	compatible := entries["compatible-ext"]
	assertEntry(t, compatible, "compatible-ext", compatibleRange, updater.ConclusionCompatible)
	if reason := fixtures.String(compatible, "reason"); reason != "" {
		t.Fatalf("the compatible extension carries the failure reason %q", reason)
	}
	if got := fixtures.String(compatible, "componentFrameworkCompatible"); got != "True" {
		t.Fatalf("compatible component FrameworkCompatible = %q, want True", got)
	}

	// The range admits no served version: the entry is incompatible, names the
	// extension and its range, and the component state is incompatible (C-3).
	incompatible := entries["incompatible-ext"]
	assertEntry(t, incompatible, "incompatible-ext", incompatibleRange, updater.ConclusionIncompatible)
	if reason := fixtures.String(incompatible, "reason"); !strings.Contains(reason, incompatibleRange) {
		t.Fatalf("the incompatible reason %q does not name its range %q", reason, incompatibleRange)
	}
	if got := fixtures.String(incompatible, "componentFrameworkCompatible"); got != "False" {
		t.Fatalf("incompatible component FrameworkCompatible = %q, want False", got)
	}

	// The page and the stored component objects agree, judged on the objects and
	// the package declarations rather than on the page's own claims (C-3).
	states := componentStates(t, shell)
	for name, entry := range entries {
		component := fixtures.ComponentOf(t, world.Cluster, name)
		if got, want := fixtures.String(entry, "componentPhase"), component.Status.Phase; got != want {
			t.Fatalf("extension %q component phase = %q, want the stored %q", name, got, want)
		}
		if got, want := fixtures.String(states[name], "phase"), component.Status.Phase; got != want {
			t.Fatalf("component page phase for %q = %q, want the stored %q", name, got, want)
		}
		condition, found := conditionOf(component.Status.Conditions, extension.ConditionFrameworkCompatible)
		if !found {
			t.Fatalf("component %q carries no FrameworkCompatible condition", name)
		}
		if got, want := fixtures.String(entry, "componentFrameworkCompatible"), string(condition.Status); got != want {
			t.Fatalf("extension %q condition = %q, want the stored %q", name, got, want)
		}

		declared := fixtures.DeclaredRange(t, world, name)
		if got := fixtures.String(entry, "targetRange"); got != declared {
			t.Fatalf("extension %q target range = %q, want the packaged %q", name, got, declared)
		}
		constraint, err := extension.ParseConstraint(strings.TrimSpace(declared))
		if err != nil {
			t.Fatalf("extension %q range %q does not parse: %v", name, declared, err)
		}
		wantConclusion := updater.ConclusionIncompatible
		if constraint.Allows(extension.FrameworkVersion) {
			wantConclusion = updater.ConclusionCompatible
		}
		if got := fixtures.String(entry, "conclusion"); got != wantConclusion {
			t.Fatalf("extension %q conclusion = %q, want %q for range %q against %q",
				name, got, wantConclusion, declared, extension.FrameworkVersion)
		}
		if wantConclusion == updater.ConclusionIncompatible && component.Status.Phase != extension.PhaseIncompatible {
			t.Fatalf("extension %q is judged incompatible but its component phase is %q", name, component.Status.Phase)
		}
		if wantConclusion == updater.ConclusionCompatible && component.Status.Phase == extension.PhaseIncompatible {
			t.Fatalf("extension %q is judged compatible but its component phase is %q", name, component.Status.Phase)
		}
	}
}

// TestTC55IncompatibleEntrySurvivesAnUnreadablePackage keeps the entry for an
// extension whose package cannot be read: the item does not invent a range or a
// conclusion, and the component state is still reported (C-3, C-7).
func TestTC55IncompatibleEntrySurvivesAnUnreadablePackage(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t), fixtures.Extension(t, "compatible-ext", compatibleRange))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// One package disappears from the artifact store after the install.
	world.Artifacts = extension.MemoryArtifacts{}

	shell := world.PageShell(t)
	page := fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageUpdates)
	value := fixtures.ValueOf(t, fixtures.ItemsByID(t, page), updater.ItemCompatibility)
	entries := entriesByComponent(t, fixtures.ListOf(t, value, "extensions"))
	entry, found := entries["compatible-ext"]
	if !found {
		t.Fatalf("the extension whose package is unreadable is missing from the item: %v", entries)
	}
	if entry["available"] != false {
		t.Fatalf("the unreadable extension reports available = %v, want false", entry["available"])
	}
	if reason := fixtures.String(entry, "reason"); reason == "" {
		t.Fatal("the unreadable extension carries no reason")
	}
	if got := fixtures.String(entry, "conclusion"); got != updater.ConclusionUnknown {
		t.Fatalf("the unreadable extension conclusion = %q, want %q", got, updater.ConclusionUnknown)
	}
	if got := fixtures.String(entry, "targetRange"); got != "" {
		t.Fatalf("the unreadable extension reports the invented range %q", got)
	}
	if got := fixtures.String(entry, "componentPhase"); got != extension.PhaseInstalled {
		t.Fatalf("the unreadable extension component phase = %q, want %q", got, extension.PhaseInstalled)
	}
}

// assertEntry checks the fields C-3 requires of one extension entry.
func assertEntry(t *testing.T, entry map[string]any, identifier, targetRange, conclusion string) {
	t.Helper()
	if got := fixtures.String(entry, "identifier"); got != identifier {
		t.Fatalf("entry identifier = %q, want %q", got, identifier)
	}
	if got := fixtures.String(entry, "targetRange"); got != targetRange {
		t.Fatalf("entry %q target range = %q, want %q", identifier, got, targetRange)
	}
	if got := fixtures.String(entry, "frameworkVersion"); got != extension.FrameworkVersion {
		t.Fatalf("entry %q framework version = %q, want %q", identifier, got, extension.FrameworkVersion)
	}
	if got := fixtures.String(entry, "conclusion"); got != conclusion {
		t.Fatalf("entry %q conclusion = %q, want %q", identifier, got, conclusion)
	}
}

// entriesByComponent indexes the compatibility entries by component name.
func entriesByComponent(t *testing.T, entries []any) map[string]map[string]any {
	t.Helper()
	byComponent := make(map[string]map[string]any, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("compatibility entry %d is not an object: %v", index, raw)
		}
		component := fixtures.String(entry, "component")
		if component == "" {
			t.Fatalf("compatibility entry %d names no component: %v", index, entry)
		}
		if _, duplicated := byComponent[component]; duplicated {
			t.Fatalf("component %q is judged twice", component)
		}
		byComponent[component] = entry
	}
	return byComponent
}

// componentStates indexes the component page's extension-state entries by name.
func componentStates(t *testing.T, shell *pages.Service) map[string]map[string]any {
	t.Helper()
	page := fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageComponents)
	value := fixtures.ValueOf(t, fixtures.ItemsByID(t, page), updater.ItemExtensionState)
	states := make(map[string]map[string]any)
	for index, raw := range fixtures.ListOf(t, value, "components") {
		state, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("extension state %d is not an object: %v", index, raw)
		}
		states[fixtures.String(state, "name")] = state
	}
	return states
}

// sortedKeys returns the map keys in order.
func sortedKeys(entries map[string]map[string]any) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

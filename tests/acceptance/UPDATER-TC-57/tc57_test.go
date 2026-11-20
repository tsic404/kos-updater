// Package updatertc57 verifies the update page's blocked-upgrade item: the
// extensions it lists are the installed ones whose declared target range only
// the removed framework API versions satisfy, the stored record names the same
// set, and the page offers no entry that would let the blocked upgrade go on
// (UPDATER-AC-29, UPDATER-TC-57).
package updatertc57

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/upgrade"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

const (
	// strandedRange admits the served framework version and no target one, so
	// the extension depends on a version the platform upgrade removes (C-16).
	strandedRange = ">=1.0 <2.0"
	// survivingRange admits the target version as well, so the upgrade does not
	// strand the extension.
	survivingRange = ">=1.0 <3.0"
	// targetFramework is the framework API version set the upgrade moves to.
	targetFramework = "2.0"
)

func TestTC57BlockedUpgradeListsTheDeclaredDependents(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t,
		fixtures.Product(t),
		fixtures.Extension(t, "legacy-ext", strandedRange),
		fixtures.Extension(t, "surviving-ext", survivingRange),
	)
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// The preflight runs before the page is read: UPDATER-TC-57's scenario does
	// not skip this step.
	record := world.Preflight(t, upgrade.PreflightRequest{
		Kind:             upgrade.KindPlatform,
		TargetVersion:    "v1.38.0",
		Downtime:         "15m",
		Nodes:            []string{"node-a"},
		FrameworkTargets: []string{targetFramework},
	})

	// The expected set is derived from the installed declarations, not from the
	// record or the page, so a preflight that computed the wrong dependents
	// fails here (C-16).
	expected := strandedExtensions(t, world)
	if !contains(expected, "legacy-ext") {
		t.Fatalf("the case built no stranded extension: %v", expected)
	}
	if contains(expected, "surviving-ext") {
		t.Fatalf("the extension whose range admits the target version is stranded: %v", expected)
	}

	page := fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates)
	items := fixtures.ItemsByID(t, page)
	value := fixtures.ValueOf(t, items, updater.ItemBlockedUpgrades)
	if value["available"] != true {
		t.Fatalf("the blocked item is unavailable after a preflight: %v", value)
	}
	if value["blocked"] != true {
		t.Fatalf("the blocked item does not report the upgrade as blocked: %v", value)
	}
	if got, want := stringsOf(t, fixtures.ListOf(t, value, "removedFrameworkVersions")), removedFrameworks(targetFramework); !equal(got, want) {
		t.Fatalf("presented removed framework versions = %v, want the served set minus the target %v", got, want)
	}
	presented := componentsOf(t, fixtures.ListOf(t, value, "dependents"))
	if got := sortedKeys(presented); !equal(got, expected) {
		t.Fatalf("presented dependents = %v, want the extensions declaring the removed version %v", got, expected)
	}
	for component, entry := range presented {
		declared := fixtures.DeclaredRange(t, world, component)
		if got := fixtures.String(entry, "frameworkVersion"); got != declared {
			t.Fatalf("dependent %q range = %q, want the packaged %q", component, got, declared)
		}
		if got := fixtures.String(entry, "identifier"); got == "" {
			t.Fatalf("dependent %q carries no extension identifier: %v", component, entry)
		}
	}

	// The record and the page agree on the same set (UPDATER-TC-57's second
	// criterion).
	recorded := make(map[string]map[string]any, len(record.Result.FrameworkDependents))
	for _, dependent := range record.Result.FrameworkDependents {
		recorded[dependent.Component] = map[string]any{"identifier": dependent.Identifier}
	}
	if got := sortedKeys(recorded); !equal(got, sortedKeys(presented)) {
		t.Fatalf("record dependents = %v, page dependents = %v", got, sortedKeys(presented))
	}

	// No entry lets the blocked upgrade continue (C-5): the only entries the
	// page offers are the control-plane preflight and upgrade writes.
	assertNoContinueEntry(t, page)
}

// strandedExtensions derives the installed components whose declared range only
// the removed framework API versions satisfy (C-16), reading each declaration
// from the installed package rather than from the record.
func strandedExtensions(t *testing.T, world *fixtures.World) []string {
	t.Helper()
	removed := removedFrameworks(targetFramework)
	stranded := make([]string, 0)
	for _, component := range world.Components(t) {
		declared := fixtures.DeclaredRange(t, world, component)
		if dependsOnRemoved(declared, removed, []string{targetFramework}) {
			stranded = append(stranded, component)
		}
	}
	sort.Strings(stranded)
	return stranded
}

// removedFrameworks lists the served framework API versions the target set
// stops serving.
func removedFrameworks(target string) []string {
	removed := make([]string, 0)
	for _, served := range extension.SupportedFrameworks() {
		if extension.SameFrameworkVersion(served, target) {
			continue
		}
		removed = append(removed, served)
	}
	sort.Strings(removed)
	return removed
}

// dependsOnRemoved applies C-16 to one declared range: it strands the upgrade
// when it admits a removed version and no target version.
func dependsOnRemoved(declared string, removed, target []string) bool {
	trimmed := strings.TrimSpace(declared)
	if trimmed == "" {
		return false
	}
	constraint, err := extension.ParseConstraint(trimmed)
	if err != nil {
		// An unreadable range cannot prove the extension survives the upgrade.
		return true
	}
	admitsRemoved := false
	for _, version := range removed {
		if constraint.Allows(version) {
			admitsRemoved = true
			break
		}
	}
	if !admitsRemoved {
		return false
	}
	for _, version := range target {
		if constraint.Allows(version) {
			return false
		}
	}
	return true
}

// assertNoContinueEntry checks that the page offers no way past the block: the
// only entries are C-6's two control-plane writes, and no item carries an entry
// of its own (C-5, C-6).
func assertNoContinueEntry(t *testing.T, page pages.Page) {
	t.Helper()
	want := map[string]string{
		updater.ActionPreflight: "/api/v1/upgrade-preflights",
		updater.ActionUpgrade:   "/api/v1/upgrade-requests",
	}
	if len(page.Actions) != len(want) {
		t.Fatalf("update page offers %d actions, want only the two C-6 writes: %v", len(page.Actions), page.Actions)
	}
	for _, action := range page.Actions {
		path, known := want[action.ID]
		if !known {
			t.Fatalf("update page offers action %q, which is not one of C-6's two writes", action.ID)
		}
		if action.Entry.Path != path {
			t.Fatalf("action %q writes %q, want %q", action.ID, action.Entry.Path, path)
		}
		if action.Entry.Method != http.MethodPost {
			t.Fatalf("action %q method = %q, want POST", action.ID, action.Entry.Method)
		}
	}
	for _, item := range page.Items {
		if item.Entry != nil {
			t.Fatalf("item %q carries its own entry %+v, so the page offers an entry beyond C-6's two writes", item.ID, item.Entry)
		}
	}
}

// componentsOf indexes the presented dependents by component.
func componentsOf(t *testing.T, entries []any) map[string]map[string]any {
	t.Helper()
	components := make(map[string]map[string]any, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("dependent %d is not an object: %v", index, raw)
		}
		components[fixtures.String(entry, "component")] = entry
	}
	return components
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

// stringsOf renders one projected list of strings, sorted.
func stringsOf(t *testing.T, entries []any) []string {
	t.Helper()
	values := make([]string, 0, len(entries))
	for index, raw := range entries {
		value, ok := raw.(string)
		if !ok {
			t.Fatalf("list entry %d is not a string: %v", index, raw)
		}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

// contains reports whether one value is in the set.
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
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

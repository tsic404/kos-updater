// Package updatertc56 verifies that a display item whose source went stale and
// an item whose source cannot be observed both stay on the page: the first keeps
// its value and carries the stale marker, the second states that it is
// unavailable and why, and neither is omitted or left blank (UPDATER-AC-1,
// UPDATER-TC-56).
package updatertc56

import (
	"testing"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

func TestTC56StaleSourceKeepsItsItem(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Provider(t, "standard.example", fixtures.Capability{
		Name: "storage.block", Version: "1", Scope: "node", State: "Verified",
	})
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// A read inside the freshness bound carries no marker, so the marker below
	// judges the observation time rather than being always on (C-13).
	fresh := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageComponents))
	if fresh[updater.ItemCapabilities]["stale"] != false {
		t.Fatalf("a fresh read is marked stale: %v", fresh[updater.ItemCapabilities])
	}

	// The source was observed one minute beyond the shared threshold.
	world.ReadNow = func() time.Time {
		return fixtures.Clock.Add(-(controlplane.StaleThreshold + time.Minute))
	}
	page := fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageComponents)
	items := fixtures.ItemsByID(t, page)

	// The item is not omitted: the page still carries every declared item.
	if len(items) != len(updater.ComponentItems) {
		t.Fatalf("component page has %d items, want the %d UPDATER-C-1 declares: %v",
			len(items), len(updater.ComponentItems), items)
	}
	stale, found := items[updater.ItemCapabilities]
	if !found {
		t.Fatal("the stale item is missing from the page")
	}
	if stale["stale"] != true {
		t.Fatalf("the item past the freshness bound is not marked stale: %v", stale)
	}
	if stale["observedAt"] == nil {
		t.Fatalf("the stale item carries no observation time: %v", stale)
	}

	// The item is not blank: it keeps the value the source judged.
	value := fixtures.ValueOf(t, items, updater.ItemCapabilities)
	if value["available"] != true {
		t.Fatalf("the stale item reports available = %v, want the value it observed", value["available"])
	}
	capabilities := fixtures.ListOf(t, value, "capabilities")
	if len(capabilities) != 1 {
		t.Fatalf("the stale item carries %d capabilities, want the one provider registered", len(capabilities))
	}
	entry, ok := capabilities[0].(map[string]any)
	if !ok {
		t.Fatalf("the stale capability entry is not an object: %v", capabilities[0])
	}
	if got := fixtures.String(entry, "capability"); got != "storage.block" {
		t.Fatalf("stale capability = %q, want the registered name", got)
	}
	if got := fixtures.String(entry, "state"); got != "Verified" {
		t.Fatalf("stale capability state = %q, want the verification result", got)
	}
}

func TestTC56UnobservableSourceKeepsItsItem(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t), fixtures.Extension(t, "compatible-ext", ">=1.0 <2.0"))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// The distribution exposes no extension package source, so the framework
	// range of the installed extensions cannot be observed.
	world.Artifacts = nil

	page := fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates)
	items := fixtures.ItemsByID(t, page)

	// The unavailable item is not omitted: the page still carries every
	// declared item.
	if len(items) != len(updater.UpdateItems) {
		t.Fatalf("update page has %d items, want the %d UPDATER-C-2 declares: %v",
			len(items), len(updater.UpdateItems), items)
	}
	value := fixtures.ValueOf(t, items, updater.ItemCompatibility)
	if value["available"] != false {
		t.Fatalf("the unobservable item reports available = %v, want false", value["available"])
	}
	if reason := fixtures.String(value, "reason"); reason == "" {
		t.Fatalf("the unavailable item carries no reason: %v", value)
	}

	// The other item keeps its own value: one unobservable source does not blank
	// the page (C-7).
	versions := fixtures.ValueOf(t, items, updater.ItemVersions)
	if versions["available"] != true {
		t.Fatalf("the version item reports available = %v, want the read value", versions["available"])
	}
	kos, ok := versions["kos"].(map[string]any)
	if !ok {
		t.Fatalf("the version item carries no KOS block: %v", versions)
	}
	if got := fixtures.String(kos, "storageVersion"); got == "" {
		t.Fatalf("the version item carries no storage version: %v", kos)
	}
	cluster, ok := versions["cluster"].(map[string]any)
	if !ok {
		t.Fatalf("the version item carries no cluster block: %v", versions)
	}
	if nodes := fixtures.ListOf(t, cluster, "nodes"); len(nodes) != 1 {
		t.Fatalf("the version item lists %d nodes, want the one node", len(nodes))
	}
}

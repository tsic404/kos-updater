// Package updatertc58 verifies the two pages' entries by role: the update page
// offers the Owner exactly the control-plane preflight write and the upgrade
// write, a Member or Guest session sees neither, the component page offers no
// modification entry, and neither page offers a force-eviction entry
// (UPDATER-AC-30, UPDATER-TC-58).
package updatertc58

import (
	"sort"
	"strings"
	"testing"

	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/upgrade"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

// ownerEntries are the control-plane writes C-6 gives the Owner alone.
var ownerEntries = map[string]string{
	updater.ActionPreflight: "/api/v1/upgrade-preflights",
	updater.ActionUpgrade:   "/api/v1/upgrade-requests",
}

func TestTC58OwnerSeesTheTwoWrites(t *testing.T) {
	world := newWorld(t)
	page := fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates)

	if page.ReadOnly {
		t.Fatalf("the update page is read-only, so it offers no entry at all: %+v", page)
	}
	if got, want := sortedIDs(page), []string{updater.ActionPreflight, updater.ActionUpgrade}; !equal(got, want) {
		t.Fatalf("the Owner sees entries %v, want C-6's two %v", got, want)
	}
	for _, action := range page.Actions {
		if path := ownerEntries[action.ID]; action.Entry.Path != path {
			t.Fatalf("entry %q writes %q, want the control-plane path %q", action.ID, action.Entry.Path, path)
		}
	}
	// No item carries an entry of its own: C-6 gives the two writes to the page,
	// and an item-level entry would be a third one the contract does not name.
	items := fixtures.ItemsByID(t, page)
	if len(items) != len(updater.UpdateItems) {
		t.Fatalf("the update page carries %d items, want %d: %v", len(items), len(updater.UpdateItems), items)
	}
	for _, item := range page.Items {
		if item.Entry != nil {
			t.Fatalf("item %q carries its own entry %+v, want the page-level entries C-6 gives", item.ID, item.Entry)
		}
	}
}

func TestTC58MemberAndGuestSeeNoEntry(t *testing.T) {
	world := newWorld(t)
	shell := world.PageShell(t)
	for name, session := range map[string]identity.Session{"Member": fixtures.Member, "Guest": fixtures.Guest} {
		page := fixtures.ReadPage(t, shell, session, updater.PageUpdates)
		if len(page.Actions) != 0 {
			t.Fatalf("%s sees %d entries on the update page, want none: %v", name, len(page.Actions), page.Actions)
		}
		// The items stay, so the role reads the same page without the writes.
		if len(page.Items) != len(updater.UpdateItems) {
			t.Fatalf("%s sees %d items, want the %d the Owner sees: %v", name, len(page.Items), len(updater.UpdateItems), page.Items)
		}
		for _, item := range page.Items {
			if item.Entry != nil {
				t.Fatalf("%s sees item %q with entry %+v, want no entry", name, item.ID, item.Entry)
			}
		}
	}
}

func TestTC58ComponentPageIsReadOnly(t *testing.T) {
	world := newWorld(t)
	page := fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageComponents)

	if !page.ReadOnly {
		t.Fatalf("the component page is not read-only: %+v", page)
	}
	if len(page.Actions) != 0 {
		t.Fatalf("the read-only component page offers %d entries: %v", len(page.Actions), page.Actions)
	}
	if strings.TrimSpace(page.ReadOnlyReason) == "" {
		t.Fatalf("the read-only component page states no reason: %+v", page)
	}
	if len(page.Items) != len(updater.ComponentItems) {
		t.Fatalf("the component page carries %d items, want %d: %v", len(page.Items), len(updater.ComponentItems), page.Items)
	}
	for _, item := range page.Items {
		if item.Entry != nil {
			t.Fatalf("the component page item %q carries a modification entry: %+v", item.ID, item.Entry)
		}
		if item.Action != "" {
			t.Fatalf("the component page item %q names action %q, want none", item.ID, item.Action)
		}
	}
}

func TestTC58NoForceEvictionEntry(t *testing.T) {
	world := newWorld(t)
	// A node-local application the platform upgrade would have to take out: the
	// page reports the outage but offers no entry to force the eviction (C-6).
	world.Platform = fixtures.ReportPlatform(upgrade.Report{Nodes: []upgrade.NodePlan{{
		Node:         "node-a",
		Applications: []string{"team/shop"},
		Volumes:      []string{"team/shop"},
	}}})
	record := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "10m",
		Nodes:         []string{"node-a"},
	})
	if len(record.Result.AffectedApplications) == 0 {
		t.Fatal("the preflight recorded no affected application, so the case would prove nothing")
	}

	shell := world.PageShell(t)
	for _, read := range []struct {
		page    string
		session identity.Session
	}{
		{updater.PageUpdates, fixtures.Owner},
		{updater.PageComponents, fixtures.Owner},
	} {
		page := fixtures.ReadPage(t, shell, read.session, read.page)
		for _, action := range page.Actions {
			if _, known := ownerEntries[action.ID]; !known {
				t.Fatalf("page %q offers entry %q, which C-6 does not give", read.page, action.ID)
			}
		}
		assertNoForceEntry(t, read.page, page)
	}

	// The outage itself is still reported: the page states what the upgrade
	// takes out without offering a way to force it (C-4, C-6).
	value := fixtures.ValueOf(t,
		fixtures.ItemsByID(t, fixtures.ReadPage(t, shell, fixtures.Owner, updater.PageUpdates)),
		updater.ItemPreflight)
	if got, want := stringsOf(t, fixtures.ListOf(t, value, "affectedApplications")), record.Result.AffectedApplications; !equal(got, want) {
		t.Fatalf("presented affected applications = %v, want the record's %v", got, want)
	}
}

// assertNoForceEntry scans every projected entry and item for a force or
// eviction path, so an entry added under another identity still fails.
func assertNoForceEntry(t *testing.T, pageID string, page pages.Page) {
	t.Helper()
	banned := []string{"force", "evict", "驱逐", "强制"}
	offends := func(values ...string) bool {
		for _, value := range values {
			for _, word := range banned {
				if strings.Contains(strings.ToLower(value), word) {
					return true
				}
			}
		}
		return false
	}
	for _, action := range page.Actions {
		if offends(action.ID, action.Entry.Path) {
			t.Fatalf("page %q offers entry %q at %q, which force-evicts a node-local application", pageID, action.ID, action.Entry.Path)
		}
	}
	for _, item := range page.Items {
		if item.Entry == nil {
			continue
		}
		if offends(item.Action, item.Entry.Path) {
			t.Fatalf("page %q item %q carries a force-eviction entry at %q", pageID, item.ID, item.Entry.Path)
		}
	}
}

// newWorld installs the product beside one extension that declares the served
// framework range, so both pages render every declared item.
func newWorld(t *testing.T) *fixtures.World {
	t.Helper()
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t), fixtures.Extension(t, "compatible-ext", ">=1.0 <2.0"))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	return world
}

// sortedIDs renders one page's entry identities in order.
func sortedIDs(page pages.Page) []string {
	ids := make([]string, 0, len(page.Actions))
	for _, action := range page.Actions {
		ids = append(ids, action.ID)
	}
	sort.Strings(ids)
	return ids
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

// Package updatertc54 verifies the update page's preflight item in the two
// states C-4 fixes: before any preflight the preflight and blocked items stay
// on the page with an unavailable value and a reason, and after a preflight the
// presented affected applications, downtime and extension contribution
// difference equal what the stored record returned (UPDATER-AC-27,
// UPDATER-TC-54).
package updatertc54

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/upgrade"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

func TestTC54RecordAgeDrivesTheStaleMarker(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	record := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "20m",
		Nodes:         []string{"node-a"},
	})
	if record.ObservedAt.IsZero() {
		t.Fatal("the stored record carries no observation time, so the case would prove nothing")
	}

	// Inside the freshness bound the two record-backed items carry the record's
	// own observation time and no marker (C-4, C-7, KOSD-C-13).
	fresh := fixtures.Clock.Add(controlplane.StaleThreshold)
	world.ReadNow = func() time.Time { return fresh }
	world.ShellNow = func() time.Time { return fresh }
	inside := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates))
	for _, id := range []string{updater.ItemPreflight, updater.ItemBlockedUpgrades} {
		item := inside[id]
		if item["stale"] != false {
			t.Fatalf("item %q inside the freshness bound is marked stale: %v", id, item)
		}
		if got := observedAtOf(t, item); !got.Equal(record.ObservedAt) {
			t.Fatalf("item %q observed at %s, want the record's %s", id, got, record.ObservedAt)
		}
	}

	// One second past the bound the same two items stay on the page, keep the
	// record's observation time and carry the marker; a read that stamped its own
	// clock would move the observation time forward and never mark them (C-7).
	stale := fixtures.Clock.Add(controlplane.StaleThreshold + time.Second)
	world.ReadNow = func() time.Time { return stale }
	world.ShellNow = func() time.Time { return stale }
	after := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates))
	if len(after) != len(updater.UpdateItems) {
		t.Fatalf("update page has %d items, want the %d UPDATER-C-2 declares: %v",
			len(after), len(updater.UpdateItems), after)
	}
	for _, id := range []string{updater.ItemPreflight, updater.ItemBlockedUpgrades} {
		item := after[id]
		if item["stale"] != true {
			t.Fatalf("item %q past the freshness bound is not marked stale: %v", id, item)
		}
		if got := observedAtOf(t, item); !got.Equal(record.ObservedAt) {
			t.Fatalf("item %q observed at %s, want the record's %s", id, got, record.ObservedAt)
		}
		if value := fixtures.ValueOf(t, after, id); value["available"] != true {
			t.Fatalf("item %q dropped its value when it went stale: %v", id, value)
		}
	}
}

// TestTC54OnePageReadUsesOneRecord covers the race a second preflight opens: the
// preflight and blocked items of one page read must present the same record, so
// a write landing between their reads cannot make one page show two upgrade
// plans (UPDATER-C-4, UPDATER-C-5).
func TestTC54OnePageReadUsesOneRecord(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	first := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "20m",
		Nodes:         []string{"node-a"},
	})
	second := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.39.0",
		Downtime:      "30m",
		Nodes:         []string{"node-a"},
	})
	if first.ID == second.ID {
		t.Fatal("the two preflights stored one record, so the case would prove nothing")
	}

	// The source hands out the older record first and the newer one on any
	// further read: that is the interleaving a write between the two item reads
	// produces. One page read must ask once (C-4, C-5).
	source := &fixtures.AdvancingRecords{Served: []upgrade.Record{first, second}}
	world.RecordSource = source
	page := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates))

	if source.Calls != 1 {
		t.Fatalf("one page read asked the record source %d times, want one shared snapshot", source.Calls)
	}
	preflight := fixtures.ValueOf(t, page, updater.ItemPreflight)
	blocked := fixtures.ValueOf(t, page, updater.ItemBlockedUpgrades)
	if got, want := fixtures.String(preflight, "recordId"), first.ID; got != want {
		t.Fatalf("the preflight item presents record %q, want the first read's %q", got, want)
	}
	if got, want := fixtures.String(blocked, "recordId"), first.ID; got != want {
		t.Fatalf("the blocked item presents record %q, want the same %q as the preflight item", got, want)
	}
	if got, want := fixtures.String(preflight, "targetVersion"), first.Result.TargetVersion; got != want {
		t.Fatalf("the preflight item presents target %q, want the first record's %q", got, want)
	}
}

// observedAtOf reads one projected item's observation time.
func observedAtOf(t *testing.T, item map[string]any) time.Time {
	t.Helper()
	text := fixtures.String(item, "observedAt")
	if text == "" {
		t.Fatalf("the item carries no observation time: %v", item)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatalf("the item's observation time %q is not a timestamp: %v", text, err)
	}
	return parsed.UTC()
}

// TestTC54ConcurrentReadsOnOneContextKeepTheirRecords covers the interleaving a
// shared caller context allows: two reads of the update page run at the same
// time over one context, and neither page may present the other's record. Each
// read resolves its record once, and both of its record-backed items come from
// that read's record (UPDATER-C-4, UPDATER-C-5).
func TestTC54ConcurrentReadsOnOneContextKeepTheirRecords(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	first := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "20m",
		Nodes:         []string{"node-a"},
	})
	second := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.39.0",
		Downtime:      "30m",
		Nodes:         []string{"node-a"},
	})
	if first.ID == second.ID {
		t.Fatal("the two preflights stored one record, so the case would prove nothing")
	}

	// Both reads resolve their record before either resolves a second one, so a
	// grouping key the two share would hand one page the other's record.
	source := &fixtures.IdentityRecords{Served: []upgrade.Record{first, second}}
	world.RecordSource = source

	// One shell, one caller context, two reads in flight: the case the exported
	// read path allows and a context-keyed snapshot cannot separate.
	shell := world.PageShell(t)
	shared := context.Background()
	rendered := make([]pages.Page, 2)
	failures := make([]string, 2)
	var wait sync.WaitGroup
	for index := range rendered {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			page, failure := shell.Read(shared, fixtures.Owner, updater.PageUpdates)
			if failure != nil {
				failures[index] = failure.Message
				return
			}
			rendered[index] = page
		}(index)
	}
	wait.Wait()
	for index, failure := range failures {
		if failure != "" {
			t.Fatalf("concurrent read %d failed: %s", index, failure)
		}
	}

	// Each read carries its own identity even though both share one context, and
	// both are live while they read: that is what lets a snapshot be separated by
	// read rather than by caller context (KOSD-C-53).
	observed := source.Observations()
	if len(observed) != len(rendered) {
		t.Fatalf("two concurrent page reads asked the record source %d times, want one each", len(observed))
	}
	for index, observation := range observed {
		if !observation.Live {
			t.Fatalf("record read %d outside a live page read, so it cannot be grouped (record %q)",
				index, observation.Record.ID)
		}
	}
	if observed[0].ID == observed[1].ID {
		t.Fatalf("two concurrent reads share page read identity %d, so their records cannot be separated",
			observed[0].ID)
	}

	// Each page's two record-backed items come from the one record that page's
	// read resolved, and the two pages do not present the same record.
	seen := make(map[string]bool, len(rendered))
	for index, page := range rendered {
		items := fixtures.ItemsByID(t, page)
		preflight := fixtures.ValueOf(t, items, updater.ItemPreflight)
		blocked := fixtures.ValueOf(t, items, updater.ItemBlockedUpgrades)
		record := fixtures.String(preflight, "recordId")
		if record != first.ID && record != second.ID {
			t.Fatalf("read %d presents record %q, want one of the two stored", index, record)
		}
		if got := fixtures.String(blocked, "recordId"); got != record {
			t.Fatalf("read %d mixes records: the preflight item shows %q, the blocked item %q", index, record, got)
		}
		if seen[record] {
			t.Fatalf("both concurrent reads present record %q, want each read its own", record)
		}
		seen[record] = true
	}
}

// TestTC54ReadOutsideAShellReadIsNotGrouped covers the safe fallback: a reader
// the shell did not hand a live read identity reads the record per item instead
// of guessing a group, so it never presents a record it cannot prove belongs to
// the same read (UPDATER-C-4, UPDATER-C-5).
func TestTC54ReadOutsideAShellReadIsNotGrouped(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	first := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "20m",
		Nodes:         []string{"node-a"},
	})
	second := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.39.0",
		Downtime:      "30m",
		Nodes:         []string{"node-a"},
	})

	source := &fixtures.AdvancingRecords{Served: []upgrade.Record{first, second}}
	readings := &updater.Readings{Dynamic: world.Cluster, Artifacts: world.Artifacts, Preflights: source}
	ctx := context.Background()
	for _, item := range updater.Declaration().Contributions.Pages[1].Items {
		if _, failure := readings.Read(ctx, fixtures.Owner, item); failure != nil {
			t.Fatalf("read item %q: %s", item.ID, failure.Message)
		}
	}
	if source.Calls != 2 {
		t.Fatalf("two record-backed items outside a page read asked the source %d times, want one each", source.Calls)
	}
}

func TestTC54PreflightBeforeAndAfter(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")
	// The platform preflight reports the node-local applications the outage
	// stops and the declared downtime, which is what the record carries.
	world.Platform = fixtures.ReportPlatform(upgrade.Report{Nodes: []upgrade.NodePlan{{
		Node:         "node-a",
		Applications: []string{"team/shop", "team/cache"},
		Volumes:      []string{"team/cache"},
		Downtime:     "20m",
	}}})

	// No preflight has run: both record-backed items stay on the page, each
	// stating that it carries no value and why (C-4, C-7).
	before := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates))
	if len(before) != len(updater.UpdateItems) {
		t.Fatalf("update page has %d items, want the %d UPDATER-C-2 declares: %v",
			len(before), len(updater.UpdateItems), before)
	}
	for _, id := range []string{updater.ItemPreflight, updater.ItemBlockedUpgrades} {
		value := fixtures.ValueOf(t, before, id)
		if value["available"] != false {
			t.Fatalf("item %q before any preflight reports available = %v, want false", id, value["available"])
		}
		if reason := fixtures.String(value, "reason"); strings.TrimSpace(reason) == "" {
			t.Fatalf("item %q before any preflight carries no reason: %v", id, value)
		}
	}

	// One platform preflight over a node that carries node-local applications.
	record := world.Preflight(t, upgrade.PreflightRequest{
		Kind:          upgrade.KindPlatform,
		TargetVersion: "v1.38.0",
		Downtime:      "20m",
		Nodes:         []string{"node-a"},
	})
	if len(record.Result.AffectedApplications) == 0 {
		t.Fatal("the preflight recorded no affected applications, so the case would prove nothing")
	}

	after := fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates))
	value := fixtures.ValueOf(t, after, updater.ItemPreflight)
	if value["available"] != true {
		t.Fatalf("the preflight item is unavailable after a preflight: %v", value)
	}
	if got := fixtures.String(value, "recordId"); got != record.ID {
		t.Fatalf("preflight item record = %q, want the stored %q", got, record.ID)
	}
	if got, want := stringsOf(t, fixtures.ListOf(t, value, "affectedApplications")), record.Result.AffectedApplications; !equal(got, want) {
		t.Fatalf("presented affected applications = %v, want the record's %v", got, want)
	}
	if got, want := fixtures.String(value, "declaredDowntime"), record.Result.DeclaredDowntime; got != want {
		t.Fatalf("presented declared downtime = %q, want the record's %q", got, want)
	}
	if got, want := fixtures.String(value, "estimatedDowntime"), record.Result.EstimatedDowntime; got != want {
		t.Fatalf("presented estimated downtime = %q, want the record's %q", got, want)
	}
}

// TestTC54ExtensionPreflightPresentsTheContributionDiff covers C-4's third
// presented value: an extension upgrade reports each target's contribution
// difference, and the page shows exactly what the record stored (C-4, C-55).
func TestTC54ExtensionPreflightPresentsTheContributionDiff(t *testing.T) {
	world := fixtures.NewWorld(t)
	world.Install(t, fixtures.Product(t))
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// The target package drops the updates page the installed one contributes,
	// so the preflight records that page as removed.
	target := updater.Declaration()
	target.Version = "1.1.0"
	target.Contributions.Pages = []extension.Page{target.Contributions.Pages[0]}
	digest := world.AddPackage(t, "kos-updater-next", target)

	record := world.Preflight(t, upgrade.PreflightRequest{
		Kind:     upgrade.KindExtension,
		Downtime: "5m",
		Targets:  []upgrade.Target{{Component: "kos-updater", Reference: "kos-updater-next", Digest: digest}},
	})
	if len(record.Result.Components) != 1 || len(record.Result.Components[0].Changes) == 0 {
		t.Fatalf("the preflight recorded no contribution difference: %+v", record.Result.Components)
	}

	value := fixtures.ValueOf(t,
		fixtures.ItemsByID(t, fixtures.ReadPage(t, world.PageShell(t), fixtures.Owner, updater.PageUpdates)),
		updater.ItemPreflight)
	if value["available"] != true {
		t.Fatalf("the preflight item is unavailable after an extension preflight: %v", value)
	}
	presented := componentsOf(t, fixtures.ListOf(t, value, "components"))
	if len(presented) != 1 {
		t.Fatalf("presented components = %v, want the one target the record holds", presented)
	}
	recorded := record.Result.Components[0]
	presentedTarget := presented[recorded.Component]
	if presentedTarget == nil {
		t.Fatalf("the page does not present component %q: %v", recorded.Component, presented)
	}
	if got, want := fixtures.String(presentedTarget, "from"), recorded.From; got != want {
		t.Fatalf("presented from = %q, want the record's %q", got, want)
	}
	if got, want := fixtures.String(presentedTarget, "to"), recorded.To; got != want {
		t.Fatalf("presented to = %q, want the record's %q", got, want)
	}
	changes := changesOf(t, fixtures.ListOf(t, presentedTarget, "changes"))
	if len(changes) != len(recorded.Changes) {
		t.Fatalf("presented changes = %v, want the record's %+v", changes, recorded.Changes)
	}
	for index, change := range recorded.Changes {
		if got := fixtures.String(changes[index], "class"); got != change.Class {
			t.Fatalf("presented change %d class = %q, want %q", index, got, change.Class)
		}
		if got, want := optionalStrings(t, changes[index], "removed"), change.Removed; !equal(got, want) {
			t.Fatalf("presented change %d removed = %v, want %v", index, got, want)
		}
		if got, want := optionalStrings(t, changes[index], "added"), change.Added; !equal(got, want) {
			t.Fatalf("presented change %d added = %v, want %v", index, got, want)
		}
	}
}

// componentsOf indexes the presented contribution differences by component.
func componentsOf(t *testing.T, entries []any) map[string]map[string]any {
	t.Helper()
	components := make(map[string]map[string]any, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("component %d is not an object: %v", index, raw)
		}
		components[fixtures.String(entry, "component")] = entry
	}
	return components
}

// changesOf reads the change list of one presented component. A list the
// encoder omitted is an empty difference, not a missing answer.
func changesOf(t *testing.T, entries []any) []map[string]any {
	t.Helper()
	changes := make([]map[string]any, 0, len(entries))
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("change %d is not an object: %v", index, raw)
		}
		changes = append(changes, entry)
	}
	return changes
}

// optionalStrings renders one projected list of strings that the encoder omits
// when the difference is empty, sorted so two sets compare independent of the
// order the encoder chose.
func optionalStrings(t *testing.T, object map[string]any, key string) []string {
	t.Helper()
	raw, present := object[key]
	if !present {
		return nil
	}
	entries, ok := raw.([]any)
	if !ok {
		t.Fatalf("field %q is not a list: %v", key, raw)
	}
	return stringsOf(t, entries)
}

// stringsOf renders one projected list of strings, sorted so two sets compare
// independent of the order the encoder chose.
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

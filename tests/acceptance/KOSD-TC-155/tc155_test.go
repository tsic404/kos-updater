// Package kosdtc155 verifies that kos-updater ships as an independently
// installable extension package: the artifact carries the author SDK's structure
// with a digest over the declaration, the kernel activates exactly the two
// declared pages, uninstalling the component withdraws them, and a reinstall or
// a second product leaves the kernel object model and the other product
// untouched (KOSD-AC-72, KOSD-TC-155).
package kosdtc155

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"sort"
	"testing"

	"github.com/tsic404/kos-sdk/packaging"
	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/pages"

	"github.com/tsic404/kos-updater/pkg/updater"
	"github.com/tsic404/kos-updater/tests/acceptance/fixtures"
)

func TestTC155PackageStructure(t *testing.T) {
	pkg, archive, err := updater.Build(nil)
	if err != nil {
		t.Fatalf("build the extension package: %v", err)
	}
	verified, err := packaging.Verify(archive, pkg.Digest)
	if err != nil {
		t.Fatalf("verify the built package: %v", err)
	}
	if len(verified.Assets) != 0 {
		t.Fatalf("the package carries assets the product did not declare: %+v", verified.Assets)
	}
	if entries := entriesOf(t, archive); len(entries) != 2 ||
		entries[0] != packaging.ManifestEntry || entries[1] != packaging.DigestEntry {
		t.Fatalf("package entries = %v, want %q and %q", entries, packaging.ManifestEntry, packaging.DigestEntry)
	}

	// The declaration's closed field set is the one KOSD-C-18 fixes; nothing is
	// added and nothing is left out (SDK-C-3).
	declared := projected(t, verified.Manifest)
	fields := make([]string, 0, len(declared))
	for field := range declared {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	want := []string{"config", "contributions", "dependencies", "frameworkVersion", "id", "permissions", "status", "uninstallRetention", "version"}
	if len(fields) != len(want) {
		t.Fatalf("manifest fields = %v, want the closed set %v", fields, want)
	}
	for index, field := range want {
		if fields[index] != field {
			t.Fatalf("manifest fields = %v, want %v", fields, want)
		}
	}

	// The declaration the kernel activates and the declaration the package ships
	// are the same contribution: one table produces both (C-17, C-18).
	activated := fixtures.DeclarationMap(t)
	if declared["id"] != activated["identifier"] {
		t.Fatalf("package id = %v, want %v", declared["id"], activated["identifier"])
	}
	if declared["version"] != activated["version"] || declared["frameworkVersion"] != activated["frameworkVersion"] {
		t.Fatalf("package version pair = %v/%v, want %v/%v",
			declared["version"], declared["frameworkVersion"], activated["version"], activated["frameworkVersion"])
	}
	shipped, ok := declared["contributions"].(map[string]any)
	if !ok {
		t.Fatalf("packaged contributions are not an object: %v", declared["contributions"])
	}
	loaded, ok := activated["contributions"].(map[string]any)
	if !ok {
		t.Fatalf("activated contributions are not an object: %v", activated["contributions"])
	}
	if !sameJSON(t, shipped["pages"], loaded["pages"]) {
		t.Fatalf("packaged pages = %v, want the activated ones %v", shipped["pages"], loaded["pages"])
	}

	// The package declares the two pages the product owns and no other page.
	pageIDs := make([]string, 0)
	for _, raw := range shipped["pages"].([]any) {
		page, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("packaged page is not an object: %v", raw)
		}
		pageIDs = append(pageIDs, page["id"].(string))
	}
	sort.Strings(pageIDs)
	if len(pageIDs) != 2 || pageIDs[0] != updater.PageComponents || pageIDs[1] != updater.PageUpdates {
		t.Fatalf("packaged pages = %v, want %q and %q", pageIDs, updater.PageComponents, updater.PageUpdates)
	}
}

func TestTC155DigestCoversAssets(t *testing.T) {
	plain, _, err := updater.Build(nil)
	if err != nil {
		t.Fatalf("build the bare package: %v", err)
	}
	first, _, err := updater.Build([]packaging.Asset{{Path: packaging.AssetPrefix + "updater.js", Content: []byte("v1")}})
	if err != nil {
		t.Fatalf("build the package with one asset: %v", err)
	}
	second, _, err := updater.Build([]packaging.Asset{{Path: packaging.AssetPrefix + "updater.js", Content: []byte("v2")}})
	if err != nil {
		t.Fatalf("build the package with a changed asset: %v", err)
	}
	if plain.Digest == first.Digest {
		t.Fatal("adding a UI asset did not change the package digest")
	}
	if first.Digest == second.Digest {
		t.Fatal("changing a UI asset did not change the package digest")
	}
}

func TestTC155LoadUnloadReload(t *testing.T) {
	product := fixtures.Product(t)
	other := fixtures.OtherProduct(t)
	world := fixtures.NewWorld(t)
	world.Node(t, "node-a", "v1.37.0", "KOS 1.1")

	// Install: both pages appear and render every declared item, and the second
	// product keeps its own page.
	world.Install(t, product, other)
	shell := world.PageShell(t)
	assertListed(t, shell, updater.PageComponents, true)
	assertListed(t, shell, updater.PageUpdates, true)
	assertListed(t, shell, "other-page", true)
	assertItems(t, shell, updater.PageComponents, len(updater.ComponentItems))
	assertItems(t, shell, updater.PageUpdates, len(updater.UpdateItems))

	// Uninstall kos-updater alone: its pages disappear with the component, the
	// other product keeps its page, and the kernel model is untouched (C-29).
	world.Install(t, other)
	shell = world.PageShell(t)
	assertListed(t, shell, updater.PageComponents, false)
	assertListed(t, shell, updater.PageUpdates, false)
	assertListed(t, shell, "other-page", true)
	for _, id := range []string{updater.PageComponents, updater.PageUpdates} {
		if _, failure := shell.Read(context.Background(), fixtures.Owner, id); failure == nil {
			t.Fatalf("the uninstalled page %q is still readable", id)
		} else if failure.Code != controlplane.CodeNotFound {
			t.Fatalf("reading the uninstalled page %q failed with %q, want %q", id, failure.Code, controlplane.CodeNotFound)
		}
	}
	if classes := world.Registry.ObjectClasses(); len(classes) != 0 {
		t.Fatalf("a page-only product declares object classes: %+v", classes)
	}
	if got, want := len(world.Registry.Capabilities()), len(extension.BaselineCapabilities()); got != want {
		t.Fatalf("uninstalling changed the kernel capability baseline: %d, want %d", got, want)
	}

	// Reinstall the same package: the same two pages come back (KOSD-AC-72).
	world.Install(t, product, other)
	shell = world.PageShell(t)
	assertListed(t, shell, updater.PageComponents, true)
	assertListed(t, shell, updater.PageUpdates, true)
	assertItems(t, shell, updater.PageComponents, len(updater.ComponentItems))
	assertItems(t, shell, updater.PageUpdates, len(updater.UpdateItems))

	// A package whose artifact does not match the named digest installs nothing:
	// neither page appears (C-17).
	broken := product
	broken.Package = &extension.Package{
		Reference: product.Package.Reference,
		Digest:    "sha256:" + zeros(),
		Content:   product.Package.Content,
	}
	world.Install(t, broken)
	if pages := world.Registry.Pages(); len(pages) != 0 {
		t.Fatalf("a digest mismatch contributed a page: %+v", pages)
	}
}

// TestTC155FirstPartyAndThirdPartyShareThePackageStructure checks that the
// package the product ships has the structure a third-party extension gets from
// the same SDK path (SDK-C-4).
func TestTC155FirstPartyAndThirdPartyShareThePackageStructure(t *testing.T) {
	_, firstParty, err := updater.Build(nil)
	if err != nil {
		t.Fatalf("build the product package: %v", err)
	}
	_, thirdParty, err := packaging.Pack(map[string]any{
		"id": "third-party-demo", "version": "1.0.0", "frameworkVersion": ">=1.0 <2.0",
		"contributions":      map[string]any{"pages": []any{map[string]any{"id": "demo"}}},
		"dependencies":       map[string]any{},
		"permissions":        map[string]any{},
		"config":             map[string]any{},
		"status":             map[string]any{},
		"uninstallRetention": map[string]any{},
	}, nil)
	if err != nil {
		t.Fatalf("pack the third-party declaration: %v", err)
	}
	firstEntries := entriesOf(t, firstParty)
	thirdEntries := entriesOf(t, thirdParty)
	if len(firstEntries) != len(thirdEntries) {
		t.Fatalf("first-party entries = %v, third-party entries = %v", firstEntries, thirdEntries)
	}
	for index := range firstEntries {
		if firstEntries[index] != thirdEntries[index] {
			t.Fatalf("first-party entry %d = %q, third-party = %q", index, firstEntries[index], thirdEntries[index])
		}
	}
}

// assertListed checks whether one page appears in the shell's list.
func assertListed(t *testing.T, shell *pages.Service, id string, want bool) {
	t.Helper()
	listed := make(map[string]bool)
	for _, summary := range shell.List(fixtures.Owner) {
		listed[summary.ID] = true
	}
	if listed[id] != want {
		t.Fatalf("page %q listed = %v, want %v (listed: %v)", id, listed[id], want, listed)
	}
}

// assertItems checks that one page renders exactly the declared item count.
func assertItems(t *testing.T, shell *pages.Service, id string, want int) {
	t.Helper()
	page := fixtures.ReadPage(t, shell, fixtures.Owner, id)
	if len(page.Items) != want {
		t.Fatalf("page %q has %d items, want %d: %+v", id, len(page.Items), want, page.Items)
	}
}

// projected renders one value as the JSON a package carries.
func projected(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode the packaged value: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode the packaged value: %v", err)
	}
	return decoded
}

// entriesOf lists the archive entry names in order.
func entriesOf(t *testing.T, archive []byte) []string {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("open the package archive: %v", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	entries := make([]string, 0)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the package archive: %v", err)
		}
		entries = append(entries, header.Name)
	}
	return entries
}

// sameJSON compares two decoded JSON values by their canonical encoding.
func sameJSON(t *testing.T, left, right any) bool {
	t.Helper()
	leftBytes, err := json.Marshal(left)
	if err != nil {
		t.Fatalf("encode the packaged value: %v", err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		t.Fatalf("encode the activated value: %v", err)
	}
	return bytes.Equal(leftBytes, rightBytes)
}

// zeros is a digest body that matches no artifact.
func zeros() string {
	return "0000000000000000000000000000000000000000000000000000000000000000"
}

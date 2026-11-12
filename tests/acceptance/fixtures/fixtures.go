// Package fixtures builds the pieces the acceptance cases need: the page shell,
// a throwaway cluster object set, and installed components carrying the status
// the kernel's own install evaluation writes. Nothing here reaches a real
// cluster or database.
package fixtures

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/tsic404/kosd/api"
	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/pages"
	pagescluster "github.com/tsic404/kosd/pkg/pages/cluster"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-updater/pkg/updater"
)

// Clock is the moment a case reads at unless the case moves it.
var Clock = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// Owner is the account the cases read with.
var Owner = identity.Session{Username: "owner", Role: write.RoleOwner}

// World is one cluster with the components installed into it.
type World struct {
	// Cluster is the object set the page sources read.
	Cluster dynamic.Interface
	// Artifacts serves the packages the installed components name; nil models a
	// distribution that exposes no package source.
	Artifacts extension.ArtifactSource
	// Registry is the contribution projection the install produced.
	Registry *extension.Registry
	// ReadNow is the moment the product's reads observe; nil reads Clock.
	ReadNow func() time.Time
	// ShellNow is the moment the shell judges freshness by; nil reads Clock.
	ShellNow func() time.Time
}

// NewWorld builds an empty cluster with no installed component.
func NewWorld(t *testing.T) *World {
	t.Helper()
	return &World{Cluster: objectSet(t), Artifacts: extension.MemoryArtifacts{}}
}

// Install evaluates the install requests with the kernel's own rules, replaces
// the installed components with the outcome, and keeps the packages the
// components name available to the page.
func (w *World) Install(t *testing.T, requests ...extension.Request) {
	t.Helper()
	assessment := extension.Evaluate(requests)
	resource := kernelResource(t, "SystemComponent")

	existing, err := w.Cluster.Resource(resource).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list the installed components: %v", err)
	}
	for index := range existing.Items {
		name := existing.Items[index].GetName()
		if err := w.Cluster.Resource(resource).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("clear component %q: %v", name, err)
		}
	}

	byName := make(map[string]extension.Request, len(requests))
	artifacts := extension.MemoryArtifacts{}
	for _, request := range requests {
		byName[request.Name] = request
		if request.Package != nil {
			artifacts[request.Package.Reference] = request.Package.Content
		}
	}
	for _, result := range assessment.Components {
		request, known := byName[result.Name]
		if !known {
			t.Fatalf("the install evaluation named unknown component %q", result.Name)
		}
		create(t, w.Cluster, resource, componentObject(t, request, result))
	}

	w.Registry = assessment.Registry
	w.Artifacts = artifacts
}

// PageShell wires the page shell over this world's contributions and readers.
func (w *World) PageShell(t *testing.T) *pages.Service {
	t.Helper()
	readings := &updater.Readings{
		Dynamic:   w.Cluster,
		Artifacts: w.Artifacts,
		Now:       w.readClock(),
	}
	return pages.New(w.Registry, pages.Config{
		Readings: pages.Router{updater.Identifier: readings},
		Now:      w.shellClock(),
	})
}

// readClock is the clock the product's reads stamp values with.
func (w *World) readClock() func() time.Time {
	if w.ReadNow != nil {
		return w.ReadNow
	}
	return func() time.Time { return Clock }
}

// shellClock is the clock the shell measures freshness against.
func (w *World) shellClock() func() time.Time {
	if w.ShellNow != nil {
		return w.ShellNow
	}
	return func() time.Time { return Clock }
}

// Product is the install request carrying this product's declaration.
func Product(t *testing.T) extension.Request {
	t.Helper()
	content, err := extension.Marshal(updater.Declaration())
	if err != nil {
		t.Fatalf("encode the product declaration: %v", err)
	}
	return installRequest("kos-updater", content, nextCreated())
}

// DeclarationContent is the product declaration document one install request
// carries (KOSD-C-17).
func DeclarationContent(t *testing.T) []byte {
	t.Helper()
	content, err := extension.Marshal(updater.Declaration())
	if err != nil {
		t.Fatalf("encode the product declaration: %v", err)
	}
	return content
}

// DeclarationMap is the product declaration as a JSON object.
func DeclarationMap(t *testing.T) map[string]any {
	t.Helper()
	var declaration map[string]any
	if err := json.Unmarshal(DeclarationContent(t), &declaration); err != nil {
		t.Fatalf("decode the product declaration: %v", err)
	}
	return declaration
}

// OtherProduct is a second extension the isolation cases install beside
// kos-updater: one page with one item, nothing shared.
func OtherProduct(t *testing.T) extension.Request {
	t.Helper()
	declaration := extension.Declaration{
		Identifier:       "other-product",
		Version:          "1.0.0",
		FrameworkVersion: ">=1.0 <2.0",
		Contributions: extension.Contributions{
			Pages: []extension.Page{{
				ID:             "other-page",
				Title:          "其他产品",
				Location:       "panel",
				ReadOnly:       true,
				ReadOnlyReason: "本页只读",
				Items: []extension.PageItem{{
					ID: "other-item", Label: "其他项", Source: extension.PageSourceObject,
					Reference: "other-product/other-item",
				}},
			}},
		},
	}
	content, err := extension.Marshal(declaration)
	if err != nil {
		t.Fatalf("encode the other product declaration: %v", err)
	}
	return installRequest("other", content, nextCreated())
}

// Extension is the install request of one test extension with the given target
// framework range and no contribution of its own.
func Extension(t *testing.T, name, frameworkRange string) extension.Request {
	t.Helper()
	declaration := extension.Declaration{
		Identifier:       name,
		Version:          "1.0.0",
		FrameworkVersion: frameworkRange,
	}
	content, err := extension.Marshal(declaration)
	if err != nil {
		t.Fatalf("encode the %s declaration: %v", name, err)
	}
	return installRequest(name, content, nextCreated())
}

// installRequest binds one declaration document to the install request that names it
// (KOSD-C-17).
func installRequest(name string, content []byte, created time.Time) extension.Request {
	return extension.Request{
		Name:    name,
		Created: created,
		Package: &extension.Package{
			Reference: name + "-package",
			Digest:    extension.DigestOf(content),
			Content:   content,
		},
	}
}

// createdCounter orders install requests without depending on wall time.
var createdCounter int

// nextCreated returns the next install order.
func nextCreated() time.Time {
	createdCounter++
	return Clock.Add(time.Duration(createdCounter) * time.Second)
}

// componentObject is the SystemComponent one install request leaves behind: the
// package it names and the status the kernel's evaluation writes (C-17, C-28).
func componentObject(t *testing.T, request extension.Request, result extension.ComponentResult) map[string]any {
	t.Helper()
	pkg := map[string]any{}
	if request.Package != nil {
		pkg["reference"] = request.Package.Reference
		pkg["digest"] = request.Package.Digest
	}
	spec := map[string]any{"package": pkg}
	if request.IsDisabled {
		spec["disable"] = true
	}
	componentStatus := extension.StatusFor(result, nil)
	status, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&componentStatus)
	if err != nil {
		t.Fatalf("encode the status of component %q: %v", result.Name, err)
	}
	return map[string]any{
		"apiVersion": api.Group + "/" + v1beta1.GroupVersion.Version,
		"kind":       "SystemComponent",
		"metadata":   map[string]any{"name": result.Name},
		"spec":       spec,
		"status":     status,
	}
}

// Capability is one capability a provider registers and the result KOS verified
// it with (C-09, C-10).
type Capability struct {
	Name    string
	Version string
	Scope   string
	State   string
}

// Provider writes one registered provider with its effective and verified
// capabilities.
func (w *World) Provider(t *testing.T, name string, capabilities ...Capability) {
	t.Helper()
	registered := make([]any, 0, len(capabilities))
	effective := make([]any, 0, len(capabilities))
	results := make([]any, 0, len(capabilities))
	for _, capability := range capabilities {
		registered = append(registered, map[string]any{
			"name": capability.Name, "version": capability.Version, "scope": capability.Scope,
			"caseSet": "cases", "caseSetDigest": "sha256:cases",
		})
		effective = append(effective, map[string]any{
			"name": capability.Name, "version": capability.Version, "scope": capability.Scope,
			"caseSet": "cases", "caseSetDigest": "sha256:cases",
		})
		results = append(results, map[string]any{
			"name": capability.Name, "version": capability.Version,
			"source": "KOS", "state": capability.State,
		})
	}
	create(t, w.Cluster, kernelResource(t, "Provider"), map[string]any{
		"apiVersion": api.Group + "/" + v1beta1.GroupVersion.Version,
		"kind":       "Provider",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"version":        "1.2.0",
			"storageClasses": []any{},
			"capabilities":   registered,
		},
		"status": map[string]any{
			"health":                map[string]any{"state": string(v1beta1.HealthStateHealthy)},
			"effectiveCapabilities": effective,
			"capabilities":          results,
		},
	})
}

// Node writes one node with the version fields the version item reads.
func (w *World) Node(t *testing.T, name, kubeletVersion, osImage string) {
	t.Helper()
	create(t, w.Cluster, schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata":   map[string]any{"name": name},
		"status": map[string]any{
			"nodeInfo": map[string]any{"kubeletVersion": kubeletVersion, "osImage": osImage},
		},
	})
}

// ComponentOf reads one installed component object back from the cluster, so a
// case judges the projection against the stored object rather than a fixture.
func ComponentOf(t *testing.T, cluster dynamic.Interface, name string) v1beta1.SystemComponent {
	t.Helper()
	object, err := cluster.Resource(kernelResource(t, "SystemComponent")).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read component %q: %v", name, err)
	}
	var component v1beta1.SystemComponent
	if failure := pagescluster.Decode(object.Object, &component); failure != nil {
		t.Fatalf("decode component %q: %s", name, failure.Message)
	}
	return component
}

// DeclaredRange reads the target framework range one installed component's
// package declares, through the object's package reference and the artifact
// store. A case judges the page against this stored declaration.
func DeclaredRange(t *testing.T, world *World, componentName string) string {
	t.Helper()
	component := ComponentOf(t, world.Cluster, componentName)
	reference := component.Spec.Package.Reference
	if reference == "" {
		t.Fatalf("component %q names no package", componentName)
	}
	content, err := world.Artifacts.Fetch(context.Background(), reference)
	if err != nil {
		t.Fatalf("read the package of component %q: %v", componentName, err)
	}
	declaration, err := extension.Parse(content)
	if err != nil {
		t.Fatalf("parse the package of component %q: %v", componentName, err)
	}
	loaded, err := declaration.Load()
	if err != nil {
		t.Fatalf("load the declaration of component %q: %v", componentName, err)
	}
	return loaded.FrameworkVersion
}

// ReadPage renders one declared page for the session and fails the case when the
// shell refuses it.
func ReadPage(t *testing.T, shell *pages.Service, session identity.Session, id string) pages.Page {
	t.Helper()
	page, failure := shell.Read(context.Background(), session, id)
	if failure != nil {
		t.Fatalf("read page %q: %s (%s)", id, failure.Message, failure.Code)
	}
	return page
}

// ItemOf returns one rendered item by identity.
func ItemOf(t *testing.T, page pages.Page, id string) pages.Item {
	t.Helper()
	for _, item := range page.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("page %q has no item %q", page.ID, id)
	return pages.Item{}
}

// ItemsByID projects one page the way the control-plane API serializes it and
// indexes its items by identity, so a case judges the projection a browser
// receives.
func ItemsByID(t *testing.T, page pages.Page) map[string]map[string]any {
	t.Helper()
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("encode page %q: %v", page.ID, err)
	}
	var projected map[string]any
	if err := json.Unmarshal(encoded, &projected); err != nil {
		t.Fatalf("decode page %q: %v", page.ID, err)
	}
	raw, ok := projected["items"].([]any)
	if !ok {
		t.Fatalf("page %q carries no items: %v", page.ID, projected)
	}
	items := make(map[string]map[string]any, len(raw))
	for index, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("page %q item %d is not an object: %v", page.ID, index, entry)
		}
		id, _ := item["id"].(string)
		if _, duplicated := items[id]; duplicated {
			t.Fatalf("page %q renders item %q twice", page.ID, id)
		}
		items[id] = item
	}
	return items
}

// ValueOf reads one projected item's value object.
func ValueOf(t *testing.T, items map[string]map[string]any, id string) map[string]any {
	t.Helper()
	item, found := items[id]
	if !found {
		t.Fatalf("the page has no item %q", id)
	}
	value, ok := item["value"].(map[string]any)
	if !ok {
		t.Fatalf("item %q value is not an object: %v", id, item["value"])
	}
	return value
}

// String reads a non-empty string field of one projected object.
func String(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// ListOf reads one array field of a projected object.
func ListOf(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	values, ok := object[key].([]any)
	if !ok {
		t.Fatalf("field %q is not a list: %v", key, object[key])
	}
	return values
}

// objectSet builds the fake dynamic client holding the kernel objects the cases
// shape. The scheme registers the objects as unstructured, so the fake stores
// exactly the JSON the page sources read.
func objectSet(t *testing.T) dynamic.Interface {
	t.Helper()
	scheme := runtime.NewScheme()
	lists := map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "nodes"}: "NodeList",
	}
	add := func(gvk schema.GroupVersionKind, resource string) {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
		lists[gvk.GroupVersion().WithResource(resource)] = gvk.Kind + "List"
	}
	add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, "nodes")
	for _, kind := range []string{"Application", "ApplicationDefinition", "Provider", "SystemComponent"} {
		resource, ok := api.ResourceName(kind)
		if !ok {
			t.Fatalf("kernel kind %q has no resource name", kind)
		}
		add(schema.GroupVersionKind{Group: api.Group, Version: v1beta1.GroupVersion.Version, Kind: kind}, resource)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, lists)
}

// kernelResource resolves one kernel kind's resource.
func kernelResource(t *testing.T, kind string) schema.GroupVersionResource {
	t.Helper()
	resource, ok := api.ResourceName(kind)
	if !ok {
		t.Fatalf("kernel kind %q has no resource name", kind)
	}
	return schema.GroupVersionResource{Group: api.Group, Version: v1beta1.GroupVersion.Version, Resource: resource}
}

// create stores one object in the fake client.
func create(t *testing.T, cluster dynamic.Interface, resource schema.GroupVersionResource, object map[string]any) {
	t.Helper()
	if _, err := cluster.Resource(resource).Create(context.Background(), &unstructured.Unstructured{Object: object}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create %s: %v", resource.Resource, err)
	}
}

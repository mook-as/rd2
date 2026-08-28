// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// spyingClient wraps a client.Client to count Create/Update/status-Update
// calls, so tests can assert on idempotency (i.e. that a reconcile that
// changes nothing does not attempt a redundant spec write) and on whether a
// ComposeProject was created. The controller now reconciles via
// Get+mutate+Update (client-side apply, not server-side Apply), so writes
// show up as plain Create/Update calls plus Status().Update() calls.
type spyingClient struct {
	client.Client
	createCalls       int
	updateCalls       int
	statusUpdateCalls int
}

func (c *spyingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.createCalls++
	if obj.GetUID() == "" {
		// The fake client (unlike a real API server) does not assign a UID on
		// creation; do so here so that code relying on GetUID() to detect
		// "found" vs "not found" objects (e.g. removeComposeProjectMembership)
		// behaves the same way it would against a real cluster.
		obj.SetUID(uuid.NewUUID())
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c *spyingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updateCalls++
	return c.Client.Update(ctx, obj, opts...)
}

func (c *spyingClient) Status() client.SubResourceWriter {
	return &spyingStatusWriter{SubResourceWriter: c.Client.Status(), spy: c}
}

// spyingStatusWriter counts Status().Update() calls on the wrapping
// spyingClient, since client.Client.Status() is otherwise unwrapped by
// embedding.
type spyingStatusWriter struct {
	client.SubResourceWriter
	spy *spyingClient
}

func (w *spyingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.spy.statusUpdateCalls++
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

// writes returns the total number of write calls (Create + Update +
// Status().Update()) observed so far.
func (c *spyingClient) writes() int {
	return c.createCalls + c.updateCalls + c.statusUpdateCalls
}

// membersIndexFunc builds a client.IndexerFunc for the ".status.members[*].uid"
// client-side index, mirroring what is set up in SetupWithManager.
func membersIndexFunc(obj client.Object) []string {
	project, ok := obj.(*v1alpha1.ComposeProject)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(project.Status.Members))
	for _, member := range project.Status.Members {
		out = append(out, string(member.UID))
	}
	return out
}

// newReconciler builds a reconciler backed by a fake client seeded with objs,
// and a buffered fake event recorder.
func newReconciler(t *testing.T, objs ...client.Object) (*reconciler, *spyingClient) {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, v1alpha1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ComposeProject{}).
		WithObjects(objs...).
		WithIndex(&v1alpha1.ComposeProject{}, ".status.members[*].uid", membersIndexFunc)

	fakeClient := &spyingClient{Client: builder.Build()}

	return &reconciler{
		Client: fakeClient,
		Scheme: scheme,
	}, fakeClient
}

// newContainer builds a Container mirror with the given k8s namespace/name,
// docker-context namespace (status.namespace), and docker labels. The UID is
// derived from name, so it's stable and unique per test resource.
func newContainer(k8sNamespace, name, dockerNamespace string, labels map[string]string) *v1alpha1.Container {
	return &v1alpha1.Container{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: k8sNamespace,
			UID:       types.UID(name),
		},
		Status: v1alpha1.ContainerStatus{
			Namespace: dockerNamespace,
			Labels:    labels,
		},
	}
}

// newVolume builds a Volume mirror with the given k8s namespace/name,
// docker-context namespace (status.namespace), and docker labels. The UID is
// derived from name, so it's stable and unique per test resource.
func newVolume(k8sNamespace, name, dockerNamespace string, labels map[string]string) *v1alpha1.Volume {
	return &v1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: k8sNamespace,
			UID:       types.UID(name),
		},
		Status: v1alpha1.VolumeStatus{
			Namespace: dockerNamespace,
			Labels:    labels,
		},
	}
}

// newImage builds an Image mirror with the given k8s namespace/name,
// docker-context namespace (status.namespace), and docker labels. The UID is
// derived from name, so it's stable and unique per test resource.
func newImage(k8sNamespace, name, dockerNamespace string, labels map[string]string) *v1alpha1.Image {
	return &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: k8sNamespace,
			UID:       types.UID(name),
		},
		Status: v1alpha1.ImageStatus{
			Namespace: dockerNamespace,
			Labels:    labels,
		},
	}
}

// reconcileKind runs the reconciler for the given kind/key/uid, mirroring
// how a watch event would enqueue a composeProjectRequest.
func reconcileKind(r *reconciler, kind string, key types.NamespacedName, uid types.UID) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), composeProjectRequest{
		Kind:           kind,
		NamespacedName: key,
		UID:            uid,
	})
}

// testReconcileResource runs the battery of subtests shared between
// TestReconcileContainer, TestReconcileImage, and TestReconcileVolume, since
// they're all reconciled identically once fetched: each is keyed off the
// same com.docker.compose.project* labels via the shared
// reconcileFromResource. newResource builds a resource of type T with the
// given k8s namespace/name, docker-context namespace, and docker labels.
// setStatusLabels updates an existing (already-fetched) resource's
// status.labels in place, so tests can simulate a label change without
// racing the fake client's optimistic-concurrency check. kind is the Kind
// string used in recorded events and composeProjectRequest (e.g.
// "Container"/"Image"/"Volume").
func testReconcileResource[T client.Object](
	t *testing.T,
	kind string,
	newResource func(k8sNamespace, name, dockerNamespace string, labels map[string]string) T,
) {
	t.Helper()

	const k8sNamespace = "rancher-desktop"
	const dockerNamespace = "moby"
	lowerKind := strings.ToLower(kind)

	t.Run(fmt.Sprintf("ignores %s without the compose project label", lowerKind), func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{"unrelated": "label"})
		r, fakeClient := newReconciler(t, resource)

		result, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.writes(), 0)
	})

	t.Run(fmt.Sprintf("ignores %s with no labels at all", lowerKind), func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{})
		r, fakeClient := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.writes(), 0)
	})

	t.Run(fmt.Sprintf("returns no error for %s that no longer exists", lowerKind), func(t *testing.T) {
		t.Parallel()
		r, fakeClient := newReconciler(t)

		_, err := reconcileKind(r, kind, types.NamespacedName{Namespace: k8sNamespace, Name: "gone"}, "gone-uid")
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.writes(), 0)
	})

	t.Run(fmt.Sprintf("ignores %s with the compose project label but no config-hash label", lowerKind), func(t *testing.T) {
		// A resource must carry the config-hash label to be considered a real
		// compose-managed resource; `docker compose down` itself only ever
		// discovers/removes containers that have this label, so anything
		// missing it cannot actually be reconciled/torn down as a compose
		// project member.
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel: "myproject",
		})
		r, fakeClient := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.writes(), 0)

		var project v1alpha1.ComposeProject
		err = r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &project)
		assert.Assert(t, apierrors.IsNotFound(err), "expected no ComposeProject to be created, got err=%v", err)
	})

	t.Run("creates a ComposeProject with just the identity when only the project and config-hash labels are set", func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, fakeClient := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)

		var project v1alpha1.ComposeProject
		err = r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &project)
		assert.NilError(t, err)
		assert.Equal(t, project.Spec.Namespace, dockerNamespace)
		assert.Equal(t, project.Spec.Name, "myproject")
		assert.Equal(t, project.Spec.WorkingDir, "")
		assert.Equal(t, len(project.Spec.Configs), 0)
		assert.DeepEqual(t, project.Status.Members, []v1alpha1.ComposeProjectMember{
			{Name: kind + "/" + resource.GetName(), UID: resource.GetUID()},
		})
	})

	t.Run("populates workingDir and configs, relative to workingDir, when the labels are present", func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel:     "myproject",
			composeConfigHashLabel:  "somehash",
			composeWorkingDirLabel:  "/home/user/myproject",
			composeConfigFilesLabel: "/home/user/myproject/compose.yaml,/home/user/myproject/compose.override.yaml,/etc/other/compose.yaml",
		})
		r, _ := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)

		var project v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &project))
		assert.Equal(t, project.Spec.WorkingDir, "/home/user/myproject")
		// The first two files are under workingDir, so they become relative;
		// the third is outside workingDir, so it is kept as an absolute path.
		assert.DeepEqual(t, project.Spec.Configs, []string{
			"compose.yaml",
			"compose.override.yaml",
			"/etc/other/compose.yaml",
		})
	})

	t.Run("is idempotent: reconciling twice with no changes re-applies without error", func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
			composeWorkingDirLabel: "/home/user/myproject",
		})
		r, fakeClient := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)

		_, err = reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		// No new Create/Update of the ComposeProject's spec, since nothing
		// changed; but the status is unconditionally re-applied on every
		// reconcile regardless of whether anything changed.
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.updateCalls, 0)
		assert.Equal(t, fakeClient.statusUpdateCalls, 2)
	})
}

func TestReconcileContainer(t *testing.T) {
	t.Parallel()
	testReconcileResource(t, v1alpha1.ContainerKind, newContainer)
}

func TestReconcileImage(t *testing.T) {
	t.Parallel()
	testReconcileResource(t, v1alpha1.ImageKind, newImage)
}

func TestReconcileVolume(t *testing.T) {
	t.Parallel()
	testReconcileResource(t, v1alpha1.VolumeKind, newVolume)
}

func TestComposeConfigFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		workingDir  string
		raw         string
		rawWindows  string
		want        []string
		wantWindows []string
	}{
		{
			name: "empty input yields nil",
			want: nil,
		},
		{
			name:        "unknown workingDir keeps paths unchanged",
			workingDir:  "",
			raw:         "/a/b/c.yaml,/a/b/d.yaml",
			rawWindows:  `C:\a\b\c.yaml,C:\a\b\d.yaml`,
			want:        []string{"/a/b/c.yaml", "/a/b/d.yaml"},
			wantWindows: []string{`C:\a\b\c.yaml`, `C:\a\b\d.yaml`},
		},
		{
			name:        "paths under workingDir become relative",
			workingDir:  "/a/b",
			raw:         "/a/b/c.yaml,/a/b/sub/d.yaml",
			rawWindows:  `C:\a\b\c.yaml,C:\a\b\sub\d.yaml`,
			want:        []string{"c.yaml", "sub/d.yaml"},
			wantWindows: []string{`c.yaml`, `sub\d.yaml`},
		},
		{
			name:        "a path outside workingDir is kept absolute",
			workingDir:  "/a/b",
			raw:         "/a/b/c.yaml,/somewhere/else/d.yaml",
			rawWindows:  `C:\a\b\c.yaml,C:\somewhere\else\d.yaml`,
			want:        []string{"c.yaml", "/somewhere/else/d.yaml"},
			wantWindows: []string{`c.yaml`, `C:\somewhere\else\d.yaml`},
		},
		{
			name:        "empty paths are ignored",
			workingDir:  "/a/b",
			raw:         "/a/b/c.yaml,  ,/a/b/d.yaml,",
			rawWindows:  `C:\a\b\c.yaml,  ,C:\a\b\d.yaml,`,
			want:        []string{"c.yaml", "d.yaml"},
			wantWindows: []string{`c.yaml`, `d.yaml`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				got := composeConfigFiles(tt.workingDir, tt.rawWindows)
				assert.DeepEqual(t, got, tt.wantWindows)
			} else {
				got := composeConfigFiles(tt.workingDir, tt.raw)
				assert.DeepEqual(t, got, tt.want)
			}
		})
	}
}

func TestReconcile_dispatch(t *testing.T) {
	t.Parallel()

	t.Run("logs and does not error for an unknown kind", func(t *testing.T) {
		t.Parallel()
		r, _ := newReconciler(t)
		result, err := r.Reconcile(t.Context(), composeProjectRequest{
			Kind:           "SomeUnknownKind",
			NamespacedName: types.NamespacedName{Namespace: "rancher-desktop", Name: "whatever"},
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
	})

	t.Run("no-ops for ComposeProjectKind", func(t *testing.T) {
		t.Parallel()
		r, fakeClient := newReconciler(t)
		result, err := r.Reconcile(t.Context(), composeProjectRequest{
			Kind:           v1alpha1.ComposeProjectKind,
			NamespacedName: types.NamespacedName{Namespace: "rancher-desktop", Name: "whatever"},
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.updateCalls, 0)
		assert.Equal(t, fakeClient.statusUpdateCalls, 0)
	})

	t.Run("dispatches ContainerKind to reconcileFromResource", func(t *testing.T) {
		t.Parallel()
		container := newContainer("rancher-desktop", "c1", "moby", map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, fakeClient := newReconciler(t, container)

		result, err := r.Reconcile(t.Context(), composeProjectRequest{
			Kind:           v1alpha1.ContainerKind,
			NamespacedName: client.ObjectKeyFromObject(container),
			UID:            container.GetUID(),
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)
	})

	t.Run("dispatches ImageKind to reconcileFromResource", func(t *testing.T) {
		t.Parallel()
		image := newImage("rancher-desktop", "i1", "moby", map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, fakeClient := newReconciler(t, image)

		result, err := r.Reconcile(t.Context(), composeProjectRequest{
			Kind:           v1alpha1.ImageKind,
			NamespacedName: client.ObjectKeyFromObject(image),
			UID:            image.GetUID(),
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)
	})

	t.Run("dispatches VolumeKind to reconcileFromResource", func(t *testing.T) {
		t.Parallel()
		volume := newVolume("rancher-desktop", "v1", "moby", map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, fakeClient := newReconciler(t, volume)

		result, err := r.Reconcile(t.Context(), composeProjectRequest{
			Kind:           v1alpha1.VolumeKind,
			NamespacedName: client.ObjectKeyFromObject(volume),
			UID:            volume.GetUID(),
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.createCalls, 1)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)
	})
}

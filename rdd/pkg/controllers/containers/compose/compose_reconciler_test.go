// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
)

// spyingClient wraps a client.Client to count Create/Update/status-Update
// calls, so tests can assert on idempotency (i.e. that a reconcile that
// changes nothing does not attempt a redundant write) and on whether a
// Compose was created. Writes show up as plain Create/Update calls plus
// Status().Update() calls.
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
		// "found" vs "not found" objects behaves the same way it would
		// against a real cluster.
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

// membersIndexFunc builds a client.IndexerFunc for the ".status.members[*].name"
// client-side index, mirroring what is set up in SetupWithManager.
func membersIndexFunc(obj client.Object) []string {
	compose, ok := obj.(*v1alpha1.Compose)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(compose.Status.Members))
	for _, member := range compose.Status.Members {
		out = append(out, member.Name)
	}
	return out
}

// newReconciler builds a reconciler backed by a fake client seeded with objs.
// fakeCommandExecutor returns a commandExecutor stub for tests that don't
// exercise the `docker compose up`/`down` process-spawning paths, so that
// reconciler fields can be non-nil without accidentally invoking real
// processes.
func fakeCommandExecutor(t *testing.T) commandExecutor {
	t.Helper()
	return func(_ context.Context, _, _ string, _ ...string) (command, error) {
		//nolint:forbidigo // t.Fatal because this should never be reached.
		t.Fatal("unexpected attempt to run a command in this test")
		return nil, nil
	}
}

func newReconciler(t *testing.T, objs ...client.Object) (*reconciler, *spyingClient) {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, v1alpha1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.Compose{}).
		WithObjects(objs...).
		WithIndex(&v1alpha1.Compose{}, ".status.members[*].name", membersIndexFunc)

	fakeClient := &spyingClient{Client: builder.Build()}

	return &reconciler{
		ctx:          context.Background(),
		Client:       fakeClient,
		procs:        &processTracker{executor: fakeCommandExecutor(t)},
		completionCh: make(chan event.TypedGenericEvent[*v1alpha1.Compose], 16),
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

// reconcileKind runs the reconciler for the given kind/key/uid, mirroring how
// a watch event would enqueue a composeRequest.
func reconcileKind(r *reconciler, kind string, key types.NamespacedName, uid types.UID) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), composeRequest{
		Kind:           kind,
		NamespacedName: key,
		UID:            uid,
	})
}

// testReconcileResource runs the battery of subtests shared between
// TestReconcileContainer, TestReconcileImage, and TestReconcileVolume, since
// they're all reconciled identically once fetched: each is keyed off the same
// com.docker.compose.project* labels via the shared reconcileFromResource.
// newResource builds a resource of type T with the given k8s namespace/name,
// docker-context namespace, and docker labels. kind is the Kind string used
// in composeRequest (e.g. "Container"/"Image"/"Volume").
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

		var compose v1alpha1.Compose
		err = r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &compose)
		assert.Assert(t, apierrors.IsNotFound(err), "expected no Compose to be created, got err=%v", err)
	})

	t.Run("creates a Compose with just the identity when only the project and config-hash labels are set", func(t *testing.T) {
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

		var compose v1alpha1.Compose
		err = r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &compose)
		assert.NilError(t, err)
		assert.Equal(t, compose.Status.Namespace, dockerNamespace)
		assert.Equal(t, compose.Status.Name, "myproject")
		assert.Equal(t, compose.Status.WorkingDir, "")
		assert.Equal(t, len(compose.Status.Configs), 0)
		assert.DeepEqual(t, compose.Status.Members, []v1alpha1.ComposeMember{
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

		var compose v1alpha1.Compose
		assert.NilError(t, r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &compose))
		assert.Equal(t, compose.Status.WorkingDir, "/home/user/myproject")
		// The first two files are under workingDir, so they become relative;
		// the third is outside workingDir, so it is kept as an absolute path.
		assert.DeepEqual(t, compose.Status.Configs, []string{
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
		// Create is attempted every reconcile, but the second attempt is a
		// no-op (AlreadyExists is ignored) since the object already exists;
		// the status is unconditionally re-applied on every reconcile
		// regardless of whether anything changed.
		assert.Equal(t, fakeClient.createCalls, 2)
		assert.Equal(t, fakeClient.updateCalls, 0)
		assert.Equal(t, fakeClient.statusUpdateCalls, 2)
	})

	t.Run(fmt.Sprintf("removes membership when a labeled %s is deleted", lowerKind), func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, fakeClient := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)

		assert.NilError(t, r.Delete(t.Context(), resource))
		fakeClient.statusUpdateCalls = 0

		_, err = reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, fakeClient.statusUpdateCalls, 1)

		var compose v1alpha1.Compose
		assert.NilError(t, r.Get(t.Context(), types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}, &compose))
		assert.Equal(t, len(compose.Status.Members), 0)
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

	type testCase struct {
		raw        string
		workingDir string
		want       []string
	}

	tests := []struct {
		name    string
		unix    testCase
		windows testCase
	}{
		{
			name: "empty input yields nil",
		},
		{
			name:    "unknown workingDir keeps paths unchanged",
			unix:    testCase{workingDir: "/a/b", raw: "/c/d.yaml,/e/f.yaml", want: []string{"/c/d.yaml", "/e/f.yaml"}},
			windows: testCase{workingDir: `C:\a\b`, raw: `C:\c\d.yaml,C:\e\f.yaml`, want: []string{`C:\c\d.yaml`, `C:\e\f.yaml`}},
		},
		{
			name:    "paths under workingDir become relative",
			unix:    testCase{workingDir: "/a/b", raw: "/a/b/c.yaml,/a/b/sub/d.yaml", want: []string{"c.yaml", "sub/d.yaml"}},
			windows: testCase{workingDir: `C:\a\b`, raw: `C:\a\b\c.yaml,C:\a\b\sub\d.yaml`, want: []string{`c.yaml`, `sub\d.yaml`}},
		},
		{
			name:    "a path outside workingDir is kept absolute",
			unix:    testCase{workingDir: "/a/b", raw: "/a/b/c.yaml,/somewhere/else/d.yaml", want: []string{"c.yaml", "/somewhere/else/d.yaml"}},
			windows: testCase{workingDir: `C:\a\b`, raw: `C:\a\b\c.yaml,C:\somewhere\else\d.yaml`, want: []string{`c.yaml`, `C:\somewhere\else\d.yaml`}},
		},
		{
			name:    "empty paths are ignored",
			unix:    testCase{workingDir: "/a/b", raw: "/a/b/c.yaml,  ,/a/b/d.yaml,", want: []string{"c.yaml", "d.yaml"}},
			windows: testCase{workingDir: `C:\a\b`, raw: `C:\a\b\c.yaml,  ,C:\a\b\d.yaml,`, want: []string{`c.yaml`, `d.yaml`}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				got := composeConfigFiles(tt.windows.workingDir, tt.windows.raw)
				assert.DeepEqual(t, got, tt.windows.want)
			} else {
				got := composeConfigFiles(tt.unix.workingDir, tt.unix.raw)
				assert.DeepEqual(t, got, tt.unix.want)
			}
		})
	}
}

func TestReconcile_dispatch(t *testing.T) {
	t.Parallel()

	t.Run("logs and does not error for an unknown kind", func(t *testing.T) {
		t.Parallel()
		r, _ := newReconciler(t)
		result, err := r.Reconcile(t.Context(), composeRequest{
			Kind:           "SomeUnknownKind",
			NamespacedName: types.NamespacedName{Namespace: "rancher-desktop", Name: "whatever"},
		})
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
	})

	t.Run("no-ops for ComposeKind", func(t *testing.T) {
		t.Parallel()
		r, fakeClient := newReconciler(t)
		result, err := r.Reconcile(t.Context(), composeRequest{
			Kind:           v1alpha1.ComposeKind,
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

		result, err := r.Reconcile(t.Context(), composeRequest{
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

		result, err := r.Reconcile(t.Context(), composeRequest{
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

		result, err := r.Reconcile(t.Context(), composeRequest{
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

// newCompose builds a Compose object with the given members, ready to
// exercise the finalizer/HasMembers/reap branches of reconcileCompose.
func newCompose(namespace, name string, members []v1alpha1.ComposeMember) *v1alpha1.Compose {
	return &v1alpha1.Compose{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			UID:             types.UID(name + "-uid"),
			ResourceVersion: "1",
		},
		Status: v1alpha1.ComposeStatus{
			Namespace: "moby",
			Name:      name,
			Members:   members,
		},
	}
}

func TestReconcileCompose_Finalizer(t *testing.T) {
	t.Parallel()

	t.Run("adds the cleanup finalizer to a non-deleted Compose", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		r, _ := newReconciler(t, compose)
		key := client.ObjectKeyFromObject(compose)

		_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)

		var latest v1alpha1.Compose
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		assert.Assert(t, base.HasCleanupFinalizer(&latest))
	})
}

func TestReconcileCompose_Reaping(t *testing.T) {
	t.Parallel()

	t.Run("deletes the Compose once HasMembers has been False for longer than reapDelay", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		apimeta.SetStatusCondition(&compose.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ComposeConditionHasMembers,
			Status:             metav1.ConditionFalse,
			Reason:             v1alpha1.ComposeHasMembersReasonDeleted,
			LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * defaultReapDelay)),
		})
		compose.Finalizers = []string{base.CleanupFinalizerName}
		r, _ := newReconciler(t, compose)
		r.procs = &processTracker{executor: fakeProcessCommandExecutor(func([]string) (error, bool) { return nil, false })}
		key := client.ObjectKeyFromObject(compose)

		// Reaping first marks the object for deletion (docker compose down
		// then runs to remove the finalizer); reconcile until it's gone,
		// waiting for the background process to finish before each retry.
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
			assert.NilError(t, err)

			err = r.Get(t.Context(), key, &v1alpha1.Compose{})
			if apierrors.IsNotFound(err) {
				break
			}
			assert.NilError(t, err)

			if _, ok := r.procs.get(compose.GetUID()); ok {
				waitForProcessFinished(t, r.procs, compose.GetUID(), 5*time.Second)
			}
		}

		err := r.Get(t.Context(), key, &v1alpha1.Compose{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected Compose to have been deleted, got: %v", err)
	})

	t.Run("requeues instead of deleting while HasMembers=False has not persisted for reapDelay", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		apimeta.SetStatusCondition(&compose.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ComposeConditionHasMembers,
			Status:             metav1.ConditionFalse,
			Reason:             v1alpha1.ComposeHasMembersReasonDeleted,
			LastTransitionTime: metav1.Now(),
		})
		compose.Finalizers = []string{base.CleanupFinalizerName}
		r, _ := newReconciler(t, compose)
		key := client.ObjectKeyFromObject(compose)

		result, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)
		assert.Equal(t, result.RequeueAfter, defaultReapDelay/2)

		assert.NilError(t, r.Get(t.Context(), key, &v1alpha1.Compose{}))
	})

	t.Run("does not delete the Compose while it has members", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", []v1alpha1.ComposeMember{{Name: "Container/c1", UID: "c1"}})
		apimeta.SetStatusCondition(&compose.Status.Conditions, metav1.Condition{
			Type:   v1alpha1.ComposeConditionHasMembers,
			Status: metav1.ConditionTrue,
			Reason: v1alpha1.ComposeHasMembersReasonFound,
		})
		compose.Finalizers = []string{base.CleanupFinalizerName}
		r, _ := newReconciler(t, compose)
		key := client.ObjectKeyFromObject(compose)

		_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)

		assert.NilError(t, r.Get(t.Context(), key, &v1alpha1.Compose{}))
	})
}

func TestReconcileCompose_Delete(t *testing.T) {
	t.Parallel()

	t.Run("runs docker compose down and removes the finalizer once it succeeds", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		compose.Finalizers = []string{base.CleanupFinalizerName}
		compose.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		r, _ := newReconciler(t, compose)
		r.procs = &processTracker{executor: fakeProcessCommandExecutor(func([]string) (error, bool) { return nil, false })}
		key := client.ObjectKeyFromObject(compose)

		// First reconcile starts the process; wait for it to finish, then
		// subsequent reconciles remove the finalizer and let deletion proceed.
		_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)
		waitForProcessFinished(t, r.procs, compose.GetUID(), 5*time.Second)

		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
			assert.NilError(t, err)

			err = r.Get(t.Context(), key, &v1alpha1.Compose{})
			if apierrors.IsNotFound(err) {
				break
			}
			assert.NilError(t, err)
		}

		err = r.Get(t.Context(), key, &v1alpha1.Compose{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected Compose to have been deleted, got: %v", err)
	})

	t.Run("removes the finalizer even if docker compose down fails", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		compose.Finalizers = []string{base.CleanupFinalizerName}
		compose.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		r, _ := newReconciler(t, compose)
		r.procs = &processTracker{executor: fakeProcessCommandExecutor(func([]string) (error, bool) {
			return errors.New("exit status 1"), false
		})}
		key := client.ObjectKeyFromObject(compose)

		_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)
		waitForProcessFinished(t, r.procs, compose.GetUID(), 5*time.Second)

		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
			assert.NilError(t, err)

			err = r.Get(t.Context(), key, &v1alpha1.Compose{})
			if apierrors.IsNotFound(err) {
				break
			}
			assert.NilError(t, err)
		}

		err = r.Get(t.Context(), key, &v1alpha1.Compose{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected Compose to have been deleted despite the failure, got: %v", err)
	})

	t.Run("is a no-op once the finalizer is already gone", func(t *testing.T) {
		t.Parallel()
		compose := newCompose("rancher-desktop", "myproject", nil)
		compose.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		// The fake client refuses to create an object with a deletionTimestamp
		// but no finalizers at all, so keep an unrelated one present; only the
		// cleanup finalizer's absence is what reconcileCompose actually checks.
		compose.Finalizers = []string{"unrelated.example.com/finalizer"}
		r, fakeClient := newReconciler(t, compose)
		key := client.ObjectKeyFromObject(compose)

		result, err := reconcileKind(r, v1alpha1.ComposeKind, key, compose.GetUID())
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
		assert.Equal(t, fakeClient.writes(), 0)
	})

	t.Run("aborts a stale process for a UID that no longer exists", func(t *testing.T) {
		t.Parallel()
		r, _ := newReconciler(t)
		cmd := newFakeCommand([]string{"compose", "down"})
		const staleUID = types.UID("stale-uid")
		r.procs = &processTracker{procs: map[types.UID]processState{staleUID: {cmd: cmd}}}

		_, err := reconcileKind(r, v1alpha1.ComposeKind,
			types.NamespacedName{Namespace: "rancher-desktop", Name: "gone"}, staleUID)
		assert.NilError(t, err)

		select {
		case <-cmd.killed:
		default:
			assert.Assert(t, false, "expected the stale process to have been killed")
		}

		_, ok := r.procs.get(staleUID)
		assert.Assert(t, !ok, "stale process state should be removed")
	})
}

func TestUpRequestReconcile(t *testing.T) {
	t.Parallel()

	t.Run("transitions Running to Succeeded when the compose command succeeds", func(t *testing.T) {
		t.Parallel()
		upReq := newComposeUpRequest("rancher-desktop", "myproject")
		r, _ := newUpRequestReconciler(t, upReq)
		r.procs = &processTracker{executor: fakeProcessCommandExecutor(func([]string) (error, bool) { return nil, false })}
		key := client.ObjectKeyFromObject(upReq)

		for range 5 {
			_, err := r.Reconcile(t.Context(), upRequest{NamespacedName: key, UID: upReq.GetUID()})
			assert.NilError(t, err)

			var latest v1alpha1.ComposeUpRequest
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if apimeta.IsStatusConditionTrue(latest.Status.Conditions, v1alpha1.ComposeUpRequestConditionSettled) {
				break
			}
			if state, ok := r.procs.get(upReq.GetUID()); ok && !state.finished {
				waitForProcessFinished(t, r.procs, upReq.GetUID(), 5*time.Second)
			}
		}

		var latest v1alpha1.ComposeUpRequest
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeUpRequestConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeUpRequestSettledReasonSucceeded)

		_, ok := r.procs.get(upReq.GetUID())
		assert.Assert(t, !ok, "process state should be removed once handled")
	})

	t.Run("transitions Running to Errored when the compose command fails", func(t *testing.T) {
		t.Parallel()
		upReq := newComposeUpRequest("rancher-desktop", "myproject")
		r, _ := newUpRequestReconciler(t, upReq)
		r.procs = &processTracker{executor: fakeProcessCommandExecutor(func([]string) (error, bool) {
			return errors.New("exit status 1"), false
		})}
		key := client.ObjectKeyFromObject(upReq)

		for range 5 {
			_, err := r.Reconcile(t.Context(), upRequest{NamespacedName: key, UID: upReq.GetUID()})
			assert.NilError(t, err)

			var latest v1alpha1.ComposeUpRequest
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if apimeta.IsStatusConditionTrue(latest.Status.Conditions, v1alpha1.ComposeUpRequestConditionSettled) {
				break
			}
			if state, ok := r.procs.get(upReq.GetUID()); ok && !state.finished {
				waitForProcessFinished(t, r.procs, upReq.GetUID(), 5*time.Second)
			}
		}

		var latest v1alpha1.ComposeUpRequest
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeUpRequestConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeUpRequestSettledReasonErrored)
	})

	t.Run("reaps the request once Settled has been True for longer than reapDelay", func(t *testing.T) {
		t.Parallel()
		upReq := newComposeUpRequest("rancher-desktop", "myproject")
		apimeta.SetStatusCondition(&upReq.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ComposeUpRequestConditionSettled,
			Status:             metav1.ConditionTrue,
			Reason:             v1alpha1.ComposeUpRequestSettledReasonSucceeded,
			LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * defaultReapDelay)),
		})
		r, _ := newUpRequestReconciler(t, upReq)
		key := client.ObjectKeyFromObject(upReq)

		_, err := r.Reconcile(t.Context(), upRequest{NamespacedName: key, UID: upReq.GetUID()})
		assert.NilError(t, err)

		err = r.Get(t.Context(), key, &v1alpha1.ComposeUpRequest{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected ComposeUpRequest to have been reaped, got: %v", err)
	})

	t.Run("requeues instead of reaping while Settled=True has not persisted for reapDelay", func(t *testing.T) {
		t.Parallel()
		upReq := newComposeUpRequest("rancher-desktop", "myproject")
		apimeta.SetStatusCondition(&upReq.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ComposeUpRequestConditionSettled,
			Status:             metav1.ConditionTrue,
			Reason:             v1alpha1.ComposeUpRequestSettledReasonSucceeded,
			LastTransitionTime: metav1.Now(),
		})
		r, _ := newUpRequestReconciler(t, upReq)
		key := client.ObjectKeyFromObject(upReq)

		result, err := r.Reconcile(t.Context(), upRequest{NamespacedName: key, UID: upReq.GetUID()})
		assert.NilError(t, err)
		assert.Equal(t, result.RequeueAfter, defaultReapDelay/2)

		assert.NilError(t, r.Get(t.Context(), key, &v1alpha1.ComposeUpRequest{}))
	})

	t.Run("aborts a running process once the request no longer exists", func(t *testing.T) {
		t.Parallel()
		r, _ := newUpRequestReconciler(t)
		cmd := newFakeCommand([]string{"compose", "up"})
		const uid = types.UID("gone-uid")
		r.procs = &processTracker{procs: map[types.UID]processState{uid: {cmd: cmd}}}

		_, err := r.Reconcile(t.Context(), upRequest{
			NamespacedName: types.NamespacedName{Namespace: "rancher-desktop", Name: "gone"},
			UID:            uid,
		})
		assert.NilError(t, err)

		select {
		case <-cmd.killed:
		default:
			assert.Assert(t, false, "expected the tracked process to have been killed")
		}

		_, ok := r.procs.get(uid)
		assert.Assert(t, !ok, "process state should be removed once the object is gone")
	})
}

// newComposeUpRequest builds a ComposeUpRequest with a correctly-computed
// metadata.name, ready to be reconciled.
func newComposeUpRequest(namespace, name string) *v1alpha1.ComposeUpRequest {
	return &v1alpha1.ComposeUpRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:            generateProjectName(namespace, name),
			ResourceVersion: strconv.FormatUint(1, 10),
		},
		Spec: v1alpha1.ComposeUpRequestSpec{
			Namespace: namespace,
			Name:      name,
		},
	}
}

// newUpRequestReconciler builds an upRequestReconciler backed by a fake
// client seeded with objs, mirroring newReconciler's role for reconciler.
func newUpRequestReconciler(t *testing.T, objs ...client.Object) (*upRequestReconciler, *spyingClient) {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, v1alpha1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ComposeUpRequest{}).
		WithObjects(objs...)

	fakeClient := &spyingClient{Client: builder.Build()}

	return &upRequestReconciler{
		ctx:          context.Background(),
		Client:       fakeClient,
		procs:        &processTracker{executor: fakeCommandExecutor(t)},
		completionCh: make(chan event.TypedGenericEvent[*v1alpha1.ComposeUpRequest], 16),
	}, fakeClient
}

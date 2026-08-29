// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

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
		ctx:              t.Context(),
		Client:           fakeClient,
		Scheme:           scheme,
		procCompletionCh: make(chan event.TypedGenericEvent[processCompletionEvent], 16),
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

	t.Run(fmt.Sprintf("removes the %s's UID and marks HasMembers=False once it has no remaining members", lowerKind), func(t *testing.T) {
		t.Parallel()
		resource := newResource(k8sNamespace, "r1", dockerNamespace, map[string]string{
			composeProjectLabel:    "myproject",
			composeConfigHashLabel: "somehash",
		})
		r, _ := newReconciler(t, resource)

		_, err := reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)

		projectKey := types.NamespacedName{
			Namespace: k8sNamespace,
			Name:      generateProjectName(dockerNamespace, "myproject"),
		}
		var project v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), projectKey, &project))

		// Delete the resource, and reconcile it.
		assert.NilError(t, r.Delete(t.Context(), resource))
		_, err = reconcileKind(r, kind, client.ObjectKeyFromObject(resource), resource.GetUID())
		assert.NilError(t, err)

		// We may take a few iterations to reach steady state.  Keep reconciling
		// until we reach the desired state.
		for range 5 {
			_, err = reconcileKind(r, v1alpha1.ComposeProjectKind, projectKey, project.GetUID())
			assert.NilError(t, err)
			assert.NilError(t, r.Get(t.Context(), projectKey, &project))
			if apimeta.IsStatusConditionFalse(project.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers) {
				break
			}
		}

		// We are at the target state; ensure everything is as we expect.
		assert.NilError(t, r.Get(t.Context(), projectKey, &project))
		assert.Equal(t, len(project.Status.Members), 0)
		assert.Assert(
			t,
			apimeta.IsStatusConditionFalse(project.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers),
			"expected HasMembers=False, got %v", apimeta.FindStatusCondition(project.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers))
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

// fakeCommand is an in-memory test double for the command interface. It never
// spawns a real process: wait() blocks until either the test supplies a
// result via done, or kill() is called (which makes wait() return errKilled).
type fakeCommand struct {
	argv     []string
	done     chan error
	killed   chan struct{}
	killOnce sync.Once
	// outputStr is returned by output(); tests may set it before the command
	// completes to simulate captured stderr output.
	outputStr string
}

// errKilled is returned by fakeCommand.wait() when kill() won the race
// against a supplied result.
var errKilled = errors.New("signal: killed")

func newFakeCommand(argv []string) *fakeCommand {
	return &fakeCommand{
		argv:   argv,
		done:   make(chan error, 1),
		killed: make(chan struct{}),
	}
}

// args implements [command].
func (c *fakeCommand) args() []string { return c.argv }

// kill implements [command]. It causes a pending wait() to return errKilled.
func (c *fakeCommand) kill(context.Context) error {
	c.killOnce.Do(func() { close(c.killed) })
	return nil
}

// output implements [command].
func (c *fakeCommand) output() string { return c.outputStr }

// wait implements [command].
func (c *fakeCommand) wait(ctx context.Context) error {
	select {
	case err := <-c.done:
		return err
	case <-c.killed:
		return errKilled
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakeCommandExecutor returns a commandExecutor that produces fakeCommands
// instead of spawning real processes. resultFor is called with the command's
// args (e.g. []string{"compose", "up"}) to decide the outcome: if block is
// true, the returned command does not complete until kill() is called (or
// the test sends to its done channel directly); otherwise it completes
// immediately with result.
func fakeCommandExecutor(resultFor func(args []string) (result error, block bool)) commandExecutor {
	return func(_ context.Context, _, name string, args ...string) (command, error) {
		c := newFakeCommand(append([]string{name}, args...))
		result, block := resultFor(args)
		if !block {
			c.done <- result
		}
		return c, nil
	}
}

// waitForProcessFinished polls r.procs[uid] until its processState is marked
// finished (i.e. the background `docker compose` command has exited), or
// fails the test after timeout.
func waitForProcessFinished(t *testing.T, r *reconciler, uid types.UID, timeout time.Duration) processState {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		r.procsLock.Lock()
		state, ok := r.procs[uid]
		r.procsLock.Unlock()
		assert.Assert(t, ok, "process state for %s not found", uid)
		if state.finished {
			return state
		}
		if time.Now().After(deadline) {
			assert.Assert(t, false, "timed out waiting for process to finish for %s", uid)
		}
		time.Sleep(time.Millisecond)
	}
}

// newComposeProjectForAction builds a ComposeProject requesting the given
// action, ready to be reconciled.
func newComposeProjectForAction(namespace, name string, uid types.UID, action v1alpha1.ComposeProjectAction) *v1alpha1.ComposeProject {
	return &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			UID:             uid,
			ResourceVersion: strconv.FormatUint(rand.Uint64(), 10),
			Annotations: map[string]string{
				v1alpha1.AnnotationAction: string(action),
			},
		},
		Spec: v1alpha1.ComposeProjectSpec{
			Namespace: "moby",
			Name:      name,
		},
	}
}

func TestReconcileProject_Up(t *testing.T) {
	t.Parallel()

	t.Run("transitions Starting to Started when the compose command succeeds", func(t *testing.T) {
		t.Parallel()

		const namespace = "rancher-desktop"
		project := newComposeProjectForAction(namespace, "myproject", "project-uid", v1alpha1.ComposeProjectActionUp)
		r, _ := newReconciler(t, project)
		r.execCommand = fakeCommandExecutor(func([]string) (error, bool) { return nil, false })
		key := client.ObjectKeyFromObject(project)

		// Reconcile until the action was accepted (annotation removed)
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if latest.Annotations[v1alpha1.AnnotationAction] == "" {
				break
			}
		}

		var latest v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		assert.Equal(t, latest.Annotations[v1alpha1.AnnotationAction], "")
		assert.Assert(t, latest.Status.LastAction != nil)
		assert.Equal(t, latest.Status.LastAction.Action, v1alpha1.ComposeProjectActionUp)
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonStarting)
		assert.Equal(t, condition.Status, metav1.ConditionFalse)

		waitForProcessFinished(t, r, project.GetUID(), 5*time.Second)

		// Once the process completes, the next reconcile (triggered by
		// procCompletionCh in production) transitions to Started.
		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
		assert.NilError(t, err)

		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition = apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonStarted)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)

		// The process state should have been cleaned up.
		r.procsLock.Lock()
		_, ok := r.procs[project.GetUID()]
		r.procsLock.Unlock()
		assert.Assert(t, !ok, "process state should be removed once handled")
	})

	t.Run("transitions Starting to Errored when the compose command fails", func(t *testing.T) {
		t.Parallel()

		const namespace = "rancher-desktop"
		project := newComposeProjectForAction(namespace, "myproject", "project-uid", v1alpha1.ComposeProjectActionUp)
		r, _ := newReconciler(t, project)
		r.execCommand = fakeCommandExecutor(func([]string) (error, bool) {
			return errors.New("exit status 1"), false
		})
		key := client.ObjectKeyFromObject(project)

		// Reconcile until the action was accepted (annotation removed)
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if latest.Annotations[v1alpha1.AnnotationAction] == "" {
				break
			}
		}

		waitForProcessFinished(t, r, project.GetUID(), 5*time.Second)

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
		assert.NilError(t, err)

		var latest v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonErrored)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Assert(t, cmp.Contains(condition.Message, "failed to start"))
	})
}

func TestReconcileProject_Down(t *testing.T) {
	t.Parallel()

	t.Run("transitions Stopping to Stopped when the compose command succeeds", func(t *testing.T) {
		t.Parallel()

		const namespace = "rancher-desktop"
		project := newComposeProjectForAction(namespace, "myproject", "project-uid", v1alpha1.ComposeProjectActionDown)
		r, _ := newReconciler(t, project)
		r.execCommand = fakeCommandExecutor(func([]string) (error, bool) { return nil, false })
		key := client.ObjectKeyFromObject(project)

		// The reconciler should accept the action: the annotation should be remmoved,
		// and the requested action should be underway.
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if latest.Annotations[v1alpha1.AnnotationAction] == "" {
				break
			}
		}
		// Check that it's at the desired state.
		var latest v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		assert.Equal(t, latest.Annotations[v1alpha1.AnnotationAction], "")
		assert.Assert(t, latest.Status.LastAction != nil)
		assert.Equal(t, latest.Status.LastAction.Action, v1alpha1.ComposeProjectActionDown)
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonStopping)
		assert.Equal(t, condition.Status, metav1.ConditionFalse)

		waitForProcessFinished(t, r, project.GetUID(), 5*time.Second)

		// The process completed; we should transition to Stopped.
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if apimeta.IsStatusConditionTrue(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled) {
				break
			}
		}

		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition = apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonStopped)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)

		// The process state should have been cleaned up.
		r.procsLock.Lock()
		_, ok := r.procs[project.GetUID()]
		r.procsLock.Unlock()
		assert.Assert(t, !ok, "process state should be removed once handled")
	})

	t.Run("transitions Stopping to Errored when the compose command fails", func(t *testing.T) {
		t.Parallel()

		const namespace = "rancher-desktop"
		project := newComposeProjectForAction(namespace, "myproject", "project-uid", v1alpha1.ComposeProjectActionDown)
		r, _ := newReconciler(t, project)
		r.execCommand = fakeCommandExecutor(func([]string) (error, bool) {
			return errors.New("exit status 1"), false
		})
		key := client.ObjectKeyFromObject(project)

		// Reconcile until the action was accepted (annotation removed)
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if latest.Annotations[v1alpha1.AnnotationAction] == "" {
				break
			}
		}

		waitForProcessFinished(t, r, project.GetUID(), 5*time.Second)

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
		assert.NilError(t, err)

		var latest v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		condition := apimeta.FindStatusCondition(latest.Status.Conditions, v1alpha1.ComposeProjectConditionSettled)
		assert.Assert(t, condition != nil)
		assert.Equal(t, condition.Reason, v1alpha1.ComposeProjectSettledReasonErrored)
		assert.Equal(t, condition.Status, metav1.ConditionTrue)
		assert.Assert(t, cmp.Contains(condition.Message, "failed to stop"))
	})
}

// newProjectWithMembers builds a ComposeProject with no pending action,
// ready to exercise the `HasMembers`-driven recompute/reap branches of
// reconcileProject.
func newProjectWithMembers(name string, members []v1alpha1.ComposeProjectMember) *v1alpha1.ComposeProject {
	return &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "rancher-desktop",
			UID:             types.UID(name + "-uid"),
			ResourceVersion: strconv.FormatUint(rand.Uint64(), 10),
		},
		Spec:   v1alpha1.ComposeProjectSpec{Namespace: "moby", Name: name},
		Status: v1alpha1.ComposeProjectStatus{Members: members},
	}
}

func TestReconcileProject_Reaping(t *testing.T) {
	t.Parallel()

	t.Run("deletes the project once HasMembers is already False", func(t *testing.T) {
		t.Parallel()

		project := newProjectWithMembers("myproject", nil)
		apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:   v1alpha1.ComposeProjectConditionHasMembers,
			Status: metav1.ConditionFalse,
			Reason: v1alpha1.ComposeProjectHasMembersReasonDeleted,
		})
		r, _ := newReconciler(t, project)
		key := client.ObjectKeyFromObject(project)

		// It takes a while to reach steady state.
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			err = r.Get(t.Context(), key, &v1alpha1.ComposeProject{})
			if apierrors.IsNotFound(err) {
				break
			}
			assert.NilError(t, err)
		}

		err := r.Get(t.Context(), key, &v1alpha1.ComposeProject{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected ComposeProject to be reaped, got: %v", err)
	})

	t.Run("does not delete the project while HasMembers is Unknown", func(t *testing.T) {
		t.Parallel()

		// Unknown indicates an action is in progress; the project must not be
		// reaped out from under it even though it currently has no members.
		project := newProjectWithMembers("myproject", nil)
		apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:   v1alpha1.ComposeProjectConditionHasMembers,
			Status: metav1.ConditionUnknown,
			Reason: v1alpha1.ComposeProjectHasMembersReasonCalculating,
		})
		r, _ := newReconciler(t, project)
		key := client.ObjectKeyFromObject(project)

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
		assert.NilError(t, err)

		assert.NilError(t, r.Get(t.Context(), key, &v1alpha1.ComposeProject{}))
	})

	t.Run("does not delete the project while it has members", func(t *testing.T) {
		t.Parallel()

		project := newProjectWithMembers("myproject", []v1alpha1.ComposeProjectMember{{Name: "Container/c1", UID: "c1"}})
		apimeta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:   v1alpha1.ComposeProjectConditionHasMembers,
			Status: metav1.ConditionTrue,
			Reason: v1alpha1.ComposeProjectHasMembersReasonFound,
		})
		r, _ := newReconciler(t, project)
		key := client.ObjectKeyFromObject(project)

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
		assert.NilError(t, err)

		assert.NilError(t, r.Get(t.Context(), key, &v1alpha1.ComposeProject{}))
	})

	t.Run("recomputes HasMembers to True once members are recorded", func(t *testing.T) {
		t.Parallel()

		project := newProjectWithMembers("myproject", []v1alpha1.ComposeProjectMember{{Name: "Container/c1", UID: "c1"}})
		r, _ := newReconciler(t, project)
		key := client.ObjectKeyFromObject(project)

		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			assert.NilError(t, r.Get(t.Context(), key, &latest))
			if apimeta.IsStatusConditionTrue(latest.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers) {
				break
			}
		}
		var latest v1alpha1.ComposeProject
		assert.NilError(t, r.Get(t.Context(), key, &latest))
		assert.Assert(t, apimeta.IsStatusConditionTrue(latest.Status.Conditions, v1alpha1.ComposeProjectConditionHasMembers))
	})

	t.Run("recomputes HasMembers to False, then reaps on the following reconcile", func(t *testing.T) {
		t.Parallel()

		// No condition set at all yet (e.g. a project that was auto-detected
		// but never had an explicit action run against it), and its last
		// member has just been removed.
		project := newProjectWithMembers("myproject", nil)
		r, _ := newReconciler(t, project)
		key := client.ObjectKeyFromObject(project)

		// Reconcile until we reach the desired state.
		for range 5 {
			_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, key, project.GetUID())
			assert.NilError(t, err)

			var latest v1alpha1.ComposeProject
			err = r.Get(t.Context(), key, &latest)
			if apierrors.IsNotFound(err) {
				break
			}
			assert.NilError(t, err)
		}

		err := r.Get(t.Context(), key, &v1alpha1.ComposeProject{})
		assert.Assert(t, apierrors.IsNotFound(err), "expected ComposeProject to be reaped, got: %v", err)
	})
}

func TestReconcileProject_Delete(t *testing.T) {
	t.Parallel()

	t.Run("kills a running process once the object is gone", func(t *testing.T) {
		t.Parallel()

		// The ComposeProject is intentionally never added to the fake client:
		// deletion is unconditional (no finalizer), so by the time the
		// reconciler observes it, the object itself has already been removed
		// from the API server; only the in-memory process state remains.
		project := newComposeProjectForAction("rancher-desktop", "myproject", "project-uid", v1alpha1.ComposeProjectActionUnset)
		r, _ := newReconciler(t)
		cmd := newFakeCommand([]string{"compose", "up"})
		r.procs = map[types.UID]processState{project.GetUID(): {cmd: cmd}}

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, client.ObjectKeyFromObject(project), project.GetUID())
		assert.NilError(t, err)

		select {
		case <-cmd.killed:
		default:
			assert.Assert(t, false, "expected the tracked process to have been killed")
		}

		r.procsLock.Lock()
		_, ok := r.procs[project.GetUID()]
		r.procsLock.Unlock()
		assert.Assert(t, !ok, "process state should be removed once the object is gone")
	})

	t.Run("does not attempt to kill an already-finished process", func(t *testing.T) {
		t.Parallel()

		project := newComposeProjectForAction("rancher-desktop", "myproject", "project-uid", v1alpha1.ComposeProjectActionUnset)
		r, _ := newReconciler(t)
		cmd := newFakeCommand([]string{"compose", "up"})
		r.procs = map[types.UID]processState{project.GetUID(): {cmd: cmd, finished: true}}

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, client.ObjectKeyFromObject(project), project.GetUID())
		assert.NilError(t, err)

		select {
		case <-cmd.killed:
			assert.Assert(t, false, "an already-finished process should not be killed")
		default:
		}

		r.procsLock.Lock()
		_, ok := r.procs[project.GetUID()]
		r.procsLock.Unlock()
		assert.Assert(t, !ok, "process state should be removed once the object is gone")
	})

	t.Run("is a no-op when there is no tracked process", func(t *testing.T) {
		t.Parallel()

		project := newComposeProjectForAction("rancher-desktop", "myproject", "project-uid", v1alpha1.ComposeProjectActionUnset)
		r, _ := newReconciler(t)

		result, err := reconcileKind(r, v1alpha1.ComposeProjectKind, client.ObjectKeyFromObject(project), project.GetUID())
		assert.NilError(t, err)
		assert.DeepEqual(t, result, ctrl.Result{})
	})

	t.Run("kills a running process for a stale UID when the object was deleted and recreated", func(t *testing.T) {
		t.Parallel()

		// Simulate a ComposeProject that was deleted and quickly recreated
		// with the same namespace/name but a new UID, before the stale
		// reconcile request (still carrying the old UID) was processed.
		recreated := newComposeProjectForAction("rancher-desktop", "myproject", "new-project-uid", v1alpha1.ComposeProjectActionUnset)
		r, _ := newReconciler(t, recreated)
		cmd := newFakeCommand([]string{"compose", "up"})
		const staleUID = types.UID("old-project-uid")
		r.procs = map[types.UID]processState{staleUID: {cmd: cmd}}

		_, err := reconcileKind(r, v1alpha1.ComposeProjectKind, client.ObjectKeyFromObject(recreated), staleUID)
		assert.NilError(t, err)

		select {
		case <-cmd.killed:
		default:
			assert.Assert(t, false, "expected the stale process to have been killed")
		}

		r.procsLock.Lock()
		_, staleOK := r.procs[staleUID]
		_, newOK := r.procs[recreated.GetUID()]
		r.procsLock.Unlock()
		assert.Assert(t, !staleOK, "stale process state should be removed")
		assert.Assert(t, !newOK, "the recreated project should not have inherited any process state")
	})
}

func TestRunComposeCommand_AbortsPreviouslyRunningCommand(t *testing.T) {
	t.Parallel()

	project := &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "myproject",
			Namespace:       "rancher-desktop",
			UID:             "project-uid",
			ResourceVersion: strconv.FormatUint(rand.Uint64(), 10),
		},
		Spec: v1alpha1.ComposeProjectSpec{Namespace: "moby", Name: "myproject"},
	}
	r, _ := newReconciler(t, project)
	r.execCommand = fakeCommandExecutor(func(args []string) (error, bool) {
		// The "up" command blocks until killed; "down" completes immediately.
		return nil, len(args) > 0 && args[len(args)-1] == "up"
	})

	_, err := r.runComposeCommand(t.Context(), project, "up")
	assert.NilError(t, err)

	r.procsLock.Lock()
	firstCmd, _ := r.procs[project.GetUID()].cmd.(*fakeCommand)
	r.procsLock.Unlock()
	assert.Assert(t, firstCmd != nil)

	// Starting a second command for the same project should kill the first.
	project.ResourceVersion = strconv.FormatUint(rand.Uint64(), 10)
	_, err = r.runComposeCommand(t.Context(), project, "down")
	assert.NilError(t, err)

	waitForProcessFinished(t, r, project.GetUID(), 5*time.Second)

	r.procsLock.Lock()
	secondCmd := r.procs[project.GetUID()].cmd
	r.procsLock.Unlock()
	assert.Assert(t, secondCmd != command(firstCmd), "expected a new process to replace the killed one")

	select {
	case <-firstCmd.killed:
	default:
		assert.Assert(t, false, "expected the first compose command to have been killed")
	}
}

func TestRunComposeCommand_PassesProjectIdentity(t *testing.T) {
	t.Parallel()

	project := &v1alpha1.ComposeProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "myproject",
			Namespace:       "rancher-desktop",
			UID:             "project-uid",
			ResourceVersion: strconv.FormatUint(rand.Uint64(), 10),
		},
		Spec: v1alpha1.ComposeProjectSpec{
			Namespace: "moby",
			Name:      "custom-project-name",
			Configs:   []string{"one.yaml", "two.yaml"},
		},
	}
	r, _ := newReconciler(t, project)

	var gotArgs []string
	r.execCommand = fakeCommandExecutor(func(args []string) (error, bool) {
		gotArgs = args
		return nil, false
	})

	_, err := r.runComposeCommand(t.Context(), project, "up")
	assert.NilError(t, err)
	assert.DeepEqual(t, gotArgs, []string{
		"compose", "--project-name", "custom-project-name",
		"--file", "one.yaml", "--file", "two.yaml", "up",
	})

	project.ResourceVersion = strconv.FormatUint(rand.Uint64(), 10)

	_, err = r.runComposeCommand(t.Context(), project, "down", "--remove-orphans", "--volumes")
	assert.NilError(t, err)
	assert.DeepEqual(t, gotArgs, []string{
		"compose", "--project-name", "custom-project-name",
		"--file", "one.yaml", "--file", "two.yaml", "down", "--remove-orphans", "--volumes",
	})
}

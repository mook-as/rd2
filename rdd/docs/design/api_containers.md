# Rancher Desktop Containers API

> [!CAUTION]
> The Rancher Desktop Containers API is still in the concept stage and the details
will need to be ironed out.

The Rancher Desktop Containers API mirrors the container engine state into
Kubernetes resources.  The engine controller connects to the container engine,
performs a full sync of containers, images, and volumes, then watches the engine
event stream for live updates.

All objects are in the `containers.rancherdesktop.io` API group.

All times are in RFC3339 format, per usual Kubernetes conventions.

## Terminology

Capitalized `Container`, `Image`, `Volume`, and `ContainerNamespace`
refer to the resource types in this API group. Lowercase `container`,
`image`, and `volume` refer to the underlying Docker engine objects.
The rdd LimaVM also runs k3s, so "k8s container" would be ambiguous —
this doc, code comments, and commit messages rely on capitalization
instead.

Where capitalization alone is ambiguous (sentence start, or prose that
mentions both the engine object and the resource), the resources are
called "Container mirrors", "Image mirrors", and "Volume mirrors", after
the engine controller's role. The code uses the same terminology: the
finalizer is `engine.rancherdesktop.io/mirror`, the cleanup
helper is `cleanupMirrorResources`, and the name helper is
`volumeMirrorName`.

When running `containerd`, the containerd namespace is listed as the `namespace`
label rather than re-using the Kubernetes namespace.  When running `dockerd`,
namespaces are not supported and we always use `moby` as the value for that label.

For the `*Request` resources, they use the `Complete` and `Failed` conditions to
express state; those are mutually exclusive (only one of the two can be set to
`True` at once).  Once either is set to `True`, the request object is considered
to be in a terminal state and will be removed after some timeout.  This will be
at least one minute (so the caller can read any data), but the precise timing is
unspecified.

This API is mainly for use by the Rancher Desktop front end; all other users are
strongly urged to use the relevant CLI or other API instead.

## Engine Mirroring

The engine controller (`pkg/controllers/app/engine/`) watches the `App` resource
for the `Running` condition.  When the VM is running with the `moby` backend,
the controller:

1. Connects to the Docker engine via the host socket.
2. Creates the `rancher-desktop` Kubernetes namespace and the `moby`
   `ContainerNamespace` resource.
3. Lists all Docker containers, images, and volumes and creates the
   corresponding `Container`, `Image`, and `Volume` mirrors.
4. Watches the Docker event stream for create, update, and delete events.

Containerd mirroring is not implemented yet; with `containerEngine.name=containerd`
the controller sets `ContainerEngineReady` to `True` with reason `NotApplicable`
and takes no mirroring action.

The controller sets the `ContainerEngineReady` condition on the `App` resource
to `True` after the initial sync completes.  Scripts can wait for readiness:

```sh
rdd ctl wait --for=condition=ContainerEngineReady=True app/app
```

When the VM shuts down or the container engine becomes unreachable, the
controller removes all mirror resources and sets `ContainerEngineReady` to
`False`.

### Finalizer lifecycle

Each mirror carries the `engine.rancherdesktop.io/mirror`
finalizer. A K8s-side delete triggers the finalizer handler, which
deletes the corresponding engine object and then strips the finalizer
so the mirror can be garbage-collected.

An engine-side delete (for example, `docker rm`) goes the other way:
the engine controller strips the finalizer and deletes the mirror
directly, without calling back into the engine.

## Namespaces

`ContainerNamespace` objects reflect the container engine namespaces.  This is
only useful when using the `containerd` backend; when using `dockerd`, the only
valid instance will have a name of `moby`, and it cannot be modified in any way.

`ContainerNamespace` objects only have the default metadata, since they
currently do not need anything else.

```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ContainerNamespace
metadata:
  name: k8s.io # `moby` when using the dockerd engine.
  namespace: rancher-desktop
```

## Containers

`Container` objects reflect the running containers. The spec is empty:
the engine owns the observed state in `status`, and the user drives the
container through the action annotation described below.

```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: Container
metadata:
  name: 8eb6f2cf72b6616aa743cf9187f350af84c9749dab65474db2530f26745d2ef3 # container ID
  namespace: rancher-desktop
  annotations:
    # Request a one-shot action. See "Container actions" below.
    containers.rancherdesktop.io/action: pause
spec: {}
status:
  name: magical_gates
  namespace: k8s.io # Refers to a `ContainerNamespace` object
  path: /bin/sh
  args: [-c, 'sleep inf']
  # Image ID; corresponds to `Image` object's `.status.id` field.
  image: "sha256:999adf320e40662dc96119a14f07459af9959a081d10ccab7c405257030ab96b"
  ports:
    - name: 80/tcp
      bindings:
      - hostIP: 0.0.0.0
        hostPort: 32768
      - hostIP: '::'
        hostPort: 32768
  labels:
    org.opensuse.base.vendor: openSUSE Project
  status: running
  pid: 5059
  exitCode: 0
  error: ""
  createdAt: "2025-11-22T00:34:07.153640108Z" # Time
  startedAt: "2025-12-09T22:05:27.774478174Z"
  finishedAt: "2025-11-29T00:35:49.155454569Z"
  conditions:
  - type: Running
    status: True
  - type: Paused
    status: False
  - type: Restarting
    status: False
  - type: OOMKilled
    status: False
  - type: Dead
    status: False
  # Outcome of the most recent action. Persists until the next action
  # overwrites it, regardless of any observable state changes (e.g. a
  # direct `docker stop`) in between. The `error` field is set only
  # when `state` is `Failed`.
  lastAction:
    action: pause
    state: Succeeded
    observedAt: "2026-04-15T10:30:00Z"
    completedAt: "2026-04-15T10:30:00Z"
```

### Container state

`status.status` always reflects the actual Docker state. The engine
writes status on every sync and never writes the container's spec, so
Docker's restart policy, out-of-band `docker start`, and explicit
actions all converge through the same observed path.

The Container API is deliberately status-only. Early designs used a
level-triggered `spec.state` field, but Docker's restart policy and
direct CLI writes are concurrent writers, and a level-trigger fought
them both. Actions are now expressed as one-shot annotations (see
below).

### Container actions

#### Create container

To create a container, create a `ContainerCreateRequest` object:
```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ContainerCreateRequest
metadata:
  name: whatever-12345
  namespace: rancher-desktop
spec:
  name: magical_gates # If unset, generate one randomly
  namespace: k8s.io # Refers to a `ContainerNamespace` object
  state: running # Default to `running`
  path: /bin/sh # defaults to image entry point / command
  args: [-c, 'sleep inf'] # defaults to image entry point / command
  image: "sha256:999adf320e40662dc96119a14f07459af9959a081d10ccab7c405257030ab96b" # accepts image tag
  ports: # merged with image defaults
    - name: 80/tcp
      bindings:
      - hostIP: 0.0.0.0
        hostPort: 32768
      - hostIP: '::'
        hostPort: 32768
  labels: # merged with image labels
    org.opensuse.base.vendor: openSUSE Project
status:
  # Resulting .metadata.name, which is the container ID.  It must be in the
  # same Kubernetes namespace as the ContainerCreateRequest.
  name: 8eb6f2cf72b6616aa743cf9187f350af84c9749dab65474db2530f26745d2ef3
  conditions:
  - type: Complete
    status: True
  - type: Failed
    status: False
```

If `.spec.namespace` / `.spec.name` duplicates an existing container, a
`CreateFailed` status is set with some details.

An admission controller will ensure that we cannot have multiple
`ContainerRequest` objects at the same time for the same containerd
`name`/`namespace` pair.

#### Change container state

Set the `containers.rancherdesktop.io/action` annotation on the
`Container` to request a one-shot action. The engine reconciler reads
the annotation, dispatches the matching Docker call, records the
outcome in `status.lastAction`, and removes the annotation.

Valid values: `start`, `stop`, `pause`, `unpause`, `restart`.

A single annotation holds at most one pending action. Writing a new
value replaces the old, so a user who requests `pause` and then
`unpause` before the reconciler has run will see only the unpause. No
queue, no accumulating history.

The engine calls Docker before patching `status.lastAction` and
removing the annotation, so a crash mid-flight leaves the annotation
in place and the next reconcile replays the action. Start, stop,
pause, and unpause are idempotent against a container already in the
target state, so replay is safe. Restart has no target state to match:
a replay sends SIGTERM and waits the grace period a second time, which
the controller cannot distinguish from a deliberate re-request.

If the Docker call fails (for example, `pause` on a container that is
not running), the reconciler still removes the annotation and records
the failure in `status.lastAction`:

```yaml
status:
  lastAction:
    action: pause
    state: Failed
    error: "Error response from daemon: Container 8eb6f2 is not running"
    observedAt: "2026-04-15T10:30:00Z"
    completedAt: "2026-04-15T10:30:00Z"
```

The GUI is the intended caller for these actions. CLI users should
reach for `docker start`, `docker stop`, etc. instead: the engine
mirrors Docker state back into `status.status` either way.

#### Fetch container logs
An endpoint at `/passthrough/.../logs/${container}` will speak WebSocket;
messages are one way, as stream of bytes; messages should not be buffered.
Message text must be UTF-8 encoded.  The last portion of the path must be the
full container ID.

The following query parameters are accepted:

Parameter | Description | Default
--- | --- | ---
`tail` | Only print the given number of lines (before following). | All
`follow` | Follow the log stream. | `true`

#### Exec (shell) in container
An endpoint at `/passthrough/.../exec` will speak WebSocket; messages are
bidirectional, unbuffered binary as in the logs endpoint.  Any text must be
UTF-8 encoded.

#### Delete container
Delete the `Container` object; a finalizer will be used to delete the container,
at which point the `Container` object will actually be deleted.

## Images

`Image` objects reflect images in the container engine.  Each tag is represented
as a new `Image` object; therefore, there may be multiple `Image` objects for
the same image ID (one per tag).  If an image without any tags exists, that will
be represented by an `Image` object without `.status.repoTag` and
`.status.namespace`.

```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: Image
metadata:
  # `img-` plus hex SHA-256. A tagged image hashes `id + "\0" + tag`;
  # a dangling image (no tags) hashes the id alone. The raw id is
  # kept in `.status.id` and the tag in `.status.repoTag`.
  name: img-2b0d7f4e7d2f2e2d3c6f0a8a4b5a6c7d8e9f0a1b2c3d4e5f607182a3b4c5d6e7
  namespace: rancher-desktop # not the containerd namespace
status:
  namespace: moby # Refers to a `ContainerNamespace` object
  # Image ID, in the raw form.
  id: 'sha256:999adf320e40662dc96119a14f07459af9959a081d10ccab7c405257030ab96b'
  repoDigests:
  - registry.opensuse.org/opensuse/leap@sha256:999adf320e40662dc96119a14f07459af9959a081d10ccab7c405257030ab96b
  # repoTag is unset if the image is not tagged
  repoTag: 'registry.opensuse.org/opensuse/leap:latest'
  createdAt: "2025-11-17T03:14:16Z"
  architecture: arm64
  os: linux
  size: 45150437
  labels:
    org.opensuse.base.vendor: openSUSE Project
  conditions: []
```

### Image Actions

#### Pull image
Create an `ImagePullRequest` object:
```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ImagePullRequest
metadata:
  name: image-fetch-12345
  namespace: rancher-desktop
spec:
  namespace: moby # Refers to a `ContainerNamespace` object
  repoTag: 'registry.opensuse.org/opensuse/leap:latest'
status:
  conditions:
  - type: Complete
    status: True
  - type: Failed
    status: False
```

#### Build image
Not sure; do something with the `Resource` API maybe?

We may need an `ImageBuildRequest` job-thing or something?

#### Push image
Create an `ImagePushRequest` object; it will be removed some time after the push
has completed:
```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ImagePushRequest
metadata:
  name: image-push-12345
  namespace: rancher-desktop
spec:
  # `.metadata.name` of the image tag to push.
  imageRef: img-2b0d7f4e7d2f2e2d3c6f0a8a4b5a6c7d8e9f0a1b2c3d4e5f607182a3b4c5d6e7
status:
  conditions:
  - type: Complete
    status: True
  - type: Failed
    status: False
```

#### Scan image
We will need a new object type for this; maybe something like
```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ImageScanRequest
metadata:
  name: image-scan-12345
  namespace: rancher-desktop # not containerd namespace
spec:
  # The `.metadata.name` of an `Image` object.
  imageRef: img-2b0d7f4e7d2f2e2d3c6f0a8a4b5a6c7d8e9f0a1b2c3d4e5f607182a3b4c5d6e7
status:
  conditions:
  - type: Complete
    status: True
  - type: Failed
    status: False
  result:
    # Just dump the raw Trivy result JSON here (without converting to YAML).
    '{ ... }'
```

#### Untag image
Delete the `Image` object through the K8s API; the finalizer runs
`ImageRemove` on the matching Docker reference. Docker keeps the underlying
image while another tag or a running container references it, so removing
one tag may leave the image in place.

The engine controller mirrors untag events in the reverse direction: on
a Docker `untag` event it re-inspects the image and removes any K8s
`Image` resources whose `.status.repoTag` is no longer in Docker's tag
list. If the image becomes dangling, a new `Image` object without
`.status.repoTag` takes its place.

#### Delete untagged image
Delete the `Image` object (which does not have any `.status.repoTag` set).  An
admission controller must be set up so that this is not allowed if there is a
running container that uses that image.

## Volumes

```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: Volume
metadata:
  # `vol-` plus hex SHA-256 of the original Docker volume name.
  # Docker allows uppercase and underscores, which are invalid in
  # RFC 1123 subdomains; the controller hashes the name and keeps
  # the original in `.status.name`.
  name: vol-d404559327842434dee6f7a10d8998594be5b49a7ef9a91a42ca2b3d0174ab9d
  namespace: rancher-desktop
status:
  name: volume-name
  namespace: k8s.io # Refers to a `ContainerNamespace` object
  createdAt: "2025-11-17T03:14:16Z"
  driver: local
  mountpoint: /var/lib/docker/volumes/volume-name/_data
  labels: {}
  scope: local
  options: {}
```

### Volume Actions

#### Create volume
Create a `VolumeCreateRequest`:
```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: VolumeCreateRequest
metadata:
  name: volume-create-12345
  namespace: default
spec:
  name: volume-name
  namespace: k8s.io # Refers to a `ContainerNamespace` object
  driver: local
status:
  conditions:
  - type: Complete
    status: True
  - type: Failed
    status: False
```
Only local volumes are supported initially.
The `.spec` is expected to expand in the future, as more options are supported.

#### Delete volume
Delete the `Volume` object; finalizers will cause deletion of the container
engine side volume.
Webhooks will be needed for validation to reject deleting volumes that are in
use.

## Compose Projects

`ComposeProject` objects do not reflect actual container engine objects;
instead, they reflect `docker compose` projects.

```yaml
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeProject
metadata:
  name: foo
  namespace: rancher-desktop
spec:
  namespace: k8s.io
  name: my-project
  workingDir: /opt/foo/project-dir
  configs: []
status:
  lastAction:
    action: up
    state: Succeeded
    observedAt: "2026-04-15T10:30:00Z"
    completedAt: "2026-04-15T10:30:00Z"
    error: ""
  members:
  - name: Container/8eb6f2cf72b6616aa743cf9187f350af84c9749dab65474db2530f26745d2ef3
    uid: 239ef6a0-63bd-4e87-9a7e-c5d4435ddcd4
  conditions: []
```

- **metadata.name**: This name must be constructed by the following:
  - Take `spec.namespace` followed by a literal slash (`/`), then lower-cased
    `spec.name`.
  - Take the SHA256 of the above string, as a lower case hexadecimal string.
- **spec.namespace**: The container namespace; refers to a
  [`ContainerNamespace`](#namespaces) object.  Immutable once created.
- **spec.name**: The compose project name; immutable once created.
- **spec.workingDir**: Optional; the compose project directory on the host (i.e.
  relative to where the RDD process runs).  Used to look up any files needed.
- **spec.configs**: Optional; the list of compose files used to create the
  project.  Relative to `spec.workingDir`, which means it's also a path on the
  host.  It is not guaranteed that this is sufficient to recreate the project.
- **status.lastAction**: Information about the last observed action; see
  [Compose Actions](#compose-actions) below.  This is optional; before any
  actions are taken, this is omitted.
- **status.lastAction.action**: The last action command that was observed.
- **status.lastAction.state**: The state of the action at its completion; this
  may be `Succeeded` or `Failed` (or unset).
- **status.lastAction.observedAt**: The timestamp at which the annotation was
  observed; used to detect action time outs.  Not updated when the action
  completes.
- **status.lastAction.completedAt**: The timestamp at which point the action
  has completed; `status.lastAction.state` must be unset when this is not set.
  When this _is_ set, `status.lastAction.state` must be set.
- **status.lastAction.error**: A message related to the failure when
  `status.lastAction.state` is set to `Failed`.  This string may be longer
  than appropriate to embed into a sentence.  However, it should not be more
  than a few lines of text.
- **status.members**: A list of objects that are part of this project.  The
  `name` is the resource type (`Container`, `Volume`, `Image`), followed by a
  slash (`/`), followed by the object name (i.e. `.metadata.name`).  Each `name`
  must be unique.  The `uid` is the object UID (i.e. `.metadata.uid`), used to
  track when the object has been recreated.
- **status.conditions**: The normal status conditions; see [below](#status-conditions)

An admission controller ensures that each `spec.namespace` / `spec.name`
combination is unique; having multiple `ComposeProject` objects referring to the
same combination will be rejected.

`ComposeProject` objects may be automatically created when resources (containers,
volumes, etc.) with the typical labels have been detected; the `HasMembers`
status condition will be set to `True` to indicate that resources have been
detected.  However, in that case `workingDir` and `configs` may be unset.  The
related objects are tracked in `status.members`; when the last object is removed,
the `HasMembers` status condition will be set to `False`, and a future reconcile
is expected to delete the dangling `ComposeProject`.

Because the information in the spec can **not** recreate a running project
(because the profile selection, as well as environment variables set at
`compose up` time, are not available), changing the spec will _not_
automatically update the project.  Instead, actions will need to be explicitly
requested via annotations.

The user may manually create `ComposeProject` objects, to arrange for `docker
compose up` to be run.  The object cannot be relied upon to stay; it will be
deleted once the underlying resources has been deleted.

### Compose Actions

The user may request the reconciler to act upon compose projects by setting the
`containers.rancherdesktop.io/action` annotation on the `ComposeProject`
resource to one of the following values below.  Once the reconciler sees a valid
annotation, it initiates the actions as described below.  Immediately after,
before the asynchronous action completes, it sets the `HasMembers` status
condition to `Unknown` to prevent the `ComposeProject` from being reaped, and
updates `status.lastAction` to the following values:

Property | Description
--- | ---
`action` | The observed annotation value
`state` | Cleared
`observedAt` | The current timestamp
`completedAt` | Cleared
`error` | Cleared

The annotation itself is then removed to avoid repeated invocations.  This
ordering ensures that if the controller crashes, the action can be retried on
next reconcile.

If a reconcile notices that `status.lastAction.state` is unset and
`status.lastAction.observedAt` is a long time ago, it may attempt to retry the
action.  Any previous attempts at the action may need to be aborted first.  The
`status.lastAction` must eventually converge even if the controller restarts;
how that happens is left as an implementation detail.

#### `up`

Run `docker compose up`; profiles and environment variables are not supported.
Containers are always run detached.  The process is run asynchronously; once
the process exits, the result is kept in memory and another reconcile is queued
to process the result.  This will lead to `status.lastAction.state` to be
updated, as well as `status.lastAction.completedAt`.

Running the `up` action while `spec.workingDir` is not set is not supported,
even if `spec.configs` contains absolute paths.  It will be rejected.

If this action is retried, the implementation must ensure that there will be no
concurrent invocations of `docker compose up`.

#### `down`

Run `docker compose down --remove-orphans --volumes` asynchronously.  Once all
resources are removed, the `HasMembers` status condition will be set to `False`
via the normal route, and the `ComposeProject` will be deleted eventually
(assuming no further actions causing different condition changes).

Note: this may leave behind resources created by `docker swarm`.

### Deleting Projects

Deleting a `ComposeProject` aborts the processes it controls (spawned in
response to action annotations); however, it does _not_ remove any resources.
Any partially created or removed resources will remain, possibly incorrectly
orphaned.  This also means a deleted `ComposeProject` may be immediately
recreated as the relevant resources still exist.  To remove the project, use the
`down` action first to remove the resources, which will also cause the
`ComposeProject` to be reaped due to a lack of members.

### Status Conditions

The following status conditions are defined:

<table>
<tr><th>Type<th>Reason<th>Status<th>Description
<tr><td rowspan=5>Settled
    <td>Starting<td>False<td>Ran `docker compose up`, waiting to finish.
<tr><td>Started<td>True<td>`docker compose up` completed.
<tr><td>Errored<td>True<td>`docker compose up` failed.
<tr><td>Stopping<td>False<td>The project is being torn down.
<tr><td>Stopped<td>True<td>The project was torn down; object will be deleted.
<tr><td rowspan=3>HasMembers
    <td>Found<td>True<td>Objects matching this project were found.
<tr><td>Deleted<td>False<td>The last object for this project was deleted; this project may be reaped.
<tr><td>Calculating<td>Unknown<td>Action is being processed.
</table>

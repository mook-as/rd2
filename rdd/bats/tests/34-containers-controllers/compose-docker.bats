# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# ComposeProject tests - verify that we create ComposeProject resources based on
# the detected containers &c, and we can run "up" and "down" actions on them.
# Also, that they get deleted once all the resources go away.

# For the moby backend, container namespaces are not supported; all objects are
# always in the "moby" namespace.
CONTAINER_NAMESPACE="moby"

local_setup_file() {
    skip_unless_docker
    # Disable the test if we don't have `docker compose`; this needs to go away
    # once we can ensure the reconciler can find `docker compose` correctly,
    # since the test does not actually use it.
    docker_has_compose || skip "docker compose plugin is not installed"
    start_docker_engine
    # Pre-pull the image
    docker pull --quiet "${IMAGE_BUSYBOX}"
    RDD_NAMESPACE=$(rdd ctl get app app -o jsonpath='{.spec.namespace}')
    export RDD_NAMESPACE
}

# compose_project_name returns the deterministic metadata.name for a
# ComposeProject with the given container-namespace/project-name pair,
# matching generateProjectName in composeproject_utils.go: SHA256 of
# "<namespace>/<lower-cased name>".
compose_project_name() { # <project>
    printf '%s' "${CONTAINER_NAMESPACE}/${1,,}" | sha256sum | cut -d' ' -f1
}

# request_action sets the action annotation on a ComposeProject and blocks
# until the reconciler has consumed it (the annotation is removed).
request_action() { # <name> <action>
    local name=$1 action=$2
    rdd ctl annotate ComposeProject "${name}" --namespace="${RDD_NAMESPACE}" \
        --overwrite "containers.rancherdesktop.io/action=${action}"
    try --max 30 --delay 1 -- assert_action_consumed "${name}"
}

# assert_action_consumed reports success once the reconciler has removed the
# action annotation, which is how we know the dispatch has started.
assert_action_consumed() { # <name>
    local name=$1
    run -0 rdd ctl get ComposeProject/"${name}" --namespace="${RDD_NAMESPACE}" \
        --output "jsonpath={.metadata.annotations['containers\.rancherdesktop\.io/action']}"
    refute_output
}

@test "a container with compose labels is auto-detected into a ComposeProject with member tracking" {
    local project=rdd-bats-detect
    run_e -0 docker run --detach --name test-compose-detect \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s

    wait_for_resource_status_condition_reason ComposeProject "${name}" HasMembers Found

    run -0 rdd ctl get "ComposeProject/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.spec.namespace}/{.spec.name}'
    assert_output "${CONTAINER_NAMESPACE}/${project}"

    run -0 rdd ctl get "ComposeProject/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.status.members[*].name}'
    assert_output

    # Tear down the container directly (not via RDD) so this test only
    # exercises auto-detection; the ComposeProject should disappear once its
    # last member is removed.
    docker rm --force "${container_id}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        "ComposeProject/${name}" --timeout=30s
}

@test "ComposeProject action=up runs docker compose up for the project's own configs" {
    local project=rdd-bats-custom-configs
    # Deliberately a custom compose file name, so it can only be found via
    # .spec.configs
    cat >"${BATS_TEST_TMPDIR}/app-stack.yaml" <<EOF
services:
  app:
    image: ${IMAGE_BUSYBOX}
    command: sleep 300
EOF
    run -0 host_path "${BATS_TEST_TMPDIR}"
    local working_dir=${output}

    local name
    name=$(compose_project_name "${project}")

    rdd ctl apply -f - <<EOF
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeProject
metadata:
  name: ${name}
  namespace: ${RDD_NAMESPACE}
  annotations:
    containers.rancherdesktop.io/action: up
spec:
  namespace: ${CONTAINER_NAMESPACE}
  name: ${project}
  workingDir: ${working_dir}
  configs:
    - app-stack.yaml
EOF

    wait_for_resource_status_condition_reason ComposeProject "${name}" Settled Started

    run -0 docker ps --filter "label=com.docker.compose.project=${project}" \
        --quiet
    container_id=${output}

    run -0 docker inspect "${container_id}" --format '{{.State.Status}}'
    assert_output "running"

    docker rm --force "${container_id}"
}

@test "requesting the down action tears down a ComposeProject's resources" {
    local project=rdd-bats-down
    run_e -0 docker run --detach --name test-compose-down \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s

    request_action "${name}" down
    # The container should be (eventually) removed.
    try --until-fail docker inspect "${container_id}" --format '{{.State.Status}}'

    # The ComposeProject should be auto-deleted because its resources went away.
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s
}

@test "deleting a ComposeProject does not tear down its resources" {
    local project=rdd-bats-delete-orphans
    run_e -0 docker run --detach --name test-compose-delete-orphans \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s

    # Deleting the ComposeProject should succeed immediately, even though its
    # member resources are still present -- deletion is unconditional.
    run -0 rdd ctl delete "ComposeProject/${name}" --namespace="${RDD_NAMESPACE}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s

    # The container should be untouched: deletion orphans resources rather
    # than tearing them down.
    run -0 docker inspect "${container_id}" --format '{{.State.Status}}'
    assert_output "running"

    # Clean up the orphaned container directly; there is no ComposeProject
    # left to drive a `docker compose down` through.
    docker rm --force "${container_id}"
}

@test "invalid action annotation on a ComposeProject is rejected by the webhook" {
    local project=rdd-bats-invalid-action
    local name
    name=$(compose_project_name "${project}")

    run -1 rdd ctl apply -f - <<EOF
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeProject
metadata:
  name: ${name}
  namespace: ${RDD_NAMESPACE}
  annotations:
    containers.rancherdesktop.io/action: bogus
spec:
  namespace: ${CONTAINER_NAMESPACE}
  name: ${project}
EOF
    assert_output --partial "invalid"

    run -1 rdd ctl get "ComposeProject/${name}" --namespace="${RDD_NAMESPACE}"
}

@test "metadata.name not matching the computed hash is rejected by the webhook" {
    run -1 rdd ctl apply -f - <<EOF
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeProject
metadata:
  name: not-the-right-hash
  namespace: ${RDD_NAMESPACE}
spec:
  namespace: ${CONTAINER_NAMESPACE}
  name: rdd-bats-bad-name
EOF
    assert_output --partial "metadata.name must be"

    run -1 rdd ctl get ComposeProject/not-the-right-hash --namespace="${RDD_NAMESPACE}"
}

# assert_members_count verifies that the members array in status has the expected length.
assert_members_count() { # <name> <expected_count>
    local name=$1 expected_count=$2
    run -0 rdd ctl get "ComposeProject/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.status.members}'
    jq_output 'length'
    assert_output "${expected_count}"
}

@test "multiple containers with the same compose project label are auto-detected and tracked concurrently" {
    local project=rdd-bats-multi-detect
    run_e -0 docker run --detach --name test-compose-multi-1 \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id1=${output}

    run_e -0 docker run --detach --name test-compose-multi-2 \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id2=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        ComposeProject/"${name}" --timeout=30s

    # Wait until both members are tracked in status
    try --max 30 --delay 1 -- assert_members_count "${name}" 2

    # Stop one container; members should drop to 1
    docker rm --force "${container_id1}"
    try --max 30 --delay 1 -- assert_members_count "${name}" 1

    # Stop the second container; project should be deleted
    docker rm --force "${container_id2}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        "ComposeProject/${name}" --timeout=30s
}

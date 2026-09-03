# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# For the moby backend, container namespaces are not supported; all objects are
# always in the "moby" namespace.
CONTAINER_NAMESPACE="moby"

# The annotation key used to allow overriding the reap delay for Compose and
# ComposeUpRequest objects.
REAP_ANNOTATION="containers.rancherdesktop.io/reap-after"

local_setup_file() {
    skip_unless_docker
    # Disable the test if we don't have `docker compose`; this needs to go away
    # once we can ensure the reconciler can find `docker compose` correctly,
    # since the test does not actually use it.
    docker_has_compose || skip "docker compose plugin is not installed"
    start_docker_engine
    # Pre-pull the image
    docker pull --quiet "${IMAGE_BUSYBOX}"
    RDD_NAMESPACE=$(rdd ctl get app/app -o jsonpath='{.spec.namespace}')
    export RDD_NAMESPACE
}

# compose_project_name returns the deterministic metadata.name for a Compose
# (or ComposeUpRequest) with the given container-namespace/project-name pair:
# SHA256 of "<namespace>/<lower-cased name>".
compose_project_name() { # <project>
    printf '%s' "${CONTAINER_NAMESPACE}/${1,,}" | sha256sum | cut -d' ' -f1
}

@test "a container with compose labels is auto-detected into a Compose with member tracking" {
    local project=rdd-bats-detect
    run_e -0 docker run --detach --name test-compose-detect \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep 300
    local container_id=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        Compose/"${name}" --timeout=30s

    rdd ctl annotate Compose "${name}" --namespace="${RDD_NAMESPACE}" \
        "${REAP_ANNOTATION}=1s" --overwrite

    wait_for_resource_status_condition_reason Compose "${name}" HasMembers Found

    run -0 rdd ctl get "Compose/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.status.namespace}/{.status.name}'
    assert_output "${CONTAINER_NAMESPACE}/${project}"

    run -0 rdd ctl get "Compose/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.status.members[*].name}'
    assert_output "Container/${container_id}"

    # Test that the Compose object is reaped.
    docker rm --force "${container_id}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        "Compose/${name}" --timeout=30s
}

@test "ComposeUpRequest runs docker compose up for the project's own configs" {
    local project=rdd-bats-custom-configs
    # Deliberately a custom compose file name, so it can only be found via
    # spec.configs.
    cat >"${BATS_TEST_TMPDIR}/app-stack.yaml" <<EOF
services:
  app:
    image: ${IMAGE_BUSYBOX}
    command: sleep inf
EOF
    run -0 host_path "${BATS_TEST_TMPDIR}"
    local working_dir=${output}

    local name
    name=$(compose_project_name "${project}")

    rdd ctl apply -f - <<EOF
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeUpRequest
metadata:
  name: ${name}
  namespace: ${RDD_NAMESPACE}
spec:
  namespace: ${CONTAINER_NAMESPACE}
  name: ${project}
  workingDir: ${working_dir}
  configs:
    - app-stack.yaml
EOF

    wait_for_resource_status_condition_reason ComposeUpRequest "${name}" Settled Succeeded

    # The resulting container triggers the Compose reconciler, which creates a
    # matching Compose object with the same deterministic name.
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        Compose/"${name}" --timeout=30s

    rdd ctl annotate Compose "${name}" --namespace="${RDD_NAMESPACE}" \
        "${REAP_ANNOTATION}=1s" --overwrite

    run -0 docker ps --filter "label=com.docker.compose.project=${project}" \
        --quiet
    assert_output
    container_id=${output}

    run -0 docker inspect "${container_id}" --format '{{.State.Status}}'
    assert_output "running"

    # The ComposeUpRequest is reaped automatically some time after Settled, but
    # we don't wait for that here; just clean up the container it created.
    docker rm --force "${container_id}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        Compose/"${name}" --timeout=30s
}

@test "deleting a Compose runs docker compose down for its resources" {
    local project=rdd-bats-down
    run_e -0 docker run --detach --name test-compose-down \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep inf
    local container_id=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        Compose/"${name}" --timeout=30s
    rdd ctl annotate Compose "${name}" --namespace="${RDD_NAMESPACE}" \
        "${REAP_ANNOTATION}=1s" --overwrite

    run -0 rdd ctl delete "Compose/${name}" --namespace="${RDD_NAMESPACE}" \
        --wait=false

    # The container should be (eventually) removed.
    try --until-fail docker inspect "${container_id}" --format '{{.State.Status}}'
    echo "Expected 'error: no such object: ${container_id}' on previous line"

    # The Compose object may _already_ be gone, which means we cannot use
    # `rdd ctl wait`.  We will need to poll instead.
    try --until-fail rdd ctl get Compose/"${name}" --namespace="${RDD_NAMESPACE}"
}

@test "Compose is not created for a container without the compose project label" {
    run_e -0 docker run --detach --name test-compose-unlabeled \
        "${IMAGE_BUSYBOX}" sleep inf
    local container_id=${output}

    run -0 rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        "container/${container_id}" --timeout=30s

    # No compose labels, so no Compose object should ever appear for this
    # container: give the reconciler a moment, then assert nothing was created.
    sleep 2
    run -0 rdd ctl get Compose --namespace="${RDD_NAMESPACE}" \
        -o jsonpath="{range .items[*]}{.status.members[*].uid}{'\n'}{end}"
    refute_output --partial "${container_id}"

    docker rm --force "${container_id}"
}

@test "ComposeUpRequest with metadata.name not matching the computed hash is rejected by the webhook" {
    run -1 rdd ctl apply -f - <<EOF
apiVersion: containers.rancherdesktop.io/v1alpha1
kind: ComposeUpRequest
metadata:
  name: not-the-right-hash
  namespace: ${RDD_NAMESPACE}
spec:
  namespace: ${CONTAINER_NAMESPACE}
  name: rdd-bats-bad-name
  workingDir: /tmp
EOF
    assert_output --partial "metadata.name must be"

    run -1 rdd ctl get ComposeUpRequest/not-the-right-hash --namespace="${RDD_NAMESPACE}"
}

# assert_members_count verifies that the members array in status has the expected length.
assert_members_count() { # <name> <expected_count>
    local name=$1 expected_count=$2
    run -0 rdd ctl get "Compose/${name}" --namespace="${RDD_NAMESPACE}" \
        -o jsonpath='{.status.members}'
    jq_output 'length'
    assert_output "${expected_count}"
}

@test "multiple containers with the same compose project label are auto-detected and tracked concurrently" {
    local project=rdd-bats-multi-detect
    run_e -0 docker run --detach --name test-compose-multi-1 \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep inf
    local container_id1=${output}

    run_e -0 docker run --detach --name test-compose-multi-2 \
        --label "com.docker.compose.project=${project}" \
        --label "com.docker.compose.config-hash=bogus" \
        "${IMAGE_BUSYBOX}" sleep inf
    local container_id2=${output}

    local name
    name=$(compose_project_name "${project}")
    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        Compose/"${name}" --timeout=30s

    rdd ctl annotate Compose "${name}" --namespace="${RDD_NAMESPACE}" \
        "${REAP_ANNOTATION}=1s" --overwrite

    # Wait until both members are tracked in status
    try --max 30 --delay 1 -- assert_members_count "${name}" 2

    # Stop one container; members should drop to 1
    docker rm --force "${container_id1}"
    try --max 30 --delay 1 -- assert_members_count "${name}" 1

    # Stop the second container; project should be deleted
    docker rm --force "${container_id2}"
    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        "Compose/${name}" --timeout=30s
}

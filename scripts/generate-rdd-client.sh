#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

# Regenerate the `rdd-client` TypeScript bindings for the RDD API.
# This will start RDD in the tree; it will also use the host dockerd if
# available, otherwise it starts dockerd via RDD.

set -o errtrace -o errexit -o nounset -o pipefail

KUBERNETES_BRANCH='1.35.0'
KUBERNETES_GEN_COMMIT='dde176ff81551585a6986a4aa20b347bd374f03f'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
CLIENT_DIR="${SRC_DIR}/pkg/rdd-client"
OUT_DIR="${CLIENT_DIR}/gen"
TEMPLATE_DIR="${CLIENT_DIR}/templates"
IS_MSYS=$(command -v cygpath.exe || true)
RDD_PATH="${SRC_DIR}/rdd/bin/rdd${IS_MSYS:+.exe}"
PYTHON_IMAGE_BASE='python:3-slim'

# Use a custom RDD instance to avoid interfering with the user's RDD if it is
# not using dockerd, or if it's outdated.
export RDD_INSTANCE='rdd-client-gen'
# Ensure we are not using an outdated server.
export RDD_DEVELOPER_MODE=false

# The PID of the proxy for the Kubernetes API.
proxy_pid=''

kill_proxy() {
    if [[ -z "${proxy_pid}" ]]; then
        return 0
    fi
    if [[ -n "${IS_MSYS}" ]]; then
        MSYS2_ARG_CONV_EXCL='*' taskkill /T /PID "${proxy_pid}" /F &>/dev/null || true
    else
        kill -- "${proxy_pid}" &>/dev/null || true
        wait -- "${proxy_pid}" &>/dev/null || true
    fi
    proxy_pid=''
}

# Pick a free localhost port.
get_free_port() {
    local port _
    for _ in {1..50}; do
        port=$(((RANDOM % 20000) + 20000))
        if ! (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
            echo "${port}"
            return 0
        fi
    done
    echo "Failed to find free port" >&2
    exit 1
}

build_rdd() {
    make -C "${SRC_DIR}/rdd" build-rdd
}

start_rdd() {
    RDD_DEVELOPER_MODE=true "${RDD_PATH}" service start
    # Check for obsolete RDD service and restart if necessary.
    if ! "${RDD_PATH}" service config &>/dev/null; then
        # Assume it's because of version mismatch; try deleting the service.
        RDD_DEVELOPER_MODE=true "${RDD_PATH}" service delete
        "${RDD_PATH}" service start
    fi
}

get_api() {
    echo "Fetching OpenAPI definition from ${RDD_PATH}..."
    cleanup() {
        kill_proxy
        "${RDD_PATH}" service stop >/dev/null 2>&1 || true
    }
    trap cleanup EXIT

    start_rdd

    PORT=$(get_free_port)
    "${RDD_PATH}" ctl proxy --port="${PORT}" &>/dev/null &
    proxy_pid=$!

    # Wait for the proxy to respond on /healthz
    for _ in {1..30}; do
        if curl --fail --silent "http://127.0.0.1:${PORT}/healthz" &>/dev/null; then
            break
        fi
        sleep 1
    done

    if ! curl --fail --silent --show-error --location "http://127.0.0.1:${PORT}/openapi/v2" --output "${OUT_DIR}/swagger.json.unprocessed"; then
        echo "Failed to fetch OpenAPI definition" >&2
        exit 1
    fi

    kill_proxy
}

# Wrapper function to run docker commands; this is redefined to use RDD as
# necessary.
run_docker() {
    docker "$@"
}

sha256() {
    if command -v shasum &>/dev/null; then
        shasum --algorithm 256 "${1}" | awk '{print $1}'
    elif command -v sha256sum &>/dev/null; then
        sha256sum "${1}" | awk '{print $1}'
    else
        # Fall back to cksum
        cksum "${1}" | awk '{print $1 "." $2}'
    fi
}

process_api() {
    # Determine the hash of this script, used as the tag for the images.
    local tag
    tag=$(sha256 "${BASH_SOURCE[0]}")

    local python_image_name="rdd-client-gen-python:${tag}"

    # Ensure we can run docker.
    # shellcheck disable=SC2310 # run_docker is a single command.
    if ! run_docker system info &>/dev/null; then
        "${RDD_PATH}" set running=true containerEngine.name=moby
        run_docker() {
            "${RDD_PATH}" run docker "$@"
        }
    fi

    local has_python_image
    # If `docker images` fails here, either the image does not exist, or the
    # docker daemon is not working; either way, we can try to build the image.
    # In the latter case, that will just fail at `docker build`.
    # shellcheck disable=SC2310 # run_docker is a single command.
    has_python_image=$(run_docker images --quiet "${python_image_name}" 2>/dev/null || true)
    if [[ -z "${has_python_image}" ]]; then
        echo "Building python image ${python_image_name}..."
        run_docker build - -t "${python_image_name}" <<EOF
FROM ${PYTHON_IMAGE_BASE}
RUN pip3 install urllib3
ADD https://raw.githubusercontent.com/kubernetes-client/gen/${KUBERNETES_GEN_COMMIT}/openapi/preprocess_spec.py /
ADD https://raw.githubusercontent.com/kubernetes-client/gen/${KUBERNETES_GEN_COMMIT}/openapi/custom_objects_spec.json /
RUN chmod a+r /preprocess_spec.py /custom_objects_spec.json
ENV OPENAPI_SKIP_FETCH_SPEC=true
ENTRYPOINT ["python3", "/preprocess_spec.py", "typescript", "${KUBERNETES_BRANCH}", "/out/swagger.json", "kubernetes", "kubernetes"]
EOF
    fi

    echo 'Processing API...'
    run_docker run --rm --user="${UID:-0}" --volume="${OUT_DIR}:/out:rw" "${python_image_name}"
}

get_version() {
    local version=0.0.1 script api
    script=$(sha256 "${BASH_SOURCE[0]}")
    api=$(sha256 "${OUT_DIR}/swagger.json")
    echo "${version}-${script}${api}"
}

generate_models() {
    local version
    version=$(get_version)

    echo "Generating TypeScript models..."
    run_docker run --rm \
        --user="${UID:-0}" \
        --volume="${OUT_DIR}:/output_dir" \
        --volume="${TEMPLATE_DIR}:/templates:ro" \
        openapitools/openapi-generator-cli:v7.19.0 \
        generate \
        --input-spec /output_dir/swagger.json \
        --skip-validate-spec \
        --generator-name typescript \
        --import-mappings 'IntOrString=../../types,V1MicroTime=../../types' \
        --output /output_dir \
        --additional-properties framework=fetch-api \
        --additional-properties npmName=@rancher/rdd-client \
        --additional-properties packageAsSourceOnlyLibrary=true \
        --additional-properties platform=browser \
        --additional-properties sortParamsByRequiredFlag=true \
        --additional-properties supportsES6=true \
        --additional-properties useObjectParameters=true \
        --additional-properties importFileExtension= \
        --additional-properties modelPropertyNaming=original \
        --additional-properties npmVersion="${version}" \
        --template-dir /templates \
        --type-mappings 'int-or-string=IntOrString,date-time-micro=V1MicroTime'
}

run() {
    build_rdd
    rm -r -f -- "${OUT_DIR}"
    mkdir -p -- "${OUT_DIR}"
    get_api
    process_api
    generate_models
    echo 'Done.'
}

run

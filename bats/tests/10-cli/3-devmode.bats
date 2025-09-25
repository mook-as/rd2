load '../../helpers/load'

local_setup_file() {
    # Use the local rdd binary from the project (use absolute path to avoid relative path issues)
    cp "${PATH_REPO_ROOT}/bin/rdd${EXE}" "${BATS_FILE_TMPDIR}/rdd${EXE}"
}
# TODO check for developer mode when running inside the repo checkout

@test 'RDD_DEVELOPER_MODE=0' {
    run -0 env RDD_DEVELOPER_MODE=0 "${BATS_FILE_TMPDIR}/rdd${EXE}" svc status
    refute_line --partial "developer mode"
}

@test 'RDD_DEVELOPER_MODE=false' {
    run -0 env RDD_DEVELOPER_MODE=false "${BATS_FILE_TMPDIR}/rdd${EXE}" svc status
    refute_line --partial "developer mode"
}

@test 'RDD_DEVELOPER_MODE=1' {
    run -0 env RDD_DEVELOPER_MODE=1 "${BATS_FILE_TMPDIR}/rdd${EXE}" svc status
    assert_line --partial "developer mode"
}

@test 'RDD_DEVELOPER_MODE=true' {
    run -0 env RDD_DEVELOPER_MODE=true "${BATS_FILE_TMPDIR}/rdd${EXE}" svc status
    assert_line --partial "developer mode"
}

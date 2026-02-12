load '../../helpers/load'

ALL_KEYS="dir log_dir short_dir lima_home tls_dir kubeconfig pid_file args_file"

@test 'rdd svc paths prints all keys in table format' {
    run -0 rdd svc paths
    for key in ${ALL_KEYS}; do
        assert_line --regexp "^${key} "
    done
}

@test 'rdd svc paths --json produces valid JSON with all keys' {
    run -0 rdd svc paths --json
    local json="${output}"
    for key in ${ALL_KEYS}; do
        jq --exit-status --arg k "${key}" 'has($k)' <<<"${json}"
    done
}

@test 'rdd svc paths --shell produces shell export statements' {
    run -0 rdd svc paths --shell
    for key in ${ALL_KEYS}; do
        upper_key=$(echo "${key}" | tr '[:lower:]' '[:upper:]')
        assert_line --regexp "^export RDD_${upper_key}="
    done
}

@test 'rdd svc paths <key> prints only the value' {
    run -0 rdd svc paths log_dir
    # Output should be a single line with no key prefix
    assert_output --regexp '^(/|[A-Z]:)'
    refute_output --regexp '^log_dir'
    [[ "${#lines[@]}" -eq 1 ]]
}

@test 'rdd svc paths with invalid key fails and lists valid keys' {
    run -1 rdd svc paths no_such_key
    assert_output --partial 'unknown key'
    assert_output --partial 'no_such_key'
    assert_output --partial 'valid keys'
}

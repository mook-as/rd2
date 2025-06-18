EXE=""
PLATFORM=$OS
if is_windows; then
    PLATFORM=linux
    if using_windows_exe; then
        EXE=".exe"
        PLATFORM=win32
    fi
fi

no_cr() {
    tr -d '\r'
}
ctrctl() {
    if using_docker; then
        docker "$@"
    else
        nerdctl "$@"
    fi
}
curl() {
    command "curl$EXE" "$@"
}
rdd() {
    "$PATH_REPO_ROOT/bin/rdd" "$@"
}

########################################################################
: "${RDD_INSTANCE=bats}"
export RDD_INSTANCE

: "${RDD_TRACE:=false}"
: "${RDD_NAMESPACE:=default}"
: "${RDD_KEEP_LOGS:=1}"
export RDD_KEEP_LOGS

using_windows_exe() {
    true # TODO: WSL testing, later.
}

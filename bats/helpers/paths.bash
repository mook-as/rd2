# PATH_BATS_ROOT, PATH_BATS_LOGS, and PATH_BATS_HELPERS are already set by load.bash

PATH_REPO_ROOT=$(absolute_path "$PATH_BATS_ROOT/..")

inside_repo_clone() {
    [ -d "$PATH_REPO_ROOT/pkg/rancher-desktop-daemon" ]
}

if is_macos; then
    PATH_APP_HOME="$HOME/Library/Application Support/rancher-desktop-${RDD_INSTANCE}"
    PATH_CONFIG="$HOME/Library/Preferences/rancher-desktop-${RDD_INSTANCE}"
    PATH_CACHE="$HOME/Library/Caches/rancher-desktop-${RDD_INSTANCE}"
    PATH_LOGS="$HOME/Library/Logs/rancher-desktop-${RDD_INSTANCE}"
fi

if is_linux; then
    PATH_APP_HOME="$HOME/.local/share/rancher-desktop-${RDD_INSTANCE}"
    PATH_CONFIG="$HOME/.config/rancher-desktop-${RDD_INSTANCE}"
    PATH_CACHE="$HOME/.cache/rancher-desktop-${RDD_INSTANCE}"
    PATH_LOGS="$PATH_APP_HOME/logs"
fi

wslpath_from_win32_env() {
    # The cmd.exe _sometimes_ returns an empty string when invoked in a subshell
    # wslpath "$(cmd.exe /c "echo %$1%" 2>/dev/null)" | tr -d "\r"
    # Let's see if powershell.exe avoids this issue
    wslpath "$(powershell.exe -Command "Write-Output \${Env:$1}")" | tr -d "\r"
}

if is_windows; then
    LOCALAPPDATA="$(wslpath_from_win32_env LOCALAPPDATA)"
    PROGRAMFILES="$(wslpath_from_win32_env ProgramFiles)"
    SYSTEMROOT="$(wslpath_from_win32_env SystemRoot)"

    PATH_APP_HOME="$LOCALAPPDATA/rancher-desktop-${RDD_INSTANCE}"
    PATH_CONFIG="$LOCALAPPDATA/rancher-desktop-${RDD_INSTANCE}"
    PATH_CACHE="$PATH_APP_HOME/cache"
    PATH_LOGS="$PATH_APP_HOME/logs"
    PATH_DISTRO="$PATH_APP_HOME/distro"
    PATH_DISTRO_DATA="$PATH_APP_HOME/distro-data"
fi

LIMA_HOME="$PATH_APP_HOME/lima"

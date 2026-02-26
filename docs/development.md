# Rancher Desktop Daemon Development

For design details, please refer to [design/](design/).

## Prerequisites

This section only applies to building (and testing) Rancher Desktop Daemon; for
prerequisites for running it, please refer to user documentation.

We of course require all the things running RDD does (e.g. the ability to run
VMs).

On all platforms, we expect at least:
- Git
- GNU Make 4 or higher. (macOS ships with 3.81, which is too old.)
- GNU coreutils, and other basic tools like `gawk`, `gzip`, and `sed`.
- GNU bash version 5 or higher. (macOS ships with 3.2, which is too old.)
- Golang compiler as listed in `go.mod` (from go.dev, not the GCC toolchain or
  others).
- jq
- Perl (for check-spelling only).

We currently only support building from a checked-out source tree (i.e. with the
`.git` directory available, including any tags).

For all platforms, we only support whichever OS version we are using, which is
generally the latest release versions.

### macOS

On macOS, we expect Xcode command line tools to be available.

### Windows
Development should be done inside WSL.  Development using `cmd.exe` / PowerShell
/ MSYS2 / Git Bash / Cygwin / etc. is explicitly not supported.  We may
occasionally let things work to make CI easier, but that is the extent of it.
All Rancher Desktop Daemon processes will be run under Win32 (instead of under
WSL); developing for Linux on a Windows host is only supported when using a
full Linux VM (instead of using the WSL VM), whether or not WSL integration is
enabled.

WSL interop must be enabled (we test for `winver.exe` being around).

# Environment Variables

RDD uses environment variables prefixed with `RDD_` for configuration and path discovery.

## Configuration Variables

These variables control RDD behavior. Set them before running `rdd` commands.

| Variable | Description | Default |
| --- | --- | --- |
| `RDD_INSTANCE` | Instance identifier. Determines which control plane and directories to use. Also settable via `rdd --instance`. | `2` |
| `RDD_DEVELOPER_MODE` | Enables developer mode: exposes hidden CLI flags, detects source tree for local builds. | unset |
| `RDD_KEEP_LOGS` | When set to any non-empty value, preserves numbered log files instead of pruning old ones. Also prevents log directory removal on `rdd svc delete`. | unset |
| `RDD_LOG_TITLE` | When set, writes this string as the first line of each new log file. Useful for identifying log files from specific test runs or sessions. | unset |

### BATS Test Variables

These variables configure the BATS test framework. They have no effect on `rdd` itself.

| Variable | Description | Default |
| --- | --- | --- |
| `RDD_TRACE` | Enables verbose trace output in BATS tests. | `false` |
| `RDD_NAMESPACE` | Default Kubernetes namespace for BATS controller tests. | `default` |

## Path Variables

`rdd svc paths --shell` exports these variables. They reflect the paths RDD uses for the current instance; setting them has no effect on RDD's behavior.

```shell
source <(rdd svc paths --shell)
```

| Variable | Description | Example (macOS, instance `2`) |
| --- | --- | --- |
| `RDD_ARGS_FILE` | Saved startup arguments | `~/Library/Application Support/rancher-desktop-2/args.json` |
| `RDD_DIR` | Service instance directory | `~/Library/Application Support/rancher-desktop-2` |
| `RDD_KUBECONFIG` | Kubeconfig file | `~/.rd2/kube.config` |
| `RDD_LIMA_HOME` | Lima home directory | `~/.rd2/lima` |
| `RDD_LOG_DIR` | Log directory (survives instance deletion) | `~/Library/Logs/rancher-desktop-2` |
| `RDD_PID_FILE` | Service PID file | `~/Library/Application Support/rancher-desktop-2/rdd.pid` |
| `RDD_SHORT_DIR` | Short directory path | `~/.rd2` |
| `RDD_TLS_DIR` | TLS certificate directory | `~/Library/Application Support/rancher-desktop-2/tls` |

The short directory (`RDD_SHORT_DIR`) exists because Lima uses Unix domain sockets with a 104-byte path limit. Placing `LIMA_HOME` under `~/.rd2/lima` instead of the full application support path keeps socket paths short enough.

Path variables are listed alphabetically. To retrieve a single path, pass the key name (lowercase, without the `RDD_` prefix):

```shell
rdd svc paths log_dir
```

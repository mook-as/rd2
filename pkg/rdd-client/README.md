# rdd-client

This is a mostly-generated client to use the [Rancher Desktop Daemon] API from
JavaScript.  We originally used a forked version of [`@kubernetes/client-node`],
but it had issues working in a browser environment and hacking it up caused
issues with using it as a local package.

Many of the files here are still straight copies from
[`@kubernetes/client-node`], with trivial linting fixes.

[Rancher Desktop Daemon]: https://github.com/rancher-sandbox/rancher-desktop-2
[`@kubernetes/client-node`]: https://www.npmjs.com/package/@kubernetes/client-node

## Building

We do not actually install this as a separate package, so the only build step is
for regenerating the API definitions.  To do so:

- Be able to build and run `rdd` in this source tree (`go`, `make`, etc.); being
  able to run VMs is not necessarily required, see next point.
- Either have a running `dockerd`, or be able to run VMs so `rdd` can be used to
  provide one.
- Run `make -C rdd generate-typescript`.

## Usage

We export a `KubeConfig` class; it is distinct from the one from
[`@kubernetes/client-node`], but should be API-compatible enough for our use.
The other files are copies from upstream (at 1.4.0) with small compilation fixes.

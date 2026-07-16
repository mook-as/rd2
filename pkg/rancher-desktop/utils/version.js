// @ts-check

// Plain JavaScript, not TypeScript, so vue.config.mjs can import it; Node runs that config
// directly, with no TypeScript loader.  `@ts-check` and the JSDoc types keep it checked like
// the TypeScript around it.

import semver from 'semver';

/**
 * Whether the version string is a plain semver release with no pre-release
 * identifiers.  Development builds (git-describe output like `2.0.0-9-gabc1234`),
 * alpha/beta pre-releases, and unparsable versions all count as non-release, so
 * the pre-release styling (striped icon and nav) covers everything short of a
 * final release.
 *
 * @param {string} version The version to classify.
 * @returns {boolean}
 */
export function isReleaseVersion(version) {
  return semver.valid(version) !== null && semver.prerelease(version) === null;
}

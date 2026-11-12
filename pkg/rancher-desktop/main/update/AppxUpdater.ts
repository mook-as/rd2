import { spawn } from 'child_process';

import { AllPublishOptions, newError } from 'builder-util-runtime';
import { InstallOptions } from 'electron-updater/out/BaseUpdater';
import { NsisUpdater } from 'electron-updater/out/NsisUpdater';
import { ElectronHttpExecutor } from 'electron-updater/out/electronHttpExecutor';
import { findFile } from 'electron-updater/out/providers/Provider';
import { verifySignature } from 'electron-updater/out/windowsExecutableCodeSignatureVerifier';
import { Lazy } from 'lazy-val';

import mainEvents from '@pkg/main/mainEvents';
import { getRDDPath } from '@pkg/utils/paths';

import type { AppAdapter } from 'electron-updater/out/AppAdapter';
import type { DownloadExecutorResult, DownloadUpdateOptions } from 'electron-updater/out/AppUpdater';

/**
 * AppxUpdater implements updating for Rancher Desktop's APPX-based installer.
 *
 * There is no concept of an elevated (per-machine/admin) install any more:
 * every install is a per-user sideload via `Add-AppxPackage`, so unlike the
 * old MsiUpdater there is no `shouldElevate` check or MSIINSTALLPERUSER-style
 * argument.
 */
// We extend from NsisUpdater because extending BaseUpdater appears to cause
// issues with AppImageUpdater (where it thinks BaseUpdater is undefined)?
export default class AppxUpdater extends NsisUpdater {
  // eslint-disable-next-line no-useless-constructor -- This is used to change visibility
  constructor(options?: AllPublishOptions | null, app?: AppAdapter) {
    super(options, app);
  }

  // This implements an abstract method in BaseUpdater.
  protected doDownloadUpdate(downloadUpdateOptions: DownloadUpdateOptions): Promise<DownloadExecutorResult> {
    const { info, provider } = downloadUpdateOptions.updateInfoAndProvider;
    const fileInfo = findFile(provider.resolveFiles(info), 'appx');

    if (!fileInfo) {
      throw newError(`Could not find update information for APPX installer version ${ info.version }`,
        'ERR_UPDATER_INVALID_UPDATE_INFO');
    }

    return this.executeDownload({
      fileExtension: 'appx',
      fileInfo,
      downloadUpdateOptions,
      task:          async(destinationFile, downloadOptions, packageFile, removeTempDirIfAny) => {
        const httpExecutor: ElectronHttpExecutor = (this as any).httpExecutor;
        const mergedDownloadOptions = {
          ...downloadOptions,
          sha512: fileInfo.packageInfo?.sha512 ?? downloadOptions.sha512,
        };

        this._logger.debug?.(`Downloading update for ${ info.version } from ${ fileInfo.url } (sha512: ${ mergedDownloadOptions.sha512 })`);
        await httpExecutor.download(fileInfo.url, destinationFile, mergedDownloadOptions);
        const signatureError = await this.verifySignature_(destinationFile);

        if (signatureError) {
          await removeTempDirIfAny();
          throw newError(
            `New version ${ info.version } (${ fileInfo.url }) is not signed correctly: ${ signatureError }`,
            'ERR_UPDATER_INVALID_SIGNATURE');
        }
      },
    });
  }

  // Verify that the given file is signed by the expected entity (as configured
  // in electron-builder.yml).  APPX packages are Authenticode-signed just
  // like exe/msi files, so the same verifier works unchanged.
  private async verifySignature_(destinationFile: string): Promise<string | null> {
    let publisherName: string | string[] | null;

    try {
      const configOnDisk: Lazy<any> = (this as any).configOnDisk;

      publisherName = (await configOnDisk.value).publisherName;
      if (!publisherName) {
        return null;
      }
    } catch (e: any) {
      if (e?.code === 'ENOENT') {
        return null; // No updates configured
      }
      throw e;
    }
    const publisherNames = Array.isArray(publisherName) ? publisherName : [publisherName];

    return await verifySignature(publisherNames, destinationFile, this._logger);
  }

  protected doInstall(options: InstallOptions): boolean {
    const rdd = getRDDPath();
    const installerPath = this.installerPath;

    if (!installerPath) {
      this._logger.error('doInstall() called without an installer path');
      this.dispatchError(new Error("No valid update available, can't quit and install"));

      return false;
    }

    // Installs are always per-user sideloads now, so there is nothing
    // equivalent to msiexec's /passive vs /quiet (no UAC prompt either way).
    // We run `rdd install-update` (a thin wrapper over the WinRT
    // Windows.Management.Deployment.PackageManager API) instead of spawning
    // powershell.exe to run Add-AppxPackage, because PowerShell can be
    // disabled or run in a constrained execution mode by policy in some
    // corporate environments; rdd.exe is already shipped as part of the app
    // and needs no shell. `--force-app-shutdown` lets the install close the
    // running app for us instead of requiring that we have already exited.
    const args: string[] = ['install-update', '--force-app-shutdown', installerPath];

    this._logger.debug?.(`Will invoke installer on restart with: ${ rdd } ${ args.join(' ') }`);
    mainEvents.on('quit', () => {
      this._logger.debug?.(`Running ${ rdd } ${ args.join(' ') }`);
      const proc = spawn(rdd, args, {
        detached: true, stdio: 'ignore', windowsHide: true,
      });

      proc.on('error', (err: NodeJS.ErrnoException) => {
        this._logger.error(`Cannot run installer: error code: ${ err.code }`);
        this.dispatchError(err);
      });
      proc.on('exit', (code, signal) => {
        this._logger.debug?.(`rdd install-update exited with ${ code }/${ signal }`);
      });
      proc.unref();
    });

    return true;
  }
}

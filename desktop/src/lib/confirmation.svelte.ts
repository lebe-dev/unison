import { Code, ConnectError } from "@connectrpc/connect";
import { client } from "./api/client";
import {
  ConfirmationScope,
  ConfirmationSide,
  type Device,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { errorText } from "./errors";
import { translate } from "./i18n";

/** Content a confirmation flag covers. */
export type Scope = "clipboard" | "files";

/** Device that asks: this one ("local") or the paired one ("peer"). */
export type Side = "local" | "peer";

/** key names one switch of a device. */
function key(deviceId: string, scope: Scope, side: Side): string {
  return `${deviceId}:${scope}:${side}`;
}

/**
 * ConfirmationStore keeps the confirmation flags the user changed in this
 * window until the daemon reports them in the status. Every paired device has
 * four flags: this device and the paired one each ask, or not, before they
 * take the clipboard items and the files of the other. A paired device also
 * has the permission to change these flags remotely.
 */
class ConfirmationStore {
  /** Switches whose state is being changed. */
  busy = $state<string[]>([]);

  /** Permission switches whose state is being changed. */
  remoteBusy = $state<string[]>([]);

  #changed = $state<Record<string, boolean>>({});

  #remoteChanged = $state<Record<string, boolean>>({});

  /** require reports the flag the user set last, or the one of the device. */
  require(device: Device, scope: Scope, side: Side): boolean {
    const changed = this.#changed[key(device.deviceId, scope, side)];
    if (changed !== undefined) {
      return changed;
    }
    if (side === "local") {
      return scope === "clipboard" ? device.requireConfirmation : device.requireFileConfirmation;
    }
    return scope === "clipboard"
      ? device.peerRequireConfirmation
      : device.peerRequireFileConfirmation;
  }

  /** isBusy reports a switch with a change in flight. */
  isBusy(deviceId: string, scope: Scope, side: Side): boolean {
    return this.busy.includes(key(deviceId, scope, side));
  }

  /** remoteAllowed reports the permission the user set last, or the one of the device. */
  remoteAllowed(device: Device): boolean {
    const changed = this.#remoteChanged[device.deviceId];
    if (changed !== undefined) {
      return changed;
    }
    return device.allowRemoteSettings;
  }

  /** isRemoteBusy reports a permission switch with a change in flight. */
  isRemoteBusy(deviceId: string): boolean {
    return this.remoteBusy.includes(deviceId);
  }

  /**
   * setRemoteAllowed lets a paired device change the confirmation flags of this
   * device, or stops it, and returns the reason of a failure. The permission is
   * a property of this device, so the change does not need the device online.
   */
  async setRemoteAllowed(deviceId: string, allow: boolean): Promise<string | null> {
    if (this.remoteBusy.includes(deviceId)) {
      return null;
    }
    this.remoteBusy = [...this.remoteBusy, deviceId];
    try {
      await client.setDeviceRemoteSettings({ deviceId, allow });
    } catch (error) {
      return failureText(error);
    } finally {
      this.remoteBusy = this.remoteBusy.filter((current) => current !== deviceId);
    }
    // The new permission shows while the status reloads; then the status is the
    // source again, so a later change made elsewhere shows too.
    this.#remoteChanged = { ...this.#remoteChanged, [deviceId]: allow };
    try {
      await daemon.refresh();
    } finally {
      const rest = { ...this.#remoteChanged };
      delete rest[deviceId];
      this.#remoteChanged = rest;
    }
    return null;
  }

  /**
   * set changes one flag of a paired device and returns the reason of a
   * failure. The flag of this device changes at once; the flag of the paired
   * device is asked for over the session with it.
   */
  async set(deviceId: string, scope: Scope, side: Side, require: boolean): Promise<string | null> {
    const id = key(deviceId, scope, side);
    if (this.busy.includes(id)) {
      return null;
    }
    this.busy = [...this.busy, id];
    try {
      await client.setDeviceConfirmation({
        deviceId,
        require,
        scope: scope === "clipboard" ? ConfirmationScope.CLIPBOARD : ConfirmationScope.FILES,
        side: side === "local" ? ConfirmationSide.LOCAL : ConfirmationSide.PEER,
      });
    } catch (error) {
      return failureText(error);
    } finally {
      this.busy = this.busy.filter((current) => current !== id);
    }
    // The new flag shows while the status reloads; then the status is the
    // source again, so a later change made on the other device shows too.
    this.#changed = { ...this.#changed, [id]: require };
    try {
      await daemon.refresh();
    } finally {
      const rest = { ...this.#changed };
      delete rest[id];
      this.#changed = rest;
    }
    return null;
  }
}

/** failureText explains why a flag did not change. */
function failureText(error: unknown): string {
  if (error instanceof ConnectError && error.code === Code.Unavailable) {
    // The flag of the paired device is changed over the session with it.
    return translate("confirmation.deviceOffline");
  }
  return errorText(error, translate("settings.confirmationFailed"));
}

/** confirmation is the confirmation flag state of the app. */
export const confirmation = new ConfirmationStore();

import { Code, ConnectError } from "@connectrpc/connect";
import { client } from "./api/client";
import type { Device } from "./api/gen/unison/local/v1/local_pb";
import { errorText } from "./errors";

/**
 * ConfirmationStore keeps the confirmation flags the user changed in this
 * window. The daemon reports the flag of a paired device as it was recorded at
 * pairing, so the value set here stays on screen for the session.
 */
class ConfirmationStore {
  /** Devices whose state is being changed, by device ID. */
  busy = $state<string[]>([]);

  #changed = $state<Record<string, boolean>>({});

  /** require reports the flag the user set last, or the one of the device. */
  require(device: Device): boolean {
    return this.#changed[device.deviceId] ?? device.requireConfirmation;
  }

  /**
   * set asks a device to hold the items it receives for confirmation, or to
   * apply them automatically. It returns the reason of a failure.
   */
  async set(deviceId: string, require: boolean): Promise<string | null> {
    if (this.busy.includes(deviceId)) {
      return null;
    }
    this.busy = [...this.busy, deviceId];
    try {
      await client.setDeviceConfirmation({ deviceId, require });
      this.#changed = { ...this.#changed, [deviceId]: require };
      return null;
    } catch (error) {
      return failureText(error);
    } finally {
      this.busy = this.busy.filter((current) => current !== deviceId);
    }
  }
}

/** failureText explains why the flag of a device did not change. */
function failureText(error: unknown): string {
  if (error instanceof ConnectError && error.code === Code.Unavailable) {
    // The daemon changes the flag of a paired device over the session with it.
    return "The device is offline, so its flag cannot be changed.";
  }
  return errorText(error, "The confirmation flag could not be changed.");
}

/** confirmation is the confirmation flag state of the app. */
export const confirmation = new ConfirmationStore();

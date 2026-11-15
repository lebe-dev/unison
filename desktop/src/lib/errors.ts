import { Code, ConnectError } from "@connectrpc/connect";

/** Domain prefixes of the error messages of the daemon. */
const PREFIXES = ["api: ", "node: ", "pairing: ", "trust: ", "clip: ", "history: "];

/** stripPrefix removes the package prefix of a daemon error message. */
function stripPrefix(message: string): string {
  for (const prefix of PREFIXES) {
    if (message.startsWith(prefix)) {
      return message.slice(prefix.length);
    }
  }
  return message;
}

/** isUnimplemented reports a call the daemon does not serve yet. */
export function isUnimplemented(error: unknown): boolean {
  return error instanceof ConnectError && error.code === Code.Unimplemented;
}

/** isUnavailable reports a daemon that cannot be reached. */
export function isUnavailable(error: unknown): boolean {
  return error instanceof ConnectError && error.code === Code.Unavailable;
}

/** cleanError removes the package prefix of a daemon error message. */
export function cleanError(message: string): string {
  return stripPrefix(message.trim());
}

/**
 * errorText turns an error of the local API into a sentence for the user. The
 * messages of the daemon are kept without their package prefix; a failure
 * without a message falls back to the given text.
 */
export function errorText(error: unknown, fallback: string): string {
  if (error instanceof ConnectError) {
    if (error.code === Code.Unavailable) {
      return "The daemon is not reachable.";
    }
    const message = stripPrefix(error.rawMessage.trim());
    return message || fallback;
  }
  if (error instanceof Error) {
    return error.message || fallback;
  }
  return fallback;
}

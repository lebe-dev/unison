import {
  isPermissionGranted,
  onAction,
  registerActionTypes,
  requestPermission,
  sendNotification,
} from "@tauri-apps/plugin-notification";
import type { UnlistenFn } from "@tauri-apps/api/event";

/** Action button of a notification. */
export interface NotificationAction {
  id: string;
  title: string;
}

/** Notification to show. */
export interface NotificationRequest {
  title: string;
  body: string;
  /** Action buttons. A notification without actions stays a plain message. */
  actions?: NotificationAction[];
  /** Stable identity of the notification, kept for the action it reports. */
  key?: string;
  /** Payload that comes back with the action the user picks. */
  extra?: Record<string, unknown>;
}

/** Action the user performed on a notification. */
export interface PerformedAction {
  /** Action the user picked, or "tap" for the notification itself. */
  actionId: string;
  /** Payload of the notification. */
  extra: Record<string, unknown>;
}

/** Action types the system already knows, by identifier. */
const registered = new Set<string>();

/** actionTypeID names the action type of a set of buttons. */
function actionTypeID(actions: NotificationAction[]): string {
  return `unison-${actions.map((action) => action.id).join("-")}`;
}

/** ensureActionType registers the buttons of a notification once. */
async function ensureActionType(id: string, actions: NotificationAction[]): Promise<void> {
  if (registered.has(id)) {
    return;
  }
  await registerActionTypes([{ id, actions }]);
  registered.add(id);
}

/**
 * notify shows an OS notification. The buttons of a notification are shown
 * while a listener of onNotificationAction exists, so the shell subscribes
 * before the first notification.
 */
export async function notify(request: NotificationRequest): Promise<boolean> {
  if (!(await ensurePermission())) {
    return false;
  }
  const actions = request.actions ?? [];
  const type = actions.length > 0 ? actionTypeID(actions) : "";
  if (type) {
    try {
      await ensureActionType(type, actions);
    } catch (error) {
      // The notification still carries its text, only the buttons are lost.
      console.warn("cannot register the notification actions", error);
    }
  }
  sendNotification({
    id: notificationID(request.key),
    title: request.title,
    body: request.body,
    actionTypeId: type || undefined,
    extra: request.extra,
  });
  return true;
}

/**
 * onNotificationAction runs handler for every action the user performs on a
 * notification. The returned function stops the subscription.
 */
export async function onNotificationAction(
  handler: (action: PerformedAction) => void,
): Promise<UnlistenFn> {
  const listener = await onAction((event) => {
    handler({ actionId: event.actionId, extra: event.notification?.extra ?? {} });
  });
  return () => void listener.unregister();
}

/** notificationID turns the key of a notification into its number. */
function notificationID(key: string | undefined): number | undefined {
  if (!key) {
    return undefined;
  }
  let hash = 0x811c9dc5;
  for (let index = 0; index < key.length; index += 1) {
    hash ^= key.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash & 0x7fffffff;
}

async function ensurePermission(): Promise<boolean> {
  if (await isPermissionGranted()) {
    return true;
  }
  return (await requestPermission()) === "granted";
}

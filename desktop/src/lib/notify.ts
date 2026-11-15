import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";

/** Action button of a notification. */
export interface NotificationAction {
  id: string;
  title: string;
}

/** Notification to show. */
export interface NotificationRequest {
  title: string;
  body: string;
  /** Action buttons, shown only on platforms that support them. */
  actions?: NotificationAction[];
}

/**
 * notify shows an OS notification. The current desktop plugin has no action
 * buttons, so the actions are dropped and the notification stays plain. The
 * callers keep the actions in the request, so a platform channel with buttons
 * can take them over without changing the call sites.
 */
export async function notify(request: NotificationRequest): Promise<boolean> {
  if (!(await ensurePermission())) {
    return false;
  }
  sendNotification({ title: request.title, body: request.body });
  return true;
}

async function ensurePermission(): Promise<boolean> {
  if (await isPermissionGranted()) {
    return true;
  }
  return (await requestPermission()) === "granted";
}

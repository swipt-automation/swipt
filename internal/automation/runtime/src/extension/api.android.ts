import * as accessibilityModule from "./android/accessibility";
import * as applicationsModule from "./android/applications";
import * as deviceModule from "./android/device";
import * as inputModule from "./android/input";
import * as notificationsModule from "./android/notifications";
import * as shellModule from "./android/shell";

export { handleOf, image, RasterImage } from "./image";

export const accessibility = Object.freeze({ ...accessibilityModule });
export const input = Object.freeze({ ...inputModule });
export const applications = Object.freeze({ ...applicationsModule });
export const notifications = Object.freeze({ ...notificationsModule });
export const device = Object.freeze({ ...deviceModule });
export const shell = Object.freeze({ ...shellModule });

import type { Route } from "./api";

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(timeZone: string, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = timeZone + JSON.stringify(options);
  let result = formatters.get(key);
  if (!result) {
    result = new Intl.DateTimeFormat("en-US", { timeZone, ...options });
    formatters.set(key, result);
  }
  return result;
}

/** 4:32:10 PM */
export function clockTime(unix: number, timeZone: string): string {
  return formatter(timeZone, { hour: "numeric", minute: "2-digit", second: "2-digit" }).format(unix * 1000);
}

/** 4:32 PM */
export function shortTime(unix: number, timeZone: string): string {
  return formatter(timeZone, { hour: "numeric", minute: "2-digit" }).format(unix * 1000);
}

/** Tuesday, November 24 */
export function longDate(unix: number, timeZone: string): string {
  return formatter(timeZone, { weekday: "long", month: "long", day: "numeric" }).format(unix * 1000);
}

/** Tue 24 */
export function tickDate(unix: number, timeZone: string): string {
  return formatter(timeZone, { weekday: "short", day: "numeric" }).format(unix * 1000);
}

export type Punctuality = "late" | "early" | "on-time";

/** Within a minute either way counts as on time */
export function punctuality(delaySeconds: number): Punctuality {
  if (delaySeconds >= 60) return "late";
  if (delaySeconds <= -60) return "early";
  return "on-time";
}

export function describeDelay(delaySeconds: number): string {
  const minutes = Math.round(Math.abs(delaySeconds) / 60);
  switch (punctuality(delaySeconds)) {
    case "late":
      return `${minutes} min late`;
    case "early":
      return `${minutes} min early`;
    default:
      return "On time";
  }
}

const RAIL_TYPES = new Set([0, 1, 2, 5, 7, 12]);

export function isRail(route: Pick<Route, "type">): boolean {
  return RAIL_TYPES.has(route.type);
}

export function vehicleNoun(route: Pick<Route, "type"> | undefined): string {
  if (!route) return "Vehicle";
  if (route.type === 0) return "Light rail";
  if (route.type === 1 || route.type === 2) return "Train";
  return "Bus";
}

export const UNASSIGNED_COLOR = "#8A96A3";
const FALLBACK_ROUTE_COLOR = "#0076CE";

export function routeColor(route: Pick<Route, "color"> | undefined): string {
  return route?.color ? `#${route.color}` : FALLBACK_ROUTE_COLOR;
}

export function routeTextColor(route: Pick<Route, "textColor"> | undefined): string {
  return route?.textColor ? `#${route.textColor}` : "#FFFFFF";
}

/** Rail lines first, then everything else, in natural order (2, 10, 15, 15L, ...) */
export function compareRoutes(a: Route, b: Route): number {
  const railDifference = Number(isRail(b)) - Number(isRail(a));
  if (railDifference !== 0) return railDifference;
  return (a.shortName || a.id).localeCompare(b.shortName || b.id, "en", { numeric: true });
}

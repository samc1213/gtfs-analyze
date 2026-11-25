// Client for the `gtfs-analyze serve` JSON API

const API_BASE: string = import.meta.env.VITE_API_BASE ?? "";

export interface Meta {
  agencyName: string;
  timezone: string;
  feedVersion: string;
  earliestPosition: number | null;
  latestPosition: number | null;
  center: [number, number] | null;
}

export interface Route {
  id: string;
  shortName: string;
  longName: string;
  type: number;
  color: string;
  textColor: string;
}

export interface Position {
  entityId: string;
  label: string;
  tripId: string;
  routeId: string;
  startDate: string;
  lat: number;
  lon: number;
  bearing: number;
  status: number;
  stopId: string;
  t: number;
}

export interface TripStop {
  sequence: number;
  stopId: string;
  name: string;
  lat: number;
  lon: number;
  scheduledArrival: number | null;
  predictedArrival: number | null;
  skipped: boolean;
}

export interface Trip {
  tripId: string;
  serviceDate: string;
  routeId: string;
  routeShortName: string;
  routeLongName: string;
  routeColor: string;
  routeTextColor: string;
  headsign: string;
  directionId: number;
  delaySeconds: number | null;
  stops: TripStop[];
  shape: [number, number][];
}

export interface Activity {
  from: number;
  bucketSize: number;
  vehicles: number[];
}

export type NetworkGeoJson = GeoJSON.FeatureCollection<
  GeoJSON.LineString,
  { routeId: string; shortName: string; routeType: number; color: string; directionId: number }
>;

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function getJson<T>(path: string, signal?: AbortSignal): Promise<T> {
  let response: Response;
  try {
    response = await fetch(API_BASE + path, { signal });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new ApiError(0, `Can't reach the API at ${API_BASE || window.location.origin}/api.`);
  }
  // The dev server's proxy answers 502 when `gtfs-analyze serve` isn't running
  if (response.status === 502 || response.status === 503 || response.status === 504) {
    throw new ApiError(response.status, `Can't reach the API at ${API_BASE || window.location.origin}/api.`);
  }
  if (!response.ok) {
    throw new ApiError(response.status, (await response.text()).trim() || response.statusText);
  }
  return (await response.json()) as T;
}

export const api = {
  meta: (signal?: AbortSignal) => getJson<Meta>("/api/meta", signal),
  routes: (signal?: AbortSignal) => getJson<Route[]>("/api/routes", signal),
  network: (signal?: AbortSignal) => getJson<NetworkGeoJson>("/api/network", signal),
  positions: (from: number, to: number, signal?: AbortSignal) =>
    getJson<Position[]>(`/api/positions?from=${from}&to=${to}`, signal),
  activity: (from: number, to: number, buckets: number, signal?: AbortSignal) =>
    getJson<Activity>(`/api/activity?from=${from}&to=${to}&buckets=${buckets}`, signal),
  trip: (tripId: string, date: string, at: number, signal?: AbortSignal) =>
    getJson<Trip>(
      `/api/trips/${encodeURIComponent(tripId)}?at=${Math.floor(at)}${date ? `&date=${date}` : ""}`,
      signal,
    ),
};

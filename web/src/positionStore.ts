import { api, type Position } from "./api";

// Positions are fetched in fixed chunks aligned to this many seconds
const CHUNK_SECONDS = 600;
// Chunks that may still receive data (they reach into the last few minutes) are re-fetched this often
const OPEN_CHUNK_REFRESH_MS = 15_000;
// A vehicle disappears this long after its last report
const STALE_AFTER_SECONDS = 180;
// Only interpolate between reports this close together; across bigger gaps, hold the last position
const MAX_INTERPOLATION_GAP_SECONDS = 150;
// Keep this much data either side of the playhead in memory
const RETAIN_SECONDS = 3600;

export interface VehicleAtTime {
  entityId: string;
  label: string;
  tripId: string;
  routeId: string;
  startDate: string;
  lon: number;
  lat: number;
  bearing: number;
  /** Seconds since this vehicle last reported */
  age: number;
}

interface ChunkState {
  loading: boolean;
  fetchedAt: number;
  failed: boolean;
}

/**
 * Loads vehicle positions around the playhead and answers "where was every vehicle at time t?",
 * interpolating between reports so playback is smooth at any speed.
 */
export class PositionStore {
  private tracks = new Map<string, Position[]>();
  private chunks = new Map<number, ChunkState>();
  private bearings = new Map<string, number>();
  private listeners = new Set<() => void>();
  lastError: string | null = null;

  /** Called when new data arrives, so a paused view can redraw */
  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Makes sure data around `time` is loaded or loading. Cheap to call every frame. */
  ensureLoaded(time: number): void {
    const chunk = Math.floor(time / CHUNK_SECONDS) * CHUNK_SECONDS;
    const nowSeconds = Date.now() / 1000;
    for (const start of [chunk - CHUNK_SECONDS, chunk, chunk + CHUNK_SECONDS]) {
      if (start > nowSeconds) continue;
      const state = this.chunks.get(start);
      const isOpen = start + CHUNK_SECONDS > nowSeconds - 300;
      const retryAfterMs = state?.failed ? 5_000 : OPEN_CHUNK_REFRESH_MS;
      const stale = state && (isOpen || state.failed) && Date.now() - state.fetchedAt > retryAfterMs;
      if (!state || (stale && !state.loading)) {
        void this.loadChunk(start);
      }
    }
    if (this.chunks.size > 16) this.evict(time);
  }

  private async loadChunk(start: number): Promise<void> {
    const previous = this.chunks.get(start);
    this.chunks.set(start, { loading: true, fetchedAt: previous?.fetchedAt ?? 0, failed: false });
    try {
      const positions = await api.positions(start, start + CHUNK_SECONDS - 1);
      this.merge(positions);
      this.chunks.set(start, { loading: false, fetchedAt: Date.now(), failed: false });
      this.lastError = null;
      this.listeners.forEach((listener) => listener());
    } catch (error) {
      this.chunks.set(start, { loading: false, fetchedAt: Date.now(), failed: true });
      this.lastError = error instanceof Error ? error.message : String(error);
      this.listeners.forEach((listener) => listener());
    }
  }

  private merge(positions: Position[]): void {
    const touched = new Set<string>();
    for (const position of positions) {
      let track = this.tracks.get(position.entityId);
      if (!track) {
        track = [];
        this.tracks.set(position.entityId, track);
      }
      track.push(position);
      touched.add(position.entityId);
    }
    for (const entityId of touched) {
      const track = this.tracks.get(entityId)!;
      track.sort((a, b) => a.t - b.t);
      // Re-fetched chunks repeat samples; keep one per timestamp
      const deduped = track.filter((position, i) => i === 0 || position.t !== track[i - 1].t);
      this.tracks.set(entityId, deduped);
    }
  }

  private evict(time: number): void {
    const keepFrom = time - RETAIN_SECONDS;
    const keepTo = time + RETAIN_SECONDS;
    for (const start of this.chunks.keys()) {
      if (start + CHUNK_SECONDS < keepFrom || start > keepTo) this.chunks.delete(start);
    }
    for (const [entityId, track] of this.tracks) {
      const kept = track.filter((position) => position.t >= keepFrom && position.t <= keepTo);
      if (kept.length === 0) this.tracks.delete(entityId);
      else this.tracks.set(entityId, kept);
    }
  }

  /** Every vehicle that had reported recently as of `time`, at its interpolated position */
  vehiclesAt(time: number): VehicleAtTime[] {
    const result: VehicleAtTime[] = [];
    for (const [entityId, track] of this.tracks) {
      const index = lastIndexAtOrBefore(track, time);
      if (index < 0) continue;
      const current = track[index];
      const age = time - current.t;
      if (age > STALE_AFTER_SECONDS) continue;

      let lon = current.lon;
      let lat = current.lat;
      const next = track[index + 1];
      if (next && next.t - current.t <= MAX_INTERPOLATION_GAP_SECONDS && next.t > current.t) {
        const fraction = (time - current.t) / (next.t - current.t);
        lon += (next.lon - current.lon) * fraction;
        lat += (next.lat - current.lat) * fraction;
      }

      result.push({
        entityId,
        label: current.label,
        tripId: current.tripId,
        routeId: current.routeId,
        startDate: current.startDate,
        lon,
        lat,
        bearing: this.bearingFor(entityId, current, next ?? track[index - 1], next !== undefined),
        age,
      });
    }
    return result;
  }

  /** The feed's bearing if it gives one; otherwise the direction of travel between reports */
  private bearingFor(entityId: string, current: Position, other: Position | undefined, otherIsNext: boolean): number {
    if (current.bearing !== 0) return current.bearing;
    if (other) {
      const [from, to] = otherIsNext ? [current, other] : [other, current];
      const dx = (to.lon - from.lon) * Math.cos((from.lat * Math.PI) / 180);
      const dy = to.lat - from.lat;
      // Ignore jitter while stopped (~10 m)
      if (Math.hypot(dx, dy) > 0.0001) {
        const bearing = ((Math.atan2(dx, dy) * 180) / Math.PI + 360) % 360;
        this.bearings.set(entityId, bearing);
        return bearing;
      }
    }
    return this.bearings.get(entityId) ?? 0;
  }
}

function lastIndexAtOrBefore(track: Position[], time: number): number {
  let low = 0;
  let high = track.length - 1;
  let result = -1;
  while (low <= high) {
    const middle = (low + high) >> 1;
    if (track[middle].t <= time) {
      result = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return result;
}

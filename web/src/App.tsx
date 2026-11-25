import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, type Activity, type Meta, type NetworkGeoJson, type Route, type Trip } from "./api";
import { compareRoutes, clockTime, longDate, routeColor, UNASSIGNED_COLOR } from "./format";
import { MapView, type MapController, type VehicleProperties } from "./MapView";
import { PositionStore, type VehicleAtTime } from "./positionStore";
import { RoutePicker } from "./RoutePicker";
import { TimeRibbon } from "./TimeRibbon";
import { TripPanel } from "./TripPanel";

// Live view runs this far behind real time, so there's always a newer report to move toward
const LIVE_LAG_SECONDS = 45;
const SPEEDS = [1, 10, 60] as const;
const UI_UPDATE_MS = 250;
const META_REFRESH_MS = 60_000;
const RETRY_MS = 5_000;
const TRIP_REFRESH_SECONDS = 30;

type Mode = "live" | "replay";

interface Clock {
  mode: Mode;
  time: number;
  playing: boolean;
  speed: number;
}

const nowSeconds = () => Date.now() / 1000;
const prefersReducedMotion = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;

export function App() {
  const [meta, setMeta] = useState<Meta | null>(null);
  const [routes, setRoutes] = useState<Route[]>([]);
  const [network, setNetwork] = useState<NetworkGeoJson | null>(null);
  const [activity, setActivity] = useState<Activity | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [mode, setMode] = useState<Mode>("live");
  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState<number>(1);
  const [displayTime, setDisplayTime] = useState(nowSeconds() - LIVE_LAG_SECONDS);

  const [selectedRouteId, setSelectedRouteId] = useState<string | null>(null);
  const [showUnassigned, setShowUnassigned] = useState(false);
  const [selectedEntityId, setSelectedEntityId] = useState<string | null>(null);
  const [selectedLabel, setSelectedLabel] = useState("");
  const [selectedVehicle, setSelectedVehicle] = useState<VehicleAtTime | null>(null);
  const [vehicleCount, setVehicleCount] = useState(0);
  const [dataError, setDataError] = useState<string | null>(null);

  const [trip, setTrip] = useState<Trip | null>(null);
  const [tripError, setTripError] = useState<string | null>(null);
  const [tripLoading, setTripLoading] = useState(false);

  const store = useMemo(() => new PositionStore(), []);
  const mapController = useRef<MapController | null>(null);
  const clock = useRef<Clock>({ mode: "live", time: nowSeconds() - LIVE_LAG_SECONDS, playing: true, speed: 1 });
  const routesById = useMemo(() => new Map(routes.map((route) => [route.id, route])), [routes]);

  // Values the animation loop reads without restarting
  const filters = useRef({ selectedRouteId, showUnassigned, selectedEntityId, routesById });
  filters.current = { selectedRouteId, showUnassigned, selectedEntityId, routesById };

  // What's available (time range, schedule version), refreshed periodically. Routes and the
  // network are reloaded whenever the schedule version changes, including the first time
  // the API becomes reachable. While the API is unreachable, retry sooner.
  useEffect(() => {
    const controller = new AbortController();
    let loadedVersion = "";
    let timer = 0;
    const refresh = async () => {
      let failed = false;
      try {
        const newMeta = await api.meta(controller.signal);
        setMeta(newMeta);
        if (newMeta.feedVersion && newMeta.feedVersion !== loadedVersion) {
          const [newRoutes, newNetwork] = await Promise.all([api.routes(controller.signal), api.network(controller.signal)]);
          setRoutes([...newRoutes].sort(compareRoutes));
          setNetwork(newNetwork);
          loadedVersion = newMeta.feedVersion;
        }
        if (newMeta.earliestPosition !== null && newMeta.latestPosition !== null) {
          const to = Math.max(newMeta.latestPosition, nowSeconds());
          setActivity(await api.activity(newMeta.earliestPosition, Math.floor(to), 360, controller.signal));
        }
        setLoadError(null);
      } catch (error) {
        if (controller.signal.aborted) return;
        failed = true;
        setLoadError(error instanceof Error ? error.message : String(error));
      }
      timer = window.setTimeout(refresh, failed ? RETRY_MS : META_REFRESH_MS);
    };
    void refresh();
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, []);

  const drawVehicles = useCallback(
    (time: number) => {
      const { selectedRouteId, showUnassigned, selectedEntityId, routesById } = filters.current;
      const vehicles = store.vehiclesAt(time);
      const features: GeoJSON.Feature<GeoJSON.Point, VehicleProperties>[] = [];
      let selected: VehicleAtTime | null = null;
      for (const vehicle of vehicles) {
        if (vehicle.entityId === selectedEntityId) selected = vehicle;
        const assigned = vehicle.tripId !== "";
        if (!assigned && !showUnassigned && vehicle.entityId !== selectedEntityId) continue;
        if (selectedRouteId && vehicle.routeId !== selectedRouteId) continue;
        const color = assigned ? routeColor(routesById.get(vehicle.routeId)) : UNASSIGNED_COLOR;
        features.push({
          type: "Feature",
          properties: { id: vehicle.entityId, color: color.slice(1), bearing: vehicle.bearing },
          geometry: { type: "Point", coordinates: [vehicle.lon, vehicle.lat] },
        });
      }
      mapController.current?.setVehicles({ type: "FeatureCollection", features });
      return { count: features.length, selected };
    },
    [store],
  );

  // The playback loop: advance the clock, keep data loaded, move vehicles
  useEffect(() => {
    let frame = 0;
    let last = performance.now();
    let lastDraw = 0;
    let lastUi = 0;
    let dataArrived = false;
    const unsubscribe = store.subscribe(() => (dataArrived = true));
    const drawInterval = prefersReducedMotion() ? 1000 : 1000 / 30;

    const tick = (now: number) => {
      const elapsed = (now - last) / 1000;
      last = now;
      const state = clock.current;
      const liveEdge = nowSeconds() - LIVE_LAG_SECONDS;
      if (state.mode === "live") {
        state.time = liveEdge;
      } else if (state.playing) {
        state.time += elapsed * state.speed;
        if (state.time >= liveEdge) {
          state.mode = "live";
          state.time = liveEdge;
          setMode("live");
        }
      }
      store.ensureLoaded(state.time);

      const moving = state.mode === "live" || state.playing;
      if ((moving && now - lastDraw >= drawInterval) || dataArrived) {
        const { count, selected } = drawVehicles(state.time);
        lastDraw = now;
        if (now - lastUi >= UI_UPDATE_MS || dataArrived) {
          lastUi = now;
          setDisplayTime(state.time);
          setVehicleCount(count);
          setSelectedVehicle(selected);
          setDataError(store.lastError);
        }
        dataArrived = false;
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      unsubscribe();
    };
  }, [store, drawVehicles]);

  // Redraw immediately when filters change while paused
  useEffect(() => {
    const { count, selected } = drawVehicles(clock.current.time);
    setVehicleCount(count);
    setSelectedVehicle(selected);
  }, [drawVehicles, selectedRouteId, showUnassigned, selectedEntityId, routesById]);

  const scrubTo = useCallback(
    (time: number) => {
      const state = clock.current;
      state.mode = "replay";
      state.time = Math.min(time, nowSeconds() - LIVE_LAG_SECONDS);
      setMode("replay");
      store.ensureLoaded(state.time);
      const { count, selected } = drawVehicles(state.time);
      setDisplayTime(state.time);
      setVehicleCount(count);
      setSelectedVehicle(selected);
    },
    [store, drawVehicles],
  );

  const goLive = () => {
    clock.current.mode = "live";
    clock.current.playing = true;
    setMode("live");
    setPlaying(true);
  };

  const togglePlaying = useCallback(() => {
    const state = clock.current;
    if (state.mode === "live") {
      // Pausing live freezes the current moment as a replay
      state.mode = "replay";
      state.playing = false;
      setMode("replay");
      setPlaying(false);
      return;
    }
    state.playing = !state.playing;
    setPlaying(state.playing);
  }, []);

  const changeSpeed = (newSpeed: number) => {
    clock.current.speed = newSpeed;
    setSpeed(newSpeed);
  };

  // Space plays and pauses, unless typing
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (event.key !== " " || target.closest("input, button, [role=slider]")) return;
      event.preventDefault();
      togglePlaying();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [togglePlaying]);

  const selectVehicle = useCallback(
    (entityId: string | null) => {
      setSelectedEntityId(entityId);
      setTrip(null);
      setTripError(null);
      if (entityId) {
        const vehicle = store.vehiclesAt(clock.current.time).find((candidate) => candidate.entityId === entityId);
        setSelectedLabel(vehicle?.label ?? entityId);
        setSelectedVehicle(vehicle ?? null);
      }
    },
    [store],
  );
  const closePanel = useCallback(() => selectVehicle(null), [selectVehicle]);

  // Load the selected vehicle's trip, and refresh it as the playhead moves
  const tripRequest = useRef<{ key: string; time: number } | null>(null);
  const tripId = selectedVehicle?.tripId ?? "";
  const startDate = selectedVehicle?.startDate ?? "";
  useEffect(() => {
    if (!selectedEntityId || !tripId) {
      tripRequest.current = null;
      return;
    }
    const key = `${tripId}/${startDate}`;
    const previous = tripRequest.current;
    if (previous && previous.key === key && Math.abs(previous.time - displayTime) < TRIP_REFRESH_SECONDS) return;
    if (!previous || previous.key !== key) setTrip(null);
    tripRequest.current = { key, time: displayTime };

    const controller = new AbortController();
    setTripLoading(true);
    api
      .trip(tripId, startDate, displayTime, controller.signal)
      .then((newTrip) => {
        setTrip(newTrip);
        setTripError(null);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setTrip(null);
        setTripError(
          error instanceof ApiError && error.status === 404
            ? "This trip isn't in the stored schedule, so there are no stop times to show."
            : `Couldn't load this trip: ${error instanceof Error ? error.message : String(error)}`,
        );
        // Allow a retry on the next refresh
        tripRequest.current = { key, time: displayTime };
      })
      .finally(() => {
        if (!controller.signal.aborted) setTripLoading(false);
      });
    return () => controller.abort();
  }, [selectedEntityId, tripId, startDate, displayTime]);

  const timeZone = meta?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
  const rangeFrom = meta?.earliestPosition ?? displayTime - 3600;
  const rangeTo = Math.max(meta?.latestPosition ?? 0, nowSeconds() - LIVE_LAG_SECONDS, rangeFrom + 60);
  const selectedRoute = selectedRouteId ? routesById.get(selectedRouteId) : undefined;
  const vehicleRoute = selectedVehicle ? routesById.get(selectedVehicle.routeId) : undefined;

  const emptyState = loadError
    ? {
        title: "Can't reach the API",
        body: `${loadError} Start it with "gtfs-analyze serve --db-path <your database>" and reload.`,
      }
    : meta && !meta.feedVersion
      ? {
          title: "No schedule stored yet",
          body: 'Run "gtfs-analyze store --agency <id> --db-path <your database>" to load the agency\'s schedule, then reload.',
        }
      : meta && meta.latestPosition === null
        ? {
            title: "No vehicle positions stored yet",
            body: 'Run "gtfs-analyze store --agency <id> --db-path <your database>" and leave it running for a minute, then reload.',
          }
        : null;

  return (
    <div className={selectedEntityId ? "app with-panel" : "app"}>
      <MapView
        center={meta?.center ?? null}
        network={network}
        selectedRouteId={selectedRouteId}
        selectedEntityId={selectedEntityId}
        trip={selectedEntityId ? trip : null}
        time={displayTime}
        onSelectVehicle={selectVehicle}
        controllerRef={mapController}
      />

      <header className="topbar">
        <h1 className="agency">{meta?.agencyName || "Transit map"}</h1>
        <RoutePicker routes={routes} selectedRouteId={selectedRouteId} onSelect={setSelectedRouteId} />
        <label className="toggle">
          <input type="checkbox" checked={showUnassigned} onChange={(event) => setShowUnassigned(event.target.checked)} />
          Show vehicles without a trip
        </label>
      </header>

      {selectedEntityId && (
        <TripPanel
          vehicle={selectedVehicle}
          label={selectedLabel}
          route={vehicleRoute}
          trip={trip}
          tripError={tripError}
          loading={tripLoading}
          time={displayTime}
          timeZone={timeZone}
          onClose={closePanel}
        />
      )}

      {emptyState && (
        <div className="notice" role="alert">
          <h2>{emptyState.title}</h2>
          <p>{emptyState.body}</p>
        </div>
      )}

      <footer className="timebar">
        <div className="timebar-row">
          <div className="clock" aria-live="off">
            <span className="clock-time">{clockTime(displayTime, timeZone)}</span>
            <span className="clock-date">{longDate(displayTime, timeZone)}</span>
          </div>

          <div className="controls">
            <button type="button" className="play-button" onClick={togglePlaying} aria-label={mode === "live" || playing ? "Pause" : "Play"}>
              {mode === "live" || playing ? <PauseIcon /> : <PlayIcon />}
            </button>
            <div className="speeds" role="group" aria-label="Playback speed">
              {SPEEDS.map((option) => (
                <button
                  key={option}
                  type="button"
                  aria-pressed={speed === option}
                  disabled={mode === "live"}
                  onClick={() => changeSpeed(option)}
                >
                  {option}×
                </button>
              ))}
            </div>
            {mode === "live" ? (
              <span className="live-indicator">
                <span className="live-dot" aria-hidden="true" />
                Live
              </span>
            ) : (
              <button type="button" className="go-live" onClick={goLive}>
                Go live
              </button>
            )}
          </div>

          <p className="status">
            {dataError
              ? `Couldn't load positions: ${dataError}`
              : `${vehicleCount} ${vehicleCount === 1 ? "vehicle" : "vehicles"}${selectedRoute ? ` on ${selectedRoute.shortName || selectedRoute.id}` : ""}`}
          </p>
        </div>
        <TimeRibbon activity={activity} from={rangeFrom} to={rangeTo} time={displayTime} timeZone={timeZone} onScrub={scrubTo} />
      </footer>
    </div>
  );
}

function PlayIcon() {
  return (
    <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true">
      <path d="M6 4 L16 10 L6 16 Z" fill="currentColor" />
    </svg>
  );
}

function PauseIcon() {
  return (
    <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true">
      <rect x="5" y="4" width="3.5" height="12" rx="1" fill="currentColor" />
      <rect x="11.5" y="4" width="3.5" height="12" rx="1" fill="currentColor" />
    </svg>
  );
}

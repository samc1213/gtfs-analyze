import { useEffect, useRef } from "react";
import type { Route, Trip, TripStop } from "./api";
import { describeDelay, punctuality, routeColor, shortTime, vehicleNoun } from "./format";
import { RouteBadge } from "./RoutePicker";
import type { VehicleAtTime } from "./positionStore";

interface TripPanelProps {
  vehicle: VehicleAtTime | null;
  /** The vehicle's label, kept after it stops reporting so the panel doesn't go blank */
  label: string;
  route: Route | undefined;
  trip: Trip | null;
  tripError: string | null;
  loading: boolean;
  time: number;
  timeZone: string;
  onClose: () => void;
}

export function TripPanel({ vehicle, label, route, trip, tripError, loading, time, timeZone, onClose }: TripPanelProps) {
  const nextStopRef = useRef<HTMLLIElement>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  const nextStopIndex = trip ? trip.stops.findIndex((stop) => !stop.skipped && arrival(stop) !== null && arrival(stop)! >= time) : -1;

  // Keep the next stop in view as the trip progresses
  useEffect(() => {
    nextStopRef.current?.scrollIntoView({ block: "center", behavior: "auto" });
  }, [nextStopIndex, trip?.tripId]);

  const noun = vehicleNoun(route);
  const color = trip ? routeColor({ color: trip.routeColor }) : routeColor(route);

  return (
    <aside className="trip-panel" aria-label={`${noun} ${label}`}>
      <header className="trip-header">
        <div className="trip-title">
          {trip ? (
            <RouteBadge route={{ id: trip.routeId, shortName: trip.routeShortName, color: trip.routeColor, textColor: trip.routeTextColor }} />
          ) : (
            route && <RouteBadge route={route} />
          )}
          <h2>{trip?.headsign || route?.longName || `${noun} ${label}`}</h2>
        </div>
        <button type="button" className="text-button" onClick={onClose}>
          Close
        </button>
      </header>

      <p className="trip-vehicle">
        {noun} {label}
        {vehicle && vehicle.age > 75 && <>, last reported {Math.round(vehicle.age / 60)} min before this time</>}
        {!vehicle && <>, not reporting at this time</>}
      </p>

      {trip && trip.delaySeconds !== null && (
        <p className={`trip-delay ${punctuality(trip.delaySeconds)}`}>{describeDelay(trip.delaySeconds)}</p>
      )}
      {trip && trip.delaySeconds === null && <p className="trip-delay unknown">No predictions for this trip yet</p>}

      {!trip && !loading && !tripError && (
        <p className="trip-message">This {noun.toLowerCase()} isn't running a scheduled trip right now.</p>
      )}
      {tripError && <p className="trip-message">{tripError}</p>}

      {trip && (
        <ol className="stop-list" style={{ "--route-color": color } as React.CSSProperties}>
          {trip.stops.map((stop, index) => {
            const passed = nextStopIndex === -1 ? (arrival(stop) ?? 0) < time : index < nextStopIndex;
            const isNext = index === nextStopIndex;
            return (
              <li
                key={stop.sequence}
                ref={isNext ? nextStopRef : undefined}
                className={["stop", passed && "passed", isNext && "next", stop.skipped && "skipped"].filter(Boolean).join(" ")}
              >
                <span className="stop-name">{stop.name || stop.stopId}</span>
                <StopTimes stop={stop} timeZone={timeZone} />
              </li>
            );
          })}
        </ol>
      )}
    </aside>
  );
}

function arrival(stop: TripStop): number | null {
  return stop.predictedArrival ?? stop.scheduledArrival;
}

function StopTimes({ stop, timeZone }: { stop: TripStop; timeZone: string }) {
  if (stop.skipped) return <span className="stop-times">Skipped</span>;
  const { scheduledArrival: scheduled, predictedArrival: predicted } = stop;
  if (predicted !== null && scheduled !== null && Math.abs(predicted - scheduled) >= 60) {
    return (
      <span className="stop-times">
        <s className="scheduled" aria-label={`scheduled ${shortTime(scheduled, timeZone)}`}>
          {shortTime(scheduled, timeZone)}
        </s>
        <span className={`predicted ${punctuality(predicted - scheduled)}`}>{shortTime(predicted, timeZone)}</span>
      </span>
    );
  }
  const shown = predicted ?? scheduled;
  return <span className="stop-times">{shown !== null ? shortTime(shown, timeZone) : ""}</span>;
}

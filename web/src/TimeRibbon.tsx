import { useMemo, useRef } from "react";
import type { Activity } from "./api";
import { clockTime, longDate, shortTime, tickDate } from "./format";

interface TimeRibbonProps {
  activity: Activity | null;
  from: number;
  to: number;
  time: number;
  timeZone: string;
  onScrub: (time: number) => void;
}

const TICK_STEPS = [900, 1800, 3600, 7200, 10800, 21600, 43200, 86400, 172800, 604800];
const MAX_TICKS = 8;

/**
 * The timeline: how many vehicles were in service across everything stored, with the
 * playhead on top. Drag or click to move through time; arrow keys step by a minute.
 */
export function TimeRibbon({ activity, from, to, time, timeZone, onScrub }: TimeRibbonProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const span = Math.max(1, to - from);
  const fraction = Math.min(1, Math.max(0, (time - from) / span));

  const maxVehicles = useMemo(() => Math.max(1, ...(activity?.vehicles ?? [1])), [activity]);
  const ticks = useMemo(() => makeTicks(from, to, timeZone), [from, to, timeZone]);

  const timeAtPointer = (clientX: number) => {
    const rect = trackRef.current!.getBoundingClientRect();
    const x = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    return from + x * span;
  };

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    onScrub(timeAtPointer(event.clientX));
  };
  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) onScrub(timeAtPointer(event.clientX));
  };
  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const step = event.shiftKey ? 600 : 60;
    const target = {
      ArrowLeft: time - step,
      ArrowRight: time + step,
      Home: from,
      End: to,
    }[event.key];
    if (target !== undefined) {
      event.preventDefault();
      onScrub(Math.min(to, Math.max(from, target)));
    }
  };

  const bars = activity?.vehicles ?? [];
  return (
    <div className="ribbon">
      <div
        ref={trackRef}
        className="ribbon-track"
        role="slider"
        tabIndex={0}
        aria-label="Time"
        aria-valuemin={from}
        aria-valuemax={to}
        aria-valuenow={Math.round(time)}
        aria-valuetext={`${longDate(time, timeZone)}, ${clockTime(time, timeZone)}`}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onKeyDown={onKeyDown}
      >
        <svg className="ribbon-bars" viewBox={`0 0 ${span} 100`} preserveAspectRatio="none" aria-hidden="true">
          {activity &&
            bars.map((vehicles, i) => {
              const start = activity.from + i * activity.bucketSize;
              if (vehicles === 0 || start > to) return null;
              const height = (vehicles / maxVehicles) * 92;
              return (
                <rect
                  key={i}
                  x={start - from}
                  y={100 - height}
                  width={Math.min(activity.bucketSize, to - start) * 0.86}
                  height={height}
                  className={start < time ? "bar bar-past" : "bar"}
                />
              );
            })}
        </svg>
        {ticks.map((tick) => (
          <span key={tick.time} className="ribbon-tick" style={{ left: `${((tick.time - from) / span) * 100}%` }}>
            {tick.label}
          </span>
        ))}
        <div className="playhead" style={{ left: `${fraction * 100}%` }} aria-hidden="true" />
      </div>
    </div>
  );
}

function makeTicks(from: number, to: number, timeZone: string): { time: number; label: string }[] {
  const span = to - from;
  if (span <= 0) return [];
  const step = TICK_STEPS.find((candidate) => span / candidate <= MAX_TICKS) ?? TICK_STEPS[TICK_STEPS.length - 1];
  const ticks: { time: number; label: string }[] = [];
  // Ticks fall on whole steps in local time (zones with whole-hour offsets, like America/Denver)
  const offset = localOffsetSeconds(from, timeZone);
  for (let t = Math.ceil((from + offset) / step) * step - offset; t <= to; t += step) {
    const midnight = (t + offset) % 86400 === 0;
    ticks.push({ time: t, label: step >= 86400 || midnight ? tickDate(t, timeZone) : shortTime(t, timeZone) });
  }
  return ticks;
}

/** Seconds to add to a UTC time to get local wall-clock time */
function localOffsetSeconds(unix: number, timeZone: string): number {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    hourCycle: "h23",
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
    second: "numeric",
  }).formatToParts(unix * 1000);
  const get = (type: string) => Number(parts.find((part) => part.type === type)?.value);
  const local = Date.UTC(get("year"), get("month") - 1, get("day"), get("hour"), get("minute"), get("second")) / 1000;
  return local - Math.floor(unix);
}

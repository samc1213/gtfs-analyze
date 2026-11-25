import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { Route } from "./api";
import { routeColor, routeTextColor } from "./format";

interface RoutePickerProps {
  routes: Route[];
  selectedRouteId: string | null;
  onSelect: (routeId: string | null) => void;
}

/** Search box for showing a single route. Matches route number or name. */
export function RoutePicker({ routes, selectedRouteId, onSelect }: RoutePickerProps) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [highlighted, setHighlighted] = useState(0);
  const listId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const selected = routes.find((route) => route.id === selectedRouteId);

  const matches = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return routes;
    return routes.filter(
      (route) => route.shortName.toLowerCase().startsWith(needle) || route.longName.toLowerCase().includes(needle),
    );
  }, [routes, query]);

  useEffect(() => setHighlighted(0), [query]);

  // Close when clicking elsewhere
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const choose = (route: Route) => {
    onSelect(route.id);
    setQuery("");
    setOpen(false);
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setHighlighted((index) => Math.min(matches.length - 1, index + 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlighted((index) => Math.max(0, index - 1));
    } else if (event.key === "Enter" && open && matches[highlighted]) {
      event.preventDefault();
      choose(matches[highlighted]);
    } else if (event.key === "Escape") {
      setOpen(false);
    }
  };

  return (
    <div className="route-picker" ref={rootRef}>
      {selected ? (
        <div className="route-chosen">
          <RouteBadge route={selected} />
          <span className="route-chosen-name">{selected.longName}</span>
          <button type="button" className="text-button" onClick={() => onSelect(null)}>
            Show all routes
          </button>
        </div>
      ) : (
        <>
          <input
            className="route-search"
            type="search"
            placeholder="Find a route, like 15 or Colfax"
            aria-label="Find a route"
            role="combobox"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={open && matches[highlighted] ? `${listId}-${matches[highlighted].id}` : undefined}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setOpen(true);
            }}
            onFocus={() => setOpen(true)}
            onKeyDown={onKeyDown}
          />
          {open && (
            <ul className="route-list" id={listId} role="listbox" aria-label="Routes">
              {matches.length === 0 && <li className="route-empty">No route matches "{query}"</li>}
              {matches.map((route, index) => (
                <li
                  key={route.id}
                  id={`${listId}-${route.id}`}
                  role="option"
                  aria-selected={index === highlighted}
                  className={index === highlighted ? "route-option highlighted" : "route-option"}
                  onPointerEnter={() => setHighlighted(index)}
                  onClick={() => choose(route)}
                >
                  <RouteBadge route={route} />
                  <span>{route.longName}</span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}

export function RouteBadge({ route }: { route: Pick<Route, "shortName" | "id" | "color" | "textColor"> }) {
  return (
    <span className="route-badge" style={{ background: routeColor(route), color: routeTextColor(route) }}>
      {route.shortName || route.id}
    </span>
  );
}

import { useEffect, useRef, useState } from "react";
import { Map as MapLibreMap, NavigationControl, setWorkerUrl, type ExpressionSpecification, type GeoJSONSource } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
// MapLibre loads its worker from a URL relative to its own module, which breaks once Vite
// bundles it. Have Vite bundle the worker too, and tell MapLibre where it ended up.
import maplibreWorkerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";

setWorkerUrl(maplibreWorkerUrl);
import type { NetworkGeoJson, Trip } from "./api";

const MAP_STYLE: string = import.meta.env.VITE_MAP_STYLE ?? "https://tiles.openfreemap.org/styles/positron";
const DEFAULT_CENTER: [number, number] = [-104.99, 39.74];
const AMBER = "#F2A900";
const EMPTY: GeoJSON.FeatureCollection = { type: "FeatureCollection", features: [] };

export interface VehicleProperties {
  id: string;
  /** Route color without '#', used to pick the icon */
  color: string;
  bearing: number;
}

/** Imperative handle so the playback loop can move vehicles without re-rendering React */
export interface MapController {
  setVehicles(vehicles: GeoJSON.FeatureCollection<GeoJSON.Point, VehicleProperties>): void;
}

interface MapViewProps {
  center: [number, number] | null;
  network: NetworkGeoJson | null;
  selectedRouteId: string | null;
  selectedEntityId: string | null;
  trip: Trip | null;
  /** Playhead time, to grey out stops the trip has passed */
  time: number;
  onSelectVehicle: (entityId: string | null) => void;
  controllerRef: React.RefObject<MapController | null>;
}

export function MapView(props: MapViewProps) {
  const { center, network, selectedRouteId, selectedEntityId, trip, time, onSelectVehicle, controllerRef } = props;
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const [loaded, setLoaded] = useState(false);
  const onSelectRef = useRef(onSelectVehicle);
  onSelectRef.current = onSelectVehicle;

  // Create the map once
  useEffect(() => {
    const map = new MapLibreMap({
      container: containerRef.current!,
      style: MAP_STYLE,
      center: DEFAULT_CENTER,
      zoom: 11,
      attributionControl: { compact: true },
    });
    mapRef.current = map;
    // Handy for debugging in the browser console during development
    if (import.meta.env.DEV) (window as unknown as { map: MapLibreMap }).map = map;
    map.addControl(new NavigationControl({ showCompass: true }), "top-right");

    map.on("load", () => {
      // Draw route lines under the basemap's labels so street and place names stay readable
      const firstLabelLayer = map.getStyle().layers.find((layer) => layer.type === "symbol")?.id;

      map.addSource("network", { type: "geojson", data: EMPTY });
      map.addLayer(
        {
          id: "network",
          type: "line",
          source: "network",
          layout: { "line-join": "round", "line-cap": "round" },
          paint: {
            "line-color": ["case", ["==", ["get", "color"], ""], "#0076CE", ["concat", "#", ["get", "color"]]],
            "line-width": [
              "interpolate", ["linear"], ["zoom"],
              9, ["case", ["<=", ["get", "routeType"], 2], 2, 0.6],
              14, ["case", ["<=", ["get", "routeType"], 2], 5, 2.5],
            ],
          },
        },
        firstLabelLayer,
      );

      map.addSource("trip-shape", { type: "geojson", data: EMPTY });
      map.addLayer({
        id: "trip-shape-casing",
        type: "line",
        source: "trip-shape",
        layout: { "line-join": "round", "line-cap": "round" },
        paint: { "line-color": "#FFFFFF", "line-width": ["interpolate", ["linear"], ["zoom"], 10, 6, 15, 11] },
      });
      map.addLayer({
        id: "trip-shape",
        type: "line",
        source: "trip-shape",
        layout: { "line-join": "round", "line-cap": "round" },
        paint: { "line-color": ["get", "color"], "line-width": ["interpolate", ["linear"], ["zoom"], 10, 3, 15, 6] },
      });

      map.addSource("trip-stops", { type: "geojson", data: EMPTY });
      map.addLayer({
        id: "trip-stops",
        type: "circle",
        source: "trip-stops",
        paint: {
          "circle-radius": ["interpolate", ["linear"], ["zoom"], 10, 2.5, 15, 5],
          "circle-color": "#FFFFFF",
          "circle-stroke-width": ["interpolate", ["linear"], ["zoom"], 10, 1.5, 15, 2.5],
          "circle-stroke-color": ["case", ["get", "passed"], "#A7B0BA", ["get", "color"]],
        },
      });

      map.addSource("vehicles", { type: "geojson", data: EMPTY });
      map.addLayer({
        id: "vehicle-selected",
        type: "circle",
        source: "vehicles",
        filter: ["==", ["get", "id"], ""],
        paint: {
          "circle-radius": ["interpolate", ["linear"], ["zoom"], 10, 11, 16, 20],
          "circle-color": "rgba(242, 169, 0, 0.18)",
          "circle-stroke-color": AMBER,
          "circle-stroke-width": 2.5,
        },
      });
      map.addLayer({
        id: "vehicles",
        type: "symbol",
        source: "vehicles",
        layout: {
          "icon-image": ["concat", "chevron-", ["get", "color"]],
          "icon-rotate": ["get", "bearing"],
          "icon-rotation-alignment": "map",
          "icon-allow-overlap": true,
          "icon-ignore-placement": true,
          "icon-size": ["interpolate", ["linear"], ["zoom"], 9, 0.45, 13, 0.75, 17, 1.1],
        },
      });

      // Tap targets: find the nearest vehicle within a few pixels
      map.on("click", (event) => {
        const padding = 8;
        const features = map.queryRenderedFeatures(
          [
            [event.point.x - padding, event.point.y - padding],
            [event.point.x + padding, event.point.y + padding],
          ],
          { layers: ["vehicles"] },
        );
        onSelectRef.current(features.length > 0 ? String(features[0].properties.id) : null);
      });
      map.on("mouseenter", "vehicles", () => (map.getCanvas().style.cursor = "pointer"));
      map.on("mouseleave", "vehicles", () => (map.getCanvas().style.cursor = ""));

      setLoaded(true);
    });

    controllerRef.current = {
      setVehicles(vehicles) {
        const source = map.getSource("vehicles") as GeoJSONSource | undefined;
        if (!source) return;
        // Vehicle icons are drawn as needed, one per route color, before any vehicle uses them
        for (const feature of vehicles.features) {
          const imageId = `chevron-${feature.properties.color}`;
          if (!map.hasImage(imageId)) map.addImage(imageId, drawChevron(`#${feature.properties.color}`), { pixelRatio: 2 });
        }
        source.setData(vehicles);
      },
    };

    return () => {
      controllerRef.current = null;
      map.remove();
      mapRef.current = null;
    };
  }, [controllerRef]);

  useEffect(() => {
    if (center) mapRef.current?.jumpTo({ center });
  }, [center]);

  useEffect(() => {
    if (!loaded || !network) return;
    (mapRef.current!.getSource("network") as GeoJSONSource).setData(network);
  }, [loaded, network]);

  // Highlight the selected route; fade the rest
  useEffect(() => {
    if (!loaded) return;
    const rail = ["<=", ["get", "routeType"], 2];
    const opacity = selectedRouteId
      ? ["case", ["==", ["get", "routeId"], selectedRouteId], 0.95, 0.1]
      : ["case", rail, 0.85, 0.2];
    mapRef.current!.setPaintProperty("network", "line-opacity", opacity as ExpressionSpecification);
  }, [loaded, selectedRouteId]);

  useEffect(() => {
    if (!loaded) return;
    mapRef.current!.setFilter("vehicle-selected", ["==", ["get", "id"], selectedEntityId ?? ""]);
  }, [loaded, selectedEntityId]);

  // The selected trip's path, and its stops greyed out once passed
  useEffect(() => {
    if (!loaded) return;
    const map = mapRef.current!;
    const color = trip?.routeColor ? `#${trip.routeColor}` : "#0076CE";
    const shape: GeoJSON.FeatureCollection =
      trip && trip.shape.length > 1
        ? { type: "FeatureCollection", features: [{ type: "Feature", properties: { color }, geometry: { type: "LineString", coordinates: trip.shape } }] }
        : EMPTY;
    const stops: GeoJSON.FeatureCollection = trip
      ? {
          type: "FeatureCollection",
          features: trip.stops
            .filter((stop) => stop.lat !== 0)
            .map((stop) => ({
              type: "Feature",
              properties: { color, passed: (stop.predictedArrival ?? stop.scheduledArrival ?? Infinity) < time },
              geometry: { type: "Point", coordinates: [stop.lon, stop.lat] },
            })),
        }
      : EMPTY;
    (map.getSource("trip-shape") as GeoJSONSource).setData(shape);
    (map.getSource("trip-stops") as GeoJSONSource).setData(stops);
  }, [loaded, trip, time]);

  return <div ref={containerRef} className="map" role="region" aria-label="Map of vehicles and routes" />;
}

/** A chevron pointing up (north), in the route color with a white outline */
function drawChevron(color: string): ImageData {
  const size = 44;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const context = canvas.getContext("2d")!;
  context.beginPath();
  context.moveTo(22, 5);
  context.lineTo(36, 37);
  context.lineTo(22, 29);
  context.lineTo(8, 37);
  context.closePath();
  context.lineJoin = "round";
  context.lineWidth = 5;
  context.strokeStyle = "#FFFFFF";
  context.stroke();
  context.fillStyle = color;
  context.fill();
  return context.getImageData(0, 0, size, size);
}

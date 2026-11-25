/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Origin of the gtfs-analyze API, e.g. https://api.example.com. Empty means same origin (/api). */
  readonly VITE_API_BASE?: string;
  /** MapLibre style URL for the basemap */
  readonly VITE_MAP_STYLE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

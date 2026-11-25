package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
)

// Douglas-Peucker tolerance in degrees (~5 m): invisible at city zoom, and cuts RTD's
// network from hundreds of thousands of points to a size that loads instantly
const networkSimplifyTolerance = 0.00005

type geoJsonFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJsonFeature `json:"features"`
}

type geoJsonFeature struct {
	Type       string          `json:"type"`
	Properties networkProperty `json:"properties"`
	Geometry   geoJsonGeometry `json:"geometry"`
}

type geoJsonGeometry struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type networkProperty struct {
	RouteId     string `json:"routeId"`
	ShortName   string `json:"shortName"`
	RouteType   int    `json:"routeType"`
	Color       string `json:"color"`
	DirectionId int    `json:"directionId"`
}

// handleNetwork returns the route network as GeoJSON: for each route and direction,
// its longest shape, simplified. Cached per static feed version.
func (server *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	version, err := server.latestFeedVersion(r)
	if err != nil {
		server.writeError(w, r, err)
		return
	}

	server.networkLock.Lock()
	defer server.networkLock.Unlock()
	if server.networkVersion != version {
		network, err := server.buildNetwork(r, version)
		if err != nil {
			server.writeError(w, r, err)
			return
		}
		var buffer bytes.Buffer
		if err := json.NewEncoder(&buffer).Encode(network); err != nil {
			server.writeError(w, r, err)
			return
		}
		server.networkVersion, server.networkJson = version, buffer.Bytes()
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(server.networkJson)
}

func (server *Server) buildNetwork(r *http.Request, version string) (*geoJsonFeatureCollection, error) {
	db := server.db.WithContext(r.Context())

	// Point counts of every shape used by each route and direction
	var candidates []struct {
		RouteId     string
		DirectionId int
		ShapeId     string
		Points      int
		ShortName   *string
		Type        int
		Color       *string
	}
	err := db.Raw(`SELECT t.route_id, t.direction_id, t.shape_id, COUNT(*) AS points, r.short_name, r.type, r.color
		FROM (SELECT DISTINCT route_id, direction_id, shape_id FROM trips WHERE version = ? AND shape_id != '') t
		JOIN shapes s ON s.version = ? AND s.id = t.shape_id
		LEFT JOIN routes r ON r.version = ? AND r.id = t.route_id
		GROUP BY t.route_id, t.direction_id, t.shape_id`, version, version, version).Scan(&candidates).Error
	if err != nil {
		return nil, err
	}

	// Keep the longest shape per route and direction: usually the full-length pattern
	type routeDirection struct {
		routeId     string
		directionId int
	}
	longest := make(map[routeDirection]int)
	var order []routeDirection
	for i, candidate := range candidates {
		key := routeDirection{candidate.RouteId, candidate.DirectionId}
		best, seen := longest[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || candidate.Points > candidates[best].Points {
			longest[key] = i
		}
	}

	collection := &geoJsonFeatureCollection{Type: "FeatureCollection", Features: []geoJsonFeature{}}
	for _, key := range order {
		candidate := candidates[longest[key]]
		var points []struct{ Latitude, Longitude float64 }
		err := db.Raw("SELECT latitude, longitude FROM shapes WHERE version = ? AND id = ? ORDER BY sequence", version, candidate.ShapeId).Scan(&points).Error
		if err != nil {
			return nil, err
		}
		coordinates := make([][2]float64, len(points))
		for i, point := range points {
			coordinates[i] = [2]float64{point.Longitude, point.Latitude}
		}
		collection.Features = append(collection.Features, geoJsonFeature{
			Type: "Feature",
			Properties: networkProperty{
				RouteId:     candidate.RouteId,
				ShortName:   deref(candidate.ShortName),
				RouteType:   candidate.Type,
				Color:       deref(candidate.Color),
				DirectionId: candidate.DirectionId,
			},
			Geometry: geoJsonGeometry{Type: "LineString", Coordinates: simplify(coordinates, networkSimplifyTolerance)},
		})
	}
	return collection, nil
}

// simplify reduces a polyline with the Douglas-Peucker algorithm, keeping its endpoints
func simplify(points [][2]float64, tolerance float64) [][2]float64 {
	if len(points) < 3 {
		return points
	}
	keep := make([]bool, len(points))
	keep[0], keep[len(points)-1] = true, true

	// Iterative to avoid deep recursion on long shapes
	type span struct{ start, end int }
	stack := []span{{0, len(points) - 1}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		farthest, maxDistance := -1, tolerance
		for i := current.start + 1; i < current.end; i++ {
			if distance := perpendicularDistance(points[i], points[current.start], points[current.end]); distance > maxDistance {
				farthest, maxDistance = i, distance
			}
		}
		if farthest >= 0 {
			keep[farthest] = true
			stack = append(stack, span{current.start, farthest}, span{farthest, current.end})
		}
	}

	result := make([][2]float64, 0, len(points)/4)
	for i, point := range points {
		if keep[i] {
			result = append(result, point)
		}
	}
	return result
}

func perpendicularDistance(point, lineStart, lineEnd [2]float64) float64 {
	dx, dy := lineEnd[0]-lineStart[0], lineEnd[1]-lineStart[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(point[0]-lineStart[0], point[1]-lineStart[1])
	}
	return math.Abs(dy*point[0]-dx*point[1]+lineEnd[0]*lineStart[1]-lineEnd[1]*lineStart[0]) / math.Hypot(dx, dy)
}

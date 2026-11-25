// Package gtfs_realtime contains Go bindings for the GTFS Realtime protobuf spec.
//
// The bindings are generated from gtfs-realtime.proto and committed. To regenerate
// them after updating the proto, install protoc and run `go generate ./gtfs_realtime`.
package gtfs_realtime

//go:generate go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
//go:generate protoc --go_out=. --go_opt=paths=source_relative --go_opt=Mgtfs-realtime.proto=./gtfs_realtime gtfs-realtime.proto

//go:generate protoc --go_opt=Mgtfs-realtime.proto=./gtfs_realtime --go_opt=paths=source_relative --go_out=. -I=. gtfs-realtime.proto

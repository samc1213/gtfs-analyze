#!/bin/bash
#//go:generate -comand generate.sh
folder="$(dirname "$(readlink -f "$0")")"
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
protoc \
    --go_opt=Mgtfs-realtime.proto=./gtfs_realtime \
    --go_opt=paths=source_relative \
    --go_out="$folder" \
    -I="$folder" \
    "$folder/gtfs-realtime.proto"

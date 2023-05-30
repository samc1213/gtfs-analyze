#!/bin/bash
#//go:generate -comand generate.sh
folder="$(dirname "$(readlink -f "$0")")"
protoc \
    --go_opt=Mgtfs-realtime.proto=./gtfs_realtime \
    --go_opt=paths=source_relative \
    --go_out="$folder" \
    -I="$folder" \
    "$folder/gtfs-realtime.proto"

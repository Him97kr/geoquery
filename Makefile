# Makefile — GeoQuery dev commands

.PHONY: run build tidy clean

## tidy — download and tidy Go modules
tidy:
	go mod tidy

## run — start the dev server
run:
	go run ./server/main.go

## build — compile production binary
build:
	go build -o bin/geoquery ./server/main.go

## clean — remove compiled binary
clean:
	rm -rf bin/

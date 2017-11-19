#!/bin/make
BINARY=rss-influxdb

all: build install

build:
	go build -o ${BINARY} rss-influxdb.go

install:
	install -m644 rss-influxdb.service /etc/systemd/system/.
	install -m644 ${BINARY} /usr/local/bin/.

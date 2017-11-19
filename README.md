# rss-influxdb
Dump RSS Feed events to InfluxDB for visualization in Grafana

To build:

```
go build rss-influxdb.go
```

To run:

Simply install the systemd unit to /etc/systemd/system, and start the unit if using systemd.

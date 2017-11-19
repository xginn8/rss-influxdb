# rss-influxdb
Dump RSS Feed events to InfluxDB for visualization in Grafana

To install:
```
make install
```

To just build the binary:
```
make build
```

To run:
```
systemctl start rss-influxdb.service
```

Change the default RSS feeds to follow by editing the .service file. Any RSS or Atom feed should work.

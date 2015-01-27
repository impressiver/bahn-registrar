MQTT Plumber
============

MQTT Plumber watches, processes, and answers questions about messages passing through MQTT brokers,
prividing a simple REST API for querying time-series data.

It's the plumbing between broker, datastore, and webapps.


## Install
```
$ brew install influxdb
$ go get code.google.com/p/go.tools/cmd/vet
$ make
```
### Optional tools
```
$ go get github.com/golang/lint/golint
$ go get github.com/Dieterbe/influx-cli
```

### Run
```
$ make run
```


## Notes

### InfluxDB

Default config:
-  db: mqtt_plumber
-  login: plumber:plumber
```
$ open http://localhost:8083
```

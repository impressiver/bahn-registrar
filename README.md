MQTT Plumber
============

MQTT Plumber watches, processes, and answers questions about messages passing through MQTT brokers,
prividing a simple REST API for querying time-series data.

It's the plumbing between broker, datastore, and webapps.


## Install
```
$ brew install influxdb
$ go get github.com/influxdb/influxdb
# We need to use v0.8.8 as master is unusable right now
$ cd ${GOPATH}/src/github.com/influxdb/influxdb
$ git checkout v0.8.8
$ cd -
$ make
```
### Optional tools
```
$ go get code.google.com/p/go.tools/cmd/vet
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

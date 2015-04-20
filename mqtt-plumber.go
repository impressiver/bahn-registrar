package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/satori/go.uuid"
	"github.com/vharitonsky/iniflags"

	MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
	influx "github.com/influxdb/influxdb/client"
)

//
// Globals
// todo: (iw) encapsulate
//

// MQTT $SYS date form
const dateFormSys = "0000-00-00 00:00:00-0000"

// Matcher for the above date form
var reDate = regexp.MustCompile(`^[\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2}-[\d]{4}$`)

// Match numeric values (int, float, etc)
var reNumeric = regexp.MustCompile(`^[0-9\.]+$`)

// Match json
var reJSON = regexp.MustCompile(`^\{.*\}$`)

// InfluxDB client
var db *influx.Client

// MQTT client
var mqtt *MQTT.Client

// Incoming message channel
var msgs chan [2]string

// Colors
// ------
var ERR *color.Color
var WARN *color.Color
var OK *color.Color
var INFO *color.Color
var PROMPT *color.Color

func status(status string, c *color.Color, out string) {
	put := c.SprintFunc()
	fmt.Printf("[%s] %s", put(status), out)
}

// Opts
// ----

// --qos flag
var optClientID *string

// --publish flag
var optPublish *string

// --qos flag
var optQos *int

// --verbose flag
var optVerbose *bool

// Track successful topic subscriptions
var subscriptions []string

//
// Database
//

func persist(topic string, messageTopic string, payload []byte) {
	parser := Parse(topic)
	params := parser.Params(messageTopic)
	timestamp := time.Now()
	precision := "s"

	pts := make([]influx.Point, 2)

	// Convert json to string:obj map
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		status("DB", ERR, fmt.Sprintf("Error parsing payload, cannot persist", err))
		return
	}

	status("DB", INFO, fmt.Sprintf("Data: %v\n", data))
	status("DB", INFO, fmt.Sprintf("Parsed: %v\n", params))

	tags := map[string]string{
		"subscribed": topic,
		"published":  messageTopic,
		"params":     strings.Join(params, ", "),
		"client":     *optClientID,
	}

	for i, datum := range data {
		// Move any fields beginning w/ underscore to tags
		if strings.IndexRune(i, '_') == 0 {
			tags[strings.TrimLeft(i, "_")] = datum.(string)
			delete(data, i)
			continue
		}

		// Detect obvious timestamps in payload
		// todo: (iw) make this robuster
		switch i {
		case "timestamp": // expects RFC3339
			if t, err := time.Parse(time.RFC3339Nano, datum.(string)); err == nil {
				timestamp = t
				precision = "s"
				continue
			}
			if t, err := time.Parse(time.RFC3339, datum.(string)); err == nil {
				timestamp = t
				precision = "n"
				continue
			}
		case "tst": // OwnTracks
			stamp, err := strconv.ParseInt(datum.(string), 10, 64)
			if err != nil {
				continue
			}
			timestamp = time.Unix(stamp, 0)
			precision = "s"
		}
	}

	// todo: (iw) fire off a signal that can be handled by various data formatters
	// to insert additional series/points

	pts[0] = influx.Point{
		Name:      topic,
		Tags:      tags,
		Fields:    data,
		Timestamp: timestamp,
		Precision: precision,
	}

	pts[1] = influx.Point{
		Name:      messageTopic,
		Tags:      tags,
		Fields:    data,
		Timestamp: timestamp,
		Precision: precision,
	}

	bps := influx.BatchPoints{
		Points:          pts,
		Database:        "mqtt_plumber",
		RetentionPolicy: "default",
	}
	_, err := db.Write(bps)
	if err != nil {
		log.Fatal("Error writing points to db: ", err)
	}

	status("DB", OK, fmt.Sprintf("Persisted %v (tags %v) w/ ts %v\n", data, tags, timestamp))
}

//
// Subscriptions
//

// Subscribe to topic
func subscribe(handler func(mqtt *MQTT.Client, message MQTT.Message, topic string), qos byte, topics ...string) {
	for i := range topics {
		topic := topics[i]
		if len(topic) == 0 {
			if *optVerbose {
				status("SUB", WARN, fmt.Sprintf("Skipping empty topic at index %d\n", i))
			}
			continue
		}

		// Wrap the message handler so the subscribed topic (w/ wildcards) can be
		// used to parse incoming messages (which have fully resolved topic paths)
		curried := func(topic string) func(mqtt *MQTT.Client, message MQTT.Message) {
			return func(mqtt *MQTT.Client, message MQTT.Message) {
				handler(mqtt, message, topic)
			}
		}(topic)

		if *optVerbose {
			status("SUB", INFO, fmt.Sprintln("Subscribing to", topic))
		}

		if token := mqtt.Subscribe(topic, qos, curried); token.Wait() && token.Error() != nil {
			status("SUB", ERR, fmt.Sprintln("Failed to subscribe to", topic, token.Error()))
			return
		}
		status("OK", OK, fmt.Sprintln("Subscribed to", topic))
		subscriptions = append(subscriptions, topic)
	}
	fmt.Println("")
}

func unsubscribe(topics ...string) {
	for i := range topics {
		topic := topics[i]

		if *optVerbose {
			status("UNSUB", INFO, fmt.Sprintln("Unsubscribing from ", topic))
		}
		if token := mqtt.Unsubscribe(topic); token.Wait() && token.Error() != nil {
			status("UNSUB", ERR, fmt.Sprintln("Failed to unsubscribe from", topic, token.Error()))
		}
	}
}

//
// Message parsing
//

func parse(payload []byte) (string, []byte) {
	parsed := payload
	var matched string

	if reJSON.Match(payload) {
		matched = "json"
	} else if reNumeric.Match(payload) {
		matched = "numeric"
		// Let json unmarshaler figure out what type of numeric
	} else if reDate.Match(payload) {
		matched = "date"
		// Reformat $SYS dates as ISO-8601 (w/o nanos)
		t, _ := time.Parse(dateFormSys, string(payload[:]))
		parsed = []byte(t.Format(time.RFC3339))
	} else if _, err := time.Parse(time.RFC3339Nano, string(payload[:])); err == nil {
		matched = "date"
	} else if _, err := time.Parse(time.RFC3339, string(payload[:])); err == nil {
		matched = "date"
	} else {
		matched = "string"
		// By default, quote the value as a string
	}

	if *optVerbose {
		status("PARSE", INFO, fmt.Sprintf("parsed (%s) %s\n", matched, parsed))
	}

	return matched, parsed
}

func asJSON(payload []byte) []byte {
	// Unmarshal payload by parsing the string value, wrapping in json, and
	// converting to bytes, theb using the json unmarshaler to figure out what the
	// correct numeric value types should be
	matched, parsed := parse(payload)
	var jsonPayload []byte

	switch matched {
	case "json":
		jsonPayload = parsed
	case "numeric":
		jsonPayload = []byte(fmt.Sprintf("{\"value\": %s}", parsed))
	default:
		jsonPayload = []byte(fmt.Sprintf("{\"value\": \"%s\"}", parsed))
	}

	if *optVerbose {
		status("JSON", INFO, fmt.Sprintf("%s\n", jsonPayload))
	}

	return jsonPayload
}

//
// Message handlers
//

func onSysMessageReceived(mqtt *MQTT.Client, message MQTT.Message) {
	if message.Duplicate() {
		status("SUB", WARN, fmt.Sprintf("Received duplicate message on $SYS topic: %s\n", message.Topic()))
	} else {
		status("SUB", INFO, fmt.Sprintf("Received message on $SYS topic: %s\n", message.Topic()))
	}

	if *optVerbose {
		status("SUB", INFO, fmt.Sprintf("%s\n\n", message.Payload()))
	}

	// Save the processed message
	if !message.Duplicate() {
		persist("$SYS/#", message.Topic(), asJSON(message.Payload()))
	}
	msgs <- [2]string{message.Topic(), string(message.Payload())}
}

func onTopicMessageReceived(mqtt *MQTT.Client, message MQTT.Message, topic string) {
	if message.Duplicate() {
		status("SUB", WARN, fmt.Sprintf("Received duplicate message on watched topic: %s\n", message.Topic()))
	} else {
		status("SUB", INFO, fmt.Sprintf("Received message on watched topic: %s\n", message.Topic()))
	}

	if *optVerbose {
		status("SUB", INFO, fmt.Sprintf("%s\n\n", message.Payload()))
	}

	// Save the processed message
	if !message.Duplicate() {
		persist(topic, message.Topic(), asJSON(message.Payload()))
	}
	msgs <- [2]string{message.Topic(), string(message.Payload())}
}

func onAnyMessageReceived(mqtt *MQTT.Client, message MQTT.Message) {
	if message.Duplicate() {
		status("SUB", WARN, fmt.Sprintf("Received duplicate message on unwatched topic: %s\n", message.Topic()))
	} else {
		status("SUB", INFO, fmt.Sprintf("Received message on unwatched topic: %s\n", message.Topic()))
	}

	if *optVerbose {
		status("SUB", INFO, fmt.Sprintf("%s\n\n", message.Payload()))
	}

	// Don't persist random messages
	msgs <- [2]string{message.Topic(), string(message.Payload())}
}

func onStdinReceived(in string) {
	// Empty input
	if len(in) == 0 {
		return
	}

	// Split input on first space: {the/pub/topic} {message payload with spaces}
	var pubTopic, message = func(str string) (string, string) {
		parts := strings.SplitN(str, " ", 2)
		if len(parts) < 2 {
			parts = []string{*optPublish, parts[0]}

		}
		return strings.Replace(parts[0], "{client}", *optClientID, -1), parts[1]
	}(strings.TrimSpace(in))

	// Don't publish empty messages
	if len(message) == 0 {
		return
	}

	// Publish to MQTT
	if *optVerbose {
		status("PUB", INFO, fmt.Sprintf("Publishing to %s: %s\n", pubTopic, message))
	}
	if token := mqtt.Publish(pubTopic, byte(*optQos), false, message); token.Wait() && token.Error() != nil {
		status("PUB", ERR, fmt.Sprintln("Failed to publish message", pubTopic, token.Error()))
	}
	status("PUB", OK, fmt.Sprintln("Message published to", pubTopic))
}

//
// Main
//

func main() {
	rand.Seed(time.Now().Unix())
	cid := uuid.NewV1()

	msgs = make(chan [2]string)
	in := make(chan string)

	// Config
	dbURI := flag.String("db", "http://localhost:8086", "The InfluxDB server uri")
	brokerURI := flag.String("broker", "tcp://mashtun:1883", "The MQTT server uri")
	clientID := flag.String("client-id", fmt.Sprintf("plumber-%s", cid), "The MQTT client id")
	watch := flag.String("watch", "broadcast/#", "A comma-separated list of topics")
	sys := flag.Bool("sys", false, "Persist $SYS status messages")
	publish := flag.String("publish", "broadcast/client/{client}", "Default publish topic")
	prefix := flag.String("prefix", "", "Base topic hierarchy (namespace) prepended to subscriptions")
	qos := flag.Int("qos", 0, "QoS level for subscriptions")
	clean := flag.Bool("clean", true, "Start with a clean session")
	store := flag.String("store", "", "Path to file store dir (default is in-memory)")
	verbose := flag.Bool("verbose", false, "Increased logging")

	// Support config.ini file (--config=/path/to/config.ini)
	iniflags.Parse()

	// Set global flags
	optClientID = clientID
	optPublish = publish
	optQos = qos
	optVerbose = verbose

	ERR = color.New(color.FgRed)
	WARN = color.New(color.FgYellow)
	OK = color.New(color.FgGreen)
	INFO = color.New(color.FgMagenta)
	PROMPT = color.New(color.FgCyan)

	received := 0
	sent := 0

	// Init InfluxDB client
	dbURL, err := url.Parse(*dbURI)
	if err != nil {
		log.Fatal("Error parsing db url", err)
	}
	con, err := influx.NewClient(influx.Config{
		URL:      *dbURL,
		Username: "plumber",
		Password: "plumber",
	})
	if err != nil {
		log.Fatal("Error connecting to InfluxDB", err)
	}

	dur, ver, err := con.Ping()
	if err != nil {
		log.Fatal("Error pinging InfluxDB: ", err)
	}
	status("OK", OK, fmt.Sprintf("Connected to db in %v (InfluxDB v%s)\n", dur, ver))

	db = con

	// Init MQTT options
	opts := MQTT.NewClientOptions()
	opts.AddBroker(*brokerURI)
	opts.SetClientID(*clientID)
	opts.SetCleanSession(*clean)

	if len(*store) > 0 {
		opts.SetStore(MQTT.NewFileStore(*store))
	}

	if *verbose {
		opts.SetDefaultPublishHandler(onAnyMessageReceived)
	}

	// Create client and connect
	mqtt = MQTT.NewClient(opts)
	if token := mqtt.Connect(); token.Wait() && token.Error() != nil {
		log.Panic("Error connecting to MQTT broker", token.Error())
	}

	// Successfully connected
	status("OK", OK, fmt.Sprintf("Connected to broker as %s\n\n", *clientID))

	// Subscribe to $SYS topic
	if *sys {
		status("SUB", INFO, "Subscribing to $SYS")
		mqtt.Subscribe("$SYS/#", byte(*qos), onSysMessageReceived)
	}

	// Split comma-separated watch list into array of topics
	topiclist := strings.Split(*watch, ",")
	var topics []string
	for i := range topiclist {
		topic := strings.TrimSpace(topiclist[i])
		if len(topic) == 0 {
			status("ERR", ERR, fmt.Sprintf("Skipping empty topic (item %d in '%s')\n", i+1, *watch))
			continue
		}

		// Add prefix, if configured
		if len(*prefix) > 0 {
			topic = strings.Join([]string{*prefix, topic}, "/")
		}

		topics = append(topics, topic)
	}

	// Create subscriptions
	if len(topics) > 0 {
		subscribe(onTopicMessageReceived, byte(*qos), topics[:]...)
	}

	// Watch stdin and publish input to MQTT
	go func(ch chan<- string) {
		reader := bufio.NewReader(os.Stdin)
		for {
			s, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println(err)
				close(ch)
				return
			}
			ch <- s
		}

		close(ch)
	}(in)

	prompt := true

stdinloop:
	for {
		select {
		case _, ok := <-msgs:
			prompt = true
			if !ok {
				status("ERR", ERR, "msgs channel not ok")
				break stdinloop
			}
			// fmt.Printf("Received on %s: %s\n", incoming[0], incoming[1])
			received++
		case stdin, ok := <-in:
			prompt = true
			if !ok {
				status("ERR", ERR, "stdin channel not ok")
				break stdinloop
			}
			onStdinReceived(stdin)
			sent++
		case <-time.After(1 * time.Second):
			// fmt.Printf("\x0c")
			if prompt {
				prompt = false
				PROMPT.Printf("\n(%d:%d) [topic] msg > ", received, sent)
			}
		}
	}
}

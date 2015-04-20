package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
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
const dateFormat = "0000-00-00 00:00:00-0000"

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

	// Convert json to string:obj map
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		panic(err)
	}

	// todo: (iw) parse topic wildcards into additional key/values
	data["topic"] = messageTopic
	data["params"] = strings.Join(parser.Params(messageTopic), ", ")

	// Split map into key/value arrays
	var keys []string
	var values []interface{}
	for key, value := range data {
		keys = append(keys, key)
		values = append(values, value)
	}

	// Create series for the topic, with keys for columns and values for points
	series := &influx.Series{
		Name:    topic,
		Columns: keys,
		Points: [][]interface{}{
			values,
		},
	}

	// Write to db or die trying
	if err := db.WriteSeries([]*influx.Series{series}); err != nil {
		panic(err)
	}

	status("DB", OK, fmt.Sprintf("Persisted to series %s (params %s)\n", series.Name, data["params"]))
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
				WARN.Printf("Skipping empty topic at index %d\n", i)
			}
			continue
		}

		curried := func(topic string) func(mqtt *MQTT.Client, message MQTT.Message) {
			return func(mqtt *MQTT.Client, message MQTT.Message) {
				handler(mqtt, message, topic)
			}
		}(topic)

		if *optVerbose {
			INFO.Println("Subscribing to " + topic)
		}

		if token := mqtt.Subscribe(topic, qos, curried); token.Wait() && token.Error() != nil {
			ERR.Println("Failed to subscribe to", topic, token.Error())
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
			INFO.Println("Unsubscribing from " + topic)
		}
		if token := mqtt.Unsubscribe(topic); token.Wait() && token.Error() != nil {
			ERR.Println("Failed to unsubscribe from", topic, token.Error())
		}
	}
}

//
// Message parsing
//

func parse(payload []byte) []byte {
	// Unmarshal payload by parsing the string value, wrapping in json, and
	// converting to bytes, theb using the json unmarshaler to figure out what the
	// correct numeric value types should be

	var jsonPayload []byte
	var matched string
	if reJSON.Match(payload) {
		matched = "json"
		jsonPayload = payload
	} else if reNumeric.Match(payload) {
		matched = "numeric"
		// Let json unmarshaler figure out what type of numeric
		jsonPayload = []byte(fmt.Sprintf("{\"value\": %s}", payload))
	} else if reDate.Match(payload) {
		matched = "date"
		// Reformat dates as ISO-8601 (w/o nanos)
		t, _ := time.Parse(dateFormat, string(payload[:]))
		jsonPayload = []byte(fmt.Sprintf("{\"value\": \"%s\"}", t.Format(time.RFC3339)))
	} else {
		matched = "string"
		// Quote the value as a string
		jsonPayload = []byte(fmt.Sprintf("{\"value\": \"%s\"}", payload))
	}

	if *optVerbose {
		INFO.Printf("parsed (%s) %s\n", matched, jsonPayload)
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
		INFO.Printf("%s\n\n", message.Payload())
	}

	// Save the processed message
	if !message.Duplicate() {
		persist("$SYS/#", message.Topic(), parse(message.Payload()))
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
		INFO.Printf("%s\n\n", message.Payload())
	}

	// Save the processed message
	if !message.Duplicate() {
		persist(topic, message.Topic(), parse(message.Payload()))
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
		INFO.Printf("%s\n\n", message.Payload())
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
	broker := flag.String("broker", "tcp://mashtun:1883", "The MQTT server uri")
	clientID := flag.String("client-id", fmt.Sprintf("plumber-%s", cid), "The MQTT client id")
	watch := flag.String("watch", "broadcast/#", "A comma-separated list of topics")
	sys := flag.Bool("sys", false, "Persist $SYS status messages")
	publish := flag.String("publish", "broadcast/client/{client}", "Default publish topic")
	prefix := flag.String("prefix", "", "Base topic hierarchy (namespace) prepended to subscriptions")
	qos := flag.Int("qos", 0, "QoS level for subscriptions")
	clean := flag.Bool("clean", true, "Start with a clean session")
	store := flag.String("store", "", "Path to file store dir (default is in-memory)")
	verbose := flag.Bool("verbose", false, "Increased logging")

	iniflags.Parse() // Support for config.ini file (--config)

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
	c, err := influx.NewClient(&influx.ClientConfig{
		Username: "plumber",
		Password: "plumber",
		Database: "mqtt_plumber",
	})
	if err != nil {
		panic(err)
	}

	db = c

	// Init MQTT options
	opts := MQTT.NewClientOptions()
	opts.AddBroker(*broker)
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
		panic(token.Error())
	}

	// Successfully connected
	status("OK", OK, fmt.Sprintf("Connected to %s as %s\n\n", *broker, *clientID))

	// Subscribe to $SYS topic
	if *sys {
		INFO.Println("Subscribing to $SYS")
		mqtt.Subscribe("$SYS/#", byte(*qos), onSysMessageReceived)
	}

	// Split comma-separated watch list into array of topics
	topiclist := strings.Split(*watch, ",")
	var topics []string
	for i := range topiclist {
		topic := strings.TrimSpace(topiclist[i])
		if len(topic) == 0 {
			status("ERR", ERR, fmt.Sprintf("Skipping empty watch topic at index %d\n", i))
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

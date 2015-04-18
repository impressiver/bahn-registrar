package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

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

// MQTT client
var mqtt *MQTT.Client

// Incoming message channel
var choke chan [2]string

// --verbose flag
var optVerbose *bool

//
// Database
//

func persist(topic string, payload []byte) {
	// Convert json to string:obj map
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		panic(err)
	}

	// todo: (iw) parse topic wildcards into additional key/values
	data["topic"] = topic

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

	// Create db connection
	c, err := influx.NewClient(&influx.ClientConfig{
		Username: "plumber",
		Password: "plumber",
		Database: "mqtt_plumber",
	})
	if err != nil {
		panic(err)
	}

	// Write to db or die trying
	if err := c.WriteSeries([]*influx.Series{series}); err != nil {
		panic(err)
	}

	if *optVerbose {
		fmt.Printf("Persisted: %s", series)
	}
}

//
// Subscriptions
//

// Subscribe to topic
func subscribe(qos byte, topics ...string) {
	for i := range topics {
		topic := topics[i]
		if len(topic) == 0 {
			fmt.Printf("Skipping empty topic at index %d\n", i)
			continue
		}

		fmt.Println("Subscribing to " + topic)
		if token := mqtt.Subscribe(topic, qos, onTopicMessageReceived); token.Wait() && token.Error != nil {
			fmt.Println(token.Error())
		}
	}
}

func unsubscribe(topics ...string) {
	for i := range topics {
		topic := topics[i]
		fmt.Println("Unsubscribing from " + topic)
		if token := mqtt.Unsubscribe(topic); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
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
		fmt.Printf("(%s) %s\n", matched, jsonPayload)
	}

	return jsonPayload
}

//
// Message handlers
//

func onSysMessageReceived(mqtt *MQTT.Client, message MQTT.Message) {
	fmt.Printf("Received message on $SYS topic: %s\n", message.Topic())
	fmt.Printf("%s\n", message.Payload())

	// Save the processed message
	persist(message.Topic(), parse(message.Payload()))
	choke <- [2]string{message.Topic(), string(message.Payload())}
}

func onTopicMessageReceived(mqtt *MQTT.Client, message MQTT.Message) {
	fmt.Printf("Received message on watched topic: %s\n", message.Topic())
	fmt.Printf("%s\n", message.Payload())

	// Save the processed message
	persist(message.Topic(), parse(message.Payload()))
	choke <- [2]string{message.Topic(), string(message.Payload())}
}

func onAnyMessageReceived(mqtt *MQTT.Client, message MQTT.Message) {
	fmt.Printf("Received message on unwatched topic: %s\n", message.Topic())
	fmt.Printf("%s\n", parse(message.Payload()))
	choke <- [2]string{message.Topic(), string(message.Payload())}
}

//
// Main
//

func main() {
	stdin := bufio.NewReader(os.Stdin)
	rand.Seed(time.Now().Unix())

	choke = make(chan [2]string)

	// Config
	broker := flag.String("broker", "tcp://mashtun:1883", "The MQTT server uri")
	client := flag.String("client", "plumber-"+strconv.Itoa(rand.Intn(1000)), "The client id")
	watch := flag.String("watch", "broadcast/#", "A comma-separated list of topics")
	sys := flag.Bool("sys", false, "Persist $SYS status messages")
	publish := flag.String("publish", "broadcast/client/{client}", "Default publish topic")
	prefix := flag.String("prefix", "", "Base topic hierarchy (namespace) prepended to subscriptions")
	qos := flag.Int("qos", 0, "QoS level for subscriptions")
	clean := flag.Bool("clean", true, "Start with a clean session")
	store := flag.String("store", "", "Path to file store dir (default is in-memory)")
	verbose := flag.Bool("verbose", false, "Increased logging")
	flag.Parse()

	// Set global flags
	optVerbose = verbose

	// Default publish topic (for sending stdin to MQTT)
	defaultPubTopic := strings.Replace(*publish, "{client}", *client, -1)

	received := 0

	// Init MQTT options
	opts := MQTT.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID(*client)
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
	fmt.Printf("Connected to %s as %s\n\n", *broker, *client)

	// Subscribe to $SYS topic
	if *sys {
		fmt.Println("Subscribing to $SYS")
		mqtt.Subscribe("$SYS/#", byte(*qos), onSysMessageReceived)
	}

	// Split comma-separated watch list into array of topics
	topiclist := strings.Split(*watch, ",")
	var topics []string
	for i := range topiclist {
		topic := strings.TrimSpace(topiclist[i])
		if len(topic) == 0 {
			fmt.Printf("Skipping empty watch topic at index %d\n", i)
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
		subscribe(byte(*qos), topics[:]...)
	}

	// Just for giggles
	incoming := <-choke
	fmt.Printf("RECEIVED TOPIC: %s MESSAGE: %s\n", incoming[0], incoming[1])
	received++

	// Watch stdin and publish input to MQTT
	for {
		fmt.Print("\n> ")

		// Read a line of input
		in, err := stdin.ReadString('\n')

		// Borked
		if err == io.EOF {
			os.Exit(0)
		}

		// Split input on first space: {the/pub/topic} {message payload with spaces}
		var pubTopic, message = func(parts []string) (string, string) {
			if len(parts) == 2 {
				return strings.Replace(parts[0], "{client}", *client, -1), parts[1]
			}
			return defaultPubTopic, parts[0]
		}(strings.SplitN(strings.TrimSpace(in), " ", 2))

		// Don't publish empty messages
		if len(message) == 0 {
			continue
		}

		// Publish to MQTT
		fmt.Printf("Publishing to %s: %s\n", pubTopic, message)
		if token := mqtt.Publish(pubTopic, byte(*qos), false, message); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
		}
	}
}

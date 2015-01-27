package main

import (
  "bufio"
  "flag"
  "fmt"
  "regexp"
  "io"
  "math/rand"
  "os"
  "strconv"
  "strings"
  "encoding/json"
  "time"
  influx "github.com/influxdb/influxdb/client"
  MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
)

// MQTT $SYS date form
const date_form = "0000-00-00 00:00:00-0000"

// Matcher for the above date form
var re_date     = regexp.MustCompile(`^[\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2}-[\d]{4}$`)

// Match numeric values (int, float, etc)
var re_numeric  = regexp.MustCompile(`^[0-9\.]+$`)


//
// Database
//

func persist(topic string, p []byte) {
  // Convert json to string:obj map
  var data map[string]interface{}
  if err := json.Unmarshal(p, &data); err != nil {
    panic(err)
  }

  // Split map into key/value arrays
  var keys []string
  var values []interface{}
  for key, value := range data {
    keys = append(keys, key)
    values = append(values, value)
  }

  // Create series for the topic, with keys for columns and values for points
  series := &influx.Series {
    Name:    topic,
    Columns: keys,
    Points:  [][]interface{}{
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
}


//
// Message handlers
//

func onSysMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("Received message on $SYS topic: %s\n", message.Topic())
  fmt.Printf("Message: %s ", message.Payload())

  // Unmarshal payload by parsing the string value, wrapping in json, and
  // converting to bytes, theb using the json unmarshaler to figure out what the
  // correct numeric value types should be
  
  var json_payload []byte
  if re_numeric.Match(message.Payload()) {
    fmt.Println("(numeric)")
    // Let json unmarshaler figure out what type of numeric
    json_payload = []byte(fmt.Sprintf("{\"value\": %s}", message.Payload()))
  } else if(re_date.Match(message.Payload())) {
    fmt.Println("(date)")
    // Reformat dates as ISO-8601 (w/o nanos)
    t, _ := time.Parse(date_form, string(message.Payload()[:]))
    json_payload = []byte(fmt.Sprintf("{\"value\": \"%s\"}", t.Format(time.RFC3339)))
  } else {
    fmt.Println("(string)")
    // Quote the value as a string
    json_payload = []byte(fmt.Sprintf("{\"value\": \"%s\"}", message.Payload()))
  }

  // Save the processed message
  persist(message.Topic(), json_payload)
}

func onAnyMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("Received message on some random topic: %s\n", message.Topic())
  fmt.Printf("Message: %s\n", message.Payload())
}

func onTopicMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("OMG!! Received message on MY topic: %s\n", message.Topic())
  fmt.Printf("Message: %s\n", message.Payload())

  // Save the processed message
  persist(message.Topic(), message.Payload())
}


//
// Main
//

func main() {
  stdin := bufio.NewReader(os.Stdin)
  rand.Seed(time.Now().Unix())

  // Config
  broker     := flag.String("broker", "tcp://mashtun:1883", "The MQTT server uri")
  user       := flag.String("user", "registrar"+strconv.Itoa(rand.Intn(1000)), "The client id")
  topic      := flag.String("topic", "owntracks/#", "The topic of interest")
  sys        := flag.Bool("sys", true, "Collect $SYS messages")
  qos        := flag.Int("qos", 0, "The QoS level to use for published messages")
  flag.Parse()

  // Init MQTT
  opts   := MQTT.NewClientOptions().AddBroker(*broker).SetClientId(*user).SetDefaultPublishHandler(onAnyMessageReceived).SetCleanSession(true)
  mqtt   := MQTT.NewClient(opts)
  _, err := mqtt.Start()

  // Boo
  if err != nil {
    panic(err)
  } else {
    fmt.Printf("Connected as %s to %s\n", *user, *broker)
  }

  // Subscribe to $SYS topic
  if (*sys) {
    fmt.Println("Subscribing to $SYS")
    sys_filter, _ := MQTT.NewTopicFilter("$SYS/#", 1)
    mqtt.StartSubscription(onSysMessageReceived, sys_filter)
  }

  // Subscribe to configured topic
  fmt.Println("Subscribing to " + *topic)
  topic_filter, _ := MQTT.NewTopicFilter(*topic, 1)
  mqtt.StartSubscription(onTopicMessageReceived, topic_filter)

  // Publish topic (from stdin)
  pub_topic := strings.Join([]string{"/registrar/", *user}, "")

  // Loop, waiting for stdin
  for {
    // Read a line of input
    message, err := stdin.ReadString('\n')

    // Borked
    if err == io.EOF {
      os.Exit(0)
    }

    // Publish input
    r := mqtt.Publish(MQTT.QoS(*qos), pub_topic, []byte(strings.TrimSpace(message)))
    <-r
  }
}

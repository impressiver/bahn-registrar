package main

import (
  "bufio"
  "flag"
  "fmt"
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

func onSysMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("Received message on $SYS topic: %s\n", message.Topic())
  fmt.Printf("Message: %s\n", message.Payload())

  // Create a new series w/ data point for the message
  series := &influx.Series {
    Name:    message.Topic(),
    Columns: []string{"value"},
    Points:  [][]interface{}{
      {fmt.Sprintf("%s", message.Payload())},
      },
  }

  // Connect to DB
  c, err := influx.NewClient(&influx.ClientConfig{
    Username: "bahn",
    Password: "b8hn",
    Database: "bahn_registrar",
  })

  // Failed to init DB
  if err != nil {
    panic(err)
  }

  // Write to db or die trying
  if err := c.WriteSeries([]*influx.Series{series}); err != nil {
    panic(err)
  }
}

func onAnyMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("Received message on some random topic: %s\n", message.Topic())
  fmt.Printf("Message: %s\n", message.Payload())
}

func onTopicMessageReceived(mqtt *MQTT.MqttClient, message MQTT.Message) {
  fmt.Printf("OMG!! Received message on MY topic: %s\n", message.Topic())
  fmt.Printf("Message: %s\n", message.Payload())

  // Convert JSON into string:obj map
  var data map[string]interface{}
  if err := json.Unmarshal(message.Payload(), &data); err != nil {
    panic(err)
  }

  var keys []string
  var values []interface{}
  for key, value := range data {
    keys = append(keys, key)
    values = append(values, value)
  }

  // Create a new series w/ data point for the message
  series := &influx.Series {
    Name:    message.Topic(),
    Columns: keys,
    Points:  [][]interface{}{
      values,
    },
  }

  // Connect to DB
  c, err := influx.NewClient(&influx.ClientConfig{
    Username: "bahn",
    Password: "b8hn",
    Database: "bahn_registrar",
  })

  // Failed to init DB
  if err != nil {
    panic(err)
  }

  // Write to db or die trying
  if err := c.WriteSeries([]*influx.Series{series}); err != nil {
    panic(err)
  }

}

func main() {
  stdin := bufio.NewReader(os.Stdin)
  rand.Seed(time.Now().Unix())

  // Config
  broker     := flag.String("broker", "tcp://mashtun:1883", "The MQTT server uri")
  user       := flag.String("user", "registrar"+strconv.Itoa(rand.Intn(1000)), "The client id")
  topic      := flag.String("topic", "owntracks/#", "The topic of interest")
  sys        := flag.Bool("sys", true, "Collect $SYS messages")
  qos        := flag.Int("qos", 0, "The QoS level to use when sending")

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

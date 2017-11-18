package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/influxdata/influxdb/client/v2"
	"html"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type arrayFlags []string

type Feed struct {
	Type string
	Atom AtomFeed
	RSS  RSSFeed
}

type RSSFeed struct {
	XMLName xml.Name    `xml:"rss"`
	Version string      `xml:"version,attr"`
	Channel *RSSChannel `xml:"channel"`
}

type AtomFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Version string       `xml:"version,attr"`
	Author  []AtomAuthor `xml:"author"`
	Title   string       `xml:"title"`
	Updated string       `xml:"updated"`
	Event   []AtomEvent  `xml:"entry"`
}

type AtomEvent struct {
	Title   string    `xml:"title"`
	Content string    `xml:"content"`
	Url     string    `xml:"link"`
	Updated eventTime `xml:"updated"`
	ID      string    `xml:"id"`
}

type AtomAuthor struct {
	Name string `xml:"name"`
}

type RSSChannel struct {
	XMLName       xml.Name   `xml:"channel"`
	Title         string     `xml:"title"`
	Description   string     `xml:"description"`
	Link          string     `xml:"link"`
	Language      string     `xml:"language"`
	PubDate       string     `xml:"pubDate"`
	LastBuildDate string     `xml:"lastBuildDate"`
	Event         []RSSEvent `xml:"item"`
}

type eventTime struct {
	time.Time
}

type RSSEvent struct {
	XMLName xml.Name  `xml:"item"`
	Title   string    `xml:"title"`
	Text    string    `xml:"description"`
	Link    string    `xml:"link"`
	Guid    string    `xml:"guid"`
	PubDate eventTime `xml:"pubDate"`
	Updated string    `xml:"updated"`
}

type Influx struct {
	Database string
	Host     string
	Port     int
	Username string
	Password string
}

type InfluxEvent struct {
	Title       string
	Text        string
	Url         string
	Measurement string
	ID          string
	Time        eventTime
}

func getXML(url string) (string, error) {
	resp, err := http.Get(url)
	check(err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error("Status error: %v", resp.StatusCode)
		return "", fmt.Errorf("Status error: %v", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	check(err)

	return string(data), nil
}

func check(e error) {
	if e != nil {
		log.Error(e)
		return
	}
}

func (e *eventTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	timeFmts := map[string]bool{"Mon, _2 Jan 2006 15:04:05 MST": true, time.RFC3339: true}
	var v string
	for k, _ := range timeFmts {
		d.DecodeElement(&v, &start)
		parse, err := time.Parse(k, v)
		if err != nil {
			continue
		}
		*e = eventTime{parse}
		return nil
	}
	return errors.New("Couldn't match timestamp to known format")
}

func getFeed(url string) (feed Feed) {
	log.Info("getting data from feed")
	data, err := getXML(url)
	atom := AtomFeed{}
	rss := RSSFeed{}
	feedType := "Atom"
	err = xml.Unmarshal([]byte(data), &atom)
	if err != nil {
		feedType = "RSS"
		err = xml.Unmarshal([]byte(data), &rss)
		if err != nil {
			panic(err)
		}
	}
	feed = Feed{
		Type: feedType,
		Atom: atom,
		RSS:  rss,
	}
	var length int
	if feedType == "RSS" {
		length = len(rss.Channel.Event)
	}
	if feedType == "Atom" {
		length = len(atom.Event)
	}
	log.Info(fmt.Sprintf("%d items returned from %s (%s)", length, feedType, url))
	return feed
}

// queryDB convenience function to query the database
func queryDB(clnt client.Client, cmd, db string) (res []client.Result, err error) {
	q := client.Query{
		Command:  cmd,
		Database: db,
	}
	if response, err := clnt.Query(q); err == nil {
		if response.Error() != nil {
			return res, response.Error()
		}
		res = response.Results
	} else {
		return res, err
	}
	return res, nil
}

func connectToInflux(influx Influx) (c client.Client, err error) {
	// Create a new HTTPClient
	c, err = client.NewHTTPClient(client.HTTPConfig{
		Addr:     "http://" + influx.Host + ":" + fmt.Sprintf("%d", influx.Port),
		Username: influx.Username,
		Password: influx.Password,
	})
	if err != nil {
		return nil, err
	}
	_, err = queryDB(c, fmt.Sprintf("CREATE DATABASE %s", influx.Database), influx.Database)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func writeEventToInflux(
	c client.Client,
	influx Influx,
	event InfluxEvent) (res bool, err error) {
	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  influx.Database,
		Precision: "s",
	})
	check(err)
	// Create a point and add to batch
	tags := map[string]string{
		"text":  event.Text,
		"title": event.Title,
		"url":   event.Url,
		"id":    event.ID,
	}
	fields := map[string]interface{}{
		"timestamp": event.Time.Unix(),
	}
	pt, err := client.NewPoint(event.Measurement, tags, fields, event.Time.Time)
	check(err)
	bp.AddPoint(pt)
	// Write the batch
	err = c.Write(bp)
	check(err)
	return true, nil
}

func (i *arrayFlags) String() string {
	return "rss feeds to process"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	// accept: host, port, db, rss feed(s?), loop time
	// many rss feeds?
	// what about atom?
	var feeds arrayFlags

	host := flag.String("host", "localhost", "InfluxDB hostname")
	username := flag.String("username", "", "InfluxDB username")
	password := flag.String("password", "", "InfluxDB password")
	port := flag.Int("port", 8086, "InfluxDB port")
	database := flag.String("database", "rss", "InfluxDB hostname")
	sleepTime := flag.Int("time", 60000, "milliseconds to wait in loop")
	flag.Var(&feeds, "feed", "Feeds to process (atom or rss), can pass multiple.")
	flag.Parse()
	influx := Influx{
		Host:     *host,
		Database: *database,
		Port:     *port,
		Username: *username,
		Password: *password,
	}
	log.Info("connecting to InfluxDB, creating database ", *database)
	c, err := connectToInflux(influx)
	if err != nil {
		panic("could not connect to InfluxDB")
	}
	log.Info("connected to InfluxDB successfully, starting up the collector")

	for {
		for _, rss := range feeds {
			// in main loop
			// get all the RSS events you can
			feed := getFeed(rss)
			// for each event, check if the hash is already in influxdb
			if feed.Type == "RSS" {
				for _, event := range feed.RSS.Channel.Event {
					influxEvent := InfluxEvent{
						Title:       event.Title,
						Text:        event.Text,
						Url:         event.Guid,
						Measurement: rss,
						Time:        event.PubDate,
					}
					res, err := writeEventToInflux(c, influx, influxEvent)
					check(err)
					if res != true {
						log.Error("failed to write")
					}
				}
			}
			if feed.Type == "Atom" {
				for _, event := range feed.Atom.Event {
					influxEvent := InfluxEvent{
						Title:       html.EscapeString(event.Title),
						Text:        strings.Replace(html.EscapeString(event.Content), "\n", "<br>", -1),
						ID:          html.EscapeString(event.ID),
						Measurement: rss,
						Time:        event.Updated,
					}
					res, err := writeEventToInflux(c, influx, influxEvent)
					check(err)
					if res != true {
						log.Error("failed to write")
					}
				}
			}
		}
		// loop as a daemon
		time.Sleep(time.Duration(*sleepTime) * time.Millisecond)
	}
}

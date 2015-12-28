package main

import "os"
import "net"
import "time"
import "log"
import "sync"
import "nowprovision/webhookproxy"
import "database/sql"
import "github.com/lib/pq"
import "encoding/json"

type Database struct {
	conn            *sql.DB
	connString      string
	state           map[string]*webhookproxy.Config
	mutex           sync.Mutex
	changeHandler   func(*webhookproxy.Config, *webhookproxy.Config)
	deleteHandler   func(*webhookproxy.Config)
	additionHandler func(*webhookproxy.Config)
}

type DBWebHook struct {
	Name             string
	Subdomain        string
	Secret           string
	Autoreply        bool
	FilteringEnabled bool
	Filters          []DBFilter
}

type DBFilter struct {
	Id          string
	Description string
	Type        string
	IP          string
}

func NewDatabase(connString string) *Database {

	db, err := sql.Open("postgres", connString)

	if err != nil {
		log.Fatalf("Unable to connect to db %s", err)
	}

	emptyState := make(map[string]*webhookproxy.Config)

	return &Database{db, connString, emptyState, sync.Mutex{}, nil, nil, nil}
}

func (db *Database) Load() {

	rowIterator, err := db.conn.Query("SELECT id, blob FROM webhooks")

	if err != nil {
		log.Fatalf("Unable to query webhooks table. %s", err)
	}

	for rowIterator.Next() {
		webhook := ProcessRow(rowIterator)
		db.mutex.Lock()
		db.state[webhook.Id] = webhook
		db.mutex.Unlock()
	}
}

func (db *Database) ForAll(fn func(*webhookproxy.Config)) {

	db.mutex.Lock()
	for _, config := range db.state {
		fn(config)
	}
	db.mutex.Unlock()

}

func (db *Database) StartUpdateDeleteListeners() {

	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Fatalf("Listener error. %s", err)
		}
	}

	updateListener := pq.NewListener(db.connString,
		time.Second*10,
		time.Second*10,
		reportProblem)
	updateListener.Listen("updated")

	go ProcessUpdates(db, updateListener)

	additionListener := pq.NewListener(db.connString,
		time.Second*10,
		time.Second*10,
		reportProblem)
	additionListener.Listen("added")

	go ProcessAdditions(db, additionListener)

	removeListener := pq.NewListener(db.connString,
		time.Second*10,
		time.Second*10,
		reportProblem)
	removeListener.Listen("removed")

	go ProcessRemovals(db, removeListener)
}

func (db *Database) OnChange(fn func(*webhookproxy.Config, *webhookproxy.Config)) {
	db.changeHandler = fn
}
func (db *Database) OnDelete(fn func(*webhookproxy.Config)) {
	db.deleteHandler = fn
}
func (db *Database) OnAddition(fn func(*webhookproxy.Config)) {
	db.additionHandler = fn
}

func ProcessUpdates(db *Database, listener *pq.Listener) {
	for {
		message := <-listener.Notify
		id := message.Extra
		log.Printf("Updating webhook %s", id)
		stmt, err := db.conn.Prepare("SELECT id, blob FROM webhooks WHERE id = $1")
		if err != nil {
			log.Fatal(err)
		}
		rowIterator, err := stmt.Query(id)
		if err != nil {
			log.Fatal(err)
		}
		for rowIterator.Next() {
			oldConfig := db.state[id]
			newConfig := ProcessRow(rowIterator)
			if db.changeHandler != nil {
				db.changeHandler(oldConfig, newConfig)
			}
			db.mutex.Lock()
			db.state[id] = newConfig
			db.mutex.Unlock()
		}
		log.Printf("Updated webhook %s", id)
	}
}

func ProcessAdditions(db *Database, listener *pq.Listener) {
	for {
		message := <-listener.Notify
		id := message.Extra
		log.Printf("Adding webhook %s", id)
		stmt, err := db.conn.Prepare("SELECT id, blob FROM webhooks WHERE id = $1")
		if err != nil {
			log.Fatal(err)
		}
		rowIterator, err := stmt.Query(id)
		if err != nil {
			log.Fatal(err)
		}
		for rowIterator.Next() {
			newConfig := ProcessRow(rowIterator)
			if db.additionHandler != nil {
				db.additionHandler(newConfig)
			}
			db.mutex.Lock()
			db.state[id] = newConfig
			db.mutex.Unlock()
		}
		log.Printf("Added webhook %s", id)
	}
}

func ProcessRemovals(db *Database, listener *pq.Listener) {
	for {
		message := <-listener.Notify
		id := message.Extra
		log.Printf("Removing webhook %s", id)
		db.mutex.Lock()
		oldConfig := db.state[id]
		if oldConfig != nil {
			delete(db.state, id)
		}
		db.mutex.Unlock()
		if oldConfig != nil && db.deleteHandler != nil {
			db.deleteHandler(oldConfig)
		}
		log.Printf("Removed webhook %s", id)
	}
}

func ProcessRow(rowIterator *sql.Rows) *webhookproxy.Config {

	webhook := webhookproxy.Config{}

	var rawBytes []byte
	var id string

	err := rowIterator.Scan(&id, &rawBytes)
	if err != nil {
		log.Fatal(err)
	}

	var dbWebHook DBWebHook
	json.Unmarshal(rawBytes, &dbWebHook)

	webhook.FilteringEnabled = dbWebHook.FilteringEnabled
	webhook.Secret = dbWebHook.Secret

	suffix := "." + os.Getenv("SITE_DOMAIN")

	webhook.Hostname = dbWebHook.Subdomain + suffix

	webhook.Id = id
	webhook.AutoReply = dbWebHook.Autoreply
	webhook.ShowDebugInfo = true
	webhook.TryLaterStatusCode = 503
	webhook.BackQueueSize = 100
	webhook.MaxWaitSeconds = 30 * time.Second
	webhook.LongPollWait = 30 * time.Second
	webhook.UseLongPoll = true
	webhook.MaxPayloadSize = 5000000

	webhookFilters := make([]*net.IPNet, 0)
	for _, filter := range dbWebHook.Filters {
		if filter.Type != "webhook" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(filter.IP + "/32") //for time being support single ip
		if err != nil {
			log.Printf("Skipping filter: %s for %s", filter.IP, "id")
			continue
		}
		webhookFilters = append(webhookFilters, ipnet)
	}

	webhook.WebhookFilters = webhookFilters

	pollReplyFilters := make([]*net.IPNet, 0)
	for _, filter := range dbWebHook.Filters {
		if filter.Type != "pollreply" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(filter.IP + "/32") //for time being support single ip
		if err != nil {
			log.Printf("Skipping filter: %s for %s", filter.IP, "id")
			continue
		}
		pollReplyFilters = append(pollReplyFilters, ipnet)
	}

	webhook.PollReplyFilters = pollReplyFilters

	return &webhook
}

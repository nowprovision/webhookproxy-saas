package main

import "fmt"
import "net"
import "time"
import "log"
import "sync"
import "nowprovision/webhookproxy"
import "database/sql"
import "github.com/lib/pq"
import "encoding/json"

type Database struct {
	conn          *sql.DB
	connString    string
	state         map[string]*webhookproxy.Config
	mutex         sync.Mutex
	changeHandler func(*webhookproxy.Config, *webhookproxy.Config)
	deleteHandler func(*webhookproxy.Config)
}

type DBWebHook struct {
	Name             string
	Subdomain        string
	Secret           string
	FilteringEnabled bool
	Filters          []DBFilter
}

type DBFilter struct {
	Id          string
	Description string
	IP          string
}

func NewDatabase(connString string) *Database {

	db, err := sql.Open("postgres", connString)

	if err != nil {
		log.Fatal(err)
	}

	emptyState := make(map[string]*webhookproxy.Config)

	return &Database{db, connString, emptyState, sync.Mutex{}, nil, nil}
}

func (db *Database) Load() {

	rowIterator, err := db.conn.Query("SELECT id, blob FROM webhooks")

	if err != nil {
		return
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
			fmt.Println(err.Error())
		}
	}
	updateListener := pq.NewListener(db.connString,
		time.Second*1,
		time.Second*1,
		reportProblem)
	updateListener.Listen("updated")

	go ProcessUpdates(db, updateListener)
}

func (db *Database) OnChange(fn func(*webhookproxy.Config, *webhookproxy.Config)) {
	db.changeHandler = fn
}
func (db *Database) OnDelete(fn func(*webhookproxy.Config)) {
	db.deleteHandler = fn
}

func ProcessUpdates(db *Database, listener *pq.Listener) {
	for {
		message := <-listener.Notify
		id := message.Extra
		Reread(db, id)
	}
}

func Reread(db *Database, id string) {
	stmt, err := db.conn.Prepare("SELECT id, blob FROM webhooks WHERE id = ?")
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
	webhook.Hostname = dbWebHook.Subdomain + ".webhookproxy.com"

	webhook.Id = id
	webhook.ShowDebugInfo = true
	webhook.TryLaterStatusCode = 503
	webhook.BackQueueSize = 100
	webhook.MaxWaitSeconds = 30 * time.Second
	webhook.LongPollWait = 30 * time.Second
	webhook.UseLongPoll = true
	webhook.MaxPayloadSize = 5000000

	webhookFilters := make([]*net.IPNet, 0)
	for _, filter := range dbWebHook.Filters {
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

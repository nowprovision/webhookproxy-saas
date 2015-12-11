package main

import "net"
import "net/http"
import "github.com/gorilla/mux"
import "fmt"
import "nowprovision/webhookproxy"
import "sync"
import "os"
import "strconv"
import "log"

func main() {

	log.Print("Reading environment variables")

	if len(os.Getenv("SITE_DOMAIN")) == 0 {
		log.Fatal("Unable to parse SITE_DOMAIN env variable")
	}

	postgresUser := os.Getenv("POSTGRES_USER")
	postgresPassword := os.Getenv("POSTGRES_PASSWORD")
	postgresDbName := os.Getenv("POSTGRES_DBNAME")

	if len(postgresUser) == 0 {
		log.Fatal("Unable to parse POSTGRES_USER env variable")
	}

	if len(postgresPassword) == 0 {
		log.Fatal("Unable to parse POSTGRES_PASSWORD env variable")
	}

	if len(postgresDbName) == 0 {
		log.Fatal("Unable to parse POSTGRES_DBNAME env variable")
	}

	bindingPort := os.Getenv("PORT")
	port, err := strconv.Atoi(bindingPort)

	if err != nil {
		log.Fatalf("Unable to parse PORT env variable %s", err)
	}

	connString := "user=" + postgresUser + " " +
		"password=" + postgresPassword + " " +
		"dbname=" + postgresDbName

	db := NewDatabase(connString)
	db.Load()

	hostnameHandlerMap := make(map[string]*webhookproxy.WebHookHandlers)
	hostnameConfigMap := make(map[string]*webhookproxy.Config)
	mutex := sync.Mutex{}

	db.ForAll(func(config *webhookproxy.Config) {
		log.Printf("Setting up %s\n", config.Hostname)
		hostnameHandlerMap[config.Hostname] = webhookproxy.BuildHandlers(config)
		hostnameConfigMap[config.Hostname] = config
	})

	db.StartUpdateDeleteListeners()

	db.OnChange(func(oldConfig *webhookproxy.Config, newConfig *webhookproxy.Config) {
		log.Printf("Changing  %s\n", oldConfig.Hostname)
		mutex.Lock()
		if oldConfig.Hostname != newConfig.Hostname {
			delete(hostnameHandlerMap, oldConfig.Hostname)
			delete(hostnameConfigMap, oldConfig.Hostname)
		}
		hostnameHandlerMap[newConfig.Hostname] = webhookproxy.BuildHandlers(newConfig)
		hostnameConfigMap[newConfig.Hostname] = newConfig
		mutex.Unlock()
	})

	db.OnDelete(func(oldConfig *webhookproxy.Config) {
		log.Printf("Removing  %s\n", oldConfig.Hostname)
		mutex.Lock()
		delete(hostnameHandlerMap, oldConfig.Hostname)
		delete(hostnameConfigMap, oldConfig.Hostname)
		mutex.Unlock()
	})

	db.OnAddition(func(config *webhookproxy.Config) {
		log.Printf("Setting up %s\n", config.Hostname)
		mutex.Lock()
		hostnameHandlerMap[config.Hostname] = webhookproxy.BuildHandlers(config)
		hostnameConfigMap[config.Hostname] = config
		mutex.Unlock()
	})

	r := mux.NewRouter()

	mapPath := func(action string, handlerLookup func(*webhookproxy.WebHookHandlers) func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, req *http.Request) {

			hostname, _, err := net.SplitHostPort(req.Host)
			if err != nil {
				w.WriteHeader(400)
				return
			}
			log.Printf("Recieved a %s request for %s on %s", req.Method, req.Host, req.URL)

			vars := mux.Vars(req)
			secret := vars["secret"]
			if len(secret) == 0 {
				w.WriteHeader(400)
				fmt.Fprint(w, "Secret required e.g. https://"+hostname+"/"+action+"/secret\n")
				return
			}

			mutex.Lock()
			configMap := hostnameConfigMap[hostname]
			handlers := hostnameHandlerMap[hostname]
			mutex.Unlock()

			if configMap == nil {
				w.WriteHeader(404)
				fmt.Fprint(w, "Host not valid\n")
				return
			}

			if secret == configMap.Secret {
				handlerLookup(handlers)(w, req)
			} else {
				w.WriteHeader(404)
			}
		}
	}

	r.HandleFunc("/webhook/{secret}", mapPath("webhook", func(handlers *webhookproxy.WebHookHandlers) func(http.ResponseWriter, *http.Request) {
		return handlers.HookHandler
	}))

	r.HandleFunc("/poll/{secret}", mapPath("poll", func(handlers *webhookproxy.WebHookHandlers) func(http.ResponseWriter, *http.Request) {
		return handlers.PollHandler
	}))

	r.HandleFunc("/reply/{secret}", mapPath("reply", func(handlers *webhookproxy.WebHookHandlers) func(http.ResponseWriter, *http.Request) {
		return handlers.ReplyHandler
	}))

	log.Printf("Starting webhookproxy server on port %d\n", port)

	err = http.ListenAndServe(":"+strconv.Itoa(port), r)

	if err != nil {
		log.Fatalf("Unable to start server %s", err)
	}
}

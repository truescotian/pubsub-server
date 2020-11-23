// This is the API for notifications. This makes use of the
// notification-hub package for ws connection management.
// This project is simply an HTTP server with JWT auth.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/TranquilityApp/backend-API/app/shared/database"
	config "github.com/TranquilityApp/config-manager"
	"github.com/TranquilityApp/middleware"
	jwtmiddleware "github.com/auth0/go-jwt-middleware"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/rs/cors"
	hub "github.com/truescotian/pubsub"
)

var (
	app *hub.Application
)

func init() {
	godotenv.Load()
}

// Establishes the application and HTTP server
func main() {
	database.ConnectV2(config.DB)
	defer database.DB.Close()

	app = hub.NewApp() // initializes app

	startHTTP()
}

// Starts the HTTP server. This makes use of Auth0's JWT middleware
// package, and registeres the routing endpoints.
func startHTTP() {
	go app.Run()

	// Establish CORS parameters
	c := cors.New(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:8080",
			"http://localhost:3002",
			"http://192.168.2.16:3002",
			"http://192.168.2.16:8080",
			"http://192.168.0.14:3002",
			"http://localhost:3000",
			"https://dev.tranquility.app",
			"https://api.dev.tranquility.app",
			"https://staging.tranquility.app",
			"https://api.staging.tranquility.app",
			"https://portal.tranquility.app",
			"https://api.tranquility.app",
		},
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})

	port := os.Getenv("PORT")
	if len(port) == 0 {
		port = "3001"
	}

	log.Println("listening on port", port)

	// Start HTTP server
	log.Fatal(http.ListenAndServe(":"+port, c.Handler(
		registerRoutes(middleware.InitializeMiddleware(jwtmiddleware.FromParameter("access_token"))),
	)))
}

// Registers routes with mux.
func registerRoutes(jwtMiddleware *jwtmiddleware.JWTMiddleware) *mux.Router {
	r := mux.NewRouter()

	// GET - necessary for AWS to ping
	r.Handle("/healthcheck", http.HandlerFunc(healthCheck)).Methods("GET")

	r.Handle("/message", http.HandlerFunc(message)).Methods("POST")

	msgRouter := mux.NewRouter().PathPrefix("/message").Subrouter()

	r.PathPrefix("/message").Handler(negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.Wrap(msgRouter),
	))

	// GET - handles upgrading http/https connections to ws/wss.
	// the JWT middleware is expecting an access_token
	// query parameter within the request
	r.Handle("/ws", negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.Wrap(app),
	))

	return r
}

// API endpoint used by AWS to check for server alive
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	return
}

// API endpoint to a subscribers Mailbox. This emits the message to a specific topic
// and message.
func message(w http.ResponseWriter, r *http.Request) {
	var m hub.Message

	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, fmt.Sprintf("Unable to decode message", err.Error()), http.StatusBadRequest)
		return
	}

	app.Hub.Publish(m)

	w.WriteHeader(http.StatusOK)
	return
}

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

	"github.com/TranquilityApp/backend-API/app/models"
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

var app *hub.Application

func init() {
	godotenv.Load()
}

func main() {
	database.ConnectV2(config.DB)
	defer database.DB.Close()

	app = hub.NewApp() // initializes app

	serve()
}

// serve starts the HTTP server. This uses Auth0's JWT middleware
// to verify and validate the access_token.
func serve() {
	// run the broker
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

// registerRoutes registers routes with mux.
func registerRoutes(jwtMiddleware *jwtmiddleware.JWTMiddleware) *mux.Router {
	r := mux.NewRouter()

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

// healthCheck used by AWS to check for server alive
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	return
}

type chatMessage struct {
	User         int    `json:"user"` // source user
	Channel      string `json:"channel"`
	Type         string `json:"type"`
	SubType      string `json:"subType"`
	ConnectionID int    `json:"connectionID"`
	Text         string `json:"text"`
}

// message sends a MailMessage to a subscribers Mailbox. This emits the message to a specific topic
// and message.
func message(w http.ResponseWriter, r *http.Request) {
	var chatMessage chatMessage

	if err := json.NewDecoder(r.Body).Decode(&chatMessage); err != nil {
		http.Error(w, fmt.Sprintf("Unable to decode message", err.Error()), http.StatusBadRequest)
		return
	}

	c, err := models.GetConnectionWithRecipients(chatMessage.ConnectionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get connection", err.Error()), http.StatusBadRequest)
		return
	}

	dbMsg := &models.Message{
		ConnectionID:      chatMessage.ConnectionID,
		Message:           chatMessage.Text,
		SourceUser:        *c.SourceUser,
		SourceUserID:      c.SourceUserID,
		DestinationUserID: c.DestinationUserID,
		Channel:           chatMessage.Channel,
	}

	if err := dbMsg.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Unable to save message. ", err.Error()), http.StatusInternalServerError)
		return
	}

	// prep for ws
	bytes, err := json.Marshal(chatMessage)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to marshal chat message", err.Error()), http.StatusBadRequest)
		return
	}

	pubMessage := hub.PublishMessage{
		Topic:   chatMessage.Channel,
		Payload: bytes,
	}

	app.Hub.Publish(pubMessage)

	w.WriteHeader(http.StatusOK)
	return
}

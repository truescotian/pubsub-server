// This is the API for notifications. This makes use of the
// notification-hub package for ws connection management.
// This project is simply an HTTP server with JWT auth.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/TranquilityApp/backend-API/app/models"
	"github.com/TranquilityApp/backend-API/app/shared/database"
	config "github.com/TranquilityApp/config-manager"
	"github.com/TranquilityApp/middleware"
	"github.com/TranquilityApp/middleware/helpers"
	jwtmiddleware "github.com/auth0/go-jwt-middleware"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/rs/cors"
	hub "github.com/truescotian/pubsub"
)

var broker *hub.Broker

var allowedOrigins = []string{"http://localhost:8080",
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
}

func init() {
	godotenv.Load()
}

func main() {
	database.ConnectV2(config.DB)
	defer database.DB.Close()

	broker = hub.NewBroker(allowedOrigins) // initializes app
	broker.OnSubscribe = func(s *hub.Subscription) {
		if strings.HasPrefix(s.Topic, "notifications/") {
			publishNotifications(s)
		}
	}

	serve()
}

// publishNotifications publishes the initial notifications for a user.
// Notifications published:
// Experiments, Fear Ladder, Evidence Collection
func publishNotifications(subscription *hub.Subscription) {
	// Experiment Notifications
	log.Printf("[DEBUG] Pushing notifications to user %s", subscription.Client.ID)

	user, err := models.GetUserByAuthID("auth0|" + strings.Split(subscription.Topic, "/")[1])
	if err != nil {
		log.Printf("[ERROR] Unable to get user %v ", err)
		return
	}

	type payloadMessage struct {
		Type    string `json:"type"`
		SubType string `json:"subType"`
		IDs     []uint `json:"payload"`
	}

	go func() {
		ids, err := models.GetBehaviouralExperimentNotifications(user.ID)
		if err != nil {
			log.Println("[ERROR] Unable to get behavioural experiment notifications. Error: %v", err)
			return
		}
		if len(ids) == 0 {
			ids = make([]uint, 0)
		}
		payload := payloadMessage{
			Type:    "notification",
			SubType: "BE",
			IDs:     ids,
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			log.Println("[ERROR] Unable to marshal notifications. Error: %v", err)
		}
		broker.Hub.Publish(hub.PublishMessage{
			Topic:   subscription.Topic,
			Payload: bytes,
		})
	}()

	go func() {
		ladders, err := models.GetFearLadderNotifications(user.ID)
		if err != nil {
			log.Println("[ERROR] Unable to get fear ladder notifications. Error: %v", err)
			return
		}

		ids := make([]uint, 0)
		for _, ladder := range ladders {
			ids = append(ids, ladder.LadderID)
		}
		payload := payloadMessage{
			Type:    "notification",
			SubType: "FL",
			IDs:     ids,
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			log.Println("[ERROR] Unable to marshal notifications. Error: %v", err)
		}
		broker.Hub.Publish(hub.PublishMessage{
			Topic:   subscription.Topic,
			Payload: bytes,
		})
	}()

	go func() {
		ids, err := models.GetEvidenceCollectionNotifications(user.ID)
		if err != nil {
			log.Println("[ERROR] Unable to get evidence collection notifications. Error: %v", err)
		}
		if len(ids) == 0 {
			ids = make([]uint, 0)
		}
		payload := payloadMessage{
			Type:    "notification",
			SubType: "EC",
			IDs:     ids,
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			log.Println("[ERROR] Unable to marshal notifications. Error: %v", err)
		}
		broker.Hub.Publish(hub.PublishMessage{
			Topic:   subscription.Topic,
			Payload: bytes,
		})
	}()

}

// serve starts the HTTP server. This uses Auth0's JWT middleware
// to verify and validate the access_token.
func serve() {
	// run the broker
	go broker.Run()

	// Establish CORS parameters
	c := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
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
	r.Handle("/message/{id}", http.HandlerFunc(messageDelete)).Methods("DELETE")
	r.Handle("/publish", http.HandlerFunc(publish)).Methods("POST")

	msgRouter := mux.NewRouter().PathPrefix("/message").Subrouter()
	pubRouter := mux.NewRouter().PathPrefix("/publish").Subrouter()

	r.PathPrefix("/message").Handler(negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.Wrap(msgRouter),
	))

	r.PathPrefix("/publish").Handler(negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.Wrap(pubRouter),
	))

	// GET - handles upgrading http/https connections to ws/wss.
	// the JWT middleware is expecting an access_token
	// query parameter within the request
	r.Handle("/ws", negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.HandlerFunc(AddUserID),
		negroni.Wrap(broker),
	))

	return r
}

// AddUserID is a middleware to add the AuthID of the connecting user from the Authorization
// header.
func AddUserID(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	authID := helpers.ParseJWTUserIDFromUrl(r)
	ctx := context.WithValue(r.Context(), middleware.AuthKey, authID)
	r = r.WithContext(ctx)
	next(w, r)
}

// healthCheck used by AWS to check for server alive
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	return
}

type defaultProperties struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	SubType string `json:"subType"`
}

type request struct {
	defaultProperties
	User         string `json:"user"` // source user
	ConnectionID int    `json:"connectionID"`
	Text         string `json:"text"`
}

type pubMessage struct {
	defaultProperties
	ChannelMessage models.ChannelMessage `json:"message"`
}

func publish(w http.ResponseWriter, r *http.Request) {

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pubMessage := hub.PublishMessage{
		Topic:   r.URL.Query()["topic"][0],
		Payload: body,
	}

	broker.Hub.Publish(pubMessage)

	w.WriteHeader(http.StatusOK)
	return
}

// message sends a MailMessage to a subscribers Mailbox. This emits the message to a specific topic
// and message.
func message(w http.ResponseWriter, r *http.Request) {
	var rBody request

	if err := json.NewDecoder(r.Body).Decode(&rBody); err != nil {
		http.Error(w, fmt.Sprintf("Unable to decode message", err.Error()), http.StatusBadRequest)
		return
	}

	c, err := models.GetConnectionWithRecipients(rBody.ConnectionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to get connection", err.Error()), http.StatusBadRequest)
		return
	}

	dbMsg := &models.Message{
		ConnectionID:      rBody.ConnectionID,
		Message:           rBody.Text,
		SourceUser:        *c.SourceUser,
		SourceUserID:      c.SourceUserID,
		DestinationUserID: c.DestinationUserID,
		Channel:           rBody.Channel,
	}

	if err := dbMsg.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Unable to save message. ", err.Error()), http.StatusInternalServerError)
		return
	}

	cMsg := models.ChannelMessage{
		ID:        int(dbMsg.ID),
		CreatedAt: dbMsg.CreatedAt,
		Message:   dbMsg.Message,
		ReadBy:    dbMsg.ReadBy,
		User:      rBody.User,
	}

	payload := pubMessage{
		// populate embedded struct (promoted fields)
		defaultProperties: defaultProperties{
			Channel: rBody.Channel,
			Type:    rBody.Type,
			SubType: rBody.SubType,
		},
		ChannelMessage: cMsg,
	}

	// prep for ws
	bytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to marshal chat message", err.Error()), http.StatusBadRequest)
		return
	}

	pubMessage := hub.PublishMessage{
		Topic:   rBody.Channel,
		Payload: bytes,
	}

	broker.Hub.Publish(pubMessage)

	w.WriteHeader(http.StatusOK)
	return
}

// messageDelete deletes a message and notifies the broker.
func messageDelete(w http.ResponseWriter, r *http.Request) {
	var rBody request

	if err := json.NewDecoder(r.Body).Decode(&rBody); err != nil {
		http.Error(w, fmt.Sprintf("Unable to decode message", err.Error()), http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to convert ID to int. Error: %v", err), http.StatusBadRequest)
		return
	}

	err = models.DeleteMessage(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to delete message. Error: %v", err), http.StatusInternalServerError)
		return
	}

	type deleteMessage struct {
		defaultProperties
		ID int `json:"id"`
	}

	payload := deleteMessage{
		defaultProperties: defaultProperties{
			Channel: rBody.Channel,
			Type:    "message",
			SubType: "message_deleted",
		},
		ID: id,
	}

	// prep for ws
	bytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to marshal chat message", err.Error()), http.StatusBadRequest)
		return
	}

	pubMessage := hub.PublishMessage{
		Topic:   rBody.Channel,
		Payload: bytes,
	}

	broker.Hub.Publish(pubMessage)

	w.WriteHeader(http.StatusOK)
	return

}

# Notification API 
This project is an HTTP server meant to handle the distribution of notifications, and authentication of websocket connections using the Auth0 jwt package. To handle the websocket connections and message distribution, [Notification-Hub](https://github.com/TranquilityApp/notification-hub) repository is used for connection management and acts as a message broker.

## Motivation
There was a need to separate the Notification-Hub (message broker) and its API endpoints. The Notification-Hub has now been separated and only contains functionalty pertaining to websockets. This project acts as an API which makes use of the Notification-Hub.

## Build status
TBD

## Screenshots
![image](https://imgur.com/a/04QFOUh "Successfully subscribing to topics, message from Notification-Hub")

## Tech/framework used
- Gorilla mux and negroni for routing
- Auth0's jwt middleware
- Cors middleware
- websocket hub

## Env
- Default port is 5000

## How to use?
Type `go run .`. However, some repositories may be private and require credentials.


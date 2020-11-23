FROM golang:latest

# Get Github key from args
ARG github_access_token
ARG environment

# set environment path
ENV PATH /go/bin:$PATH

WORKDIR /go/src/github.com/TranquilityApp/websocket-api

# create ssh directory
RUN mkdir ~/.ssh
RUN touch ~/.ssh/known_hosts
RUN ssh-keyscan -t rsa github.com >> ~/.ssh/known_hosts

# allow private repo pull
RUN git config --global url."https://$github_access_token:x-oauth-basic@github.com/".insteadOf "https://github.com/"

# copy the local package files to the container's workspace
ADD . /go/src/github.com/TranquilityApp/websocket-api

# Install the program
RUN go get -v
RUN go install github.com/TranquilityApp/websocket-api

ENV APP_ENV=$environment
EXPOSE 3001

CMD ["websocket-api"]

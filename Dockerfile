FROM golang:1.17

WORKDIR /code
COPY . .

# Pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them
# in subsequent builds if they change.
COPY go.mod go.sum* .
RUN go mod download && go mod verify

RUN make build-server
RUN make build-client

CMD tail -f /dev/null

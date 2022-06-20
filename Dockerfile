FROM golang:1.18-alpine AS build

COPY go.mod go.sum main.go /go/src/fsreadiness/

RUN cd /go/src/fsreadiness/ && CGO_ENABLED=0 go build -ldflags -s .

FROM scratch

COPY --from=build /go/src/fsreadiness/fsreadiness /

USER 65534

CMD ["/fsreadiness"]

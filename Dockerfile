FROM golang:alpine as builder
LABEL builder=true multistage_tag="dggarchiver-controller-builder"
WORKDIR /app
COPY . .
RUN GOOS=linux GOARCH=amd64 go build

FROM alpine:3.17
WORKDIR /app
COPY --from=builder /app/dggarchiver-controller .
ENTRYPOINT [ "./dggarchiver-controller" ]
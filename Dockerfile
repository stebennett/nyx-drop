FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /drop ./cmd/drop

FROM gcr.io/distroless/static:nonroot
COPY --from=build /drop /drop
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/drop"]

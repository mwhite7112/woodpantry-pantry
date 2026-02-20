FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/pantry ./cmd/pantry/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/pantry /bin/pantry
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/bin/pantry"]

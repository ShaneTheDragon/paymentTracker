# Build stage
FROM golang:1.21.6-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o paymentTracker .

# Final stage
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/paymentTracker .
COPY --from=builder /app/credentials/credentials.json /run/secrets/credentials.json
CMD ["./paymentTracker"]

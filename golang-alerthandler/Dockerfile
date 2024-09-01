# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o alert-handler .

# Final stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/alert-handler .
EXPOSE 8080
CMD ["./alert-handler"]
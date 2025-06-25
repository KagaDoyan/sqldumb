FROM golang:1.21-alpine

# Install MySQL client
RUN apk add --no-cache mysql-client

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN go build -o main .

# Command to run the application
CMD ["./main"] 
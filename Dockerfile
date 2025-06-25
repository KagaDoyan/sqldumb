FROM golang:1.21-alpine

# Install MariaDB client and required dependencies
RUN apk add --no-cache mariadb-client mariadb-connector-c

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
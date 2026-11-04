FROM golang:1.27-alpine3.24 AS builder

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux

# Move to working directory /build
WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build the application
RUN go build -o aci-exporter  *.go


FROM alpine:3.24

# Move to /dist directory as the place for resulting binary folder
WORKDIR /dist

# Copy binary from build to main folder
COPY --from=builder /build/aci-exporter .

# Export necessary port
EXPOSE 9643

# Run as non-root (nobody)
USER 65534:65534

# Command to run when starting the container
CMD ["/dist/aci-exporter"]

# kcptun

A fast and reliable UDP tunnel based on KCP protocol.

## Features

- High performance UDP tunnel
- Low latency network transmission
- Supports multiple platforms
- Easy configuration

## Structure

```
kcptun/
├── client/     # Client implementation
│   ├── config.go
│   ├── dial.go
│   └── main.go
├── server/     # Server implementation
│   ├── config.go
│   └── main.go
└── std/        # Standard utilities
    ├── atexit.go
    ├── comp.go
    ├── copy.go
    └── ...
```

## Building

```bash
# Build client with version injection
go build -ldflags "-X main.VERSION=$(git describe --tags)" -o client/client ./client/

# Build server with version injection
go build -ldflags "-X main.VERSION=$(git describe --tags)" -o server/server ./server/

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.VERSION=$(git describe --tags)" -o client_linux_amd64 ./client/
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.VERSION=$(git describe --tags)" -o server_linux_amd64 ./server/
```

## Usage

### Server

```bash
./server -c config.json
```

### Client

```bash
./client -c config.json
```

## Configuration

See `config.go` files in client and server directories for configuration options.

## License

MIT License
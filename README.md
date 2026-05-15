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
# Build client
go build -o client/client ./client/

# Build server
go build -o server/server ./server/
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
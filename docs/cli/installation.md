# CLI Installation

Install the `bp` command-line tool on your local machine.

## Quick Install

```bash
curl -fsSL https://pod.base.al/cli | bash
```

This installs `bp` to `/usr/local/bin/bp`.

## Manual Install

Download the binary for your platform:

### macOS (Apple Silicon)

```bash
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-darwin-arm64 \
  -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp
```

### macOS (Intel)

```bash
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-darwin-amd64 \
  -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp
```

### Linux (AMD64)

```bash
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-linux-amd64 \
  -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp
```

### Linux (ARM64)

```bash
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-linux-arm64 \
  -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp
```

## Build from Source

Requires Go 1.24+

```bash
git clone https://github.com/base-go/basepod.git
cd basepod
go build -o bp ./cmd/bp
mv bp /usr/local/bin/
```

## Verify Installation

```bash
bp version
```

Output:
```
bp version 1.0.5
```

## Next Steps

- [Quick Start](quickstart.md) - Deploy your first app
- [Reference](reference.md) - All commands

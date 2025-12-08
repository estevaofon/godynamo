# ⚡ GoDynamo

A powerful DynamoDB client for your terminal. A lightweight clone of Dynobase built with Go.

![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)

## ✨ Features

- 🔗 **Connect** to AWS DynamoDB or DynamoDB Local
- 📋 **List & Browse** all your tables
- 🔍 **Scan & Query** table data with pagination
- ✏️ **Create, Edit, Delete** items with JSON editor
- 🏗️ **Create & Delete** tables
- 🎯 **Filter** data with DynamoDB expressions
- 📦 **Export** data to JSON or CSV
- 🎨 Beautiful cyberpunk-themed terminal UI

## 📦 Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/yourusername/godynamo.git
cd godynamo

# Download dependencies
go mod tidy

# Build
go build -o godynamo .

# Run
./godynamo
```

### Using Go Install

```bash
go install github.com/godynamo@latest
```

## 🚀 Quick Start

### With DynamoDB Local

1. Start DynamoDB Local (using Docker):
```bash
docker run -p 8000:8000 amazon/dynamodb-local
```

2. Run GoDynamo:
```bash
./godynamo
```

3. Use default settings to connect:
   - Endpoint: `http://localhost:8000`
   - Region: `us-east-1`
   - Access Key: `local`
   - Secret Key: `local`

### With AWS DynamoDB

1. Configure your AWS credentials:
```bash
aws configure
```

2. Run GoDynamo:
```bash
./godynamo
```

3. Configure connection:
   - Leave endpoint empty for AWS
   - Set your region (e.g., `us-east-1`)
   - Uncheck "Use Local DynamoDB"

## ⌨️ Keyboard Shortcuts

### Global
| Key | Action |
|-----|--------|
| `Ctrl+Q` | Quit |
| `Ctrl+C` | Quit |

### Connection Screen
| Key | Action |
|-----|--------|
| `Tab` | Next field |
| `Shift+Tab` | Previous field |
| `Space` | Toggle checkbox |
| `Enter` | Connect |

### Tables List
| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate |
| `Enter` | Open table |
| `c` | Create table |
| `d` | Delete table |
| `r` | Refresh |
| `q` | Back |

### Table Data View
| Key | Action |
|-----|--------|
| `↑/↓/←/→` | Navigate |
| `Enter` | View item |
| `n` | New item |
| `e` | Edit item |
| `d` | Delete item |
| `f` | Filter |
| `x` | Export |
| `PgDown` | Next page |
| `r` | Refresh |
| `q` | Back |

### Item Editor
| Key | Action |
|-----|--------|
| `Ctrl+S` | Save |
| `Esc` | Cancel |

### Filter/Query
| Key | Action |
|-----|--------|
| `Ctrl+Enter` | Execute |
| `Ctrl+C` | Clear |
| `Esc` | Cancel |

## 🎨 Screenshots

```
┌──────────────────────────────────────────────────────────────────┐
│                      ╔═══════════════════╗                       │
│                      ║   ⚡ GoDynamo     ║                       │
│                      ╚═══════════════════╝                       │
│                                                                  │
│                     Connect to DynamoDB                          │
│                                                                  │
│  Endpoint                                                        │
│  ╭──────────────────────────────────────────────────────────╮   │
│  │ http://localhost:8000                                     │   │
│  ╰──────────────────────────────────────────────────────────╯   │
│                                                                  │
│  Region                                                          │
│  ╭──────────────────────────────────────────────────────────╮   │
│  │ us-east-1                                                 │   │
│  ╰──────────────────────────────────────────────────────────╯   │
│                                                                  │
│  [✓] Use Local DynamoDB                                          │
│                                                                  │
│                      [ Connect ]                                 │
│                                                                  │
│     Tab: Next field │ Enter: Connect │ Ctrl+Q: Quit             │
└──────────────────────────────────────────────────────────────────┘
```

## 🔧 Configuration

GoDynamo uses AWS SDK v2, which supports various authentication methods:

1. **Environment Variables**
   ```bash
   export AWS_ACCESS_KEY_ID=your_key
   export AWS_SECRET_ACCESS_KEY=your_secret
   export AWS_REGION=us-east-1
   ```

2. **AWS Credentials File** (`~/.aws/credentials`)
   ```ini
   [default]
   aws_access_key_id = your_key
   aws_secret_access_key = your_secret
   ```

3. **AWS Config File** (`~/.aws/config`)
   ```ini
   [default]
   region = us-east-1
   ```

4. **IAM Roles** (for EC2/ECS/Lambda)

## 📝 Filter Expressions

GoDynamo supports DynamoDB filter expressions:

```
# Check if attribute exists
attribute_exists(email)

# Contains substring
contains(name, "john")

# Comparison
age >= 18
price BETWEEN 10 AND 100

# Multiple conditions
attribute_exists(email) AND age >= 18
```

## 🛠️ Development

### Project Structure

```
godynamo/
├── main.go                 # Entry point
├── go.mod                  # Go modules
├── internal/
│   ├── app/
│   │   └── app.go         # Main application logic
│   ├── dynamo/
│   │   └── client.go      # DynamoDB client wrapper
│   ├── models/
│   │   └── models.go      # Data models & converters
│   └── ui/
│       ├── styles.go      # UI styles & colors
│       ├── components.go  # Reusable UI components
│       └── json_viewer.go # JSON syntax highlighting
└── README.md
```

### Building

```bash
# Development
go run .

# Production build
go build -ldflags="-s -w" -o godynamo .

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o godynamo-linux .

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build -o godynamo.exe .
```

### Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [AWS SDK Go v2](https://github.com/aws/aws-sdk-go-v2) - DynamoDB client

## 📄 License

MIT License - feel free to use this project for any purpose.

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

## 🙏 Acknowledgments

- Inspired by [Dynobase](https://dynobase.dev/)
- Built with [Charm](https://charm.sh/) libraries
- DynamoDB by AWS


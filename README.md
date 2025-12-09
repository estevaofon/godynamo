# ⚡ GoDynamo

**A fast, lightweight DynamoDB client for your terminal.**

Built with Go and [Charm](https://charm.sh/) libraries. Free and open source alternative to Dynobase.

![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Windows%20|%20Linux%20|%20macOS-blue?style=flat-square)

---

## ✨ Features

### 🔗 Smart Connection
- **Auto-connect** to AWS using your configured credentials
- **Multi-region discovery** - automatically finds regions with tables
- **Region dropdown** - easily switch between regions

### 📋 Table Management
- **List tables** with fuzzy search filtering
- **View schema** in JSON format
- **Navigate** with keyboard shortcuts

### 🔍 Powerful Querying
- **Visual Filter Builder** - no need to memorize DynamoDB syntax
- **Smart Query Detection** - automatically uses GSI indexes when available
- **Continuous Scan** - searches until finding results (with 3-min timeout)
- **Operators**: Equals, Not Equals, Greater/Less Than, Contains, Begins With, Exists

### ✏️ Data Operations
- **View items** with JSON syntax highlighting
- **Create, Edit, Delete** items with built-in JSON editor
- **Copy values** - single cell or entire row as JSON
- **Horizontal scrolling** for wide tables

### 📦 Export
- **JSON format** - full DynamoDB structure
- **CSV format** - for spreadsheets

### 🎨 User Experience
- **Cyberpunk theme** - beautiful terminal aesthetics
- **Keyboard-first** - efficient navigation
- **Unicode support** - works with accented characters
- **SSH friendly** - works on remote servers

---

## 📦 Installation

### Pre-built Binaries

Download from [Releases](https://github.com/yourusername/godynamo/releases):

```bash
# Linux
curl -L https://github.com/yourusername/godynamo/releases/latest/download/godynamo-linux-amd64 -o godynamo
chmod +x godynamo

# macOS
curl -L https://github.com/yourusername/godynamo/releases/latest/download/godynamo-darwin-amd64 -o godynamo
chmod +x godynamo

# Windows - download godynamo-windows-amd64.exe
```

### From Source

```bash
git clone https://github.com/yourusername/godynamo.git
cd godynamo
go build -o godynamo .
./godynamo
```

### Go Install

```bash
go install github.com/yourusername/godynamo@latest
```

---

## 🚀 Quick Start

1. **Configure AWS credentials** (if not already done):
```bash
aws configure
```

2. **Run GoDynamo**:
```bash
./godynamo
```

3. **That's it!** GoDynamo will:
   - Detect your AWS credentials
   - Scan all regions for DynamoDB tables
   - Show you a region selector (if multiple regions have tables)

---

## 🎯 Filter Builder

GoDynamo features a visual filter builder - no need to remember DynamoDB expression syntax!

### Available Operators

| Operator | Symbol | Description |
|----------|--------|-------------|
| Equals | `=` | Exact match |
| Not Equals | `≠` | Not equal |
| Greater Than | `>` | Greater than |
| Less Than | `<` | Less than |
| Greater or Equal | `≥` | Greater or equal |
| Less or Equal | `≤` | Less or equal |
| Contains | `∋` | String contains |
| Not Contains | `∌` | String doesn't contain |
| Begins With | `⊃` | String starts with |
| Exists | `∃` | Attribute exists |
| Not Exists | `∄` | Attribute doesn't exist |

### Smart Query Detection

When you filter by:
- **Partition Key** → Uses efficient `Query` operation
- **GSI Partition Key** → Uses `Query` on the index
- **Other attributes** → Uses `Scan` with continuous pagination

---

## 🔧 AWS Configuration

GoDynamo uses AWS SDK v2 and supports all standard authentication methods:

- **Environment Variables** (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
- **Credentials File** (`~/.aws/credentials`)
- **Config File** (`~/.aws/config`)
- **IAM Roles** (EC2, ECS, Lambda)
- **AWS SSO** (`aws sso login`)

---

## 🏗️ Building

```bash
# Development
go run .

# Production build (smaller binary)
go build -ldflags="-s -w" -o godynamo .

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o godynamo-linux .
GOOS=darwin GOARCH=amd64 go build -o godynamo-darwin .
GOOS=windows GOARCH=amd64 go build -o godynamo.exe .
```

---

## 📦 Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [AWS SDK Go v2](https://github.com/aws/aws-sdk-go-v2) - DynamoDB client

---

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

---

## 📄 License

MIT License - feel free to use this project for any purpose.

---

## 🙏 Acknowledgments

- Inspired by [Dynobase](https://dynobase.dev/)
- Built with [Charm](https://charm.sh/) libraries

---

<p align="center">
  <sub>Made with ❤️ and Go</sub>
</p>

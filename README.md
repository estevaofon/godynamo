# ⚡ GoDynamo

**A fast, lightweight DynamoDB client for your desktop and terminal.**

Built with Go and [Charm](https://charm.sh/) libraries. Free and open source alternative to Dynobase.

![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Windows%20|%20Linux%20|%20macOS-blue?style=flat-square)

---

### Godynamo GUI
<img width="1341" height="680" alt="godynamo-gui" src="https://github.com/user-attachments/assets/1d815349-6ae4-4e28-af4c-e0b87f336cff" />

### Godynamo TUI
https://github.com/user-attachments/assets/e53d2026-f4e7-4976-bf72-fa64edf743a1

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

### From Source

```bash
git clone https://github.com/yourusername/godynamo.git
cd godynamo
go build -o godynamo .
./godynamo
```

---

## 🚀 Quick Start

1. **Configure AWS credentials** (if not already done):
```bash
aws configure
```

2. **Run GoDynamo**:
```bash
./godynamo        # opens the desktop GUI (default)
./godynamo tui    # opens the terminal UI instead
```

On first run, if the desktop GUI's Electron dependencies aren't installed yet,
GoDynamo installs them automatically (showing a wait window) and then opens.

3. **That's it!** GoDynamo will:
   - Detect your AWS credentials
   - Scan all regions for DynamoDB tables
   - Show you a region selector (if multiple regions have tables)

---

## 🖥️ Desktop GUI (default)

GoDynamo ships an Electron desktop UI that is **the default interface on Windows
and Linux/Ubuntu** — running `godynamo` (no arguments) opens it. It is at full
feature parity with the terminal UI: connect (AWS region or DynamoDB Local), list
tables, scan/browse, the visual filter builder, view item JSON and schema,
create / edit / delete items, create tables, export to JSON/CSV, copy values, and
in-item JSON search. Run `godynamo tui` for the terminal UI; `godynamo gui` is an
explicit alias for the default.

Item JSON (both the detail view and the editor) renders with **syntax highlighting**
and **collapsible `{…}` / `[…]` blocks** (CodeMirror 6): fold a sub-object to skim a
large record, or — in the editor — fold then select-and-delete a whole sub-tree in one
move, the way Dynobase does.

### Setup (automatic on first run)

The first time you launch the GUI, GoDynamo runs `npm install` in `electron/`
for you (showing a wait window) if it hasn't been done yet — you just need
**Node.js + npm** on your PATH. To do it manually instead:

```bash
cd electron
npm install
cd ..
```

### Linux / Ubuntu notes

On Linux two extra steps may be needed after `npm install` (both confirmed on
Ubuntu with Node.js 24):

**1. Electron binary fails to extract (`ENOENT … path.txt`).**
On recent Node.js (e.g. v24), Electron's bundled `extract-zip` postinstall can
fail silently — it extracts only `locales/` and exits 0 without writing
`path.txt`, so launching errors with:

```
Error: ENOENT: no such file or directory, open '.../electron/node_modules/electron/dist/path.txt'
```

The downloaded zip itself is fine; only the extraction is broken. Extract it
manually with the system `unzip` and create `path.txt`:

```bash
cd electron/node_modules/electron
ZIP="$HOME/.cache/electron/"*"/electron-v$(node -p "require('./package').version")-linux-x64.zip"
rm -rf dist path.txt
mkdir -p dist
unzip -q $ZIP -d dist
[ -f dist/electron.d.ts ] && mv -f dist/electron.d.ts ./electron.d.ts
printf 'electron' > path.txt
chmod +x dist/electron
cd ../../..
```

> If `~/.cache/electron/.../*.zip` is missing (or was truncated by a dropped
> connection / VPN), delete `~/.cache/electron` and run `npm install` again to
> re-download before extracting.

**2. `chrome-sandbox` must be setuid root.**
Electron's Chromium sandbox aborts unless its helper binary is owned by root
with mode `4755`:

```
The SUID sandbox helper binary was found, but is not configured correctly. … aborting now.
```

Fix it once (re-run after any Electron reinstall):

```bash
sudo chown root:root electron/node_modules/electron/dist/chrome-sandbox
sudo chmod 4755 electron/node_modules/electron/dist/chrome-sandbox
```

After both steps, `./godynamo` opens the Electron window normally.

### Launch

```bash
go run .            # GUI is the default
go run . tui        # terminal UI instead
# or, after building:
go build -o godynamo.exe .
./godynamo.exe      # GUI (use `./godynamo.exe tui` for the terminal UI)
```

On launch you choose **AWS** (pick a region; uses your default credentials) or
**DynamoDB Local** (default endpoint `http://localhost:8000`). The Go process starts
a loopback-only HTTP bridge (127.0.0.1, random port, one-time token) and opens the
Electron window; closing the window shuts everything down.

> Status: at full feature parity with the terminal UI (filtering, CRUD,
> create-table, export, copy, in-item JSON search). Packaged installers are the
> main remaining item, planned for a later phase.

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

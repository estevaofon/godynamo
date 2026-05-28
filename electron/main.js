const { app, BrowserWindow, ipcMain } = require('electron')
const path = require('path')

const PORT = process.env.GODYNAMO_BRIDGE_PORT
const TOKEN = process.env.GODYNAMO_BRIDGE_TOKEN

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 800,
    title: 'GoDynamo',
    backgroundColor: '#0b0f1a',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })
  win.loadFile(path.join(__dirname, 'renderer', 'index.html'))
}

// The renderer never sees the token directly; the preload fetches it once via IPC.
ipcMain.handle('bridge-info', () => ({
  baseUrl: `http://127.0.0.1:${PORT}`,
  token: TOKEN,
}))

app.whenReady().then(() => {
  if (!PORT || !TOKEN) {
    const { dialog } = require('electron')
    dialog.showErrorBox(
      'GoDynamo',
      'Missing GODYNAMO_BRIDGE_PORT / GODYNAMO_BRIDGE_TOKEN.\nLaunch the GUI via: go run . gui'
    )
    app.quit()
    return
  }
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  app.quit()
})

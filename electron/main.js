const { app, BrowserWindow, ipcMain, dialog } = require('electron')
const path = require('path')
const fs = require('fs')

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

app.whenReady().then(() => {
  if (!PORT || !TOKEN) {
    dialog.showErrorBox(
      'GoDynamo',
      'Missing GODYNAMO_BRIDGE_PORT / GODYNAMO_BRIDGE_TOKEN.\nLaunch the GUI via: go run . gui'
    )
    app.quit()
    return
  }
  // The renderer never sees the token directly; the preload fetches it once via IPC.
  ipcMain.handle('bridge-info', () => ({
    baseUrl: `http://127.0.0.1:${PORT}`,
    token: TOKEN,
  }))
  // Export: native Save dialog + write file (renderer builds the content).
  ipcMain.handle('export-file', async (_event, { defaultName, content }) => {
    const { canceled, filePath } = await dialog.showSaveDialog({
      defaultPath: defaultName,
      filters: [
        { name: 'JSON', extensions: ['json'] },
        { name: 'CSV', extensions: ['csv'] },
        { name: 'All Files', extensions: ['*'] },
      ],
    })
    if (canceled || !filePath) {
      return { canceled: true }
    }
    await fs.promises.writeFile(filePath, content, 'utf8')
    return { path: filePath }
  })
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  app.quit()
})

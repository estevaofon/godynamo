# Vendored CodeMirror 6

`codemirror.bundle.js` is a generated, committed esbuild IIFE bundle that exposes
`window.CM = { createEditor, createViewer }`. It is the only file the app loads.

## Regenerate

```sh
cd electron/renderer/vendor/codemirror
npm install        # dev-only deps; produces node_modules/ (gitignored)
npm run build      # -> codemirror.bundle.js
```

`cm-entry.js` is the source of truth (theme, folding, find decorations). `demo.html`
is a standalone, AWS-free harness: open it in a browser to eyeball highlighting,
folding, and find marks. License: CodeMirror is MIT (see LICENSE).

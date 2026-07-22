# VigilAgent Chrome Extension

This directory contains the Chrome Extension for VigilAgent, an AI Code Verifier.

## Note on Icons

Currently, the `manifest.json` has been modified to omit the `icons` and `action.default_icon` fields. This is because Chrome extensions require real PNG files for icons, and they haven't been generated yet. 

To finalize this extension:
1. Create real PNG icons (`icon16.png`, `icon48.png`, `icon128.png`) inside an `icons/` directory.
2. Update `manifest.json` to include the icon references again:

```json
  "action": {
    "default_popup": "popup.html",
    "default_icon": {
      "16": "icons/icon16.png",
      "48": "icons/icon48.png",
      "128": "icons/icon128.png"
    }
  },
  "icons": {
    "16": "icons/icon16.png",
    "48": "icons/icon48.png",
    "128": "icons/icon128.png"
  }
```

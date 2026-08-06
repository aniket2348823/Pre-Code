document.addEventListener('DOMContentLoaded', () => {
  const backendUrlInput = document.getElementById('backendUrl');
  const apiKeyInput = document.getElementById('apiKey');
  const enabledInput = document.getElementById('enabled');
  const showDetailsInput = document.getElementById('showDetails');
  const saveBtn = document.getElementById('saveBtn');
  const statusDiv = document.getElementById('status');

  // Load settings. Keys are stored in chrome.storage.local — never .sync,
  // which would replicate provider API keys through the cloud sync service.
  chrome.storage.local.get({
    backendUrl: 'http://localhost:8080',
    apiKey: '',
    enabled: true,
    showDetails: true
  }, (items) => {
    backendUrlInput.value = items.backendUrl;
    apiKeyInput.value = items.apiKey;
    enabledInput.checked = items.enabled;
    showDetailsInput.checked = items.showDetails;
  });

// Never send the API key over plain HTTP to a remote host. Localhost is
// allowed for local development; anything else must be https:// — this
// mirrors the guard in content.js and the VS Code extension (client.ts).
function isSafeBackendUrl(url) {
  try {
    const parsed = new URL(url || '');
    if (parsed.protocol === 'https:') return true;
    if (parsed.protocol === 'http:') {
      return parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1' || parsed.hostname === '[::1]' || parsed.hostname === '::1';
    }
    return false;
  } catch (e) {
    return false;
  }
}

  // Save settings
  saveBtn.addEventListener('click', async () => {
    const backendUrl = backendUrlInput.value.trim().replace(/\/$/, '');
    const apiKey = apiKeyInput.value.trim();
    const enabled = enabledInput.checked;
    const showDetails = showDetailsInput.checked;

    // Refuse to persist an unsafe config while a key is present: the health
    // probe and scans carry the API key, and it must never go out over plain
    // HTTP to a non-local host. Block before saving so the user is not left
    // believing a rejected configuration was stored.
    if (apiKey && !isSafeBackendUrl(backendUrl)) {
      statusDiv.textContent = 'Error: API keys are only sent over https:// (http://localhost is allowed for local development)';
      statusDiv.className = 'status-error';
      return;
    }

    chrome.storage.local.set({
      backendUrl,
      apiKey,
      enabled,
      showDetails
    }, async () => {
      statusDiv.textContent = 'Settings saved. Testing connection...';
      statusDiv.className = '';

      try {
        const res = await fetch(`${backendUrl}/health`, {
          headers: apiKey ? { 'Authorization': `Bearer ${apiKey}` } : {}
        });
        
        if (res.ok) {
          statusDiv.textContent = 'Connected successfully!';
          statusDiv.className = 'status-success';
        } else {
          statusDiv.textContent = `Error: Server returned ${res.status}`;
          statusDiv.className = 'status-error';
        }
      } catch (e) {
        statusDiv.textContent = 'Error: Could not connect to backend';
        statusDiv.className = 'status-error';
      }
      
      setTimeout(() => {
        if (statusDiv.textContent.includes('successfully')) {
          statusDiv.textContent = '';
        }
      }, 3000);
    });
  });
});

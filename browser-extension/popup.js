document.addEventListener('DOMContentLoaded', () => {
  const backendUrlInput = document.getElementById('backendUrl');
  const apiKeyInput = document.getElementById('apiKey');
  const enabledInput = document.getElementById('enabled');
  const showDetailsInput = document.getElementById('showDetails');
  const saveBtn = document.getElementById('saveBtn');
  const statusDiv = document.getElementById('status');

  // Load settings
  chrome.storage.sync.get({
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

  // Save settings
  saveBtn.addEventListener('click', async () => {
    const backendUrl = backendUrlInput.value.trim().replace(/\/$/, '');
    const apiKey = apiKeyInput.value.trim();
    const enabled = enabledInput.checked;
    const showDetails = showDetailsInput.checked;

    chrome.storage.sync.set({
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

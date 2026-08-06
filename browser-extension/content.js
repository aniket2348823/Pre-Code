let timeout = null;

function getSettings() {
  return new Promise((resolve) => {
    // Keys live in chrome.storage.local — never .sync (cloud replication would
    // expose provider API keys through the sync service).
    chrome.storage.local.get({
      backendUrl: 'http://localhost:8080',
      apiKey: '',
      enabled: true,
      showDetails: true
    }, (items) => {
      resolve(items);
    });
  });
}

async function scanCode(code, language, settings) {
  try {
    // Never send an API key over plain HTTP to a remote host. Localhost is
    // allowed for local development; anything else must be https:// — this
    // mirrors the guard in the VS Code extension (client.ts).
    if (!isSafeBackendUrl(settings.backendUrl)) {
      throw new Error('VigilAgent refuses to send API keys over plain HTTP. Use an https:// backend URL (http://localhost is allowed for local development).');
    }
    const response = await fetch(`${settings.backendUrl}/api/v1/scan`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${settings.apiKey}`
      },
      body: JSON.stringify({ code, language })
    });
    if (!response.ok) throw new Error('Network error');
    return await response.json();
  } catch (err) {
    throw err;
  }
}

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

function createBadge(block) {
  const badge = document.createElement('div');
  badge.className = 'vigilagent-badge vigilagent-badge-loading';
  badge.innerHTML = `<span class="va-icon">⏳</span><span class="va-text">Analyzing...</span>`;
  
  const wrapper = document.createElement('div');
  wrapper.style.position = 'relative';
  wrapper.className = 'vigilagent-wrapper';
  
  block.parentNode.insertBefore(wrapper, block);
  wrapper.appendChild(block);
  wrapper.appendChild(badge);
  
  return badge;
}

function updateBadge(badge, result, settings) {
  badge.className = 'vigilagent-badge';
  let icon = '✅';
  let colorClass = 'va-color-green';
  let grade = result.grade || 'A';
  
  if (['C', 'D'].includes(grade)) {
    icon = '⚠️';
    colorClass = 'va-color-yellow';
  } else if (['F'].includes(grade)) {
    icon = '❌';
    colorClass = 'va-color-red';
  }
  
  badge.classList.add(colorClass);
  const findingsCount = result.findings ? result.findings.length : 0;
  
  badge.innerHTML = `<span class="va-icon">${icon}</span><span class="va-text">Grade: ${escapeHtml(grade)} (${findingsCount} findings)</span>`;
  
  if (settings.showDetails && findingsCount > 0) {
    const details = document.createElement('div');
    details.className = 'vigilagent-badge-expanded';
    details.style.display = 'none';
    
    result.findings.forEach(f => {
      const fDiv = document.createElement('div');
      fDiv.className = 'va-finding';
      fDiv.innerHTML = `<strong>${escapeHtml(f.severity) || 'Issue'}</strong>: ${escapeHtml(f.message)}`;
      details.appendChild(fDiv);
    });
    
    badge.appendChild(details);
    badge.addEventListener('click', () => {
      details.style.display = details.style.display === 'none' ? 'block' : 'none';
    });
    badge.style.cursor = 'pointer';
  }
}

function setErrorBadge(badge) {
  badge.className = 'vigilagent-badge vigilagent-badge-error';
  badge.innerHTML = `<span class="va-icon">⚠️</span><span class="va-text">Scan Failed</span>`;
}

// Escape untrusted server data before it touches innerHTML — finding messages
// can originate from scanned (untrusted) code and must never execute as markup.
function escapeHtml(str) {
  return String(str ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

async function processBlocks() {
  const settings = await getSettings();
  if (!settings.enabled) return;

  const blocks = document.querySelectorAll('pre code, pre, .code-block');
  for (const block of blocks) {
    if (block.hasAttribute('data-vigilagent-processed')) continue;
    if (block.tagName === 'CODE' && block.parentElement.tagName === 'PRE' && block.parentElement.hasAttribute('data-vigilagent-processed')) continue;
    
    block.setAttribute('data-vigilagent-processed', 'true');
    
    const code = block.innerText || block.textContent;
    if (!code || code.trim().length < 10) continue;

    let language = 'auto';
    const langMatch = block.className.match(/language-(\w+)/);
    if (langMatch) language = langMatch[1];
    
    const badge = createBadge(block);
    
    try {
      const result = await scanCode(code, language, settings);
      updateBadge(badge, result, settings);
    } catch (e) {
      setErrorBadge(badge);
    }
  }
}

const observer = new MutationObserver(() => {
  if (timeout) clearTimeout(timeout);
  timeout = setTimeout(processBlocks, 500);
});

observer.observe(document.body, { childList: true, subtree: true });
processBlocks();

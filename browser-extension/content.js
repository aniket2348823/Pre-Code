let timeout = null;

function getSettings() {
  return new Promise((resolve) => {
    chrome.storage.sync.get({
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
  
  badge.innerHTML = `<span class="va-icon">${icon}</span><span class="va-text">Grade: ${grade} (${findingsCount} findings)</span>`;
  
  if (settings.showDetails && findingsCount > 0) {
    const details = document.createElement('div');
    details.className = 'vigilagent-badge-expanded';
    details.style.display = 'none';
    
    result.findings.forEach(f => {
      const fDiv = document.createElement('div');
      fDiv.className = 'va-finding';
      fDiv.innerHTML = `<strong>${f.severity || 'Issue'}</strong>: ${f.message}`;
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

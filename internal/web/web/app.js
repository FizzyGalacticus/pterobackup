const output = document.getElementById('output');
const itemsBody = document.getElementById('itemsBody');
const privateKeyFile = document.getElementById('privateKeyFile');
const privateKeyValue = document.getElementById('privateKeyValue');
const publicKeyOutput = document.getElementById('publicKeyOutput');
const generateKeypair = document.getElementById('generateKeypair');
let scheduleItems = {};

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

function setArtifactTotal(detailsNode, totalMB) {
  const totalNode = detailsNode.querySelector('.artifact-total');
  if (!totalNode) {
    return;
  }

  totalNode.textContent = `Total: ${Number(totalMB || 0).toFixed(2)} MB`;
}

function log(message) {
  output.textContent = `${new Date().toISOString()} ${message}\n${output.textContent}`;
}

function formatTimestamp(v) {
  if (!v) {
    return '--';
  }

  const date = new Date(v);
  if (Number.isNaN(date.getTime())) {
    return '--';
  }

  return date.toLocaleString();
}

function normalizeKeyText(v) {
  return (v || '').replace(/\r\n/g, '\n').trim();
}

function createLocalItemID() {
  const rand = Math.random().toString(36).slice(2, 10);
  return `item-${Date.now()}-${rand}`;
}

function renderScheduleIndicators() {
  [...itemsBody.querySelectorAll('.backup-item')].forEach((tr) => {
    const idInput = tr.querySelector('input[data-key="id"]');
    const nextCell = tr.querySelector('.status-next');
    const lastCell = tr.querySelector('.status-last');
    const id = (idInput?.value || '').trim();
    const status = scheduleItems[id] || {};

    nextCell.textContent = formatTimestamp(status.nextRunAt);
    lastCell.textContent = formatTimestamp(status.lastSuccessAt);
    if (status.lastError) {
      lastCell.title = `Last error: ${status.lastError}`;
    } else {
      lastCell.title = '';
    }
  });
}

async function runSingleAction(action, itemId) {
  if (!itemId) {
    log('Row action failed: item ID is required');
    return;
  }

  try {
    if (action === 'backup') {
      const res = await callJSON('/api/backup/item', 'POST', { itemId });
      log(`Backup complete for ${itemId}: ${JSON.stringify(res.results || [])}`);
      await loadSchedule();
      await refreshOpenArtifactPanels();
      return;
    }

    await callJSON('/api/restore/item', 'POST', { itemId });
    log(`Restore complete for ${itemId}`);
  } catch (err) {
    log(`${action} failed for ${itemId}: ${err.message}`);
  }
}

async function loadItemArtifacts(itemId, listNode) {
  const detailsNode = listNode.closest('.artifact-details');

  if (!itemId) {
    listNode.innerHTML = '<li class="artifact-empty">Save config before loading artifact list.</li>';
    if (detailsNode) {
      setArtifactTotal(detailsNode, 0);
    }
    return;
  }

  try {
    const payload = await callJSON(`/api/backup/item/files?itemId=${encodeURIComponent(itemId)}`, 'GET');
    const items = payload.items || [];
    const totalMB = items.reduce((sum, item) => sum + Number(item.sizeMB || 0), 0);
    if (detailsNode) {
      setArtifactTotal(detailsNode, totalMB);
    }

    if (items.length === 0) {
      listNode.innerHTML = '<li class="artifact-empty">No backups stored yet.</li>';
      return;
    }

    listNode.innerHTML = items
      .map((item) => `
        <li class="artifact-row">
          <span class="artifact-name">${escapeHtml(item.name)}</span>
          <span class="artifact-size">${Number(item.sizeMB || 0).toFixed(2)} MB</span>
          <button type="button" class="artifact-delete danger-sm" data-name="${escapeHtml(item.name)}">Delete</button>
        </li>
      `)
      .join('');

    listNode.querySelectorAll('.artifact-delete').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const name = btn.dataset.name;
        if (!confirm(`Delete backup file "${name}"?`)) {
          return;
        }
        try {
          await callJSON(
            `/api/backup/item/file?itemId=${encodeURIComponent(itemId)}&name=${encodeURIComponent(name)}`,
            'DELETE',
          );
          log(`Deleted ${name}`);
          await loadItemArtifacts(itemId, listNode);
        } catch (err) {
          log(`Delete failed: ${err.message}`);
        }
      });
    });
  } catch (err) {
    listNode.innerHTML = `<li class="artifact-empty">Failed to load artifacts: ${escapeHtml(err.message)}</li>`;
    if (detailsNode) {
      setArtifactTotal(detailsNode, 0);
    }
  }
}

async function refreshOpenArtifactPanels() {
  const openPanels = [...itemsBody.querySelectorAll('.artifact-details[open]')];
  await Promise.all(openPanels.map(async (panel) => {
    const host = panel.closest('.backup-item');
    if (!host) {
      return;
    }
    const id = host.querySelector('input[data-key="id"]')?.value.trim() || '';
    const list = panel.querySelector('.artifact-list');
    if (!list) {
      return;
    }
    await loadItemArtifacts(id, list);
  }));
}

function itemRow(item = { id: '', containerName: '', containerPath: '', backupName: '', intervalMinutes: 60, maxBackups: 20 }) {
  const tr = document.createElement('article');
  tr.className = 'backup-item';
  const itemID = item.id || createLocalItemID();

  tr.innerHTML = `
    <input data-key="id" type="hidden" value="${itemID}" />

    <div class="item-grid">
      <label>Container
        <input data-key="containerName" value="${item.containerName}" placeholder="panel" />
      </label>
      <label>Container Path
        <input data-key="containerPath" value="${item.containerPath}" placeholder="/var/lib/app" />
      </label>
      <label>Backup Name (optional)
        <input data-key="backupName" value="${item.backupName || ''}" placeholder="panel_data" />
      </label>
      <label>Interval (minutes)
        <input data-key="intervalMinutes" type="number" min="1" value="${item.intervalMinutes || 60}" />
      </label>
      <label>Keep Latest Backups
        <input data-key="maxBackups" type="number" min="1" value="${item.maxBackups || 20}" />
      </label>
    </div>

    <div class="item-meta">
      <div>Next Run: <span class="status-next">--</span></div>
      <div>Last Success: <span class="status-last">--</span></div>
    </div>

    <div class="item-actions">
      <div class="run-actions">
        <button type="button" data-action="run-backup">Backup</button>
        <button type="button" data-action="run-restore">Restore</button>
      </div>
      <button type="button" data-action="remove">Remove Item</button>
    </div>

    <details class="artifact-details">
      <summary>
        <span>Stored Backups</span>
        <span class="artifact-total">Total: 0.00 MB</span>
      </summary>
      <ul class="artifact-list">
        <li class="artifact-empty">Expand to load backups...</li>
      </ul>
    </details>
  `;

  tr.querySelector('[data-action="remove"]').addEventListener('click', () => tr.remove());
  const idInput = tr.querySelector('input[data-key="id"]');
  idInput.addEventListener('input', renderScheduleIndicators);

  tr.querySelector('[data-action="run-backup"]').addEventListener('click', () => {
    runSingleAction('backup', idInput.value.trim());
  });

  tr.querySelector('[data-action="run-restore"]').addEventListener('click', () => {
    runSingleAction('restore', idInput.value.trim());
  });

  const details = tr.querySelector('.artifact-details');
  const listNode = tr.querySelector('.artifact-list');
  details.addEventListener('toggle', () => {
    if (!details.open) {
      return;
    }

    loadItemArtifacts(idInput.value.trim(), listNode);
  });

  return tr;
}

function getConfigFromForm() {
  const backups = [...itemsBody.querySelectorAll('.backup-item')].map((tr) => {
    const obj = {};
    tr.querySelectorAll('input').forEach((input) => {
      if (input.dataset.key === 'intervalMinutes') {
        obj[input.dataset.key] = Number(input.value || 60);
        return;
      }

      if (input.dataset.key === 'maxBackups') {
        obj[input.dataset.key] = Number(input.value || 20);
        return;
      }

      obj[input.dataset.key] = input.value.trim();
    });
    return obj;
  });

  return {
    ssh: {
      host: document.getElementById('host').value.trim(),
      port: Number(document.getElementById('port').value || 22),
      username: document.getElementById('username').value.trim(),
      authMethod: document.getElementById('authMethod').value,
      password: document.getElementById('password').value,
      privateKeyPath: '',
      privateKeyValue: normalizeKeyText(privateKeyValue.value),
    },
    backups,
  };
}

function setConfigToForm(cfg) {
  document.getElementById('host').value = cfg.ssh.host || '';
  document.getElementById('port').value = cfg.ssh.port || 22;
  document.getElementById('username').value = cfg.ssh.username || '';
  document.getElementById('authMethod').value = cfg.ssh.authMethod || 'password';
  document.getElementById('password').value = cfg.ssh.password || '';
  privateKeyValue.value = cfg.ssh.privateKeyValue || '';

  itemsBody.innerHTML = '';
  (cfg.backups || []).forEach((item) => {
    const normalized = {
      ...item,
      backupName: item.backupName || '',
      intervalMinutes: item.intervalMinutes || 60,
      maxBackups: item.maxBackups || 20,
    };
    itemsBody.appendChild(itemRow(normalized));
  });

  renderScheduleIndicators();
}

async function callJSON(url, method, body) {
  const res = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });

  const payload = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(payload.error || `request failed: ${res.status}`);
  }

  return payload;
}

async function loadConfig() {
  try {
    const cfg = await callJSON('/api/config', 'GET');
    setConfigToForm(cfg);
    log('Loaded config');
  } catch (err) {
    log(`Load failed: ${err.message}`);
  }
}

async function loadSchedule() {
  try {
    const payload = await callJSON('/api/schedule', 'GET');
    scheduleItems = payload.items || {};
    renderScheduleIndicators();
  } catch (err) {
    log(`Schedule status refresh failed: ${err.message}`);
  }
}

async function loadPublicKey() {
  try {
    const payload = await callJSON('/api/ssh/public-key', 'GET');
    if (!payload.hasKey) {
      publicKeyOutput.textContent = 'No SSH key configured.';
      return;
    }

    publicKeyOutput.textContent = payload.publicKey || 'Unable to derive public key.';
  } catch (err) {
    publicKeyOutput.textContent = `Public key error: ${err.message}`;
  }
}

privateKeyFile.addEventListener('change', async (event) => {
  const file = event.target.files && event.target.files[0];
  if (!file) {
    return;
  }

  try {
    const text = await file.text();
    privateKeyValue.value = normalizeKeyText(text);
    log(`Loaded private key from ${file.name}`);
  } catch (err) {
    log(`Private key upload failed: ${err.message}`);
  }
});

document.getElementById('addItem').addEventListener('click', () => {
  itemsBody.appendChild(itemRow());
  renderScheduleIndicators();
});

document.getElementById('saveConfig').addEventListener('click', async () => {
  try {
    const cfg = getConfigFromForm();
    await callJSON('/api/config', 'PUT', cfg);
    log('Saved config');
    await loadPublicKey();
    await refreshOpenArtifactPanels();
  } catch (err) {
    log(`Save failed: ${err.message}`);
  }
});

document.getElementById('copyPublicKey').addEventListener('click', async () => {
  const text = publicKeyOutput.textContent || '';
  if (!text || text === 'No SSH key configured.') {
    log('No public key to copy');
    return;
  }

  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    log('Copied public key to clipboard');
  } catch (err) {
    log(`Clipboard copy failed: ${err.message}`);
  }
});

generateKeypair.addEventListener('click', async () => {
  try {
    const payload = await callJSON('/api/ssh/generate-keypair', 'POST');
    privateKeyValue.value = normalizeKeyText(payload.privateKey || '');
    publicKeyOutput.textContent = payload.publicKey || 'Unable to generate public key.';
    document.getElementById('authMethod').value = 'key';
    log('Generated a new SSH keypair. Save config to persist it.');
  } catch (err) {
    log(`Keypair generation failed: ${err.message}`);
  }
});

document.getElementById('runBackup').addEventListener('click', async () => {
  try {
    const res = await callJSON('/api/backup', 'POST');
    log(`Backup complete: ${JSON.stringify(res.results || [])}`);
  } catch (err) {
    log(`Backup failed: ${err.message}`);
  }
});

document.getElementById('runRestore').addEventListener('click', async () => {
  try {
    await callJSON('/api/restore', 'POST');
    log('Restore complete');
  } catch (err) {
    log(`Restore failed: ${err.message}`);
  }
});

loadConfig();
loadSchedule();
loadPublicKey();
setInterval(loadSchedule, 30000);
setInterval(loadPublicKey, 30000);

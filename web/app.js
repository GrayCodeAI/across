async function load(path, key) {
  const target = document.querySelector('[data-list="' + key + '"]');
  if (!target) return;
  try {
    const response = await fetch(path, { credentials: 'same-origin' });
    if (!response.ok) throw new Error('HTTP ' + response.status);
    const data = await response.json();
    const items = Array.isArray(data[key]) ? data[key] : [];
    target.textContent = '';
    if (items.length === 0) {
      const empty = document.createElement('p');
      empty.textContent = 'No records';
      target.appendChild(empty);
      return;
    }
    items.forEach(function (item) {
      const row = document.createElement('p');
      row.textContent = JSON.stringify(item);
      target.appendChild(row);
    });
  } catch (error) {
    target.textContent = 'Unable to load ' + key + ': ' + error.message;
  }
}

load('/api/repos', 'repos');
load('/api/sessions', 'sessions');
load('/api/checkpoints', 'checkpoints');
load('/api/activity', 'activity');

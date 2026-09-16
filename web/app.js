// Across web console: stored text is untrusted — always textContent, never innerHTML.
async function load(path, key) {
  const params = new URLSearchParams(location.search);
  const token = params.get('token') || '';
  const res = await fetch(path, { headers: { Authorization: 'Bearer ' + token } });
  if (!res.ok) return;
  const data = await res.json();
  const items = data[key] || [];
  document.querySelectorAll('[data-list="' + key + '"]').forEach(function (el) {
    el.textContent = '';
    items.forEach(function (item) {
      const p = document.createElement('p');
      p.textContent = JSON.stringify(item); // inert: no HTML interpretation
      el.appendChild(p);
    });
  });
}
load('/api/repos', 'repos');
load('/api/activity', 'activity');

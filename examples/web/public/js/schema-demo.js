document.addEventListener('DOMContentLoaded', () => {
  const codeBlock = document.getElementById('schema-json');
  const historyList = document.getElementById('schema-history');
  const refreshBtn = document.getElementById('refresh-schema');

  if (!codeBlock || !historyList) {
    return;
  }

  async function refreshSchema() {
    try {
      const res = await fetch('/admin/schemas', { credentials: 'include' });
      if (!res.ok) {
        throw new Error('Failed to fetch schema');
      }
      const doc = await res.json();
      codeBlock.textContent = JSON.stringify(doc, null, 2);
    } catch (err) {
      console.error(err);
    }
  }

  async function refreshFeed() {
    try {
      const res = await fetch('/admin/schema-demo/feed', { credentials: 'include' });
      if (!res.ok) {
        throw new Error('Failed to fetch schema feed');
      }
      const events = await res.json();
      historyList.innerHTML = '';
      events.forEach((event) => {
        const item = document.createElement('li');
        item.innerHTML = `<span class="event-time">${event.generated_at}</span>
          <span class="event-desc">${(event.resource_names || []).join(', ')}</span>`;
        historyList.appendChild(item);
      });
    } catch (err) {
      console.error(err);
    }
  }

  async function refreshAll() {
    await Promise.all([refreshSchema(), refreshFeed()]);
  }

  if (refreshBtn) {
    refreshBtn.addEventListener('click', refreshAll);
  }

  refreshAll();
  setInterval(refreshAll, 15000);
});

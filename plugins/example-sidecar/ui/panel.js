(() => {
  window.__2AG__.registerTab({
    id: 'example-sidecar',
    title: 'Example',
    render(panel) {
      panel.innerHTML = '<h3>Example extension</h3><p style="color:rgba(255,255,255,.72)">This tab is loaded from the plugin manifest UI entry.</p><button type="button">Refresh plugin status</button><pre style="white-space:pre-wrap;color:rgba(255,255,255,.6)"></pre>';
      const output = panel.querySelector('pre');
      panel.querySelector('button').addEventListener('click', () => {
        window.__2AG__.ipc('core.plugins.list', {}).then((value) => { output.textContent = JSON.stringify(value, null, 2); }).catch((error) => { output.textContent = error.message; });
      });
    }
  });
})();
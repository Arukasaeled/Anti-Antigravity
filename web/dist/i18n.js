// Manager-owned copy uses explicit bindings and a dictionary. User content,
// commands, filenames, paths and raw tool results never enter this module.
(() => {
  'use strict';
  let preference = 'auto', system = 'en-US', effective = 'en-US', dictionary = {};
  const resolve = value => value === 'en-US' || value === 'zh-CN' ? value : /^zh/i.test(system) ? 'zh-CN' : 'en-US';
  function t(english, values = {}) {
    const text = effective === 'zh-CN' ? dictionary[english] ?? english : english;
    return text.replace(/\{(\w+)\}/g, (match, key) => Object.hasOwn(values, key) ? String(values[key]) : match);
  }
  function apply(root = document) {
    for (const node of root.querySelectorAll('[data-i18n]')) node.textContent = t(node.dataset.i18n);
    for (const attr of ['title', 'placeholder', 'aria-label']) {
      for (const node of root.querySelectorAll('[data-i18n-' + attr + ']')) node.setAttribute(attr, t(node.getAttribute('data-i18n-' + attr)));
    }
    document.documentElement.lang = effective;
    for (const select of document.querySelectorAll('[data-ui-language]')) select.value = preference;
  }
  function set(value) {
    if (!['auto', 'zh-CN', 'en-US'].includes(value)) return;
    const changed = preference !== value || effective !== resolve(value);
    preference = value; effective = resolve(value); apply();
    if (changed) window.dispatchEvent(new CustomEvent('2ag:locale', { detail: { preference, effective, system } }));
  }
  const ready = Promise.all([
    fetch('/locales.zh-CN.json').then(r => { if (!r.ok) throw Error('Language dictionary unavailable'); return r.json(); }),
    fetch('/api/v1/locale', { headers: { 'X-2Ag-Control-Token': window.__2AG_CONTROL_TOKEN || '' } }).then(r => { if (!r.ok) throw Error('Locale unavailable'); return r.json(); })
  ]).then(([labels, locale]) => { dictionary = labels; system = locale.system; effective = ''; set(locale.preference); })
    .catch(error => { console.error(error); apply(); });
  window.t = t;
  const additions = {};
  function nativeText(value) {
    const source=String(value??'');
    if(effective==='en-US')for(const [english,chinese] of Object.entries(dictionary))if(chinese===source)return english;
    return source;
  }
  function register(labels) { Object.assign(additions, labels); Object.assign(dictionary, labels); }
  ready.then(() => Object.assign(dictionary, additions));
  window.TwoAgI18n = { t, apply, set, ready, register, nativeText, get locale() { return effective; }, get preference() { return preference; }, get system() { return system; } };
})();

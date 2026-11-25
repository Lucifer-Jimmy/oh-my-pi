'use strict';

const MAX_DIFF_ROWS = 8;

const state = {
  items: [],
  itemEls: [],
  running: false,
  usage: { input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
  context: 0,
  window: 0,
  todos: [],
  asks: [],
  model: '',
  mode: '',
  cwd: '',
  connected: false,
};

const el = {};
for (const id of ['transcript', 'dock', 'todos', 'input', 'composer', 'sendBtn', 'stopBtn', 'hint', 'statusLeft',
  'statusRight', 'model', 'mode', 'conn', 'toast', 'sessionsDialog', 'sessionList', 'newSessionBtn',
  'modelDialog', 'modelInput', 'modelForm']) {
  el[id] = document.getElementById(id);
}

function h(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined && text !== null) node.textContent = text;
  return node;
}

function toast(text) {
  el.toast.textContent = text;
  el.toast.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => { el.toast.hidden = true; }, 6000);
}

async function send(type, extra) {
  const res = await fetch('/api/command', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(Object.assign({ type }, extra || {})),
  });
  let body = {};
  try { body = await res.json(); } catch (e) { body = {}; }
  if (!res.ok || body.ok === false) throw new Error(body.error || ('HTTP ' + res.status));
  return body.result;
}

function connect() {
  const es = new EventSource('/api/events');
  es.onopen = () => { state.connected = true; renderStatus(); };
  es.onerror = () => { state.connected = false; renderStatus(); };
  es.onmessage = (msg) => {
    let f;
    try { f = JSON.parse(msg.data); } catch (e) { return; }
    onFrame(f);
  };
}

function onFrame(f) {
  if (f.v !== 1) return;
  if (f.type === 'state') applyState(f.state);
  else if (f.type === 'ask') upsertAsk(f.ask);
  else if (f.type === 'error') toast(f.error);
}

function applyState(st) {
  if (!st) return;
  let from = st.from;
  if (typeof from !== 'number' || from > state.items.length || from < 0) from = 0;
  if (from === 0 && (!st.items || st.items.length === 0) && state.items.length > 0 && st.running === false && st.reason === '' && !state.running) {
    from = 0;
  }
  state.items = state.items.slice(0, from).concat(st.items || []);
  renderTranscript(from);
  state.running = !!st.running;
  if (st.usage) state.usage = st.usage;
  state.context = st.context || 0;
  state.window = st.window || 0;
  state.todos = st.todos || [];
  state.asks = st.asks || [];
  if (st.model) state.model = st.model;
  if (st.mode) state.mode = st.mode;
  if (st.cwd) state.cwd = st.cwd;
  renderDock();
  renderHeader();
  renderStatus();
  autoscroll();
}

function upsertAsk(ask) {
  if (!ask || !ask.id) return;
  const at = state.asks.findIndex((a) => a.id === ask.id);
  if (at >= 0) state.asks[at] = ask;
  else state.asks = state.asks.concat([ask]);
  renderDock();
}

function renderTranscript(from) {
  while (state.itemEls.length > from) {
    const node = state.itemEls.pop();
    if (node && node.parentNode) node.parentNode.removeChild(node);
  }
  for (let i = from; i < state.items.length; i++) {
    let node = state.itemEls[i];
    if (!node) {
      node = document.createElement('article');
      fillItem(node, state.items[i]);
      state.itemEls[i] = node;
      el.transcript.append(node);
    } else {
      updateItem(node, state.items[i]);
    }
  }
}

function sig(it) {
  if (it.kind === 'tool' && it.tool) {
    const t = it.tool;
    const p = t.progress ? [t.progress.message || '', t.progress.tokens || 0].join('|') : '';
    return [t.status, t.duration_ms || 0, (t.output || '').length, p, it.done ? 1 : 0].join('~');
  }
  return [(it.text || '').length, (it.html || '').length, it.done ? 1 : 0, it.ended || 0, it.level || ''].join('~');
}

function updateItem(node, it) {
  const next = sig(it);
  if (node.dataset.sig === next) return;
  fillItem(node, it);
}

function fillItem(node, it) {
  node.dataset.sig = sig(it);
  node.className = ['item', it.kind, it.level || '', it.tool ? 'status-' + it.tool.status : ''].filter(Boolean).join(' ');
  node.replaceChildren.apply(node, itemNodes(it));
}

function itemNodes(it) {
  if (it.kind === 'user') return [h('span', 'who', '›'), h('div', 'body user-text', it.text)];
  if (it.kind === 'text') {
    const body = h('div', 'md');
    body.innerHTML = it.html || '';
    return [body];
  }
  if (it.kind === 'thinking') {
    const body = document.createElement('details');
    body.className = 'thinking';
    const summary = document.createElement('summary');
    const ms = Math.max(0, (it.ended || Date.now()) - (it.started || 0));
    summary.textContent = it.done && ms >= 1000 ? 'Thought for ' + dur(ms) : 'Thinking…';
    body.append(summary, h('pre', 'thought', it.text || ''));
    return [body];
  }
  if (it.kind === 'notice') {
    const glyph = it.level === 'error' ? '✗' : it.level === 'warn' ? '⚠' : '·';
    return [h('span', 'glyph', glyph), h('div', 'body notice-text', it.text)];
  }
  if (it.kind === 'tool' && it.tool) return toolNodes(it);
  return [];
}

function toolNodes(it) {
  const t = it.tool;
  const head = h('div', 'tool-head');
  head.append(h('span', 'glyph', t.status === 'failed' ? '●' : t.status === 'cancelled' ? '●' : t.status === 'running' ? '◐' : '●'));
  head.append(h('span', 'tool-name', displayName(t.name)));
  const target = toolTarget(t.name, inputOf(t));
  if (target) head.append(h('span', 'tool-target', target));
  const elapsed = h('span', 'elapsed');
  elapsed.dataset.started = it.started || 0;
  elapsed.dataset.done = it.done ? 1 : 0;
  elapsed.dataset.duration = t.duration_ms || 0;
  elapsed.textContent = elapsedText(elapsed.dataset);
  head.append(elapsed);
  const nodes = [head];

  if (t.status === 'running' && t.progress) nodes.push(h('div', 'progress', progressLine(t.progress)));
  if (t.status === 'cancelled') nodes.push(h('div', 'tool-note', 'Cancelled'));

  const input = inputOf(t);
  const diff = diffLines(t.name, input);
  if (diff.length) nodes.push(diffBox(diff));
  const body = toolBody(t);
  if (body) nodes.push(body);
  return nodes;
}

function toolBody(t) {
  const out = String(t.output || '');
  if (t.status === 'running') return null;
  if (t.status === 'failed') return h('pre', 'output error', out || '(no output)');
  if (t.name === 'read' || t.name === 'glob' || t.name === 'grep') return h('div', 'tool-note', summarize(t.name, out));
  if (t.name === 'edit' || t.name === 'write') {
    const line = firstLine(out);
    return line ? h('div', 'tool-note', line) : null;
  }
  if (!out.trim()) return null;
  return h('pre', 'output', out);
}

function inputOf(t) {
  const raw = t.input;
  if (!raw) return {};
  if (typeof raw === 'string') {
    try { return JSON.parse(raw); } catch (e) { return {}; }
  }
  return raw;
}

function diffLines(name, input) {
  if (!input) return [];
  const rows = [];
  const push = (sign, text) => {
    if (text === undefined || text === null || text === '') return;
    for (const line of String(text).split('\n')) {
      rows.push({ sign, text: line });
    }
  };
  if (name === 'edit') {
    push('-', input.old_string);
    push('+', input.new_string);
  } else if (name === 'write') {
    push('+', input.content);
  }
  if (rows.length <= MAX_DIFF_ROWS) return rows;
  const kept = rows.slice(0, MAX_DIFF_ROWS);
  kept.push({ sign: '…', text: '+' + (rows.length - MAX_DIFF_ROWS) + ' lines' });
  return kept;
}

function diffBox(rows) {
  const box = h('div', 'diff');
  for (const row of rows) {
    const line = h('div', 'diff-row ' + (row.sign === '+' ? 'add' : row.sign === '-' ? 'del' : 'more'));
    line.append(h('span', 'sign', row.sign), h('span', 'text', row.text));
    box.append(line);
  }
  return box;
}

function askNodes(ask) {
  const frag = document.createDocumentFragment();
  const head = h('div', 'ask-head');
  head.append(h('span', 'bar'), h('strong', null, ask.title || 'Approval'));
  if (state.asks.length > 1) head.append(h('span', 'count', '(1 of ' + state.asks.length + ')'));
  frag.append(head);

  const body = h('div', 'ask-body');
  if (ask.html) {
    const md = h('div', 'md ask-message');
    md.innerHTML = ask.html;
    body.append(md);
  } else if (ask.message) {
    body.append(h('div', 'ask-message', ask.message));
  }
  if (ask.tool && ask.tool !== 'exit_plan_mode') body.append(askTool(ask, ask.input));
  body.append(askOptions(ask));
  frag.append(body);
  return frag;
}

function askTool(ask, raw) {
  const box = h('div', 'ask-tool');
  const input = raw && typeof raw === 'string' ? parseJSON(raw) : (raw || null);
  if (!input) return box;
  const diff = diffLines(ask.tool, input);
  if (diff.length) {
    box.append(diffBox(diff));
    return box;
  }
  if (ask.tool === 'bash' && input.command) {
    box.append(h('pre', 'cmd', input.command));
    return box;
  }
  box.append(h('pre', 'json', previewJSON(input)));
  return box;
}

function parseJSON(raw) {
  try { return JSON.parse(raw); } catch (e) { return null; }
}

function previewJSON(value) {
  let text = '';
  try { text = JSON.stringify(value, null, 2); } catch (e) { text = String(value); }
  return text.length > 2000 ? text.slice(0, 2000) + '\n… truncated' : text;
}

function askOptions(ask) {
  const row = h('div', 'ask-options');
  if (ask.method === 'select') {
    (ask.options || []).forEach((option, index) => {
      const b = h('button', index === 0 ? 'primary' : null, option);
      b.type = 'button';
      b.addEventListener('click', () => answer(ask, { value: option }));
      row.append(b);
    });
    row.append(cancelButton(ask));
    return row;
  }
  if (ask.method === 'input') {
    const form = document.createElement('form');
    form.className = 'ask-input';
    const input = document.createElement('input');
    input.placeholder = ask.placeholder || '';
    input.autocomplete = 'off';
    input.spellcheck = false;
    const b = h('button', 'primary', 'Answer');
    b.type = 'submit';
    form.append(input, b);
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      answer(ask, { value: input.value });
    });
    row.append(form, cancelButton(ask));
    return row;
  }
  const yes = h('button', 'primary', 'Yes');
  yes.type = 'button';
  yes.addEventListener('click', () => answer(ask, { confirmed: true }));
  const no = h('button', null, 'No');
  no.type = 'button';
  no.addEventListener('click', () => answer(ask, {}));
  row.append(yes, no, cancelButton(ask));
  return row;
}

function cancelButton(ask) {
  const b = h('button', 'ghost', 'cancel');
  b.type = 'button';
  b.addEventListener('click', () => answer(ask, { cancelled: true }));
  return b;
}

async function answer(ask, extra) {
  try {
    await send('answer', {
      id: ask.id,
      value: extra.value || '',
      confirmed: !!extra.confirmed,
      cancelled: !!extra.cancelled,
    });
    state.asks = state.asks.filter((a) => a.id !== ask.id);
    renderDock();
  } catch (e) {
    toast(e.message);
  }
}

function renderDock() {
  if (!state.asks.length) {
    el.dock.hidden = true;
    el.dock.replaceChildren();
    return;
  }
  el.dock.hidden = false;
  el.dock.replaceChildren(askNodes(state.asks[0]));
}

function displayName(name) {
  if (!name) return '';
  const spaced = String(name).replace(/_/g, ' ');
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function toolTarget(name, input) {
  if (!input) return '';
  const str = (key) => (typeof input[key] === 'string' ? input[key] : '');
  let out = '';
  if (name === 'bash') {
    const command = str('command').trim();
    out = command.split('\n')[0] + (command.includes('\n') ? ' …' : '');
  } else if (name === 'read') {
    out = str('path');
    if (input.offset) out += ':' + input.offset;
  } else if (name === 'edit' || name === 'write') {
    out = str('path');
  } else if (name === 'glob' || name === 'grep') {
    out = str('pattern');
    if (str('path')) out += ' in ' + str('path');
  } else if (name === 'task') {
    out = str('description') || str('prompt');
  } else if (name === 'ask') {
    out = str('question');
  } else if (name === 'skill') {
    out = str('name');
  } else if (name === 'todo') {
    out = '';
  } else {
    for (const key of ['path', 'url', 'pattern', 'command', 'description', 'question', 'name']) {
      if (str(key)) { out = str(key); break; }
    }
  }
  out = out || '';
  return out.length > 80 ? out.slice(0, 80) + '…' : out;
}

function progressLine(p) {
  if (!p) return '';
  let line = p.message || '';
  if (p.name) {
    line = displayName(p.name);
    const target = toolTarget(p.name, inputOf({ input: p.input }));
    if (target) line += ' (' + target + ')';
  }
  if (p.tokens) line += (line ? ' · ' : '') + fmtTokens(p.tokens) + ' tokens';
  return line;
}

function summarize(name, out) {
  const lines = String(out || '').split('\n').filter((line) => line.trim() !== '');
  if (!lines.length) return '(no output)';
  if (lines.length === 1) return lines[0];
  if (name === 'read') return 'Read ' + lines.length + ' lines';
  if (name === 'glob') return 'Found ' + lines.length + ' files';
  return 'Found ' + lines.length + ' lines';
}

function firstLine(text) {
  return String(text || '').trim().split('\n')[0] || '';
}

function dur(ms) {
  const s = ms / 1000;
  if (s < 60) return s.toFixed(1) + 's';
  const total = Math.round(s);
  return Math.floor(total / 60) + 'm' + String(total % 60).padStart(2, '0') + 's';
}

function elapsedText(dataset) {
  const started = Number(dataset.started || 0);
  const done = dataset.done === '1';
  const ms = done ? Number(dataset.duration || 0) : (started ? Date.now() - started : 0);
  if (!done && ms < 1000) return '';
  if (done && ms < 1000) return '';
  return ' · ' + dur(ms);
}

function fmtTokens(n) {
  n = Number(n || 0);
  if (n < 1000) return String(n);
  if (n < 1000000) return (n / 1000).toFixed(1) + 'k';
  return (n / 1000000).toFixed(1) + 'M';
}

function renderHeader() {
  el.model.textContent = state.model || '';
  if (!state.mode) return;
  if (!Array.from(el.mode.options).some((o) => o.value === state.mode)) {
    const option = document.createElement('option');
    option.value = state.mode;
    option.textContent = state.mode;
    el.mode.append(option);
  }
  el.mode.value = state.mode;
}

function renderStatus() {
  const u = state.usage || {};
  const input = (u.input_tokens || 0) + (u.cache_read_tokens || 0) + (u.cache_write_tokens || 0);
  const output = u.output_tokens || 0;
  const ctx = state.window > 0
    ? 'ctx ' + (state.context * 100 / state.window).toFixed(1) + '%/' + fmtTokens(state.window)
    : 'ctx ' + fmtTokens(state.context);
  el.statusLeft.textContent = [state.mode, ctx, '↑' + fmtTokens(input) + ' ↓' + fmtTokens(output)].filter(Boolean).join(' · ');
  el.statusRight.textContent = state.connected ? (state.running ? 'running' : 'idle') : 'reconnecting…';
  el.conn.className = 'conn ' + (state.connected ? (state.running ? 'live' : 'ok') : 'off');
  el.sendBtn.textContent = state.running ? 'Steer' : 'Send';
  el.stopBtn.hidden = !state.running;
  el.hint.textContent = state.running ? 'Enter steers the running turn' : '';
  el.input.placeholder = state.running
    ? 'Steer the running turn · Enter sends'
    : 'Ask anything · Enter sends · Shift+Enter for a new line';
  renderTodos();
}

function renderTodos() {
  const items = state.todos || [];
  if (!items.length) {
    el.todos.hidden = true;
    return;
  }
  el.todos.hidden = false;
  let done = 0;
  let current = '';
  for (const item of items) {
    if (item.status === 'completed') done++;
    else if (item.status === 'in_progress' && !current) current = item.content;
  }
  if (!current) {
    const pending = items.find((item) => item.status !== 'completed');
    current = pending ? pending.content : '';
  }
  el.todos.textContent = done === items.length
    ? '✓ all ' + items.length + ' todos done'
    : '● ' + current + '  (' + done + '/' + items.length + ')';
}

function autoscroll() {
  const box = el.transcript;
  if (box.scrollHeight - box.scrollTop - box.clientHeight < 160) box.scrollTop = box.scrollHeight;
}

function resize() {
  el.input.style.height = 'auto';
  el.input.style.height = Math.min(el.input.scrollHeight, 240) + 'px';
}

async function submit() {
  const text = el.input.value.trim();
  if (!text) return;
  try {
    await send(state.running ? 'steer' : 'prompt', { text });
    el.input.value = '';
    resize();
  } catch (e) {
    toast(e.message);
  }
}

async function openSessions() {
  el.sessionList.replaceChildren(h('li', 'muted', 'loading…'));
  el.sessionsDialog.showModal();
  try {
    const infos = (await send('list_sessions')) || [];
    el.sessionList.replaceChildren();
    if (!infos.length) {
      el.sessionList.append(h('li', 'muted', 'No earlier sessions in this directory'));
      return;
    }
    for (const info of infos.slice(0, 50)) {
      const li = document.createElement('li');
      const b = h('button', 'session');
      b.type = 'button';
      b.append(h('span', 'sid', info.id));
      const meta = [info.modified ? String(info.modified).slice(0, 16).replace('T', ' ') : '', info.messages + ' msg']
        .filter(Boolean).join(' · ');
      b.append(h('span', 'meta', meta));
      if (info.title) b.append(h('span', 'title', info.title));
      b.addEventListener('click', async () => {
        try {
          await send('resume', { text: info.id });
          el.sessionsDialog.close();
        } catch (e) {
          toast(e.message);
        }
      });
      li.append(b);
      el.sessionList.append(li);
    }
  } catch (e) {
    el.sessionList.replaceChildren(h('li', 'muted', e.message));
  }
}

el.composer.addEventListener('submit', (e) => {
  e.preventDefault();
  submit();
});

el.input.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    submit();
  }
});

el.input.addEventListener('input', resize);

el.stopBtn.addEventListener('click', async () => {
  try { await send('abort'); } catch (e) { toast(e.message); }
});

el.compactBtn.addEventListener('click', async () => {
  try { await send('compact', { text: '' }); } catch (e) { toast(e.message); }
});

el.mode.addEventListener('change', async () => {
  try {
    await send('set_mode', { mode: el.mode.value });
  } catch (e) {
    toast(e.message);
  }
});

el.sessionsBtn.addEventListener('click', openSessions);

el.newSessionBtn.addEventListener('click', async () => {
  try {
    await send('new_session');
    el.sessionsDialog.close();
  } catch (e) {
    toast(e.message);
  }
});

el.modelBtn.addEventListener('click', () => {
  el.modelInput.value = state.model || '';
  el.modelDialog.showModal();
  el.modelInput.focus();
  el.modelInput.select();
});

el.modelForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const text = el.modelInput.value.trim();
  if (!text) return;
  try {
    await send('set_model', { text });
    el.modelDialog.close();
  } catch (err) {
    toast(err.message);
  }
});

for (const button of document.querySelectorAll('[data-close]')) {
  button.addEventListener('click', () => {
    const dialog = button.closest('dialog');
    if (dialog) dialog.close();
  });
}

document.addEventListener('keydown', (e) => {
  if (el.sessionsDialog.open || el.modelDialog.open) return;
  if (!state.asks.length) return;
  const ask = state.asks[0];
  const typing = e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA');
  if (ask.method === 'input') {
    if (e.key === 'Escape' && e.target !== el.input) {
      e.preventDefault();
      answer(ask, { cancelled: true });
    }
    return;
  }
  if (typing) return;
  if (e.key === 'Escape') {
    e.preventDefault();
    answer(ask, { cancelled: true });
    return;
  }
  if (e.key === 'y' || e.key === 'Y') {
    const option = (ask.options || []).find((o) => /^yes/i.test(o));
    if (ask.method === 'confirm') {
      e.preventDefault();
      answer(ask, { confirmed: true });
    } else if (option) {
      e.preventDefault();
      answer(ask, { value: option });
    }
    return;
  }
  if (e.key === 'n' || e.key === 'N') {
    const option = (ask.options || []).find((o) => /^no/i.test(o));
    if (ask.method === 'confirm') {
      e.preventDefault();
      answer(ask, {});
    } else if (option) {
      e.preventDefault();
      answer(ask, { value: option });
    }
    return;
  }
  if (/^[1-9]$/.test(e.key)) {
    const index = Number(e.key) - 1;
    const options = ask.options || [];
    if (index < options.length) {
      e.preventDefault();
      answer(ask, { value: options[index] });
    }
  }
});

setInterval(() => {
  for (const node of document.querySelectorAll('.item.tool .elapsed')) {
    node.textContent = elapsedText(node.dataset);
  }
}, 500);

connect();
renderStatus();
resize();
el.input.focus();

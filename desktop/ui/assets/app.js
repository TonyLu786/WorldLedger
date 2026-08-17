'use strict';

// The token arrives in the address and is taken out of it immediately, so it
// does not end up in history or in a link somebody copies. It lives in a
// variable for the life of the page and is never written to storage: a session
// token that outlives its session is just a password nobody chose.
const token = new URLSearchParams(location.search).get('token') || '';
if (location.search) {
  history.replaceState(null, '', location.pathname);
}

async function call(path, options) {
  const settings = Object.assign({ headers: {} }, options);
  settings.headers['X-WorldLedger-Token'] = token;
  if (settings.body) settings.headers['Content-Type'] = 'application/json';
  const response = await fetch(path, settings);
  const body = await response.json().catch(() => null);
  if (!response.ok || (body && body.problem)) {
    const failure = new Error((body && body.problem) || ('the application answered ' + response.status));
    failure.next = body && body.next;
    throw failure;
  }
  return body;
}

// Every string that reaches the page is assigned as text. All of it comes from
// the machine being read -- folder names, server names somebody typed -- and
// none of it has any business being parsed as markup.
function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function banner(kind, title, detail) {
  const node = el('div', 'banner ' + kind);
  node.append(el('strong', null, title));
  if (detail) node.append(el('span', null, detail));
  return node;
}

function problem(host, err) {
  host.replaceChildren(banner('todo', err.message, err.next || ''));
}

function bytes(n) {
  if (n < 1024) return n + ' B';
  const units = ['KB', 'MB', 'GB'];
  let value = n / 1024;
  for (const unit of units) {
    if (value < 1024 || unit === 'GB') return value.toFixed(value < 10 ? 1 : 0) + ' ' + unit;
    value /= 1024;
  }
}

// Navigation ---------------------------------------------------------------

const steps = Array.from(document.querySelectorAll('.step'));
const screens = {
  setup: refreshSetup,
  capture: refreshCapture,
  import: refreshImport,
  declare: refreshDeclare,
  world: refreshWorld,
  travel: refreshTravel,
};

function show(name) {
  for (const step of steps) {
    const chosen = step.dataset.screen === name;
    step.setAttribute('aria-current', chosen ? 'true' : 'false');
    document.getElementById('screen-' + step.dataset.screen).hidden = !chosen;
  }
  const load = screens[name];
  if (load) load();
}

for (const step of steps) {
  step.addEventListener('click', () => show(step.dataset.screen));
}

// The rail marks what is done, so somebody can see where they are without
// reading anything. It is driven by the same next-step the server works out,
// rather than by the page keeping its own idea of progress.
const order = ['setup', 'capture', 'import', 'declare', 'world', 'travel'];
function markProgress(next) {
  const reached = { install: 0, play: 1, import: 2, declare: 3, export: 4 }[next];
  steps.forEach((step, index) => {
    step.classList.toggle('is-done', reached !== undefined && index < reached);
    step.classList.toggle('is-next', index === reached);
  });
}

// Setup --------------------------------------------------------------------

const marks = { ok: '✓', missing: '!', wrong: '×', unknown: '?' };

function renderChecks(report) {
  const host = document.getElementById('checks');
  host.replaceChildren();

  if (report.ready) {
    host.append(banner('good', 'Ready to record',
      'Start Minecraft, join a server, and play. Nothing else is needed.'));
  } else {
    const outstanding = report.checks.filter((c) => c.state !== 'ok');
    host.append(banner('todo',
      outstanding.length === 1 ? 'One thing left' : outstanding.length + ' things left',
      outstanding.length ? outstanding[0].detail : ''));
  }

  for (const check of report.checks) {
    const row = el('div', 'check is-' + check.state);
    row.append(el('div', 'check-dot', marks[check.state] || '?'));
    const middle = el('div');
    middle.append(el('div', 'check-title', check.title));
    middle.append(el('div', 'check-detail', check.detail));
    row.append(middle);
    if (check.fix) {
      const button = el('button', 'fix', check.fix);
      button.disabled = true;
      button.title = 'Not available yet';
      row.append(button);
    } else {
      row.append(el('div'));
    }
    host.append(row);
  }
}

async function refreshSetup() {
  const host = document.getElementById('checks');
  try {
    renderChecks(await call('/api/health'));
  } catch (err) {
    problem(host, err);
  }
}
document.getElementById('recheck').addEventListener('click', refreshSetup);

// Status, shared by the play and import screens -----------------------------

let lastStatus = null;

async function loadStatus() {
  lastStatus = await call('/api/status');
  markProgress(lastStatus.next);
  return lastStatus;
}

function renderCapture(status) {
  const host = document.getElementById('capture-body');
  host.replaceChildren();

  if (!status.spool) {
    host.append(banner('todo', 'Nothing is recording yet',
      'The mod has not run. Finish Set up first, then play once.'));
    return;
  }
  const waiting = status.spool.ready;
  host.append(waiting > 0
    ? banner('good', waiting + (waiting === 1 ? ' recording waiting' : ' recordings waiting'),
      'Go to Bring it in to add them to your archive.')
    : banner('todo', 'Nothing new since last time',
      'Join a server and play. Recordings appear here when you quit the game.'));

  const facts = el('div', 'facts');
  addFact(facts, 'Waiting to be brought in', String(status.spool.ready));
  addFact(facts, 'Already brought in and kept', String(status.spool.imported));
  if (status.spool.in_progress > 0) {
    addFact(facts, 'Still being written', String(status.spool.in_progress) + ' (Minecraft is running)');
  }
  if (status.spool.quarantined > 0) {
    addFact(facts, 'Set aside as unreadable', String(status.spool.quarantined));
  }
  addFact(facts, 'Recordings folder', status.spool.dir);
  host.append(facts);
}

// The one sentence for what to do next, taken from the step the server worked
// out rather than from the page guessing.
function nextSentence(next) {
  return {
    install: 'Finish Set up first.',
    play: 'Join a server and play; recordings appear when you quit the game.',
    import: 'Bring your recordings in.',
    declare: 'Next: go to Decide and say what may be shared.',
    export: 'Next: go to Make a world.',
  }[next] || '';
}

function addFact(host, label, value) {
  const row = el('div', 'fact');
  row.append(el('span', 'fact-label', label));
  row.append(el('span', 'fact-value', value));
  host.append(row);
}

async function refreshCapture() {
  const host = document.getElementById('capture-body');
  try {
    renderCapture(await loadStatus());
  } catch (err) {
    problem(host, err);
  }
}
document.getElementById('capture-refresh').addEventListener('click', refreshCapture);

// Import -------------------------------------------------------------------

function renderImport(status) {
  const host = document.getElementById('import-body');
  host.replaceChildren();
  const button = document.getElementById('import-run');

  const waiting = status.spool ? status.spool.ready : 0;
  button.disabled = waiting === 0;
  button.textContent = waiting > 0 ? 'Bring in ' + waiting : 'Nothing to bring in';

  // "Nothing waiting" is true in two quite different situations, and telling
  // somebody who has just brought in forty recordings to go and play first
  // reads as an application that did not notice.
  if (waiting > 0) {
    host.append(banner('good', waiting + ' waiting',
      'This adds them to your archive and leaves your recordings alone.'));
  } else if (status.observations > 0) {
    host.append(banner('good', 'Everything has been brought in',
      nextSentence(status.next)));
  } else {
    host.append(banner('todo', 'Nothing waiting',
      'Play on a server first; recordings appear when you quit the game.'));
  }

  const facts = el('div', 'facts');
  addFact(facts, 'In your archive', String(status.observations) + ' recordings');
  addFact(facts, 'Space used', bytes(status.object_bytes));
  addFact(facts, 'Archive folder', status.archive_dir);
  host.append(facts);
}

async function refreshImport() {
  const host = document.getElementById('import-body');
  try {
    renderImport(await loadStatus());
  } catch (err) {
    problem(host, err);
  }
}

document.getElementById('import-run').addEventListener('click', async () => {
  const host = document.getElementById('import-body');
  const button = document.getElementById('import-run');
  button.disabled = true;
  button.textContent = 'Bringing it in…';
  try {
    const result = await call('/api/import', { method: 'POST' });
    const status = await loadStatus();
    renderImport(status);
    host.prepend(banner('good',
      'Brought in ' + result.imported + ' of ' + result.total,
      'Your recordings are still in the recordings folder. Next: decide what may be shared.'));
    if (result.failed && result.failed.length) {
      const list = el('div', 'facts');
      for (const failure of result.failed) addFact(list, 'Left alone', failure);
      host.append(list);
    }
  } catch (err) {
    problem(host, err);
  }
});

// Decide -------------------------------------------------------------------

async function refreshDeclare() {
  const host = document.getElementById('declare-body');
  try {
    const [status, choices] = [await loadStatus(), await call('/api/choices')];
    host.replaceChildren();

    if (!status.servers.length) {
      host.append(banner('todo', 'Nothing to decide about yet',
        'Bring in some recordings first.'));
      return;
    }

    for (const server of status.servers) {
      const card = el('section', 'card');
      const head = el('div', 'card-head');
      head.append(el('h2', null, server.id));
      head.append(el('span', 'card-note', server.chunks + ' places recorded'));
      card.append(head);

      if (server.declared) {
        card.append(banner('good', 'Decided: ' + server.disposition,
          'You can change this by choosing again.'));
      }

      const form = el('form', 'choices');
      for (const choice of choices) {
        const label = el('label', 'choice');
        const input = el('input');
        input.type = 'radio';
        input.name = 'disposition-' + server.id;
        input.value = choice.value;
        input.dataset.needsExpiry = choice.needs_expiry ? 'yes' : '';
        label.append(input);
        const text = el('span');
        text.append(el('strong', null, choice.title));
        text.append(el('span', 'choice-meaning', choice.meaning));
        label.append(text);
        form.append(label);
      }

      const until = el('input', 'until');
      until.type = 'date';
      until.hidden = true;
      until.setAttribute('aria-label', 'Held back until');
      form.addEventListener('change', () => {
        const picked = form.querySelector('input[type=radio]:checked');
        until.hidden = !(picked && picked.dataset.needsExpiry);
      });
      form.append(until);

      const name = el('input', 'name');
      name.type = 'text';
      name.placeholder = 'Your name';
      name.value = declarerName(status);
      name.setAttribute('aria-label', 'Who is deciding');
      form.append(name);

      const submit = el('button', 'primary', 'Decide');
      submit.type = 'submit';
      form.append(submit);

      form.addEventListener('submit', async (event) => {
        event.preventDefault();
        const picked = form.querySelector('input[type=radio]:checked');
        if (!picked) {
          card.append(banner('todo', 'Pick one of the choices', ''));
          return;
        }
        submit.disabled = true;
        try {
          await call('/api/declare', {
            method: 'POST',
            body: JSON.stringify({
              server: server.id,
              disposition: picked.value,
              declared_by: name.value.trim(),
              until: until.hidden ? '' : until.value,
            }),
          });
          await refreshDeclare();
        } catch (err) {
          submit.disabled = false;
          card.append(banner('todo', err.message, err.next || ''));
        }
      });

      card.append(form);
      host.append(card);
    }
  } catch (err) {
    problem(host, err);
  }
}

// The contributor name is what they already record under, so it is the sensible
// default for who is deciding. It stays editable: the person deciding is not
// always the person who played.
function declarerName(status) {
  return status.declared_by || '';
}

// Make a world -------------------------------------------------------------

async function refreshWorld() {
  const host = document.getElementById('world-body');
  try {
    const status = await loadStatus();
    host.replaceChildren();

    const ready = status.servers.filter((s) => s.declared);
    if (!ready.length) {
      host.append(banner('todo', 'Nothing can be made into a world yet',
        status.servers.length ? 'Go to Decide first.' : 'Bring in some recordings first.'));
      return;
    }

    const answer = await call('/api/worlds');
    if (!answer.worlds.length) {
      const box = banner('todo', 'No Minecraft world to write into', '');
      host.append(box);
      const list = el('ol', 'howto');
      for (const line of answer.how_to_make || []) list.append(el('li', null, line));
      host.append(list);
      const again = el('button', 'primary', 'I have made one');
      again.addEventListener('click', refreshWorld);
      host.append(again);
      return;
    }

    host.append(banner('good', 'Pick a world to write into',
      'The newest is first. Choose one you made for this, not one you have played in.'));

    for (const world of answer.worlds) {
      const row = el('div', 'check');
      row.append(el('div', 'check-dot', world.sizeable ? '!' : '✓'));
      const middle = el('div');
      middle.append(el('div', 'check-title', world.name));
      middle.append(el('div', 'check-detail',
        bytes(world.bytes) + (world.sizeable ? ' — this looks like a world you have played in' : ' — looks freshly made')));
      row.append(middle);

      const button = el('button', 'fix', 'Write into this');
      button.addEventListener('click', async () => {
        if (world.sizeable && !confirm(
          'This world is ' + bytes(world.bytes) + ', which usually means you have played in it.\n\n' +
          'Your recordings will be written over what is there. Carry on?')) {
          return;
        }
        button.disabled = true;
        button.textContent = 'Writing…';
        try {
          const result = await call('/api/export', {
            method: 'POST',
            body: JSON.stringify({ server: ready[0].id, world_dir: world.path }),
          });
          host.replaceChildren(banner('good',
            'Wrote ' + result.chunks + ' places into ' + world.name,
            'Open Minecraft and play that world. Anything nobody saw is left as the empty world made it.'));
        } catch (err) {
          button.disabled = false;
          button.textContent = 'Write into this';
          host.prepend(banner('todo', err.message, err.next || ''));
        }
      });
      row.append(button);
      host.append(row);
    }
  } catch (err) {
    problem(host, err);
  }
}

// Time travel --------------------------------------------------------------

const travelColours = {
  changed: '#c96a1f',
  unchanged: '#3f8f63',
  'first-seen': '#2f6fbf',
  'not-revisited': '#8e959c',
  'never-seen': '#d5d9dd',
};

async function refreshTravel() {
  const host = document.getElementById('travel-body');
  try {
    const status = await loadStatus();
    host.replaceChildren();
    if (!status.servers.length) {
      host.append(banner('todo', 'Nothing to look at yet', 'Bring in some recordings first.'));
      return;
    }

    const server = status.servers[0].id;
    const moments = (await call('/api/moments?server=' + encodeURIComponent(server))).moments;
    if (moments.length < 1) {
      host.append(banner('todo', 'Nothing recorded for ' + server, 'Play and bring in some recordings.'));
      return;
    }
    // Comparing a moment with itself is a real answer and a useless one. Saying
    // why there is nothing to compare beats showing a map of one colour and
    // leaving somebody to work out that it means "come back later".
    if (moments.length < 2) {
      host.append(banner('todo', 'Only one session so far',
        'Time travel compares two moments. Play again another day, bring those recordings in, ' +
        'and this will show what changed in between.'));
      const facts = el('div', 'facts');
      addFact(facts, moments[0].label, moments[0].chunks + ' places recorded');
      host.append(facts);
      return;
    }

    const picker = el('div', 'picker');
    const from = el('select');
    const to = el('select');
    for (const moment of moments) {
      for (const select of [from, to]) {
        const option = el('option', null, moment.label + ' (' + moment.chunks + ' places)');
        option.value = moment.at;
        select.append(option);
      }
    }
    from.value = moments[0].at;
    to.value = moments[moments.length - 1].at;
    picker.append(labelled('From', from), labelled('To', to));

    const go = el('button', 'primary', 'Compare');
    picker.append(go);
    host.append(picker);

    const result = el('div');
    host.append(result);

    go.addEventListener('click', async () => {
      go.disabled = true;
      try {
        const diff = await call('/api/travel?server=' + encodeURIComponent(server) +
          '&from=' + encodeURIComponent(from.value) + '&to=' + encodeURIComponent(to.value));
        renderTravel(result, diff);
      } catch (err) {
        problem(result, err);
      } finally {
        go.disabled = false;
      }
    });
    go.click();
  } catch (err) {
    problem(host, err);
  }
}

function labelled(text, control) {
  const wrap = el('label', 'picker-field');
  wrap.append(el('span', null, text));
  wrap.append(control);
  return wrap;
}

function renderTravel(host, diff) {
  host.replaceChildren();

  const legend = el('div', 'legend');
  for (const [kind, count] of [
    ['changed', diff.changed],
    ['unchanged', diff.unchanged],
    ['not-revisited', diff.not_revisited],
    ['first-seen', diff.first_seen],
    ['never-seen', diff.never_seen],
  ]) {
    const item = el('span', 'legend-item');
    const swatch = el('span', 'swatch');
    swatch.style.background = travelColours[kind];
    item.append(swatch, el('span', null, kind.replace(/-/g, ' ') + ' ' + count));
    legend.append(item);
  }
  host.append(legend);

  host.append(drawMap(diff.chunks));
  host.append(el('p', 'honesty', diff.honesty));
}

// Drawn rather than laid out, because an archive can hold tens of thousands of
// chunks and that many elements is a page that stops responding.
function drawMap(chunks) {
  const canvas = document.createElement('canvas');
  canvas.className = 'map';
  if (!chunks.length) return canvas;

  let minX = Infinity, minZ = Infinity, maxX = -Infinity, maxZ = -Infinity;
  for (const chunk of chunks) {
    if (chunk.x < minX) minX = chunk.x;
    if (chunk.x > maxX) maxX = chunk.x;
    if (chunk.z < minZ) minZ = chunk.z;
    if (chunk.z > maxZ) maxZ = chunk.z;
  }
  const wide = maxX - minX + 1;
  const tall = maxZ - minZ + 1;
  const scale = Math.max(2, Math.min(14, Math.floor(560 / Math.max(wide, tall))));

  canvas.width = wide * scale;
  canvas.height = tall * scale;
  const context = canvas.getContext('2d');
  for (const chunk of chunks) {
    context.fillStyle = travelColours[chunk.kind] || '#000';
    context.fillRect((chunk.x - minX) * scale, (chunk.z - minZ) * scale, scale, scale);
  }
  return canvas;
}

show('setup');

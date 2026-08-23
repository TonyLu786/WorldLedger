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

// Asking ------------------------------------------------------------------

// Every consequential thing here used to be agreed to in window.confirm, which
// is wrong on three counts. It heads the question with "127.0.0.1:53211 says",
// so the one moment this application most needs somebody to read looks like a
// page misbehaving. It renders a list of file paths as one run of text. And it
// makes consent depend on script dialogs being switched on in whatever is
// showing the page, which is not something a window should have to rely on.
//
// This returns a promise, so callers read as they did before. Escape and Cancel
// both answer no: a sheet somebody cannot dismiss is one they learn to click
// through.
function ask({ title, lead, rows, confirm: confirmText, danger }) {
  const box = document.getElementById('ask');
  const yes = document.getElementById('ask-yes');
  const no = document.getElementById('ask-no');

  document.getElementById('ask-title').textContent = title;
  const leadNode = document.getElementById('ask-lead');
  leadNode.textContent = lead || '';
  leadNode.hidden = !lead;

  const detail = document.getElementById('ask-detail');
  detail.replaceChildren();
  for (const row of rows || []) {
    const line = el('div', 'ask-row');
    line.append(el('span', 'ask-row-title', row.title));
    if (row.detail) line.append(el('span', 'ask-row-detail', row.detail));
    detail.append(line);
  }

  yes.textContent = confirmText || 'Carry on';
  yes.classList.toggle('is-danger', Boolean(danger));
  box.hidden = false;
  yes.focus();

  return new Promise((resolve) => {
    const finish = (answer) => {
      box.hidden = true;
      yes.removeEventListener('click', onYes);
      no.removeEventListener('click', onNo);
      document.removeEventListener('keydown', onKey);
      resolve(answer);
    };
    const onYes = () => finish(true);
    const onNo = () => finish(false);
    const onKey = (event) => { if (event.key === 'Escape') finish(false); };
    yes.addEventListener('click', onYes);
    no.addEventListener('click', onNo);
    document.addEventListener('keydown', onKey);
  });
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
    row.append(el('div'));
    host.append(row);
  }

  // One button for everything outstanding rather than one per line. Four
  // separate installs is four chances to stop half way, and there is nothing a
  // person gains by deciding about Fabric API separately from Fabric.
  if (!report.ready && report.checks.some((c) => c.fix)) {
    host.append(fixEverything(report));
  }
  host.append(removeEverything());
}

// The install asks somebody to agree to files being written into their game, and
// what it offers in exchange is that it can be undone. That promise was made in
// the confirmation and then had nowhere to be kept: the endpoint existed, its
// own refusal message named a button on this screen, and no such button had
// ever been drawn.
function removeEverything() {
  const card = el('section', 'card');
  card.append(el('h2', null, 'Remove it again'));
  card.append(el('p', 'card-lead',
    'Puts back exactly what was there before. Only files this application wrote are removed; ' +
    'anything you installed yourself, and everything you have recorded, is left alone.'));

  const go = el('button', 'fix danger', 'Remove');
  const detail = el('div');
  card.append(go, detail);

  go.addEventListener('click', async () => {
    if (!await ask({
      title: 'Remove Fabric and the mod from your Minecraft?',
      lead: 'Anything that was replaced is put back. Only files this application wrote are removed.',
      rows: [{ title: 'Left alone', detail: 'your recordings, your archive, and any other mods' }],
      confirm: 'Remove',
      danger: true,
    })) {
      return;
    }
    go.disabled = true;
    go.textContent = 'Removing…';
    try {
      const result = await call('/api/uninstall', { method: 'POST' });
      await refreshSetup();
      const left = (result.skipped || []).length;
      document.getElementById('checks').prepend(banner('good', 'Removed',
        left ? left + ' file(s) were left alone because they had been changed since they were installed.'
          : 'Your Minecraft is back to what it was.'));
    } catch (err) {
      go.disabled = false;
      go.textContent = 'Remove';
      detail.replaceChildren(banner('todo', err.message, err.next || ''));
    }
  });
  return card;
}

function fixEverything(report) {
  const card = el('section', 'card');
  card.append(el('h2', null, 'Set it all up'));
  card.append(el('p', 'card-lead',
    'This adds Fabric and the mod to your Minecraft. It takes a few seconds, and it can be undone.'));

  const name = el('input', 'name');
  name.type = 'text';
  name.placeholder = 'The name to keep your recordings under';
  name.setAttribute('aria-label', 'The name to keep your recordings under');
  const contributor = report.checks.find((c) => c.id === 'contributor');
  if (contributor && contributor.state === 'ok') name.value = contributor.detail;
  card.append(name);

  const go = el('button', 'primary', 'Set it up');
  const detail = el('div');
  card.append(go, detail);

  go.addEventListener('click', async () => {
    if (!name.value.trim()) {
      detail.replaceChildren(banner('todo', 'A name is needed first',
        'Nothing is recorded until there is one, and nobody else can choose it.'));
      return;
    }
    go.disabled = true;
    try {
      // Shown before anything happens, with the exact files. This is the point
      // at which somebody agrees to have their game written into, and it is the
      // only one: asking four times is not four times the consent.
      const plan = await call('/api/plan?contributor=' + encodeURIComponent(name.value.trim()));
      if (plan.refusal) {
        detail.replaceChildren(banner('todo', plan.refusal, 'Nothing has been changed.'));
        go.disabled = false;
        return;
      }
      const agreed = await ask({
        title: 'This will write ' + plan.steps.length + ' files into your Minecraft',
        lead: 'Nothing else is touched. Anything replaced is kept, and Remove puts it all back.',
        rows: plan.steps.map((s) => ({ title: s.title, detail: s.target })),
        confirm: 'Write these files',
      });
      if (!agreed) {
        go.disabled = false;
        return;
      }
      go.textContent = 'Setting up…';
      const result = await call('/api/install', {
        method: 'POST',
        body: JSON.stringify({ contributor: name.value.trim() }),
      });
      await refreshSetup();
      document.getElementById('checks').prepend(banner('good',
        'Done — ' + result.done + ' files written',
        'Start Minecraft, choose the WorldLedger installation, and play.'));
    } catch (err) {
      go.disabled = false;
      go.textContent = 'Set it up';
      detail.replaceChildren(banner('todo', err.message, err.next || ''));
    }
  });
  return card;
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

  // The folder outlives the mod that made it. Somebody who has removed the mod,
  // or whose launcher replaced the mods folder, still has every recording they
  // ever made sitting here, and reading the folder's existence as "you are
  // recording" told them to go and play while nothing was being kept.
  if (!status.capturing) {
    host.append(banner('todo', 'Nothing would be recorded if you played now',
      'Your past recordings are safe and listed below. Go to Set up to put the mod back.'));
  } else if (waiting > 0) {
    host.append(banner('good',
      waiting + (waiting === 1 ? ' recording waiting' : ' recordings waiting'),
      'Go to Bring it in to add them to your archive.'));
  } else {
    host.append(banner('todo', 'Nothing new since last time',
      'Join a server and play. Recordings appear here as you go, and the last of them when you quit.'));
  }

  const facts = el('div', 'facts');
  if (status.capturing && status.contributor) {
    addFact(facts, 'Recording under the name', status.contributor);
  }
  addFact(facts, 'Waiting to be brought in', String(status.spool.ready));
  addFact(facts, 'Already brought in and kept',
    String(status.spool.imported) + (status.spool.imported_bytes ? ' (' + bytes(status.spool.imported_bytes) + ')' : ''));
  if (status.spool.in_progress > 0) {
    addFact(facts, 'Still being written', String(status.spool.in_progress) + ' (Minecraft is running)');
  }
  if (status.spool.quarantined > 0) {
    addFact(facts, 'Set aside as unreadable', String(status.spool.quarantined));
  }
  addFact(facts, 'Recordings folder', status.spool.dir);
  host.append(facts);

  if (status.spool.imported > 0) host.append(clearKept(status));
}

// Keeping recordings after they have been brought in is the safe default and
// stays the default: nothing is deleted for you. What it should not do is grow
// without ever being mentioned, which is how somebody ends up with gigabytes
// inside .minecraft that nothing here ever named.
function clearKept(status) {
  const card = el('section', 'card');
  card.append(el('h2', null, 'Clear the ones already brought in'));
  card.append(el('p', 'card-lead',
    status.spool.imported + ' recording(s), ' + bytes(status.spool.imported_bytes) +
    ', are being kept inside your Minecraft folder after being brought in. Clearing them ' +
    'frees that space. Anything not yet brought in, and anything set aside as unreadable, ' +
    'stays exactly where it is.'));

  const go = el('button', 'fix', 'Clear ' + bytes(status.spool.imported_bytes));
  const detail = el('div');
  card.append(go, detail);

  go.addEventListener('click', async () => {
    if (!await ask({
      title: 'Delete ' + status.spool.imported + ' recordings that have already been brought in?',
      lead: 'This frees ' + bytes(status.spool.imported_bytes) + ' and cannot be undone.',
      rows: [
        { title: 'Deleted', detail: status.spool.imported + ' recording(s) already added to your archive' },
        { title: 'Left alone', detail: 'your archive, anything still waiting, and anything set aside as unreadable' },
      ],
      confirm: 'Delete them',
      danger: true,
    })) {
      return;
    }
    go.disabled = true;
    go.textContent = 'Clearing…';
    try {
      const result = await call('/api/tidy', { method: 'POST' });
      renderCapture(await loadStatus());
      document.getElementById('capture-body').prepend(banner('good',
        'Cleared ' + result.removed + ' recording(s), freeing ' + bytes(result.freed),
        'Your archive still holds everything they contained.'));
    } catch (err) {
      go.disabled = false;
      go.textContent = 'Clear ' + bytes(status.spool.imported_bytes);
      detail.replaceChildren(banner('todo', err.message, err.next || ''));
    }
  });
  return card;
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
        // A decision already made is shown as made. Redrawing five empty
        // circles under a banner saying "Decided: private" leaves somebody
        // unable to tell which of them they are looking at, and makes changing
        // a decision look exactly like confirming one.
        input.checked = server.disposition === choice.value;
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
      until.setAttribute('aria-label', 'Held back until');
      const showExpiry = () => {
        const picked = form.querySelector('input[type=radio]:checked');
        until.hidden = !(picked && picked.dataset.needsExpiry);
      };
      form.addEventListener('change', showExpiry);
      form.append(until);
      showExpiry();

      const nameLabel = el('label', 'picker-field');
      nameLabel.append(el('span', null, 'Who is deciding'));
      const name = el('input', 'name');
      name.type = 'text';
      name.placeholder = 'Your name';
      name.value = declarerName(status);
      name.setAttribute('aria-label', 'Who is deciding');
      nameLabel.append(name);
      form.append(nameLabel);

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
//
// This used to read a field the status has never had, so the comment above
// described something that did not happen and the box came up empty every time
// — on the one step the application deliberately does not do for anybody.
function declarerName(status) {
  return status.contributor || '';
}

// Which server the last two screens are about ------------------------------

// An archive holds one server per place the player has been, and both of the
// screens below used to take servers[0] without saying so. On an archive with
// two, that silently answered a question nobody had been asked: making a world
// wrote whichever server sorted first, and time travel reported "only one
// session so far" while the session somebody was looking for sat beside it.
//
// The choice is remembered across the two screens, because looking at a server
// and then building it is one thought.
let chosenServer = '';

// The default is whichever holds the most, not whichever sorts first. Somebody
// with a long-played server and one evening somewhere else means the first of
// those, and an ordering of names does not know that.
function defaultServer(servers) {
  if (servers.some((s) => s.id === chosenServer)) return chosenServer;
  let best = servers[0];
  for (const server of servers) if (server.chunks > best.chunks) best = server;
  return best ? best.id : '';
}

function serverChooser(servers, onChange) {
  chosenServer = defaultServer(servers);
  const select = el('select');
  for (const server of servers) {
    const option = el('option', null, server.id + ' — ' + server.chunks + ' places');
    option.value = server.id;
    select.append(option);
  }
  select.value = chosenServer;
  select.addEventListener('change', () => {
    chosenServer = select.value;
    onChange();
  });
  // One server is not a choice, and a control that cannot be changed teaches
  // somebody only that it does not work. It is still named and still shown,
  // because which server is about to be written is worth knowing either way.
  if (servers.length < 2) select.disabled = true;
  const field = el('label', 'scope-field');
  field.append(el('span', null, 'Server'));
  field.append(select);
  return field;
}

// The same shape the moments come back in, which is what puts two dates from
// two sources on one screen without them looking like two different kinds of
// thing. Following the machine's locale here instead produced a world list
// dated one way and a moment list dated another, in the same sentence.
function whenText(iso) {
  if (!iso) return '';
  const at = new Date(iso);
  if (isNaN(at.getTime())) return '';
  return at.toLocaleString('en-GB', {
    day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

async function momentsFor(server) {
  if (!server) return [];
  try {
    return (await call('/api/moments?server=' + encodeURIComponent(server))).moments || [];
  } catch (err) {
    // A missing list of moments costs the choice of one, not the screen.
    return [];
  }
}

function momentLabel(moments, at) {
  const found = moments.find((m) => m.at === at);
  return found ? found.label : at;
}

// Make a world -------------------------------------------------------------

// Set by time travel, so that "make a world as it was then" arrives here with
// the answer already filled in rather than asking somebody to find the moment
// they were just looking at in a second list.
let pendingMoment = null;

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

    const scope = el('div', 'scope');
    scope.append(serverChooser(ready, refreshWorld));

    // Which moment is the whole reason for keeping every observation rather
    // than one snapshot, and the window could only ever write "now". The list
    // is the one time travel offers, newest first, because that is what almost
    // everybody wants and the rest is what makes this different from a world
    // downloader.
    const moments = await momentsFor(chosenServer);
    const when = el('select');
    const now = el('option', null, 'Now — the newest of everything recorded');
    now.value = '';
    when.append(now);
    for (const moment of moments.slice().reverse()) {
      const option = el('option', null, 'As it was on ' + moment.label);
      option.value = moment.at;
      when.append(option);
    }
    if (pendingMoment && moments.some((m) => m.at === pendingMoment)) when.value = pendingMoment;
    pendingMoment = null;
    if (moments.length) {
      const field = el('label', 'scope-field');
      field.append(el('span', null, 'Which moment'));
      field.append(when);
      scope.append(field);
    }
    host.append(scope);

    const answer = await call('/api/worlds');
    if (!answer.worlds.length) {
      host.append(banner('todo', 'No Minecraft world to write into', ''));
      host.append(howToMakeOne(answer));
      const again = el('button', 'primary', 'I have made one');
      again.addEventListener('click', refreshWorld);
      host.append(again);
      return;
    }

    // The instructions used to appear only when the saves folder was empty,
    // which is where they were least needed. The person who needs telling is
    // the one being offered three worlds they have played in for a year.
    host.append(answer.fresh > 0
      ? banner('good', 'Pick a world to write into',
        'The newest is first. Choose one you made for this, not one you have played in.')
      : banner('todo', 'Every world here looks like one you have played in',
        'Writing into one of these puts your recordings over what is already there. ' +
        'Making an empty world first is the safe way round.'));
    if (answer.fresh === 0) host.append(howToMakeOne(answer));

    for (const world of answer.worlds) {
      const row = el('div', 'check');
      row.append(el('div', 'check-dot', world.sizeable ? '!' : '✓'));
      const middle = el('div');
      middle.append(el('div', 'check-title', world.name));
      const touched = whenText(world.last_played);
      middle.append(el('div', 'check-detail',
        bytes(world.bytes) +
        (world.sizeable ? ' — this looks like a world you have played in' : ' — looks freshly made') +
        (touched ? ', last touched ' + touched : '')));
      row.append(middle);

      const button = el('button', 'fix', 'Write into this');
      button.addEventListener('click', async () => {
        if (world.sizeable && !await ask({
          title: 'Write into ' + world.name + '?',
          lead: 'It is ' + bytes(world.bytes) + ', which usually means somebody has played in it. ' +
            'Where your recordings overlap what is there, yours are written over it.',
          rows: [
            { title: 'Writing', detail: chosenServer },
            { title: 'Into', detail: world.path },
          ],
          confirm: 'Write into it anyway',
          danger: true,
        })) {
          return;
        }
        button.disabled = true;
        button.textContent = 'Writing…';
        try {
          const result = await call('/api/export', {
            method: 'POST',
            body: JSON.stringify({ server: chosenServer, world_dir: world.path, at: when.value }),
          });
          const moment = when.value ? ' as it was on ' + momentLabel(moments, when.value) : '';
          host.replaceChildren(banner('good',
            'Wrote ' + result.chunks + ' places from ' + chosenServer + moment + ' into ' + world.name,
            'Open Minecraft and play that world. Anything nobody saw is left as the empty world made it.'));
          if (result.withheld) {
            host.append(el('p', 'quiet',
              result.withheld + ' recording(s) were held back by a redaction and are not in it.'));
          }
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

function howToMakeOne(answer) {
  const list = el('ol', 'howto');
  for (const line of answer.how_to_make || []) list.append(el('li', null, line));
  return list;
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

    const scope = el('div', 'scope');
    scope.append(serverChooser(status.servers, refreshTravel));
    host.append(scope);

    const server = chosenServer;
    const moments = await momentsFor(server);
    if (moments.length < 1) {
      host.append(banner('todo', 'Nothing recorded for ' + server, 'Play and bring in some recordings.'));
      return;
    }
    // Comparing a moment with itself is a real answer and a useless one. Saying
    // why there is nothing to compare beats showing a map of one colour and
    // leaving somebody to work out that it means "come back later".
    if (moments.length < 2) {
      host.append(banner('todo', 'Only one session so far on ' + server,
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
        result.append(buildFromHere(status, server, moments, [from.value, to.value]));
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

// Seeing that a place changed and then being unable to go back to it is the
// screen stopping one step short of what it just demonstrated. The export has
// always taken a moment; it was only ever sent "now", so the one thing a world
// downloader structurally cannot do was visible here and reachable nowhere.
function buildFromHere(status, server, moments, chosen) {
  const card = el('section', 'card');
  card.append(el('h2', null, 'Go back to one of these'));

  const declared = status.servers.some((s) => s.id === server && s.declared);
  if (!declared) {
    card.append(el('p', 'card-lead',
      'Nothing has been said yet about what may happen to what you recorded on ' + server +
      '. Decide that first, and either of these moments can be made into a world.'));
    return card;
  }

  card.append(el('p', 'card-lead',
    'A world written from one of these moments holds what had been seen by then, ' +
    'and nothing that was only seen afterwards.'));

  for (const at of chosen) {
    const button = el('button', 'fix', 'Make a world as it was on ' + momentLabel(moments, at));
    button.addEventListener('click', () => {
      pendingMoment = at;
      chosenServer = server;
      show('world');
    });
    card.append(button);
  }
  return card;
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

// A window closing ends the program. A tab closing tells nobody anything, so
// the page says it is still here while it is open, and the application stops
// when it stops saying so. Without this it would sit in the process list until
// somebody learned what Task Manager is.
setInterval(() => { call('/api/alive').catch(() => {}); }, 10000);
call('/api/alive').catch(() => {});

// Before you start --------------------------------------------------------

// Shown once, on a machine where nobody has read it. Every launch would make it
// something people learn to click past, and once is what "explicitly told"
// means; the rail keeps a way back to it for the rest of the time.
const noticeBox = document.getElementById('notice');
const noticeAccept = document.getElementById('notice-accept');
const noticeClose = document.getElementById('notice-close');

function renderNotice(answer, alreadyRead) {
  const body = document.getElementById('notice-body');
  body.replaceChildren();
  for (const part of answer.paragraphs || []) {
    const block = el('section', 'notice-part');
    block.append(el('h2', null, part.heading));
    block.append(el('p', null, part.body));
    body.append(block);
  }
  noticeAccept.hidden = alreadyRead;
  noticeClose.hidden = !alreadyRead;
  noticeBox.hidden = false;
  (alreadyRead ? noticeClose : noticeAccept).focus();
}

noticeAccept.addEventListener('click', async () => {
  noticeAccept.disabled = true;
  try {
    await call('/api/notice', { method: 'POST' });
    noticeBox.hidden = true;
  } catch (err) {
    // Being unable to write it down is no reason to stand in somebody's way, so
    // the sheet closes either way. Saying so belongs here rather than on the
    // screen behind it, which may not be the one they are looking at.
    document.getElementById('notice-body').prepend(
      banner('todo', 'This could not be noted as read: ' + err.message,
        'You can carry on; it will be shown again next time.'));
    noticeAccept.hidden = true;
    noticeClose.hidden = false;
    noticeClose.focus();
  } finally {
    noticeAccept.disabled = false;
  }
});
noticeClose.addEventListener('click', () => { noticeBox.hidden = true; });

document.getElementById('notice-open').addEventListener('click', async () => {
  try {
    renderNotice(await call('/api/notice'), true);
  } catch (err) {
    problem(document.getElementById('checks'), err);
  }
});

async function start() {
  show('setup');
  try {
    const answer = await call('/api/notice');
    if (!answer.accepted) renderNotice(answer, false);
  } catch (err) {
    // The application still works; what it must not do is carry on as though it
    // had said something it did not manage to say.
    document.getElementById('checks').prepend(
      banner('todo', 'The notice about what this keeps could not be shown: ' + err.message,
        'It is in the NOTICE file beside the application.'));
  }
}
start();

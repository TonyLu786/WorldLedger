'use strict';

// The token arrives in the address and is taken out of it immediately, so it
// does not end up in history or in a link somebody copies. It lives in a
// variable for the life of the page and is never written to storage: a session
// token that outlives its session is just a password nobody chose.
const token = new URLSearchParams(location.search).get('token') || '';
if (location.search) {
  history.replaceState(null, '', location.pathname);
}

async function call(path) {
  const response = await fetch(path, { headers: { 'X-WorldLedger-Token': token } });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error((body && body.problem) || ('the application answered ' + response.status));
  }
  return body;
}

// Navigation ---------------------------------------------------------------

const steps = Array.from(document.querySelectorAll('.step'));

function show(name) {
  for (const step of steps) {
    const chosen = step.dataset.screen === name;
    step.setAttribute('aria-current', chosen ? 'true' : 'false');
    document.getElementById('screen-' + step.dataset.screen).hidden = !chosen;
  }
}

for (const step of steps) {
  step.addEventListener('click', () => show(step.dataset.screen));
}

// Setup --------------------------------------------------------------------

const marks = { ok: '✓', missing: '!', wrong: '×', unknown: '?' };

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  // Always assigned as text. Every string here comes from the machine being
  // inspected -- a directory name, a file name somebody chose -- and none of it
  // has any business being parsed as markup.
  if (text !== undefined) node.textContent = text;
  return node;
}

function renderChecks(report) {
  const host = document.getElementById('checks');
  host.replaceChildren();

  const banner = element('div', 'banner ' + (report.ready ? 'good' : 'todo'));
  if (report.ready) {
    banner.append(
      element('strong', null, 'Ready to record'),
      element('span', null, 'Start Minecraft, join a server, and play. Nothing else is needed.'));
  } else {
    const outstanding = report.checks.filter((check) => check.state !== 'ok');
    banner.append(
      element('strong', null,
        outstanding.length === 1 ? 'One thing left' : outstanding.length + ' things left'),
      element('span', null, outstanding.length ? outstanding[0].detail : ''));
  }
  host.append(banner);

  for (const check of report.checks) {
    const row = element('div', 'check is-' + check.state);
    row.append(element('div', 'check-dot', marks[check.state] || '?'));

    const middle = element('div');
    middle.append(element('div', 'check-title', check.title));
    middle.append(element('div', 'check-detail', check.detail));
    row.append(middle);

    if (check.fix) {
      const button = element('button', 'fix', check.fix);
      button.disabled = true;
      button.title = 'Not available yet';
      row.append(button);
    } else {
      row.append(element('div'));
    }
    host.append(row);
  }
}

async function refresh() {
  const host = document.getElementById('checks');
  try {
    renderChecks(await call('/api/health'));
  } catch (err) {
    host.replaceChildren();
    const banner = element('div', 'banner todo');
    banner.append(
      element('strong', null, 'Could not check this computer'),
      element('span', null, err.message));
    host.append(banner);
  }
}

document.getElementById('recheck').addEventListener('click', refresh);

show('setup');
refresh();
